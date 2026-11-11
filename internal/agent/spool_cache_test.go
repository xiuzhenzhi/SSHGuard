package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestSpoolReusesPagesAfterDrain(t *testing.T) {
	dir := t.TempDir()
	s, err := openSpool(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	event, _ := parseSSH("Accepted password for fixture from 192.0.2.1 port 22 ssh2", "fixture", time.Now().UTC())
	events := make([]SSHEvent, queueLimit)
	for i := range events {
		events[i] = event
	}
	var warmSize int64
	for round := 0; round < 4; round++ {
		if err = s.append(events, cursor{Position: fmt.Sprint(round)}); err != nil {
			t.Fatal(err)
		}
		for q, _ := s.counts(); q > 0; q, _ = s.counts() {
			items, err := s.batch()
			if err != nil {
				t.Fatal(err)
			}
			if err = s.acknowledge(items, false); err != nil {
				t.Fatal(err)
			}
		}
		info, err := os.Stat(filepath.Join(dir, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		if round == 1 {
			warmSize = info.Size()
		}
		if round > 1 && info.Size() > warmSize {
			t.Fatalf("database grew after warmup: %d -> %d", warmSize, info.Size())
		}
		t.Logf("fill/drain %d: database=%d bytes", round+1, info.Size())
	}
}

func TestSecondaryCursorCacheDurability(t *testing.T) {
	dir := t.TempDir()
	s, err := openSpool(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.db.Close() }()
	initial := cursor{Kind: "vnc", Position: "42"}
	if err = s.appendFor("vnc", []SSHEvent{{ID: "kept"}}, initial); err != nil {
		t.Fatal(err)
	}
	stats := s.db.Stats()
	before := stats.TxStats.GetWrite()
	for i := 0; i < 10; i++ {
		if err = s.appendFor("vnc", nil, initial); err != nil {
			t.Fatal(err)
		}
	}
	stats = s.db.Stats()
	if stats.TxStats.GetWrite() != before {
		t.Fatal("unchanged cursor caused disk writes")
	}
	if err = s.appendFor("vnc", make([]SSHEvent, queueLimit), cursor{Position: "lost"}); !errors.Is(err, errQueueFull) {
		t.Fatal(err)
	}
	if c, err := s.positionFor("vnc"); err != nil || c != initial {
		t.Fatal("failed commit changed cache", c, err)
	}
	s.db.Close()
	s, err = openSpool(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := s.positionFor("vnc"); err != nil || c != initial {
		t.Fatal("cursor lost after restart", c, err)
	}
	items, err := s.batch()
	if err != nil || len(items) != 1 || items[0].event.ID != "kept" {
		t.Fatal("event lost after restart", err)
	}
	for i := 0; i < sourceCursorCacheLimit*2; i++ {
		if _, err = s.positionFor(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.sources) > sourceCursorCacheLimit {
		t.Fatal("unbounded source cache")
	}
	if c, err := s.positionFor("vnc"); err != nil || c != initial {
		t.Fatal("cache eviction lost durable cursor", c, err)
	}
	if err = s.bindIdentity(Config{Endpoint: "https://example.test", ServerID: "new"}); err != nil {
		t.Fatal(err)
	}
	if c, err := s.positionFor("vnc"); err != nil || c != (cursor{}) {
		t.Fatal("identity reset left stale cursor", c, err)
	}
}

func TestSpoolCacheMatchesCommittedState(t *testing.T) {
	s, err := openSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	check := func(queued, rejected int) {
		t.Helper()
		if q, r := s.counts(); q != queued || r != rejected {
			t.Fatalf("cached counts = %d/%d, want %d/%d", q, r, queued, rejected)
		}
		if err := s.db.View(func(tx *bolt.Tx) error {
			if q, r := tx.Bucket([]byte("queue")).Stats().KeyN, tx.Bucket([]byte("rejected")).Stats().KeyN; q != queued || r != rejected {
				t.Errorf("durable counts = %d/%d, want %d/%d", q, r, queued, rejected)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.append(make([]SSHEvent, 999), cursor{Position: "initial"}); err != nil {
		t.Fatal(err)
	}
	for q, _ := s.counts(); q > 0; q, _ = s.counts() {
		items, err := s.batch()
		if err != nil {
			t.Fatal(err)
		}
		if err = s.acknowledge(items, true); err != nil {
			t.Fatal(err)
		}
	}
	check(0, 999)
	if err = s.append([]SSHEvent{{ID: "one"}, {ID: "two"}}, cursor{Position: "new"}); err != nil {
		t.Fatal(err)
	}
	items, err := s.batch()
	if err != nil || len(items) != 2 {
		t.Fatal("missing queued events", items, err)
	}
	if err = s.acknowledge(items, true); err == nil {
		t.Fatal("quarantine overflow accepted")
	}
	check(2, 999) // The first tentative quarantine must roll back with the second.
	if err = s.append(make([]SSHEvent, queueLimit), cursor{Position: "must not commit"}); err == nil {
		t.Fatal("queue overflow accepted")
	}
	check(2, 999)
	if position, err := s.position(); err != nil || position.Position != "new" {
		t.Fatal("failed write changed cached position", position, err)
	}
	if err = s.acknowledge(append(items, items...), false); err != nil {
		t.Fatal(err)
	}
	check(0, 999)
}
