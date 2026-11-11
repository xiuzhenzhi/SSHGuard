package api

import (
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"time"
)

var errUpdateUnavailable = errors.New("updater unavailable or busy")

// The protocol intentionally has no command, URL, script or argument fields.
func (a *App) queueAgentUpdate(c *gin.Context) {
	if _, valid := a.agentOrigin(c); !valid {
		return
	}
	id := c.Param("id")
	requestID := uuid.NewString()
	now := time.Now().UTC()
	version := a.availableAgentVersion()
	err := a.mutate(c, c.MustGet("user").(User).ID, "agent.update.request", id, func(tx *sql.Tx) error {
		var current string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT agent_version FROM servers WHERE id=? AND revoked=false FOR UPDATE`), id).Scan(&current); e != nil {
			return e
		}
		if current == version || newerAgentVersion(current, version) || legacyAgentUpdater(current) {
			return errUpdateUnavailable
		}
		var seen sql.NullTime
		var status string
		var expiry sql.NullTime
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT updater_seen_at,status,expires_at FROM agent_updates WHERE server_id=?`), id).Scan(&seen, &status, &expiry); e != nil {
			return errUpdateUnavailable
		}
		if !seen.Valid || seen.Time.Before(now.Add(-3*time.Minute)) || ((status == "pending" || status == "running") && expiry.Valid && expiry.Time.After(now)) {
			return errUpdateUnavailable
		}
		var credential string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT token_hash FROM agent_credentials WHERE server_id=? AND revoked_at IS NULL`), id).Scan(&credential); e != nil {
			return e
		}
		_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_updates SET request_id=?,credential_hash=?,status='pending',target_version=?,expires_at=?,message='' WHERE server_id=?`), requestID, credential, version, now.Add(15*time.Minute), id)
		return e
	})
	if errors.Is(err, errUpdateUnavailable) {
		bad(c, 409, "已是当前版本、已有更新任务，或更新服务不可用；旧 Agent 请先手动更新一次")
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"request_id": requestID, "version": version})
}

func (a *App) pollAgentUpdate(c *gin.Context) {
	var task any
	now := time.Now().UTC()
	err := a.agentTx(c, func(tx *sql.Tx) error {
		id := c.GetString("server_id")
		var count int
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT COUNT(*) FROM agent_updates WHERE server_id=?`), id).Scan(&count); e != nil {
			return e
		}
		if count == 0 {
			if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO agent_updates(server_id) VALUES(?)`), id); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_updates SET updater_seen_at=? WHERE server_id=?`), now, id); e != nil {
			return e
		}
		var requestID, credential, status, target string
		var expiry sql.NullTime
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT request_id,credential_hash,status,target_version,expires_at FROM agent_updates WHERE server_id=?`), id).Scan(&requestID, &credential, &status, &target, &expiry); e != nil {
			return e
		}
		if status != "pending" && status != "running" {
			return nil
		}
		// The authorized task pins its version. A later release check or a restart
		// must not invalidate an already queued update.
		_, validTarget := stableAgentVersion(target)
		if !expiry.Valid || !expiry.Time.After(now) || credential != c.GetString("agent_hash") || (!validTarget && target != AgentVersion) {
			_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_updates SET status='failed',message='Task expired, credentials changed or invalid target version' WHERE server_id=?`), id)
			return e
		}
		if status == "running" {
			return nil
		} // Never automatically replay a claimed update.
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_updates SET status='running',expires_at=? WHERE server_id=?`), now.Add(10*time.Minute), id); e != nil {
			return e
		}
		task = gin.H{"id": requestID, "version": target}
		return nil
	})
	if err != nil {
		agentFailure(c, err)
		return
	}
	ok(c, gin.H{"update": task})
}

func (a *App) finishAgentUpdate(c *gin.Context) {
	var in struct {
		ID     string `json:"id" binding:"required,uuid"`
		Status string `json:"status" binding:"required,oneof=success failed"`
	}
	if !bind(c, &in) {
		return
	}
	err := a.agentTx(c, func(tx *sql.Tx) error {
		result, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE agent_updates SET status=?,message=? WHERE server_id=? AND request_id=? AND credential_hash=? AND status='running' AND expires_at>?`), in.Status, in.Status, c.GetString("server_id"), in.ID, c.GetString("agent_hash"), time.Now().UTC())
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errInvalidAgentMessage
		}
		return a.writeAudit(c, tx, nil, "agent.update."+in.Status, c.GetString("server_id"))
	})
	if err != nil {
		agentFailure(c, err)
		return
	}
	a.hub.publish()
	ok(c, gin.H{"received": true})
}
