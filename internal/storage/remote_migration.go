package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// MySQL assigns generated CHECK names; MariaDB uses a different DROP syntax.
// SHOW CREATE also works on old MySQL versions that ignore CHECK clauses.
// Run on the locked migration connection before opening the bookkeeping tx.
func migrateMySQLRemoteLogins(ctx context.Context, conn *sql.Conn) error {
	var table, ddl, version string
	if err := conn.QueryRowContext(ctx, "SHOW CREATE TABLE ssh_events").Scan(&table, &ddl); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return err
	}
	checks := regexp.MustCompile("(?im)CONSTRAINT `([^`]+)` CHECK[^\\n]*event_type[^\\n]*").FindAllStringSubmatch(ddl, -1)
	for _, check := range checks {
		if strings.Contains(check[0], "console.login") {
			continue
		}
		verb := "DROP CHECK"
		if strings.Contains(strings.ToLower(version), "mariadb") {
			verb = "DROP CONSTRAINT"
		}
		name := "`" + strings.ReplaceAll(check[1], "`", "``") + "`"
		// One ALTER is atomic on supported servers. A retry sees the new check
		// and skips it, including after MySQL's implicit DDL commit.
		query := fmt.Sprintf("ALTER TABLE ssh_events %s %s, ADD CONSTRAINT events_remote_type CHECK(event_type IN ('ssh.login','ssh.login_failed','rdp.login','rdp.login_failed','vnc.login','vnc.login_failed','console.login','console.login_failed'))", verb, name)
		if _, err := conn.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
