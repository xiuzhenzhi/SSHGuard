package agent

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

type SSHEvent struct {
	ID       string    `json:"event_id"`
	Type     string    `json:"event_type"`
	Username string    `json:"username"`
	IP       string    `json:"source_ip"`
	Port     int       `json:"source_port,omitempty"`
	Method   string    `json:"auth_method"`
	Outcome  string    `json:"outcome"`
	At       time.Time `json:"occurred_at"`
}

func parseSSH(message, identity string, at time.Time) (SSHEvent, bool) {
	for len(message) > 0 {
		line, rest, _ := strings.Cut(message, "\n")
		message = rest
		for start := 0; start < len(line); start++ {
			if start > 0 && !sshSpace(line[start-1]) {
				continue
			}
			text := line[start:]
			failed := strings.HasPrefix(text, "Failed ")
			if failed {
				text = text[len("Failed "):]
			} else if strings.HasPrefix(text, "Accepted ") {
				text = text[len("Accepted "):]
			} else {
				continue
			}
			methodEnd := 0
			for methodEnd < len(text) && sshMethodByte(text[methodEnd]) {
				methodEnd++
			}
			if methodEnd == 0 || !strings.HasPrefix(text[methodEnd:], " for ") {
				continue
			}
			method, names := text[:methodEnd], text[methodEnd+len(" for "):]
			// The first valid header owns the rest of this line. A later header
			// would be part of its username, matching OpenSSH's existing grammar.
			for offset := 0; offset < len(names); {
				from := strings.Index(names[offset:], " from ")
				if from < 0 {
					break
				}
				from += offset
				offset = from + len(" from ")
				if from == 0 {
					continue
				}
				source := names[offset:]
				ipEnd := 0
				for ipEnd < len(source) && sshIPByte(source[ipEnd]) {
					ipEnd++
				}
				if ipEnd == 0 || !strings.HasPrefix(source[ipEnd:], " port ") {
					continue
				}
				portText := source[ipEnd+len(" port "):]
				portEnd := 0
				for portEnd < len(portText) && portText[portEnd] >= '0' && portText[portEnd] <= '9' {
					portEnd++
				}
				if portEnd == 0 || (portEnd < len(portText) && !sshSpace(portText[portEnd])) {
					continue
				}
				username := names[:from]
				if trimmed, ok := strings.CutPrefix(username, "invalid user "); ok && trimmed != "" {
					username = trimmed
				}
				ip, err := netip.ParseAddr(source[:ipEnd])
				port, portErr := strconv.Atoi(portText[:portEnd])
				if err != nil || portErr != nil || port < 1 || port > 65535 || utf8.RuneCountInString(username) > 128 || len(method) > 32 || strings.ContainsAny(username, "\r\x00") {
					return SSHEvent{}, false
				}
				if len(line) > 1024 {
					// A small username must not retain a whole 64 KiB log line in a batch.
					username, method = strings.Clone(username), strings.Clone(method)
				}
				event := SSHEvent{ID: sourceEventID(identity), Username: username, IP: ip.Unmap().String(), Port: port, Method: method, At: at.UTC(), Type: "ssh.login", Outcome: "success"}
				if failed {
					event.Type, event.Outcome = "ssh.login_failed", "failure"
				}
				return event, true
			}
			break
		}
	}
	return SSHEvent{}, false
}

func sshSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
func sourceEventID(identity string) string {
	if identity == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String()
}
func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func sshMethodByte(c byte) bool {
	return asciiLetter(c) || c >= '0' && c <= '9' || strings.IndexByte("@._/-", c) >= 0
}
func sshIPByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' || c == ':' || c == '.'
}

const queueLimit = 10000

var errQueueFull = errors.New("local queue is full; collection paused without advancing its cursor")

type cursor struct {
	Kind     string    `json:"kind"`
	Position string    `json:"position"`
	FileID   string    `json:"file_id,omitempty"`
	Offset   int64     `json:"offset,omitempty"`
	At       time.Time `json:"at"`
	LogTime  time.Time `json:"log_time,omitempty"`
}
type spool struct {
	db               *bolt.DB
	mu               sync.RWMutex
	queued, rejected int
	current          cursor
	cursorErr        error
	sources          map[string]cursor
}

// Bound retained metadata even when discovered log paths change over time.
const sourceCursorCacheLimit = 64

func (s *spool) cacheSource(source string, c cursor) {
	if s.sources == nil {
		s.sources = make(map[string]cursor)
	}
	if _, exists := s.sources[source]; !exists && len(s.sources) >= sourceCursorCacheLimit {
		for key := range s.sources {
			delete(s.sources, key)
			break
		}
	}
	s.sources[source] = c
}

// Caller holds s.mu. Cache only valid, committed positions.
func (s *spool) sourcePosition(source string) (cursor, error) {
	if c, ok := s.sources[source]; ok {
		return c, nil
	}
	var c cursor
	err := s.db.View(func(tx *bolt.Tx) error {
		if raw := tx.Bucket([]byte("meta")).Get([]byte("cursor:" + source)); raw != nil {
			return json.Unmarshal(raw, &c)
		}
		return nil
	})
	if err == nil {
		s.cacheSource(source, c)
	}
	return c, err
}

func (s *spool) bindIdentity(cfg Config) error {
	if cfg.Endpoint == "" || cfg.ServerID == "" {
		return errors.New("state identity requires a platform endpoint and server ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reset := false
	rebound := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket([]byte("meta"))
		identity := cfg.Endpoint + "|" + cfg.ServerID
		previous := meta.Get([]byte("identity"))
		newRegistration := cfg.EnrollmentID != "" && string(meta.Get([]byte("enrollment_id"))) != cfg.EnrollmentID
		if previous != nil && string(previous) == identity && !newRegistration {
			return nil
		}
		rebound = true
		// Clear queue, cursor and rejected events together under the database lock.
		// Reusing the file preserves ownership and prevents an old Agent from
		// writing to an unlinked database while a replacement is initialized.
		for _, name := range []string{"queue", "meta", "rejected"} {
			key := []byte(name)
			reset = reset || tx.Bucket(key).Stats().KeyN > 0
			if err := tx.DeleteBucket(key); err != nil {
				return err
			}
			if _, err := tx.CreateBucket(key); err != nil {
				return err
			}
		}
		meta = tx.Bucket([]byte("meta"))
		if err := meta.Put([]byte("identity"), []byte(identity)); err != nil {
			return err
		}
		if cfg.EnrollmentID != "" {
			return meta.Put([]byte("enrollment_id"), []byte(cfg.EnrollmentID))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("initialize durable Agent state: %w", err)
	}
	if rebound {
		s.queued, s.rejected = 0, 0
		s.current, s.cursorErr = cursor{}, nil
		s.sources = nil
	}
	if reset {
		log.Print("New Agent registration or server identity detected; old local queue, rejected events and collector cursor cleared")
	}
	return nil
}

func openSpool(dir string) (*spool, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, fmt.Errorf("create state directory: %w", e)
	}
	db, e := bolt.Open(filepath.Join(dir, "state.db"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, fmt.Errorf("open state.db: %w", e)
	}
	s := &spool{db: db}
	e = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"queue", "meta", "rejected"} {
			if _, e := tx.CreateBucketIfNotExists([]byte(name)); e != nil {
				return e
			}
		}
		s.queued = tx.Bucket([]byte("queue")).Stats().KeyN
		s.rejected = tx.Bucket([]byte("rejected")).Stats().KeyN
		if raw := tx.Bucket([]byte("meta")).Get([]byte("cursor")); raw != nil {
			s.cursorErr = json.Unmarshal(raw, &s.current)
		}
		return nil
	})
	if e != nil {
		db.Close()
		return nil, fmt.Errorf("initialize state.db buckets: %w", e)
	}
	return s, nil
}
func (s *spool) position() (cursor, error) {
	return s.positionFor("")
}

func (s *spool) positionFor(source string) (cursor, error) {
	s.mu.RLock()
	if source != "" {
		c, ok := s.sources[source]
		s.mu.RUnlock()
		if ok {
			return c, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.sourcePosition(source)
	}
	defer s.mu.RUnlock()
	return s.current, s.cursorErr
}

// Queue entries and source cursor are committed together and fsynced by bbolt.
func (s *spool) append(events []SSHEvent, c cursor) error {
	return s.appendFor("", events, c)
}

func (s *spool) appendFor(source string, events []SSHEvent, c cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source == "" && len(events) == 0 {
		if s.cursorErr != nil {
			return s.cursorErr
		}
		if s.current == c {
			return nil
		}
	}
	if source != "" && len(events) == 0 {
		previous, err := s.sourcePosition(source)
		if err != nil {
			return err
		}
		if previous == c {
			return nil
		}
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		q := tx.Bucket([]byte("queue"))
		if s.queued+len(events) > queueLimit {
			return errQueueFull
		}
		for _, event := range events {
			seq, e := q.NextSequence()
			if e != nil {
				return e
			}
			key := make([]byte, 8)
			binary.BigEndian.PutUint64(key, seq)
			b, e := json.Marshal(event)
			if e != nil {
				return e
			}
			if e = q.Put(key, b); e != nil {
				return e
			}
		}
		b, e := json.Marshal(c)
		if e != nil {
			return e
		}
		key := "cursor"
		if source != "" {
			key += ":" + source
		}
		return tx.Bucket([]byte("meta")).Put([]byte(key), b)
	})
	if err == nil {
		s.queued += len(events)
		if source == "" {
			s.current, s.cursorErr = c, nil
		} else {
			s.cacheSource(source, c)
		}
	}
	return err
}

type queued struct {
	key   []byte
	event SSHEvent
}

func (s *spool) batch() ([]queued, error) {
	q, _ := s.counts()
	if q == 0 {
		return nil, nil
	}
	items := make([]queued, 0, min(q, 100))
	e := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte("queue")).Cursor()
		for k, v := c.First(); k != nil && len(items) < 100; k, v = c.Next() {
			var event SSHEvent
			if e := json.Unmarshal(v, &event); e != nil {
				return fmt.Errorf("queue entry corrupt: %w", e)
			}
			items = append(items, queued{append([]byte(nil), k...), event})
		}
		return nil
	})
	return items, e
}
func (s *spool) acknowledge(items []queued, reject bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed, quarantined := 0, 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		q := tx.Bucket([]byte("queue"))
		dead := tx.Bucket([]byte("rejected"))
		for _, item := range items {
			value := q.Get(item.key)
			if value == nil {
				continue
			}
			if reject {
				if s.rejected+quarantined >= 1000 {
					return errors.New("rejected queue full; operator intervention required")
				}
				if dead.Get(item.key) == nil {
					quarantined++
				}
				if e := dead.Put(item.key, value); e != nil {
					return e
				}
			}
			if e := q.Delete(item.key); e != nil {
				return e
			}
			removed++
		}
		return nil
	})
	if err == nil {
		s.queued -= removed
		s.rejected += quarantined
	}
	return err
}
func (s *spool) counts() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queued, s.rejected
}
