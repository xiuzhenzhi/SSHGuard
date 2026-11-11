package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Called under the server row lock, before last_seen_at is updated. The alert,
// delivery jobs and heartbeat commit together, so retries cannot duplicate it.
func (a *App) agentOnline(ctx context.Context, tx *sql.Tx, id string) error {
	var name, group string
	var seen sql.NullTime
	var revoked bool
	var interval int
	if err := tx.QueryRowContext(ctx, a.db.Bind(`SELECT name,group_name,last_seen_at,revoked,`+intervalSQL("servers.id")+` FROM servers WHERE id=?`), id).Scan(&name, &group, &seen, &revoked, &interval); err != nil {
		return err
	}
	now := time.Now().UTC()
	if seen.Valid && !revoked && time.Duration(now.Unix()-seen.Time.Unix())*time.Second <= reportGrace(interval) {
		return nil
	}
	rules, err := readRulesFrom(ctx, tx, true)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.Kind != "agent_online" || (rule.Group != "" && rule.Group != group) {
			continue
		}
		message := fmt.Sprintf("服务器：%s\nAgent 已上线\n上线时间：%s", name, now.Format(time.RFC3339))
		if err := a.raiseAlert(ctx, tx, rule, id, "", "online", "info", message); err != nil {
			return err
		}
	}
	return nil
}
