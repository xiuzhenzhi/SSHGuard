package api

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"path/filepath"
	"sshguard/internal/storage"
	"testing"
	"time"
)

func TestLiveSessionDistinguishesOutageAndExpiration(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, "sqlite", filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	id, session := uuid.NewString(), token()
	if _, err = db.Exec(ctx, `INSERT INTO users(id,username,password_hash,role) VALUES(?,?,?,'admin')`, id, "fixture", "unused"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hash(session), id, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	app := NewApp(db, Config{})
	pingCalls := 0
	ping := func(context.Context) error { pingCalls++; return nil }
	if err = app.checkLiveSession(ctx, session, ping); err != nil || pingCalls != 1 {
		t.Fatal("valid session", err, pingCalls)
	}
	pingErr := errors.New("missed pong")
	if err = app.checkLiveSession(ctx, session, func(context.Context) error { return pingErr }); !errors.Is(err, pingErr) || errors.Is(err, errLiveSessionExpired) {
		t.Fatal("ping failure treated as expired", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = app.checkLiveSession(cancelled, session, ping); err == nil || errors.Is(err, errLiveSessionExpired) {
		t.Fatal("query cancellation treated as expired", err)
	}
	if err = app.checkLiveSession(ctx, session, ping); err != nil {
		t.Fatal("transient failure invalidated session", err)
	}
	if _, err = db.Exec(ctx, `UPDATE sessions SET expires_at=? WHERE token_hash=?`, time.Now().UTC().Add(-time.Minute), hash(session)); err != nil {
		t.Fatal(err)
	}
	before := pingCalls
	if err = app.checkLiveSession(ctx, session, ping); !errors.Is(err, errLiveSessionExpired) || pingCalls != before {
		t.Fatal("expired session accepted", err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM sessions WHERE token_hash=?`, hash(session)); err != nil {
		t.Fatal(err)
	}
	if err = app.checkLiveSession(ctx, session, ping); !errors.Is(err, errLiveSessionExpired) {
		t.Fatal("revoked session accepted", err)
	}
	db.Close()
	if err = app.checkLiveSession(ctx, session, ping); err == nil || errors.Is(err, errLiveSessionExpired) {
		t.Fatal("database outage treated as expired", err)
	}
}
