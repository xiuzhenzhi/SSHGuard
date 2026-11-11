package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"net"
	"sshguard/internal/ipaddress"
	"strings"
	"time"
)

func (a *App) enroll(c *gin.Context) {
	var in struct {
		Token        string `json:"token" binding:"required,len=64"`
		Hostname     string `json:"hostname" binding:"required,max=253"`
		Distribution string `json:"distribution" binding:"max=128"`
		Version      string `json:"version" binding:"required,max=32"`
	}
	if !bind(c, &in) {
		return
	}
	t := token()
	var id string
	interval := 5
	// Lock server first for a consistent lock order with revocation and token replacement.
	if e := a.db.Row(c.Request.Context(), `SELECT server_id FROM agent_enrollments WHERE token_hash=?`, hash(in.Token)).Scan(&id); e != nil {
		if e == sql.ErrNoRows {
			bad(c, 401, "注册凭证无效")
		} else {
			fail(c, e)
		}
		return
	}
	e := a.mutate(c, "", "agent.enrolled", id, func(tx *sql.Tx) error {
		var locked string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM servers WHERE id=? FOR UPDATE`), id).Scan(&locked); e != nil {
			return e
		}
		var enrolled string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT server_id FROM agent_enrollments WHERE token_hash=? AND used_at IS NULL AND expires_at>? FOR UPDATE`), hash(in.Token), time.Now().UTC()).Scan(&enrolled); e != nil {
			return e
		}
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT report_interval_seconds FROM agent_install_settings WHERE token_hash=?`), hash(in.Token)).Scan(&interval); e != nil && e != sql.ErrNoRows {
			return e
		}
		if interval < 1 || interval > 600 {
			return errInvalidAgentMessage
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_enrollments SET used_at=? WHERE token_hash=?`), time.Now().UTC(), hash(in.Token)); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_credentials SET revoked_at=CURRENT_TIMESTAMP WHERE server_id=?`), id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO agent_credentials(token_hash,server_id) VALUES(?,?)`), hash(t), id); e != nil {
			return e
		}
		if e := a.agentOnline(c.Request.Context(), tx, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE servers SET hostname=?,distribution=?,agent_version=?,last_seen_at=?,revoked=false WHERE id=?`), in.Hostname, in.Distribution, in.Version, time.Now().UTC(), id); e != nil {
			return e
		}
		if e := a.ensureServerSettings(c.Request.Context(), tx, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`DELETE FROM server_network WHERE server_id=?`), id); e != nil {
			return e
		}
		_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE server_settings SET report_interval_seconds=?,reported_interval_seconds=0,ip_addresses='[]' WHERE server_id=?`), interval, id)
		return e
	})
	if e != nil {
		if e == sql.ErrNoRows {
			bad(c, 401, "注册凭证过期或已使用")
		} else {
			fail(c, e)
		}
		return
	}
	ok(c, gin.H{"server_id": id, "token": t, "report_interval_seconds": interval})
}
func (a *App) agentAuth(c *gin.Context) {
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) != 71 {
		bad(c, 401, "Agent 凭证无效")
		return
	}
	h := hash(strings.TrimPrefix(auth, "Bearer "))
	var id string
	authCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	e := a.db.Row(authCtx, `SELECT s.id FROM agent_credentials ac JOIN servers s ON s.id=ac.server_id WHERE ac.token_hash=? AND ac.revoked_at IS NULL AND s.revoked=false`, h).Scan(&id)
	cancel()
	if e == sql.ErrNoRows {
		bad(c, 401, "Agent 凭证无效")
		return
	}
	if e != nil {
		fail(c, e)
		return
	}
	c.Set("server_id", id)
	c.Set("agent_hash", h)
	c.Next()
}
func (a *App) agentTx(c *gin.Context, fn func(*sql.Tx) error) error {
	return a.agentTransaction(c.Request.Context(), c.GetString("server_id"), c.GetString("agent_hash"), fn)
}
func (a *App) agentTransaction(ctx context.Context, serverID, credentialHash string, fn func(*sql.Tx) error) error {
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, a.db.Bind(`SELECT id FROM servers WHERE id=? AND revoked=false FOR UPDATE`), serverID).Scan(&id); e != nil {
		return e
	}
	if e = tx.QueryRowContext(ctx, a.db.Bind(`SELECT server_id FROM agent_credentials WHERE token_hash=? AND server_id=? AND revoked_at IS NULL`), credentialHash, serverID).Scan(&id); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

type Heartbeat struct {
	SourceIP   string     `json:"-"`
	Interval   int        `json:"report_interval_seconds" binding:"min=0,max=600"`
	Addresses  []string   `json:"ip_addresses" binding:"max=16,dive,ip"`
	Version    string     `json:"version" binding:"max=32"`
	State      string     `json:"collector_state" binding:"omitempty,oneof=starting healthy error"`
	Message    string     `json:"collector_message" binding:"max=256"`
	QueueDepth int        `json:"queue_depth" binding:"min=0,max=10000"`
	Rejected   int        `json:"rejected_events" binding:"min=0,max=1000"`
	LastPoll   *time.Time `json:"last_poll_at"`
	LastEvent  *time.Time `json:"last_event_at"`
}

func (a *App) recordHeartbeat(ctx context.Context, serverID, credentialHash string, in Heartbeat) error {
	if e := binding.Validator.ValidateStruct(in); e != nil {
		return errInvalidAgentMessage
	}
	e := a.agentTransaction(ctx, serverID, credentialHash, func(tx *sql.Tx) error {
		if e := a.agentOnline(ctx, tx, serverID); e != nil {
			return e
		}
		if ipaddress.Public(in.SourceIP) {
			var previous string
			err := tx.QueryRowContext(ctx, a.db.Bind(`SELECT public_ip FROM server_network WHERE server_id=?`), serverID).Scan(&previous)
			if err == sql.ErrNoRows {
				if _, err := tx.ExecContext(ctx, a.db.Bind(`INSERT INTO server_network(server_id,public_ip) VALUES(?,?)`), serverID, in.SourceIP); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if previous != in.SourceIP {
				if _, err := tx.ExecContext(ctx, a.db.Bind(`UPDATE server_network SET public_ip=? WHERE server_id=?`), in.SourceIP, serverID); err != nil {
					return err
				}
			}
		} else if in.SourceIP != "" {
			// A changed route/proxy must not leave an old public source displayed forever.
			if _, err := tx.ExecContext(ctx, a.db.Bind(`DELETE FROM server_network WHERE server_id=?`), serverID); err != nil {
				return err
			}
		}
		if in.Interval > 0 {
			if e := a.ensureServerSettings(ctx, tx, serverID); e != nil {
				return e
			}
			addresses := []string{}
			seen := map[string]bool{}
			for _, value := range in.Addresses {
				ip := net.ParseIP(value)
				if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
					continue
				}
				value = ip.String()
				if !seen[value] {
					addresses = append(addresses, value)
					seen[value] = true
				}
			}
			raw, _ := json.Marshal(addresses)
			if _, e := tx.ExecContext(ctx, a.db.Bind(`UPDATE server_settings SET reported_interval_seconds=?,ip_addresses=? WHERE server_id=? AND (reported_interval_seconds<>? OR ip_addresses<>?)`), in.Interval, string(raw), serverID, in.Interval, string(raw)); e != nil {
				return e
			}
		}
		if in.State != "" {
			now := time.Now().UTC()
			for _, at := range []*time.Time{in.LastPoll, in.LastEvent} {
				if at != nil && (at.After(now.Add(5*time.Minute)) || at.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))) {
					return errInvalidAgentMessage
				}
			}
			var count int
			if e := tx.QueryRowContext(ctx, a.db.Bind(`SELECT COUNT(*) FROM agent_health WHERE server_id=?`), serverID).Scan(&count); e != nil {
				return e
			}
			if count == 0 {
				if _, e := tx.ExecContext(ctx, a.db.Bind(`INSERT INTO agent_health(server_id) VALUES(?)`), serverID); e != nil {
					return e
				}
			}
			if _, e := tx.ExecContext(ctx, a.db.Bind(`UPDATE agent_health SET state_since=CASE WHEN collector_state<>? THEN ? ELSE state_since END,collector_state=?,collector_message=?,queue_depth=?,rejected_events=?,last_poll_at=?,last_event_at=?,updated_at=? WHERE server_id=?`), in.State, now, in.State, in.Message, in.QueueDepth, in.Rejected, in.LastPoll, in.LastEvent, now, serverID); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, a.db.Bind(`UPDATE servers SET last_seen_at=?,agent_version=CASE WHEN ?='' THEN agent_version ELSE ? END WHERE id=?`), time.Now().UTC(), in.Version, in.Version, serverID)
		return e
	})
	if e == nil {
		a.hub.publish()
	}
	return e
}
func (a *App) heartbeat(c *gin.Context) {
	var in Heartbeat
	if !bind(c, &in) {
		return
	}
	in.SourceIP = c.ClientIP()
	if e := a.recordHeartbeat(c.Request.Context(), c.GetString("server_id"), c.GetString("agent_hash"), in); e != nil {
		agentFailure(c, e)
		return
	}
	ok(c, gin.H{"received": true})
}

type Event struct {
	ID       string    `json:"event_id" binding:"required,uuid"`
	Type     string    `json:"event_type" binding:"required,oneof=ssh.login ssh.login_failed rdp.login rdp.login_failed vnc.login vnc.login_failed console.login console.login_failed"`
	Username string    `json:"username" binding:"max=128"`
	IP       string    `json:"source_ip" binding:"omitempty,ip"`
	Port     *int      `json:"source_port" binding:"omitempty,min=1,max=65535"`
	Method   string    `json:"auth_method" binding:"max=32"`
	Outcome  string    `json:"outcome" binding:"required,oneof=success failure"`
	At       time.Time `json:"occurred_at" binding:"required"`
}

func validEvent(e Event, now time.Time) bool {
	protocol, kind, ok := strings.Cut(e.Type, ".")
	if !ok || (protocol != "ssh" && protocol != "rdp" && protocol != "vnc" && protocol != "console") {
		return false
	}
	if protocol == "ssh" && (e.Username == "" || net.ParseIP(e.IP) == nil) {
		return false
	}
	if protocol == "console" && (e.Username != "" || e.IP != "" || e.Port != nil) {
		return false
	}
	if e.Port != nil && e.IP == "" {
		return false
	}
	return !e.At.Before(now.AddDate(0, 0, -30)) && !e.At.After(now.Add(5*time.Minute)) && ((kind == "login" && e.Outcome == "success") || (kind == "login_failed" && e.Outcome == "failure")) && !strings.ContainsAny(e.Username, "\r\n\x00")
}

var errInvalidAgentMessage = errors.New("invalid agent message")

type EventBatch struct {
	Events []Event `json:"events" binding:"required,min=1,max=100,dive"`
}
type BatchResult struct {
	Accepted   int `json:"accepted"`
	Inserted   int `json:"inserted"`
	Duplicates int `json:"duplicates"`
}

func agentFailure(c *gin.Context, e error) {
	switch {
	case errors.Is(e, sql.ErrNoRows):
		bad(c, 401, "Agent 凭证已失效")
	case errors.Is(e, errInvalidAgentMessage):
		bad(c, 400, "事件字段、时间或类型无效")
	default:
		bad(c, 503, "暂时无法保存，请保留事件并重试")
	}
}
func (a *App) batch(c *gin.Context) {
	var in EventBatch
	if !bind(c, &in) {
		return
	}
	result, e := a.ingest(c.Request.Context(), c.GetString("server_id"), c.GetString("agent_hash"), in)
	if e != nil {
		agentFailure(c, e)
		return
	}
	ok(c, result)
}

// Both transports acknowledge only after the same transaction commits.
func (a *App) ingest(ctx context.Context, serverID, credentialHash string, in EventBatch) (BatchResult, error) {
	result := BatchResult{}
	if e := binding.Validator.ValidateStruct(in); e != nil {
		return result, errInvalidAgentMessage
	}
	now := time.Now().UTC()
	for i, event := range in.Events {
		if !validEvent(event, now) {
			return result, errInvalidAgentMessage
		}
		in.Events[i].At = event.At.UTC()
		in.Events[i].ID = uuid.MustParse(event.ID).String()
		if event.IP != "" {
			in.Events[i].IP = net.ParseIP(event.IP).String()
		}
	}
	rules, e := a.readRules(ctx, true)
	if e != nil {
		return result, e
	}
	e = a.agentTransaction(ctx, serverID, credentialHash, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT cidr FROM ip_allowlist`)
		if e != nil {
			return e
		}
		allowed := Rule{}
		for rows.Next() {
			var prefix string
			if e = rows.Scan(&prefix); e != nil {
				rows.Close()
				return e
			}
			allowed.Trusted = append(allowed.Trusted, prefix)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		var name, group string
		if e := tx.QueryRowContext(ctx, a.db.Bind(`SELECT name,group_name FROM servers WHERE id=?`), serverID).Scan(&name, &group); e != nil {
			return e
		}
		for _, event := range in.Events {
			var n int
			if e := tx.QueryRowContext(ctx, a.db.Bind(`SELECT COUNT(*) FROM ssh_events WHERE server_id=? AND event_id=?`), serverID, event.ID).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				continue
			}
			if _, e := tx.ExecContext(ctx, a.db.Bind(`INSERT INTO ssh_events(event_id,server_id,event_type,username,source_ip,source_port,auth_method,outcome,occurred_at) VALUES(?,?,?,?,?,?,?,?,?)`), event.ID, serverID, event.Type, event.Username, event.IP, event.Port, event.Method, event.Outcome, event.At); e != nil {
				return e
			}
			if !trusted(allowed, event.IP) {
				if e := a.evaluateEvent(ctx, tx, rules, serverID, name, group, event); e != nil {
					return e
				}
			}
			result.Inserted++
		}
		return nil
	})
	if e != nil {
		return BatchResult{}, e
	}
	result.Accepted = len(in.Events)
	result.Duplicates = result.Accepted - result.Inserted
	if result.Inserted > 0 {
		a.hub.publish()
	}
	return result, nil
}
