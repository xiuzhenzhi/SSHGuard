package api

import (
	"net"
	"sshguard/internal/originurl"
	"strings"

	"github.com/gin-gonic/gin"
)

// Preserve the browser Host. Only a configured trusted peer may supply the
// external scheme; forwarded hosts and browser Origin never select the origin.
func (a *App) browserOrigin(c *gin.Context) string {
	if a.cfg.PublicURL != "" {
		return a.cfg.PublicURL
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else if a.trustedProxy(c) {
		if proto := c.GetHeader("X-Forwarded-Proto"); proto == "https" || proto == "http" {
			scheme = proto
		}
	}
	u, err := originurl.Parse(scheme + "://" + c.Request.Host)
	if err != nil {
		return "invalid-origin"
	}
	return originurl.Canonical(u)
}

func (a *App) trustedProxy(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, cidr := range a.cfg.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *App) secureCookie(c *gin.Context) bool {
	return a.cfg.SecureCookie || strings.HasPrefix(a.browserOrigin(c), "https://")
}

func (a *App) validBrowserOrigin(c *gin.Context) bool {
	origin := a.browserOrigin(c)
	u, err := originurl.Parse(origin)
	if err != nil || c.GetHeader("Origin") != origin {
		return false
	}
	// Automatic HTTPS-only mode must reject plaintext setup/login as well as
	// Agent commands. A Secure cookie alone does not protect submitted passwords.
	return a.cfg.PublicURL != "" || u.Scheme == "https" || a.cfg.AllowInsecureHTTP
}

// Use the same external origin for browser requests and generated Agent commands.
func (a *App) agentOrigin(c *gin.Context) (string, bool) {
	origin := a.browserOrigin(c)
	u, err := originurl.Parse(origin)
	if err != nil {
		bad(c, 409, "主控访问地址无效，请使用服务器 IP 和端口重新打开控制台")
		return "", false
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !a.cfg.AllowInsecureHTTP) {
		bad(c, 409, "请启用 HTTPS，或在主控配置中显式启用 ALLOW_INSECURE_HTTP")
		return "", false
	}
	return origin, true
}
