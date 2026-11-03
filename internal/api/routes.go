package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
	"log"
	"net/http"
	"sshguard/internal/geoip"
	"sshguard/internal/storage"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	maintenance    databaseMaintenance
	db             *storage.DB
	cfg            Config
	dummyHash      string
	hub            *liveHub
	dashboardMu    sync.Mutex
	dashboardCache map[string]dashboardSnapshot
	countryFlights singleflight.Group
	countryEpoch   uint64
	geo            geoIPState
}

var Version = "0.3.25"
var AgentVersion = "0.3.22"

func New(db *storage.DB, cfg Config) *gin.Engine {
	return NewApp(db, cfg).Router()
}
func NewApp(db *storage.DB, cfg Config) *App {
	h, _ := bcrypt.GenerateFromPassword([]byte(token()), 12)
	return &App{db: db, cfg: cfg, dummyHash: string(h), hub: newLiveHub(), geo: geoIPState{client: geoip.NewClient()}}
}
func (a *App) Router() *gin.Engine {
	// Pass c.Request.Context() to SQL and background-capable helpers. Gin pools
	// its Context after the handler returns; driver cancellation goroutines may
	// still be observing the context even after a query has completed.
	db := a.db
	r := gin.New()
	r.ContextWithFallback = true
	r.Use(gin.Recovery())
	if err := r.SetTrustedProxies(a.cfg.TrustedProxyCIDRs); err != nil {
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path != "/api/v1/live" && c.Request.URL.Path != "/api/v1/agent/stream" {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	})
	r.GET("/api/v1/health", func(c *gin.Context) {
		if e := db.PingContext(c.Request.Context()); e != nil {
			bad(c, 503, "数据库不可用")
			return
		}
		ok(c, gin.H{"status": "ok", "database": db.Driver, "version": Version})
	})
	r.GET("/api/v1/setup/status", func(c *gin.Context) { ok(c, gin.H{"required": false}) })
	// Cookie mutations require exact Origin; CLI clients must send Origin too.
	origin := func(c *gin.Context) {
		if c.Request.Method != "GET" && !a.validBrowserOrigin(c) {
			bad(c, 403, "Origin 校验失败")
			return
		}
		c.Next()
	}
	authLimit := authThrottle()
	managementLimit := authThrottle()
	r.POST("/api/v1/auth/login", origin, authLimit, a.login)
	r.GET("/api/v1/auth/protection", a.loginProtection)
	web := r.Group("/api/v1", origin, a.session)
	web.GET("/auth/me", func(c *gin.Context) { ok(c, c.MustGet("user")) })
	web.GET("/auth/protection/settings", role("admin"), a.captchaSettings)
	web.PATCH("/auth/protection/settings", role("admin"), managementLimit, a.saveCaptchaSettings)
	web.GET("/auth/blocked-ips", role("admin"), a.blockedIPs)
	web.DELETE("/auth/blocked-ips/:id", role("admin"), a.unblockIP)
	web.POST("/auth/logout", a.logout)
	web.POST("/auth/password", managementLimit, a.changePassword)
	web.GET("/agent/update-commands", role("admin"), func(c *gin.Context) {
		origin, valid := a.agentOrigin(c)
		if !valid {
			return
		}
		ok(c, gin.H{"commands": updateCommands(origin), "version": AgentVersion, "endpoint": origin, "insecure_http": strings.HasPrefix(origin, "http://")})
	})
	web.GET("/settings/database", role("admin"), a.databaseSettings)
	web.PATCH("/settings/database", role("admin"), a.saveDatabaseSettings)
	web.POST("/settings/database/reclaim", role("admin"), a.reclaimDatabase)
	web.GET("/dashboard/summary", a.summary)
	web.GET("/agent/uninstall-commands", role("admin"), func(c *gin.Context) {
		if origin, valid := a.agentOrigin(c); valid {
			ok(c, gin.H{"commands": uninstallCommands(origin)})
		}
	})
	web.GET("/dashboard/overview", a.overview)
	web.GET("/dashboard/country-ranking", a.countryRanking)
	web.GET("/live", a.live)
	web.GET("/rules", role("admin"), a.rules)
	web.GET("/ip-allowlist", role("admin"), a.allowlist)
	web.POST("/ip-allowlist", role("admin"), a.saveAllowlist)
	web.DELETE("/ip-allowlist/:id", role("admin"), a.deleteAllowlist)
	web.POST("/rules", role("admin"), a.saveRule)
	web.PATCH("/rules/:id", role("admin"), a.saveRule)
	web.GET("/alerts", a.alerts)
	web.POST("/alerts/acknowledge", role("admin", "operator"), a.acknowledgeAlerts)
	web.POST("/alerts/:id/acknowledge", role("admin", "operator"), a.acknowledgeAlert)
	web.GET("/channels", role("admin"), a.channels)
	web.GET("/channels/:id", role("admin"), a.channel)
	web.POST("/channels", role("admin"), a.saveChannel)
	web.PATCH("/channels/:id", role("admin"), a.saveChannel)
	web.POST("/channels/:id/test", role("admin"), a.testChannel)
	web.GET("/notifications", role("admin"), a.deliveries)
	web.POST("/notifications/:id/retry", role("admin"), a.retryDelivery)
	web.GET("/servers", a.servers)
	web.GET("/settings/geoip", role("admin"), a.geoIPSettings)
	web.PATCH("/settings/geoip", role("admin"), a.saveGeoIPSettings)
	web.POST("/settings/geoip/update", role("admin"), a.updateGeoIPDatabase)
	web.POST("/settings/geoip/test", role("admin"), managementLimit, a.testGeoIP)
	web.GET("/servers/:id", a.server)
	web.POST("/servers", role("admin", "operator"), a.saveServer)
	web.PATCH("/servers/:id", role("admin", "operator"), a.saveServer)
	web.PATCH("/servers/:id/group", role("admin", "operator"), a.saveServerGroup)
	web.DELETE("/servers/:id", role("admin"), a.deleteServer)
	web.POST("/servers/:id/enrollment", role("admin"), a.enrollment)
	web.POST("/servers/:id/revoke", role("admin"), a.revoke)
	web.POST("/servers/:id/update", role("admin"), a.queueAgentUpdate)
	web.GET("/events", a.events)
	web.GET("/events/:id", a.event)
	web.GET("/audit-logs", role("admin"), func(c *gin.Context) {
		a.list(c, `SELECT a.id,u.username AS actor,a.action,a.target,a.created_at,x.source_ip,x.user_agent FROM audit_logs a LEFT JOIN users u ON a.actor_id=u.id LEFT JOIN audit_context x ON x.audit_id=a.id ORDER BY a.id DESC LIMIT ? OFFSET ?`)
	})
	web.GET("/users", role("admin"), func(c *gin.Context) {
		a.list(c, `SELECT id,username,role,created_at FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`)
	})
	web.POST("/users", role("admin"), a.createUser)
	r.POST("/api/v1/agent/enroll", a.enroll)
	agent := r.Group("/api/v1/agent", a.agentAuth)
	agent.GET("/stream", a.agentStream)
	agent.POST("/heartbeat", a.heartbeat)
	agent.POST("/update/poll", a.pollAgentUpdate)
	agent.POST("/update/result", a.finishAgentUpdate)
	agent.POST("/events/batch", a.batch)
	r.NoRoute(a.serveWeb)
	return r
}

// Login and authenticated operations use separate bounded source budgets.
func authThrottle() gin.HandlerFunc {
	type bucket struct {
		count int
		until time.Time
	}
	var mu sync.Mutex
	buckets := map[string]bucket{}
	inflight := make(chan struct{}, 8)
	return func(c *gin.Context) {
		key := "ip:" + sourceIP(c)
		if value, exists := c.Get("user"); exists {
			key = "user:" + value.(User).ID
		}
		now := time.Now()
		mu.Lock()
		for k, v := range buckets {
			if !now.Before(v.until) {
				delete(buckets, k)
			}
		}
		v, exists := buckets[key]
		allowed := (exists || len(buckets) < 4096) && v.count < 60
		if allowed {
			if !exists {
				v.until = now.Add(time.Minute)
			}
			v.count++
			buckets[key] = v
		}
		mu.Unlock()
		if !allowed {
			c.Header("Retry-After", "60")
			bad(c, 429, "请求过多，请稍后再试")
			return
		}
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }()
		default:
			c.Header("Retry-After", "1")
			bad(c, 429, "请求过多，请稍后再试")
			return
		}
		c.Next()
	}
}

func ok(c *gin.Context, v any)                   { c.JSON(200, gin.H{"data": v}) }
func bad(c *gin.Context, status int, msg string) { c.AbortWithStatusJSON(status, gin.H{"error": msg}) }
func fail(c *gin.Context, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		bad(c, 404, "记录不存在或凭证已失效")
		return
	}
	log.Printf("request failed: %T", e)
	bad(c, 500, "操作失败，请检查服务端和数据库状态")
}
func bind(c *gin.Context, v any) bool {
	if e := c.ShouldBindJSON(v); e != nil {
		bad(c, 400, "请求字段或 JSON 格式无效")
		return false
	}
	return true
}
func page(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	s, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if p < 1 {
		p = 1
	}
	if p > 100000 {
		p = 100000
	}
	if s < 1 || s > 100 {
		s = 20
	}
	return s, (p - 1) * s
}
func (a *App) list(c *gin.Context, q string, args ...any) {
	size, offset := page(c)
	args = append(args, size+1, offset)
	rows, e := a.db.List(c.Request.Context(), q, args...)
	if e != nil {
		fail(c, e)
		return
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	ok(c, gin.H{"items": rows, "has_more": more})
}
func (a *App) mutate(c *gin.Context, actor, action, target string, fn func(*sql.Tx) error) error {
	tx, e := a.db.BeginTx(c.Request.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	var who any
	if actor != "" {
		who = actor
	}
	if e = a.writeAudit(c, tx, who, action, target); e != nil {
		return e
	}
	return tx.Commit()
}
func (a *App) summary(c *gin.Context) {
	a.dashboardMu.Lock()
	defer a.dashboardMu.Unlock()
	if data, valid := a.cachedDashboard("summary"); valid {
		ok(c, data)
		return
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	online := time.Now().UTC().Unix()
	var total, on, rev, events, success int64
	if e := a.db.Row(c.Request.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN revoked=false AND `+a.freshSQL("last_seen_at", "servers.id")+` THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN revoked=true THEN 1 ELSE 0 END),0) FROM servers`, online).Scan(&total, &on, &rev); e != nil {
		fail(c, e)
		return
	}
	if e := a.db.Row(c.Request.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN outcome='success' THEN 1 ELSE 0 END),0) FROM ssh_events WHERE occurred_at>=?`, since).Scan(&events, &success); e != nil {
		fail(c, e)
		return
	}
	data := gin.H{"version": Version, "servers": total, "online": on, "offline_or_pending": total - on - rev, "revoked": rev, "events_24h": events, "success_24h": success, "failure_24h": events - success, "database": a.db.Driver}
	a.saveDashboard("summary", data)
	ok(c, data)
}
