package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func defaultStateDir() string { return "/opt/SSHGuard/Agent/sshguard-agent" }
func fileIdentity(f *os.File) (string, error) {
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	return fileIdentityFromInfo(f, info)
}
func fileIdentityFromInfo(_ *os.File, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("cannot identify SSH log")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
func journalAvailable() bool {
	if _, err := os.Stat("/run/systemd/journal/socket"); err != nil {
		return false
	}
	_, err := exec.LookPath("journalctl")
	return err == nil
}

func collectPlatform(ctx context.Context, s *spool, explicit string) (time.Time, error) {
	if explicit != "" {
		return collectFile(ctx, s, explicit)
	}
	// Prefer the active system journal over potentially stale files left by a removed syslog daemon.
	if journalAvailable() {
		return collectJournal(ctx, s)
	}
	var unreadable string
	for _, path := range []string{"/opt/SSHGuard/Agent/logs/auth.log", "/var/log/auth.log", "/var/log/secure", "/var/log/messages"} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err == nil {
			file.Close()
			return collectFile(ctx, s, path)
		}
		if unreadable == "" {
			unreadable = path
		}
	}
	if unreadable != "" {
		return time.Time{}, fmt.Errorf("SSH log is missing or unreadable: %s; check log source and permissions", unreadable)
	}
	if _, e := exec.LookPath("journalctl"); e == nil {
		return collectJournal(ctx, s)
	}
	return time.Time{}, errors.New("no SSH log source found; enable sshd journaling or syslog authentication logging")
}

type journalRecord struct {
	Identifier string `json:"SYSLOG_IDENTIFIER"`
	Cursor     string `json:"__CURSOR"`
	Timestamp  string `json:"__REALTIME_TIMESTAMP"`
	Message    string `json:"MESSAGE"`
}

func journalArguments(s *spool) (cursor, []string, error) {
	previous, e := s.position()
	if e != nil {
		return cursor{}, nil, e
	}
	args := []string{"--no-pager", "--quiet", "--output=json", "--output-fields=__CURSOR,__REALTIME_TIMESTAMP,MESSAGE,SYSLOG_IDENTIFIER", "--identifier=sshd", "--identifier=sshd-session", "--identifier=login", "--identifier=vncserver-x11", "--identifier=vncserver-x11-serviced", "--identifier=Xvnc", "--identifier=Xtigervnc", "--identifier=x0vncserver"}
	if previous.Kind != "journal" {
		previous = cursor{Kind: "journal", At: time.Now().UTC()}
		if e = s.append(nil, previous); e != nil {
			return cursor{}, nil, e
		}
	}
	if previous.Position != "" {
		args = append(args, "--after-cursor="+previous.Position)
	} else {
		args = append(args, "--since="+previous.At.Format("2006-01-02 15:04:05.000000 MST"))
	}
	return previous, args, nil
}

func collectJournal(ctx context.Context, s *spool) (time.Time, error) {
	previous, args, e := journalArguments(s)
	if e != nil {
		return time.Time{}, e
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "journalctl", args...)
	var stderr diagnosticBuffer
	cmd.Stderr = &stderr
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return time.Time{}, e
	}
	if e = cmd.Start(); e != nil {
		return time.Time{}, errors.New("journal reader could not start")
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 65536)
	next := previous
	events := []SSHEvent{}
	last := time.Time{}
	limited := false
	for n := 0; scanner.Scan(); n++ {
		var record journalRecord
		if e = json.Unmarshal(scanner.Bytes(), &record); e != nil || record.Cursor == "" {
			cancel()
			cmd.Wait()
			return time.Time{}, errors.New("invalid SSH journal record")
		}
		micros, err := strconv.ParseInt(record.Timestamp, 10, 64)
		if err != nil {
			cancel()
			cmd.Wait()
			return time.Time{}, errors.New("invalid SSH journal timestamp")
		}
		at := time.UnixMicro(micros).UTC()
		next.Position, next.At = record.Cursor, at
		var event SSHEvent
		var ok bool
		if strings.Contains(strings.ToLower(record.Identifier), "vnc") {
			event, ok = parseVNC("auto", record.Message, "journal|"+record.Cursor, at)
		} else if record.Identifier == "login" {
			event, ok = parseLogin(record.Message, "journal|"+record.Cursor, at)
		} else {
			event, ok = parseSSH(record.Message, "journal|"+record.Cursor, at)
		}
		if ok {
			events = append(events, event)
			last = at
		}
		if n >= 499 {
			limited = true
			cancel()
			break
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		// Stop the producer before waiting: it may be blocked on its stdout pipe.
		cancel()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return time.Time{}, ctx.Err()
	}
	if scanErr != nil {
		return time.Time{}, errors.New("SSH journal record exceeds read limit")
	}
	if waitErr != nil && !limited {
		if previous.Position != "" && strings.Contains(strings.ToLower(stderr.String()), "cursor") {
			previous.Position = ""
			if e = s.append(nil, previous); e != nil {
				return time.Time{}, e
			}
			return time.Time{}, errors.New("SSH journal cursor expired; replaying from saved timestamp")
		}
		return time.Time{}, errors.New("SSH journal cannot be read; check journal permissions")
	}
	diagnostic := strings.ToLower(stderr.String())
	if strings.Contains(diagnostic, "permission") || strings.Contains(diagnostic, "no journal") {
		return time.Time{}, errors.New("SSH journal unavailable or permission denied")
	}
	if e = s.append(events, next); e != nil {
		return time.Time{}, e
	}
	return last, nil
}
