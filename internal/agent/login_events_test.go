package agent

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestRemoteLoginParsers(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct{ format, message, kind, ip string }{
		{"console", "pam_unix(login:session): session opened for user root(uid=0) by LOGIN(uid=0)", "console.login", ""},
		{"console", "pam_unix(login:auth): authentication failure; user=root", "console.login_failed", ""},
		{"console", "pam_unix(sudo:session): session opened for user root", "", ""},
		{"console", "pam_unix(sshd:session): session opened for user root", "", ""},
		{"realvnc", "Connections: authenticated: user (from 192.0.2.8::48200), as user (f permissions)", "vnc.login", "192.0.2.8"},
		{"realvnc", "Connections: connected: 192.0.2.8::48200", "", ""},
		{"realvnc", "Connections: authentication failed: user (from 192.0.2.8::48200)", "vnc.login_failed", "192.0.2.8"},
		{"tigervnc", "VNCSConnST: Server default pixel format depth 24 (32bpp) little-endian rgb888", "vnc.login", ""},
		{"tigervnc", "Connections: accepted: 192.0.2.8::48200", "", ""},
		{"tightvnc", "Full-control authentication passed by 192.0.2.8", "vnc.login", "192.0.2.8"},
		{"tightvnc", "Authenticated with view-only password = 0", "vnc.login", ""},
		{"tightvnc", "rfbVncAuthProcessResponse: authentication failed from 192.0.2.8", "vnc.login_failed", "192.0.2.8"},
		{"tightvnc", "rfbProcessClientInitMessage: client gone", "", ""},
		{"ultravnc", "Password authentication accepted from 192.0.2.8 (Interactive) (ID:1)", "vnc.login", "192.0.2.8"},
		{"ultravnc", "authenticated connection", "vnc.login", ""},
		{"ultravnc", "unauthenticated connection", "", ""},
		{"ultravnc", "Connection received from 192.0.2.8", "", ""},
		{"unknown", "authenticated connection", "", ""},
	} {
		t.Run(tc.format+tc.message, func(t *testing.T) {
			var e SSHEvent
			var ok bool
			if tc.format == "console" {
				e, ok = parseLogin(tc.message, "fixture", now)
			} else {
				e, ok = parseVNC(tc.format, tc.message, "fixture", now)
			}
			if ok != (tc.kind != "") || e.Type != tc.kind || e.IP != tc.ip || e.Username != "" {
				t.Fatalf("unexpected event: %+v (%v)", e, ok)
			}
		})
	}
}

func TestVNCPeerUnicodePrefix(t *testing.T) {
	for _, prefix := range []string{"İ", "Ⱥ", strings.Repeat("Ⱥ", 64), "中文"} {
		e, ok := parseVNC("tightvnc", prefix+" Full-control authentication passed by 192.0.2.8", "fixture", time.Now())
		if !ok || e.IP != "192.0.2.8" {
			t.Fatalf("Unicode prefix %q: %+v, %v", prefix, e, ok)
		}
	}
}

func TestRemoteFileCursorIsolationAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "vnc.log")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openSpool(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	parse := func(line, id string, at time.Time) (SSHEvent, bool) { return parseVNC("tightvnc", line, id, at) }
	primary := cursor{Kind: "journal", Position: "ssh-cursor"}
	if err = s.append(nil, primary); err != nil {
		t.Fatal(err)
	}
	if _, err = collectFileSource(ctx, s, path, "vnc:test", parse); err != nil {
		t.Fatal(err)
	}
	line := time.Now().Format("02/01/2006 15:04:05") + " Full-control authentication passed by 192.0.2.8\n"
	if err = os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = collectFileSource(ctx, s, path, "vnc:test", parse); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.position(); c != primary {
		t.Fatal("VNC overwrote SSH cursor")
	}
	s.db.Close()
	s, err = openSpool(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if _, err = collectFileSource(ctx, s, path, "vnc:test", parse); err != nil {
		t.Fatal(err)
	}
	items, err := s.batch()
	if err != nil || len(items) != 1 || items[0].event.Type != "vnc.login" {
		t.Fatal("lost or duplicated login", items, err)
	}
	// Partial lines must not advance the cursor or emit an event.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(line[:len(line)-1])
	f.Close()
	if _, err = collectFileSource(ctx, s, path, "vnc:test", parse); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.counts(); q != 1 {
		t.Fatal("partial line emitted")
	}
}

func TestVNCPeerAndTime(t *testing.T) {
	for _, tc := range []struct {
		value, ip string
		port      int
	}{{"192.0.2.8::48200", "192.0.2.8", 48200}, {"[2001:db8::8]:48200", "2001:db8::8", 48200}, {"2001:db8::8", "2001:db8::8", 0}, {"localhost", "", 0}} {
		ip, port := peerAddress(tc.value)
		if ip != tc.ip || port != tc.port {
			t.Fatal(tc, ip, port)
		}
	}
	for _, line := range []string{"[1234] 2026-11-03 14:12:01 @ test", "03/11/2026 14:12:01\ttest", "Tue Nov  3 14:12:01 2026"} {
		if at, ok := vncTimestamp(line, time.Now()); !ok || at.Year() != 2026 || at.Month() != 11 || at.Day() != 3 {
			t.Fatal(line, at, ok)
		}
	}
}

func TestTightVNCUnicodeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvnserver.log")
	if err := os.WriteFile(path, []byte{255, 254}, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openSpool(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	parse := func(line, id string, at time.Time) (SSHEvent, bool) { return parseVNC("tightvnc", line, id, at) }
	if _, err = collectFileSource(context.Background(), s, path, "wide", parse); err != nil {
		t.Fatal(err)
	}
	line := "[123/456] " + time.Now().Format("2006-01-02 15:04:05") + " : Authenticated with view-only password = 0\r\n"
	words := utf16.Encode([]rune(line))
	raw := make([]byte, len(words)*2)
	for i, w := range words {
		binary.LittleEndian.PutUint16(raw[i*2:], w)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write(raw[:len(raw)-1])
	f.Close()
	if _, err = collectFileSource(context.Background(), s, path, "wide", parse); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.counts(); q != 0 {
		t.Fatal("half Unicode newline emitted")
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write(raw[len(raw)-1:])
	f.Close()
	if _, err = collectFileSource(context.Background(), s, path, "wide", parse); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.counts(); q != 1 {
		t.Fatal("Unicode login missing")
	}
	if c, _ := s.positionFor("wide"); c.Offset != int64(len(raw)+2) {
		t.Fatal("offset is not bytes", c)
	}
}
