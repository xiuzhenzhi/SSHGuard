package api

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (a *App) acknowledgeAlerts(c *gin.Context) {
	var in struct {
		IDs []string `json:"ids" binding:"max=1000"`
		All bool     `json:"all"`
	}
	if !bind(c, &in) {
		return
	}
	if in.All == (len(in.IDs) > 0) {
		bad(c, 400, "请选择告警，或指定确认全部待处理告警")
		return
	}
	unique := map[string]bool{}
	for _, id := range in.IDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			bad(c, 400, "告警 ID 无效")
			return
		}
		unique[parsed.String()] = true
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	clause := ""
	target := "all-pending"
	if !in.All {
		clause = ` AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
		target = fmt.Sprintf("selected:%d:%s", len(ids), hash(strings.Join(ids, ",")))
	}
	var count int64
	actor := c.MustGet("user").(User).ID
	err := a.mutate(c, actor, "alert.acknowledge.batch", target, func(tx *sql.Tx) error {
		if !in.All {
			// Lock in stable order and validate the entire selection before writing.
			rows, err := tx.QueryContext(c.Request.Context(), a.db.Bind(`SELECT id FROM alerts WHERE 1=1`+clause+` ORDER BY id FOR UPDATE`), args...)
			if err != nil {
				return err
			}
			found := 0
			for rows.Next() {
				found++
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if found != len(ids) {
				return sql.ErrNoRows
			}
		}
		values := append([]any{actor, time.Now().UTC()}, args...)
		result, err := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE alerts SET acknowledged_by=?,acknowledged_at=? WHERE acknowledged_at IS NULL AND resolved_at IS NULL`+clause), values...)
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		return err
	})
	if err != nil {
		fail(c, err)
		return
	}
	a.dashboardMu.Lock()
	a.dashboardEpoch++
	delete(a.dashboardCache, "24h")
	delete(a.dashboardCache, "7d")
	delete(a.dashboardCache, "14d")
	delete(a.dashboardCache, "30d")
	a.dashboardMu.Unlock()
	a.hub.publish()
	ok(c, gin.H{"acknowledged": count})
}
