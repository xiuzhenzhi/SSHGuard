package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Rule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name" binding:"required,max=128"`
	Kind      string   `json:"kind" binding:"required,oneof=login root failure_threshold agent_offline agent_online collector_error"`
	Enabled   bool     `json:"enabled"`
	Threshold int      `json:"threshold_count" binding:"min=1,max=10000"`
	Window    int      `json:"window_seconds" binding:"min=30,max=86400"`
	Cooldown  int      `json:"cooldown_seconds" binding:"min=30,max=86400"`
	Group     string   `json:"server_group" binding:"max=64"`
	Trusted   []string `json:"trusted_cidrs" binding:"max=100,dive,max=64"`
	Channels  []string `json:"channel_ids" binding:"max=20,dive,uuid"`
}

func (a *App) rules(c *gin.Context) {
	items, e := a.readRules(c.Request.Context(), false)
	if e != nil {
		fail(c, e)
		return
	}
	ok(c, gin.H{"items": items, "has_more": false})
}
func (a *App) readRules(ctx context.Context, enabledOnly bool) ([]Rule, error) {
	return readRulesFrom(ctx, a.db, enabledOnly)
}
func readRulesFrom(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, enabledOnly bool) ([]Rule, error) {
	query := `SELECT id,name,kind,enabled,threshold_count,window_seconds,cooldown_seconds,server_group,trusted_cidrs,channel_ids FROM alert_rules`
	if enabledOnly {
		query += ` WHERE enabled=true`
	}
	query += ` ORDER BY created_at LIMIT 100`
	rows, e := db.QueryContext(ctx, query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []Rule{}
	for rows.Next() {
		var r Rule
		var cidrs, channels string
		if e = rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Enabled, &r.Threshold, &r.Window, &r.Cooldown, &r.Group, &cidrs, &channels); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(cidrs), &r.Trusted); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(channels), &r.Channels); e != nil {
			return nil, e
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func (a *App) saveRule(c *gin.Context) {
	var in Rule
	if !bind(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		bad(c, 400, "规则名称不能为空")
		return
	}
	for i, cidr := range in.Trusted {
		prefix, e := normalizePrefix(cidr)
		if e != nil {
			bad(c, 400, "白名单必须是 IP 或 CIDR 网段")
			return
		}
		in.Trusted[i] = prefix
	}
	id := c.Param("id")
	create := id == ""
	if create {
		id = uuid.NewString()
	}
	in.ID = id
	cidrs, _ := json.Marshal(in.Trusted)
	channels, _ := json.Marshal(in.Channels)
	e := a.mutate(c, c.MustGet("user").(User).ID, "rule.save", id, func(tx *sql.Tx) error {
		// Serialize count+insert without database-specific partial indexes.
		var locked string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM users WHERE role='admin' ORDER BY id LIMIT 1 FOR UPDATE`)).Scan(&locked); e != nil {
			return e
		}
		for _, channel := range in.Channels {
			var n int
			if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT COUNT(*) FROM notification_channels WHERE id=?`), channel).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return errChannelConflict
			}
		}
		if create {
			var n int
			if e := tx.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM alert_rules`).Scan(&n); e != nil {
				return e
			}
			if n >= 100 {
				return errChannelConflict
			}
			_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO alert_rules(id,name,kind,enabled,threshold_count,window_seconds,cooldown_seconds,server_group,trusted_cidrs,channel_ids) VALUES(?,?,?,?,?,?,?,?,?,?)`), id, in.Name, in.Kind, in.Enabled, in.Threshold, in.Window, in.Cooldown, in.Group, string(cidrs), string(channels))
			return e
		}
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM alert_rules WHERE id=? FOR UPDATE`), id).Scan(&locked); e != nil {
			return e
		}
		_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE alert_rules SET name=?,kind=?,enabled=?,threshold_count=?,window_seconds=?,cooldown_seconds=?,server_group=?,trusted_cidrs=?,channel_ids=? WHERE id=?`), in.Name, in.Kind, in.Enabled, in.Threshold, in.Window, in.Cooldown, in.Group, string(cidrs), string(channels), id)
		return e
	})
	if e == errChannelConflict {
		bad(c, 409, "渠道不存在或规则数量已达到 100 条限制")
		return
	}
	if e != nil {
		fail(c, e)
		return
	}
	ok(c, in)
}
func trusted(r Rule, ip string) bool {
	parsed := net.ParseIP(ip)
	for _, prefix := range r.Trusted {
		_, network, e := net.ParseCIDR(prefix)
		if e == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}

func privilegedUser(username string) bool {
	if username == "root" {
		return true
	}
	if i := strings.LastIndex(username, `\`); i >= 0 {
		username = username[i+1:]
	}
	return strings.EqualFold(username, "administrator")
}

// Call only while holding this server's row lock. Events, alerts, cooldown state
// and notification jobs share one transaction, including HTTP and WS retries.
func (a *App) evaluateEvent(ctx context.Context, tx *sql.Tx, rules []Rule, serverID, name, group string, event Event) error {
	for _, rule := range rules {
		if !rule.Enabled || (rule.Group != "" && rule.Group != group) || trusted(rule, event.IP) {
			continue
		}
		match := false
		severity := "info"
		switch rule.Kind {
		case "login":
			match = event.Outcome == "success"
			if privilegedUser(event.Username) {
				severity = "high"
			}
		case "root":
			match = event.Outcome == "success" && privilegedUser(event.Username)
			severity = "high"
		case "failure_threshold":
			if event.Outcome != "failure" || event.IP == "" {
				continue
			}
			var err error
			match, err = a.failureThreshold(ctx, tx, serverID, event, rule)
			if err != nil {
				return err
			}
			severity = "high"
		}
		if !match {
			continue
		}
		port := ""
		if event.Port != nil {
			port = fmt.Sprint(*event.Port)
		}
		source, username := event.IP, event.Username
		if source == "" {
			source = "不可获取"
		} else if port != "" {
			source = net.JoinHostPort(source, port)
		}
		if username == "" {
			username = "未记录"
		}
		text := fmt.Sprintf("服务器：%s\n类型：%s\n用户：%s\n来源：%s\n认证方式：%s\n结果：%s\n发生时间：%s", name, event.Type, username, source, event.Method, event.Outcome, event.At.Format(time.RFC3339))
		scope := event.IP
		if !strings.HasPrefix(event.Type, "ssh.") {
			scope = event.Type + "|" + scope
		}
		if rule.Kind != "failure_threshold" {
			scope += "|" + event.Username
		}
		if e := a.raiseAlert(ctx, tx, rule, serverID, event.ID, scope, severity, text); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) raiseAlert(ctx context.Context, tx *sql.Tx, rule Rule, serverID, eventID, scope, severity, message string) error {
	now := time.Now().UTC()
	key := hash(rule.ID + "|" + serverID + "|" + scope)
	var last time.Time
	// The server row already serializes this rule's state. Locking a missing key
	// here causes MySQL REPEATABLE READ gap-lock deadlocks across different servers.
	// A plain read keeps compatibility with MySQL 5.6 statement-based binlogs too.
	e := tx.QueryRowContext(ctx, a.db.Bind(`SELECT last_alert_at FROM alert_state WHERE state_key=?`), key).Scan(&last)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == nil && now.Sub(last) < time.Duration(rule.Cooldown)*time.Second {
		return nil
	}
	if e == sql.ErrNoRows {
		if _, e = tx.ExecContext(ctx, a.db.Bind(`INSERT INTO alert_state(state_key,last_alert_at) VALUES(?,?)`), key, now); e != nil {
			return e
		}
	} else {
		if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE alert_state SET last_alert_at=? WHERE state_key=?`), now, key); e != nil {
			return e
		}
	}
	id := uuid.NewString()
	var event any
	if eventID != "" {
		event = eventID
	}
	if _, e = tx.ExecContext(ctx, a.db.Bind(`INSERT INTO alerts(id,rule_id,server_id,event_id,severity,title,message,created_at) VALUES(?,?,?,?,?,?,?,?)`), id, rule.ID, serverID, event, severity, rule.Name, message, now); e != nil {
		return e
	}
	q := `SELECT id FROM notification_channels WHERE enabled=true`
	args := []any{}
	if len(rule.Channels) > 0 {
		q += ` AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(rule.Channels)), ",") + `)`
		for _, v := range rule.Channels {
			args = append(args, v)
		}
	}
	rows, e := tx.QueryContext(ctx, a.db.Bind(q), args...)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var channel string
		if e = rows.Scan(&channel); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, channel)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, channel := range ids {
		priority := 0
		if severity == "high" {
			priority = 10
		}
		if _, e = tx.ExecContext(ctx, a.db.Bind(`INSERT INTO notification_jobs(id,alert_id,channel_id,next_attempt_at,priority) VALUES(?,?,?,?,?)`), uuid.NewString(), id, channel, now, priority); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) alerts(c *gin.Context) {
	query := `SELECT a.*,s.name AS server_name,n.public_ip AS observed_public_ip,ss.ip_addresses FROM alerts a LEFT JOIN servers s ON s.id=a.server_id LEFT JOIN server_network n ON n.server_id=a.server_id LEFT JOIN server_settings ss ON ss.server_id=a.server_id WHERE 1=1`
	args := []any{}
	if c.Query("unacknowledged") == "true" {
		query += ` AND a.acknowledged_at IS NULL`
	}
	if v := c.Query("severity"); v != "" {
		query += ` AND a.severity=?`
		args = append(args, v)
	}
	if v := c.Query("server_id"); v != "" {
		query += ` AND a.server_id=?`
		args = append(args, v)
	}
	size, offset := page(c)
	args = append(args, size+1, offset)
	rows, err := a.db.List(c.Request.Context(), query+` ORDER BY a.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		fail(c, err)
		return
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	for _, row := range rows {
		row["server_ip_addresses"] = serverAddresses(map[string]any{"ip_addresses": row["ip_addresses"], "observed_public_ip": row["observed_public_ip"]})
		delete(row, "ip_addresses")
		delete(row, "observed_public_ip")
	}
	ok(c, gin.H{"items": rows, "has_more": more})
}
func (a *App) acknowledgeAlert(c *gin.Context) {
	id := c.Param("id")
	e := a.mutate(c, c.MustGet("user").(User).ID, "alert.acknowledge", id, func(tx *sql.Tx) error {
		var existing string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM alerts WHERE id=? FOR UPDATE`), id).Scan(&existing); e != nil {
			return e
		}
		_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE alerts SET acknowledged_by=?,acknowledged_at=? WHERE id=? AND acknowledged_at IS NULL`), c.MustGet("user").(User).ID, time.Now().UTC(), id)
		return e
	})
	if e != nil {
		fail(c, e)
		return
	}
	a.hub.publish()
	ok(c, gin.H{"acknowledged": true})
}

// Scan windows containing a late event, retaining only Threshold timestamps.
func (a *App) failureThreshold(ctx context.Context, tx *sql.Tx, serverID string, event Event, rule Rule) (bool, error) {
	window := time.Duration(rule.Window) * time.Second
	rows, err := tx.QueryContext(ctx, a.db.Bind(`SELECT occurred_at FROM ssh_events WHERE server_id=? AND source_ip=? AND outcome='failure' AND occurred_at>=? AND occurred_at<=? ORDER BY occurred_at`), serverID, event.IP, event.At.Add(-window), event.At.Add(window))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	times := make([]time.Time, rule.Threshold)
	count := 0
	for rows.Next() {
		var at time.Time
		if err := rows.Scan(&at); err != nil {
			return false, err
		}
		times[count%len(times)] = at
		count++
		if count >= len(times) && !at.Before(event.At) && at.Sub(times[count%len(times)]) <= window {
			return true, nil
		}
	}
	return false, rows.Err()
}
