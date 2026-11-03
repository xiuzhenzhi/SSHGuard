package api

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type trendBucket struct {
	At      time.Time `json:"at"`
	Success int64     `json:"success"`
	Failure int64     `json:"failure"`
}

func (a *App) overview(c *gin.Context) {
	rangeName := c.DefaultQuery("range", "24h")
	count, step := 24, time.Hour
	switch rangeName {
	case "24h":
	case "7d":
		count, step = 7, 24*time.Hour
	case "14d":
		count, step = 14, 24*time.Hour
	case "30d":
		count, step = 30, 24*time.Hour
	default:
		bad(c, 400, "不支持的时间范围")
		return
	}
	a.dashboardMu.Lock()
	defer a.dashboardMu.Unlock()
	if data, valid := a.cachedDashboard(rangeName); valid {
		ok(c, data)
		return
	}
	now := time.Now().UTC()
	since := now.Truncate(step).Add(-time.Duration(count-1) * step)
	buckets := make([]trendBucket, count)
	for i := range buckets {
		buckets[i].At = since.Add(time.Duration(i) * step)
	}
	expression := `FLOOR(EXTRACT(EPOCH FROM occurred_at)/3600)`
	if a.db.Driver == "mysql" {
		expression = `FLOOR(UNIX_TIMESTAMP(occurred_at)/3600)`
	}
	if a.db.Driver == "sqlite" {
		expression = `CAST(strftime('%s',substr(occurred_at,1,19)) AS INTEGER)/3600`
	}
	if step == 24*time.Hour {
		expression = strings.ReplaceAll(expression, "3600", "86400")
	}
	rows, e := a.db.QueryContext(c.Request.Context(), a.db.Bind(`SELECT `+expression+`,SUM(CASE WHEN outcome='success' THEN 1 ELSE 0 END),SUM(CASE WHEN outcome='failure' THEN 1 ELSE 0 END) FROM ssh_events WHERE occurred_at>=? AND occurred_at<=? GROUP BY `+expression+` ORDER BY 1`), since, now)
	if e != nil {
		fail(c, e)
		return
	}
	for rows.Next() {
		var hour, success, failure int64
		if e = rows.Scan(&hour, &success, &failure); e != nil {
			rows.Close()
			fail(c, e)
			return
		}
		i := hour - since.Unix()/int64(step/time.Second)
		if i >= 0 && i < int64(count) {
			buckets[i].Success = success
			buckets[i].Failure = failure
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		fail(c, e)
		return
	}
	var healthy, attention, pending, dead, channels int64
	if e = a.db.Row(c.Request.Context(), `SELECT COUNT(*) FROM agent_health h JOIN servers s ON s.id=h.server_id WHERE s.revoked=false AND `+a.freshSQL("s.last_seen_at", "s.id")+` AND h.collector_state='healthy' AND `+a.freshSQL("h.updated_at", "s.id")+` AND `+a.freshSQL("h.last_poll_at", "s.id")+``, now.Unix(), now.Unix(), now.Unix()).Scan(&healthy); e != nil {
		fail(c, e)
		return
	}
	if e = a.db.Row(c.Request.Context(), `SELECT COUNT(*) FROM alerts WHERE acknowledged_at IS NULL AND resolved_at IS NULL`).Scan(&attention); e != nil {
		fail(c, e)
		return
	}
	if e = a.db.Row(c.Request.Context(), `SELECT COALESCE(SUM(CASE WHEN status='pending' OR status='sending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='dead' THEN 1 ELSE 0 END),0) FROM notification_jobs`).Scan(&pending, &dead); e != nil {
		fail(c, e)
		return
	}
	if e = a.db.Row(c.Request.Context(), `SELECT COUNT(*) FROM notification_channels WHERE enabled=true`).Scan(&channels); e != nil {
		fail(c, e)
		return
	}
	alerts, e := a.db.List(c.Request.Context(), `SELECT a.id,a.title,a.severity,a.created_at,a.acknowledged_at,a.resolved_at,s.name AS server_name FROM alerts a LEFT JOIN servers s ON s.id=a.server_id ORDER BY a.created_at DESC LIMIT 5`)
	if e != nil {
		fail(c, e)
		return
	}
	data := gin.H{"trend": buckets, "range": rangeName, "healthy_collectors": healthy, "unacknowledged_alerts": attention, "pending_deliveries": pending, "failed_deliveries": dead, "enabled_channels": channels, "recent_alerts": alerts, "as_of": now}
	a.saveDashboard(rangeName, data)
	ok(c, data)
}
