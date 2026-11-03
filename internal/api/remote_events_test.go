package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"sshguard/internal/storage"
)

func TestRemoteEventIngestion(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, "sqlite", filepath.Join(t.TempDir(), "remote.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	testRemoteEventIngestion(t, db)
}

func testRemoteEventIngestion(t *testing.T, db *storage.DB) {
	t.Helper()
	ctx := context.Background()
	var err error
	id, credential := uuid.NewString(), token()
	if _, err = db.Exec(ctx, `INSERT INTO servers(id,name) VALUES(?,?)`, id, "remote fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO agent_credentials(token_hash,server_id) VALUES(?,?)`, hash(credential), id); err != nil {
		t.Fatal(err)
	}
	app := NewApp(db, Config{})
	batch := EventBatch{}
	for _, protocol := range []string{"rdp", "vnc", "console"} {
		for _, outcome := range []string{"success", "failure"} {
			kind := protocol + ".login"
			if outcome == "failure" {
				kind += "_failed"
			}
			e := Event{ID: uuid.NewString(), Type: kind, Outcome: outcome, At: time.Now().UTC()}
			if protocol == "rdp" {
				e.Username = `HOST\Administrator`
				e.IP = "192.0.2.8"
			}
			batch.Events = append(batch.Events, e)
		}
	}
	result, err := app.ingest(ctx, id, hash(credential), batch)
	if err != nil || result.Inserted != 6 {
		t.Fatal(result, err)
	}
	result, err = app.ingest(ctx, id, hash(credential), batch)
	if err != nil || result.Duplicates != 6 {
		t.Fatal("replay", result, err)
	}
	var count int
	if err = db.Row(ctx, `SELECT COUNT(*) FROM ssh_events WHERE event_type LIKE 'console.%' AND username='' AND source_ip='' AND source_port IS NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatal("console privacy", count, err)
	}
	for _, e := range []Event{
		{Type: "ssh.login", Outcome: "success"},
		{Type: "console.login", Username: "root", Outcome: "success"},
		{Type: "console.login", IP: "127.0.0.1", Outcome: "success"},
		{Type: "vnc.login", Outcome: "failure"},
		{Type: "rdp.login", IP: "not-an-ip", Outcome: "success"},
	} {
		e.ID = uuid.NewString()
		e.At = time.Now()
		if _, err = app.ingest(ctx, id, hash(credential), EventBatch{Events: []Event{e}}); err == nil {
			t.Fatal("accepted invalid event", e)
		}
	}
}
