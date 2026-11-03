package api

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sshguard/internal/originurl"
	"strconv"
	"strings"
)

type Config struct {
	WebDir, TLSCertFile, TLSKeyFile                                      string
	SetupFile, SetupToken                                                string
	SetupPending                                                         bool
	Driver, DatabaseURL, PublicURL, Listen, AdminUsername, AdminPassword string
	SecureCookie                                                         bool
	AllowInsecureHTTP                                                    bool
	EncryptionKey                                                        string
	NotificationAllowedHosts                                             []string
	TrustedProxyCIDRs                                                    []string
	EventRetentionDays, AlertRetentionDays, AuditRetentionDays           int
}

func LoadConfig() (Config, error) {
	c := Config{Driver: os.Getenv("DB_DRIVER"), DatabaseURL: os.Getenv("DATABASE_URL"), PublicURL: strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"), Listen: os.Getenv("LISTEN_ADDR"), AdminUsername: os.Getenv("ADMIN_USERNAME"), AdminPassword: os.Getenv("ADMIN_PASSWORD"), SecureCookie: true}
	c.EncryptionKey = os.Getenv("ENCRYPTION_KEY")
	c.AllowInsecureHTTP = os.Getenv("ALLOW_INSECURE_HTTP") == "true"
	c.TrustedProxyCIDRs = []string{"127.0.0.1/32", "::1/128"}
	if raw := os.Getenv("TRUSTED_PROXY_CIDRS"); raw != "" {
		c.TrustedProxyCIDRs = nil
		if raw != "none" {
			for _, value := range strings.Split(raw, ",") {
				value = strings.TrimSpace(value)
				_, network, err := net.ParseCIDR(value)
				if err != nil {
					return c, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain IP CIDR ranges")
				}
				if ones, _ := network.Mask.Size(); ones == 0 {
					return c, fmt.Errorf("TRUSTED_PROXY_CIDRS must not trust the entire Internet")
				}
				c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, value)
			}
		}
	}
	if os.Getenv("WEB_SETUP") == "true" {
		c.SetupFile = os.Getenv("SETUP_CONFIG_FILE")
		if c.SetupFile == "" {
			c.SetupFile = "/var/lib/sshguard-api/setup.json"
		}
		record, err := readSetupRecord(c.SetupFile)
		if err == nil {
			c.Driver, c.DatabaseURL, c.AdminUsername = record.Driver, record.DatabaseURL, record.AdminUsername
			c.AdminPassword = ""
		} else if os.IsNotExist(err) {
			c.SetupPending = true
			c.Driver = "sqlite"
			c.DatabaseURL = "file:" + filepath.ToSlash(filepath.Join(filepath.Dir(c.SetupFile), "sshguard.db"))
			c.SetupToken = os.Getenv("SETUP_TOKEN")
			if len(c.SetupToken) < 32 {
				return c, fmt.Errorf("SETUP_TOKEN must contain at least 32 characters")
			}
		} else {
			return c, fmt.Errorf("cannot read initialization manifest")
		}
	}
	c.WebDir = os.Getenv("WEB_DIR")
	if c.WebDir == "" {
		if executable, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(executable), "web")
			if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && info.Mode().IsRegular() {
				c.WebDir = candidate
			}
		}
	}
	c.TLSCertFile, c.TLSKeyFile = os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return c, fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be configured together")
	}
	for _, setting := range []struct {
		name     string
		target   *int
		fallback int
	}{{"EVENT_RETENTION_DAYS", &c.EventRetentionDays, 30}, {"ALERT_RETENTION_DAYS", &c.AlertRetentionDays, 90}, {"AUDIT_RETENTION_DAYS", &c.AuditRetentionDays, 180}} {
		*setting.target = setting.fallback
		if raw := os.Getenv(setting.name); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 30 || n > 3650 {
				return c, fmt.Errorf("%s must be between 30 and 3650", setting.name)
			}
			*setting.target = n
		}
	}
	if key, e := hex.DecodeString(c.EncryptionKey); e != nil || len(key) != 32 {
		return c, fmt.Errorf("ENCRYPTION_KEY must be 64 hex characters; preserve it with database backups")
	}
	if hosts := os.Getenv("NOTIFICATION_ALLOWED_HOSTS"); hosts != "" {
		c.NotificationAllowedHosts = strings.Split(hosts, ",")
	}
	if c.Driver == "" {
		c.Driver = "postgres"
	}
	if c.Driver != "postgres" && c.Driver != "mysql" && c.Driver != "sqlite" {
		return c, fmt.Errorf("DB_DRIVER must be postgres, mysql or sqlite")
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.AdminUsername == "" {
		c.AdminUsername = "admin"
	}
	if c.PublicURL == "" {
		c.SecureCookie = !c.AllowInsecureHTTP || c.TLSCertFile != ""
		if c.DatabaseURL == "" {
			return c, fmt.Errorf("DATABASE_URL required")
		}
		return c, nil
	}
	u, e := originurl.Parse(c.PublicURL)
	if e != nil {
		return c, fmt.Errorf("PUBLIC_URL must be an origin, e.g. https://guard.example.com")
	}
	c.PublicURL = originurl.Canonical(u)
	if u.Scheme != "https" {
		if u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && !c.AllowInsecureHTTP) {
			return c, fmt.Errorf("public deployments require HTTPS")
		}
		c.SecureCookie = false
	}
	if c.TLSCertFile != "" && u.Scheme != "https" {
		return c, fmt.Errorf("built-in TLS requires an HTTPS PUBLIC_URL")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL required")
	}
	return c, nil
}
