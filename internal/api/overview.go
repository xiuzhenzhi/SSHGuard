package api

import (
	"context"
	"fmt"
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
	slot := map[string]int{"24h": 1, "7d": 2, "14d": 3, "30d": 4}[rangeName]
	a.dashboardBuild[slot].Lock()
	defer a.dashboardBuild[slot].Unlock()
	data, valid, epoch := a.readDashboard(rangeName)
	if valid {
		ok(c, data)
		return
	}
	now := time.Now().UTC()
	buckets, e := a.loginTrend(c.Request.Context(), now, count, step)
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
	data = gin.H{"trend": buckets, "range": rangeName, "healthy_collectors": healthy, "unacknowledged_alerts": attention, "pending_deliveries": pending, "failed_deliveries": dead, "enabled_channels": channels, "recent_alerts": alerts, "as_of": now}
	a.publishDashboard(rangeName, data, epoch)
	ok(c, data)
}

// Each disjoint range uses the occurred_at index, avoiding per-event timestamp
// conversion and the temporary GROUP BY sort. UNION ALL keeps one DB round trip.
func (a *App) loginTrend(ctx context.Context, now time.Time, count int, step time.Duration) ([]trendBucket, error) {
	since := now.UTC().Truncate(step).Add(-time.Duration(count-1) * step)
	buckets := make([]trendBucket, count)
	queries := make([]string, count)
	args := make([]any, 0, count*2)
	for i := range buckets {
		start := since.Add(time.Duration(i) * step)
		end, op := start.Add(step), "<"
		if i == count-1 {
			end, op = now, "<="
		}
		buckets[i].At = start
		queries[i] = fmt.Sprintf(`SELECT %d,COALESCE(SUM(CASE WHEN outcome='success' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN outcome='failure' THEN 1 ELSE 0 END),0) FROM ssh_events WHERE occurred_at>=? AND occurred_at%s?`, i, op)
		args = append(args, start, end)
	}
	rows, err := a.db.QueryContext(ctx, a.db.Bind(strings.Join(queries, " UNION ALL ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i int
		var success, failure int64
		if err := rows.Scan(&i, &success, &failure); err != nil {
			return nil, err
		}
		buckets[i].Success, buckets[i].Failure = success, failure
	}
	return buckets, rows.Err()
}
