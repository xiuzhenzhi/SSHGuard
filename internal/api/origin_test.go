package api

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAutomaticProxyOrigin(t *testing.T) {
	for _, tc := range []struct{ name, peer, proto, want string }{
		{"local HTTPS", "127.0.0.1:5000", "https", "https://guard.example.com:8443"},
		{"IPv6 proxy", "[::1]:5000", "https", "https://guard.example.com:8443"},
		{"untrusted peer", "192.0.2.1:5000", "https", "http://guard.example.com:8443"},
		{"ambiguous protocol", "127.0.0.1:5000", "https,http", "http://guard.example.com:8443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "http://guard.example.com:8443/api/v1/auth/login", nil)
			c.Request.RemoteAddr = tc.peer
			c.Request.Header.Set("X-Forwarded-Proto", tc.proto)
			c.Request.Header.Set("X-Forwarded-Host", "attacker.example")
			c.Request.Header.Set("Origin", "https://attacker.example")
			a := &App{cfg: Config{AllowInsecureHTTP: true, TrustedProxyCIDRs: []string{"127.0.0.1/32", "::1/128"}}}
			if got := a.browserOrigin(c); got != tc.want {
				t.Fatalf("origin = %s, want %s", got, tc.want)
			}
			if got, valid := a.agentOrigin(c); !valid || got != tc.want {
				t.Fatalf("agent origin = %s (%v)", got, valid)
			}
			if a.secureCookie(c) != strings.HasPrefix(tc.want, "https://") {
				t.Fatal("cookie security did not follow external protocol")
			}
		})
	}
}

func TestDirectOriginWithoutDomain(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("ab", 32))
	t.Setenv("DATABASE_URL", "file:test.db")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("PUBLIC_URL", "")
	t.Setenv("TLS_CERT_FILE", "")
	t.Setenv("TLS_KEY_FILE", "")
	t.Setenv("ALLOW_INSECURE_HTTP", "true")
	cfg, err := LoadConfig()
	if err != nil || cfg.SecureCookie {
		t.Fatalf("direct configuration: %v", err)
	}
	a := &App{cfg: cfg}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "http://192.0.2.10:8080/api/v1/auth/login", nil)
	c.Request.Header.Set("X-Forwarded-Host", "attacker.example")
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	if a.browserOrigin(c) != "http://192.0.2.10:8080" {
		t.Fatal("untrusted proxy headers changed origin")
	}
	for _, origin := range []string{"", "null", "https://attacker.example", "http://192.0.2.10:8081"} {
		if origin == a.browserOrigin(c) {
			t.Fatal("cross origin accepted")
		}
	}
	a.cfg.PublicURL = "https://guard.example.com"
	if a.browserOrigin(c) != a.cfg.PublicURL {
		t.Fatal("configured reverse proxy origin ignored")
	}
	t.Setenv("ALLOW_INSECURE_HTTP", "")
	if secure, err := LoadConfig(); err != nil || !secure.SecureCookie {
		t.Fatal("automatic HTTPS configuration must use secure cookies", err)
	}
}

func TestAgentOriginDirectAndProxyModes(t *testing.T) {
	for _, test := range []struct {
		name, request, configured, want string
		allowHTTP                       bool
	}{
		{"direct IPv4", "http://192.0.2.10:8080", "", "http://192.0.2.10:8080", true},
		{"direct IPv6", "http://[2001:db8::1]:8080", "", "http://[2001:db8::1]:8080", true},
		{"HTTP disabled", "http://192.0.2.10:8080", "", "", false},
		{"configured HTTPS", "http://127.0.0.1:8080", "https://guard.example.com", "https://guard.example.com", false},
		{"configured HTTP", "http://127.0.0.1:8080", "http://192.0.2.10:8081", "http://192.0.2.10:8081", true},
		{"bad port", "http://192.0.2.10:65536", "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", test.request+"/api/v1/agent/update-commands", nil)
			c.Request.Header.Set("X-Forwarded-Host", "attacker.example")
			c.Request.Header.Set("X-Forwarded-Proto", "https")
			a := &App{cfg: Config{PublicURL: test.configured, AllowInsecureHTTP: test.allowHTTP}}
			got, valid := a.agentOrigin(c)
			if got != test.want || valid != (test.want != "") {
				t.Fatalf("origin %q, valid %v", got, valid)
			}
			if !valid && w.Code != 409 {
				t.Fatal("invalid origin not rejected")
			}
		})
	}
}

func TestConfiguredOriginMatchesBrowserSerialization(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("ab", 32))
	t.Setenv("DATABASE_URL", "file:test.db")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("TLS_CERT_FILE", "")
	t.Setenv("TLS_KEY_FILE", "")
	t.Setenv("WEB_SETUP", "")
	t.Setenv("PUBLIC_URL", "https://GUARD.example.com:443/")
	cfg, err := LoadConfig()
	if err != nil || cfg.PublicURL != "https://guard.example.com" || !cfg.SecureCookie {
		t.Fatal("browser origin mismatch", err)
	}
	for _, raw := range []string{"https://guard.example.com:", "https://guard.example.com:65536", "https://guard.example.com?", "https://guard.example.com#"} {
		t.Setenv("PUBLIC_URL", raw)
		if _, err := LoadConfig(); err == nil {
			t.Errorf("invalid PUBLIC_URL accepted: %s", raw)
		}
	}
}
