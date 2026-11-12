package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sshguard/internal/storage"
)

func trendTestDB(t testing.TB) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), "sqlite", filepath.Join(t.TempDir(), "trend.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, sql := range []string{
		`CREATE TABLE ssh_events(occurred_at TIMESTAMP NOT NULL,outcome TEXT NOT NULL,source_ip TEXT)`,
		`CREATE INDEX events_dashboard ON ssh_events(occurred_at,outcome,source_ip)`,
	} {
		if _, err := db.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestLoginTrendBoundaries(t *testing.T) {
	for _, count := range []int{24, 7, 14, 30} {
		t.Run(time.Duration(count).String(), func(t *testing.T) {
			db := trendTestDB(t)
			app := &App{db: db}
			ctx := context.Background()
			step := 24 * time.Hour
			if count == 24 {
				step = time.Hour
			}
			now := time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)
			since := now.Truncate(step).Add(-time.Duration(count-1) * step)
			for _, event := range []struct {
				at      time.Time
				outcome string
			}{
				{since.Add(-time.Second), "failure"},
				{since, "success"},
				{since.Add(step - time.Second), "failure"},
				{since.Add(step), "failure"},
				{now, "success"},
				{now.Add(time.Second), "failure"},
			} {
				if _, err := db.Exec(ctx, `INSERT INTO ssh_events(occurred_at,outcome) VALUES(?,?)`, event.at, event.outcome); err != nil {
					t.Fatal(err)
				}
			}
			buckets, err := app.loginTrend(ctx, now, count, step)
			if err != nil {
				t.Fatal(err)
			}
			if len(buckets) != count {
				t.Fatal(len(buckets))
			}
			for i, b := range buckets {
				var success, failure int64
				if i == 0 || i == count-1 {
					success = 1
				}
				if i == 0 || i == 1 {
					failure = 1
				}
				if !b.At.Equal(since.Add(time.Duration(i)*step)) || b.Success != success || b.Failure != failure {
					t.Fatalf("bucket %d: %+v, want %d/%d", i, b, success, failure)
				}
			}
		})
	}
}

func BenchmarkLoginTrend(b *testing.B) {
	db := trendTestDB(b)
	ctx := context.Background()
	now := time.Date(2026, 11, 12, 12, 30, 0, 0, time.UTC)
	since := now.Truncate(24 * time.Hour).Add(-29 * 24 * time.Hour)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO ssh_events(occurred_at,outcome) VALUES(?,'failure')`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 300000; i++ {
		if _, err := stmt.Exec(since.Add(time.Duration(i%42000) * time.Minute)); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.Run("indexed_ranges", func(b *testing.B) {
		app := &App{db: db}
		for i := 0; i < b.N; i++ {
			if _, err := app.loginTrend(ctx, now, 30, 24*time.Hour); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("previous_group_by", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rows, err := db.QueryContext(ctx, `SELECT CAST(strftime('%s',substr(occurred_at,1,19)) AS INTEGER)/86400,SUM(CASE WHEN outcome='success' THEN 1 ELSE 0 END),SUM(CASE WHEN outcome='failure' THEN 1 ELSE 0 END) FROM ssh_events WHERE occurred_at>=? AND occurred_at<=? GROUP BY CAST(strftime('%s',substr(occurred_at,1,19)) AS INTEGER)/86400 ORDER BY 1`, since, now)
			if err != nil {
				b.Fatal(err)
			}
			for rows.Next() {
				var bucket, success, failure int64
				if err := rows.Scan(&bucket, &success, &failure); err != nil {
					b.Fatal(err)
				}
			}
			if err := rows.Err(); err != nil {
				b.Fatal(err)
			}
			rows.Close()
		}
	})
}
