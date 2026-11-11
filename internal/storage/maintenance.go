package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Usage measures the current database only. SQLite includes WAL and SHM files.
func (d *DB) Usage(ctx context.Context) (int64, error) {
	if d.Driver == "postgres" {
		var size int64
		err := d.Row(ctx, `SELECT pg_database_size(current_database())`).Scan(&size)
		return size, err
	}
	if d.Driver == "mysql" {
		var size int64
		err := d.Row(ctx, `SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&size)
		return size, err
	}
	rows, err := d.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return 0, err
	}
	var file string
	for rows.Next() {
		var seq int
		var name, path string
		if err = rows.Scan(&seq, &name, &path); err != nil {
			rows.Close()
			return 0, err
		}
		if name == "main" {
			file = path
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if file == "" {
		return 0, errors.New("database is not stored in a local file")
	}
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(file + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}

// These are application-owned tables, never user-supplied identifiers or all tables in a shared schema.
var maintenanceTables = []string{"schema_migrations", "users", "sessions", "login_attempts", "servers", "agent_enrollments", "agent_credentials", "audit_logs", "ssh_events", "agent_health", "notification_channels", "alert_rules", "alerts", "alert_state", "notification_jobs", "agent_updates", "server_settings", "ip_allowlist", "audit_context", "audit_actor_snapshots", "login_protection", "login_guards", "login_failures", "captcha_used", "agent_install_settings", "geoip_settings", "geoip_database", "server_network", "retention_settings"}

func (d *DB) MaintenanceAction() string {
	switch d.Driver {
	case "sqlite":
		return "vacuum"
	case "postgres":
		return "vacuum_analyze"
	case "mysql":
		return "optimize"
	}
	return ""
}

func (d *DB) optimizeTables(ctx context.Context) error {
	for _, table := range maintenanceTables {
		if d.Driver == "postgres" {
			if _, err := d.ExecContext(ctx, `VACUUM (ANALYZE) "`+table+`"`); err != nil {
				return fmt.Errorf("maintain %s: %w", table, err)
			}
			continue
		}
		rows, err := d.QueryContext(ctx, "OPTIMIZE TABLE `"+table+"`")
		if err != nil {
			return fmt.Errorf("maintain %s: %w", table, err)
		}
		succeeded := false
		for rows.Next() {
			var name, operation, kind, message string
			if err = rows.Scan(&name, &operation, &kind, &message); err != nil {
				break
			}
			if strings.EqualFold(kind, "error") {
				err = fmt.Errorf("table %s optimization rejected: %s", table, message)
				break
			}
			if strings.EqualFold(kind, "status") && strings.EqualFold(message, "OK") {
				succeeded = true
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if !succeeded {
			return fmt.Errorf("table %s optimization did not report success", table)
		}
	}
	return nil
}

// Reclaim does not delete application records. VACUUM cannot run in a transaction.
func (d *DB) Reclaim(ctx context.Context) error {
	if d.Driver == "postgres" || d.Driver == "mysql" {
		return d.optimizeTables(ctx)
	}
	if d.Driver != "sqlite" {
		return errors.New("unsupported maintenance driver")
	}
	conn, err := d.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	checkpoint := func() error {
		var busy, frames, done int
		err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &frames, &done)
		if err != nil {
			return err
		}
		if busy != 0 {
			return errors.New("database is busy; retry maintenance later")
		}
		return nil
	}
	if err = checkpoint(); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	return checkpoint()
}
