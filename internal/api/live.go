package api

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
)

type subscriber struct {
	user    string
	updates chan struct{}
}
type liveHub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]subscriber
	agents  map[string]*websocket.Conn
	closed  bool
}

func newLiveHub() *liveHub {
	return &liveHub{clients: map[*websocket.Conn]subscriber{}, agents: map[string]*websocket.Conn{}}
}
func (h *liveHub) publish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.clients {
		select {
		case s.updates <- struct{}{}:
		default:
		}
	}
}
func (h *liveHub) add(conn *websocket.Conn, user string) (chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, s := range h.clients {
		if s.user == user {
			count++
		}
	}
	if h.closed || len(h.clients) >= 256 || count >= 4 {
		return nil, false
	}
	ch := make(chan struct{}, 1)
	h.clients[conn] = subscriber{user, ch}
	return ch, true
}
func (h *liveHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}
func (a *App) Shutdown() {
	a.maintenance.stop()
	a.hub.mu.Lock()
	defer a.hub.mu.Unlock()
	a.hub.closed = true
	for conn := range a.hub.clients {
		conn.CloseNow()
	}
	for _, conn := range a.hub.agents {
		conn.CloseNow()
	}
}
func (a *App) live(c *gin.Context) {
	origin := a.browserOrigin(c)
	if !a.validBrowserOrigin(c) {
		bad(c, 403, "WebSocket Origin 无效")
		return
	}
	u, _ := url.Parse(origin)
	conn, e := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{OriginPatterns: []string{u.Host}, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	updates, allowed := a.hub.add(conn, c.MustGet("user").(User).ID)
	if !allowed {
		conn.Close(websocket.StatusTryAgainLater, "connection limit")
		return
	}
	defer a.hub.remove(conn)
	ctx := conn.CloseRead(c.Request.Context())
	session, _ := c.Cookie("sshguard_session")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	refresh := time.NewTicker(5 * time.Second)
	defer refresh.Stop()
	write := func() error {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return wsjson.Write(writeCtx, conn, gin.H{"type": "refresh", "at": time.Now().UTC()})
	}
	if e = write(); e != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			// Keep one buffered invalidation per browser, independent of Agent count.
			select {
			case <-updates:
				if write() != nil {
					return
				}
			default:
			}
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			var valid int
			e := a.db.Row(checkCtx, `SELECT COUNT(*) FROM sessions WHERE token_hash=? AND expires_at>?`, hash(session), time.Now().UTC()).Scan(&valid)
			if e == nil && valid == 1 {
				e = conn.Ping(checkCtx)
			}
			cancel()
			if e != nil || valid != 1 {
				conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
		}
	}
}
