package api

import "time"

// A server may miss two reports before becoming offline. Preserve the existing
// 90-second grace for short intervals and legacy Agents.
func reportGrace(seconds int) time.Duration {
	if seconds < 1 || seconds > 600 {
		seconds = 5
	}
	return time.Duration(max(90, 3*seconds)) * time.Second
}

func intervalSQL(serverID string) string {
	return `COALESCE((SELECT CASE WHEN reported_interval_seconds BETWEEN 1 AND 600 THEN reported_interval_seconds WHEN report_interval_seconds BETWEEN 1 AND 600 THEN report_interval_seconds ELSE 5 END FROM server_settings WHERE server_id=` + serverID + `),5)`
}

// Identifiers are internal SQL expressions, never user input. The parameter is
// the current Unix timestamp, shared by SQLite, PostgreSQL and MySQL.
func (a *App) freshSQL(column, serverID string) string {
	interval := intervalSQL(serverID)
	grace := `(CASE WHEN ` + interval + `>30 THEN 3*` + interval + ` ELSE 90 END)`
	epoch := `FLOOR(EXTRACT(EPOCH FROM ` + column + `))`
	if a.db.Driver == "mysql" {
		epoch = `FLOOR(UNIX_TIMESTAMP(` + column + `))`
	}
	if a.db.Driver == "sqlite" {
		epoch = `CAST(strftime('%s',substr(` + column + `,1,19)) AS INTEGER)`
	}
	return `(` + epoch + `>=?-` + grace + `)`
}
