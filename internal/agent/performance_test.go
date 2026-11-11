package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkParseSSH(b *testing.B) {
	for _, sample := range []struct{ name, line string }{
		{"login", "sshd[123]: Accepted publickey for DOMAIN\\John Doe from 2001:db8::1 port 2222 ssh2: RSA SHA256:xxx"},
		{"failure", "sshd[123]: Failed password for invalid user admin from 192.0.2.1 port 54321 ssh2"},
		{"other", "sshd[123]: Connection closed by authenticating user root 192.0.2.1 port 54321 [preauth]"},
	} {
		b.Run(sample.name, func(b *testing.B) {
			at := time.Now().UTC()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = parseSSH(sample.line, "stable-source-cursor", at)
			}
		})
	}
}

func BenchmarkSpoolCounts(b *testing.B) {
	s, err := openSpool(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.db.Close()
	if err = s.append(make([]SSHEvent, queueLimit), cursor{Position: "full"}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.counts()
	}
}

func BenchmarkSpoolPosition(b *testing.B) {
	s, err := openSpool(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.db.Close()
	if err = s.append(nil, cursor{Kind: "file:auth.log", FileID: "file-identity", Offset: 12345, At: time.Now().UTC()}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.position()
	}
}

func BenchmarkSpoolSecondary(b *testing.B) {
	s, err := openSpool(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.db.Close()
	c := cursor{Kind: "file:vnc", Position: "42", At: time.Now().UTC()}
	if err = s.appendFor("vnc:test", nil, c); err != nil {
		b.Fatal(err)
	}
	b.Run("position", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.positionFor("vnc:test"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("unchanged", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := s.appendFor("vnc:test", nil, c); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSpoolBatch(b *testing.B) {
	s, err := openSpool(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.db.Close()
	events := make([]SSHEvent, 100)
	for i := range events {
		events[i], _ = parseSSH("Accepted password for user from 192.0.2.1 port 22 ssh2", "fixture", time.Now().UTC())
	}
	if err = s.append(events, cursor{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.batch(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIdleFilePoll(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "auth.log")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		b.Fatal(err)
	}
	s, err := openSpool(filepath.Join(dir, "state"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.db.Close()
	ctx := context.Background()
	if _, err = collectFile(ctx, s, path); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err = collectFile(ctx, s, path); err != nil {
			b.Fatal(err)
		}
	}
}
