package api

import (
	"context"
	"sshguard/internal/storage"
	"testing"
	"time"
)

// Runs in the existing SQLite/PostgreSQL/MySQL integration matrix.
func testReportFreshness(t *testing.T, db *storage.DB, id string) {
	t.Helper()
	ctx := context.Background()
	app := &App{db: db}
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct{ configured, reported, grace int }{
		{5, 5, 90}, {30, 30, 90}, {60, 60, 180}, {600, 600, 1800},
		{600, 0, 1800}, {600, 5, 90}, {600, 601, 1800}, {601, 0, 90},
	} {
		if _, err := db.Exec(ctx, `UPDATE server_settings SET report_interval_seconds=?,reported_interval_seconds=? WHERE server_id=?`, tc.configured, tc.reported, id); err != nil {
			t.Fatal(err)
		}
		var seconds int
		if err := db.Row(ctx, `SELECT `+intervalSQL("servers.id")+` FROM servers WHERE id=?`, id).Scan(&seconds); err != nil {
			t.Fatal(err)
		}
		if reportGrace(seconds) != time.Duration(tc.grace)*time.Second {
			t.Fatal("monitor grace differs", tc, seconds)
		}
		for _, age := range []int{tc.grace - 1, tc.grace, tc.grace + 1} {
			if _, err := db.Exec(ctx, `UPDATE servers SET last_seen_at=? WHERE id=?`, now.Add(-time.Duration(age)*time.Second), id); err != nil {
				t.Fatal(err)
			}
			var fresh int
			if err := db.Row(ctx, `SELECT CASE WHEN `+app.freshSQL("last_seen_at", "servers.id")+` THEN 1 ELSE 0 END FROM servers WHERE id=?`, now.Unix(), id).Scan(&fresh); err != nil {
				t.Fatal(err)
			}
			if (fresh == 1) != (age <= tc.grace) {
				t.Fatal("freshness boundary mismatch", tc, age, fresh)
			}
		}
	}
	if _, err := db.Exec(ctx, `UPDATE server_settings SET report_interval_seconds=600,reported_interval_seconds=600 WHERE server_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE servers SET last_seen_at=? WHERE id=?`, now, id); err != nil {
		t.Fatal(err)
	}
}
