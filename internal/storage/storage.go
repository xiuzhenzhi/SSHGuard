package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
	"net/url"
	"sshguard/internal/storage/migrations"
	"strings"
	"time"
)

type DB struct {
	*sql.DB
	Driver string
}

func Open(ctx context.Context, driver, dsn string) (*DB, error) {
	if driver != "mysql" && driver != "postgres" && driver != "sqlite" {
		return nil, fmt.Errorf("unsupported database driver")
	}
	if driver == "sqlite" {
		base, raw, _ := strings.Cut(dsn, "?")
		params, err := url.ParseQuery(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid SQLite DSN")
		}
		params.Set("_txlock", "immediate")
		params.Set("_time_format", "sqlite")
		params.Add("_pragma", "busy_timeout(10000)")
		params.Add("_pragma", "foreign_keys(1)")
		params.Add("_pragma", "journal_mode(WAL)")
		dsn = base + "?" + params.Encode()
	}
	name := driver
	if driver == "postgres" {
		name = "pgx"
	}
	var p *sql.DB
	var e error
	if driver == "mysql" {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid MySQL DSN")
		}
		cfg.ParseTime = true
		if err := cfg.Apply(mysql.Charset("utf8mb4", "utf8mb4_unicode_ci")); err != nil {
			return nil, err
		}
		cfg.Loc = time.UTC
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params["time_zone"] = "'+00:00'"
		cfg.MultiStatements = false
		cfg.Timeout = 10 * time.Second
		cfg.ReadTimeout = 15 * time.Second
		cfg.WriteTimeout = 15 * time.Second
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid MySQL connection settings")
		}
		p = sql.OpenDB(connector)
	} else {
		p, e = sql.Open(name, dsn)
	}
	if e != nil {
		return nil, e
	}
	p.SetMaxOpenConns(20)
	p.SetMaxIdleConns(5)
	p.SetConnMaxLifetime(5 * time.Minute)
	if driver == "sqlite" {
		p.SetMaxOpenConns(1)
		p.SetMaxIdleConns(1)
		p.SetConnMaxLifetime(0)
	}
	if e = p.PingContext(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return &DB{p, driver}, nil
}
func (d *DB) Bind(q string) string {
	if d.Driver == "sqlite" {
		// BEGIN IMMEDIATE serializes writers before any transaction reads.
		return strings.TrimSuffix(q, " FOR UPDATE")
	}
	if d.Driver != "postgres" {
		return q
	}
	n := 0
	var b strings.Builder
	for _, c := range q {
		if c == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
func (d *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return d.ExecContext(ctx, d.Bind(q), args...)
}
func (d *DB) Row(ctx context.Context, q string, args ...any) *sql.Row {
	return d.QueryRowContext(ctx, d.Bind(q), args...)
}
func (d *DB) List(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, e := d.QueryContext(ctx, d.Bind(q), args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range values {
			ptr[i] = &values[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		item := map[string]any{}
		for i, k := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			item[k] = v
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// MySQL DDL auto-commits: migrations must be idempotent for retry after interruption.
func Migrate(ctx context.Context, d *DB) error {
	if d.Driver == "sqlite" {
		return migrateSQLite(ctx, d)
	}
	conn, e := d.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	if d.Driver == "postgres" {
		if _, e = conn.ExecContext(ctx, `SELECT pg_advisory_lock(704201)`); e != nil {
			return e
		}
		defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(704201)`)
	} else {
		var ok int
		if e = conn.QueryRowContext(ctx, `SELECT GET_LOCK('sshguard_migrations',30)`).Scan(&ok); e != nil || ok != 1 {
			return fmt.Errorf("migration lock failed: %v", e)
		}
		defer conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('sshguard_migrations')`)
	}
	if _, e = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version varchar(128) PRIMARY KEY, applied_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP)`); e != nil {
		return e
	}
	dir := d.Driver
	entries, e := migrations.Files.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, f := range entries {
		var n int
		if e = conn.QueryRowContext(ctx, d.Bind(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`), f.Name()).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			continue
		}
		if d.Driver == "mysql" && f.Name() == "012_remote_logins.sql" {
			if e = migrateMySQLRemoteLogins(ctx, conn); e != nil {
				return fmt.Errorf("%s: %w", f.Name(), e)
			}
		}
		b, e := migrations.Files.ReadFile(dir + "/" + f.Name())
		if e != nil {
			return e
		}
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		for _, statement := range strings.Split(string(b), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, e = tx.ExecContext(ctx, statement); e != nil {
				// MySQL DDL commits independently. An interrupted index migration
				// can be retried without marking partially applied DDL as complete.
				var mysqlErr *mysql.MySQLError
				if d.Driver == "mysql" && strings.HasPrefix(strings.TrimSpace(statement), "CREATE INDEX ") && errors.As(e, &mysqlErr) && mysqlErr.Number == 1061 {
					continue
				}
				tx.Rollback()
				return fmt.Errorf("%s: %w", f.Name(), e)
			}
		}
		if _, e = tx.ExecContext(ctx, d.Bind(`INSERT INTO schema_migrations(version) VALUES(?)`), f.Name()); e != nil {
			tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}

// Cleanup only removes expired authentication metadata, never events/audit logs.
// Each pass is bounded to avoid large transactions and long row locks.
func Cleanup(ctx context.Context, d *DB) error {
	now := time.Now().UTC()
	for _, item := range []struct {
		table, key, predicate string
		cutoff                time.Time
	}{
		{"sessions", "token_hash", "expires_at", now},
		{"agent_enrollments", "token_hash", "expires_at", now},
		{"login_attempts", "bucket_key", "window_start", now.Add(-24 * time.Hour)},
		{"login_failures", "id", "occurred_at", now.Add(-5 * time.Minute)},
		{"login_guards", "source_hash", "updated_at", now.Add(-25 * time.Hour)},
		{"captcha_used", "token_hash", "expires_at", now},
		{"agent_install_settings", "token_hash", "expires_at", now},
	} {
		rows, e := d.QueryContext(ctx, d.Bind("SELECT "+item.key+" FROM "+item.table+" WHERE "+item.predicate+"<? LIMIT 1000"), item.cutoff)
		if e != nil {
			return e
		}
		ids := []any{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) == 0 {
			continue
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		// Recheck expiry: a concurrent login can refresh or block a selected source.
		ids = append(ids, item.cutoff)
		if _, e = d.Exec(ctx, "DELETE FROM "+item.table+" WHERE "+item.key+" IN ("+marks+") AND "+item.predicate+"<?", ids...); e != nil {
			return e
		}
	}
	return nil
}

// IsUniqueViolation identifies duplicate keys without exposing database errors.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return state.SQLState() == "23505"
	}
	var code interface{ Code() int }
	if errors.As(err, &code) {
		return code.Code() == 2067 || code.Code() == 1555
	}
	return false
}
