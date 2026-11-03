package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshguard/internal/storage/migrations"
)

func TestRemoteMigrationPreservesHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ctx, `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	files, _ := migrations.Files.ReadDir("sqlite")
	for _, file := range files {
		if file.Name() >= "012_remote_logins.sql" {
			continue
		}
		data, _ := migrations.Files.ReadFile("sqlite/" + file.Name())
		for _, stmt := range strings.Split(string(data), ";") {
			if strings.TrimSpace(stmt) != "" {
				if _, err = db.Exec(ctx, stmt); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err = db.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, file.Name()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, `INSERT INTO servers(id,name) VALUES('server','fixture')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.Exec(ctx, `INSERT INTO ssh_events(id,event_id,server_id,event_type,username,source_ip,outcome,occurred_at) VALUES(99,'old','server','ssh.login','root','192.0.2.1','success',?)`, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var id int
	var user string
	if err = db.Row(ctx, `SELECT id,username FROM ssh_events WHERE event_id='old'`).Scan(&id, &user); err != nil || id != 99 || user != "root" {
		t.Fatal("history changed", id, user, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO ssh_events(event_id,server_id,event_type,username,source_ip,outcome,occurred_at) VALUES('new','server','console.login','','','success',?)`, now); err != nil {
		t.Fatal(err)
	}
	if err = db.Row(ctx, `SELECT id FROM ssh_events WHERE event_id='new'`).Scan(&id); err != nil || id <= 99 {
		t.Fatal("sequence changed", id, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO ssh_events(event_id,server_id,event_type,username,source_ip,outcome,occurred_at) VALUES('bad','server','invalid','','','success',?)`, now); err == nil {
		t.Fatal("type constraint missing")
	}
}
