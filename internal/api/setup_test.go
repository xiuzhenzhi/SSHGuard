package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshguard/internal/storage"
)

func TestSetupAuthorizationAndCompletion(t *testing.T) {
	cfg := Config{SetupFile: filepath.Join(t.TempDir(), "setup.json"), SetupToken: strings.Repeat("a", 64), PublicURL: "https://guard.example.com"}
	setup := NewSetup(cfg)
	router := setup.Router()
	body := map[string]any{"driver": "sqlite", "username": "admin", "password": "test-password-12345"}
	request := func(path, token, origin string) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Setup-Token", token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, test := range []struct{ token, origin string }{{"", cfg.PublicURL}, {"wrong", cfg.PublicURL}, {cfg.SetupToken, "https://evil.example"}, {cfg.SetupToken, ""}} {
		if w := request("/api/v1/setup/complete", test.token, test.origin); w.Code != 403 {
			t.Fatalf("unauthorized setup: %d %s", w.Code, w.Body)
		}
	}
	if _, err := os.Stat(cfg.SetupFile); !os.IsNotExist(err) {
		t.Fatal("unauthorized initialization wrote config")
	}
	if w := request("/api/v1/auth/login", cfg.SetupToken, cfg.PublicURL); w.Code != 404 {
		t.Fatalf("login exposed before initialization: %d", w.Code)
	}
	body["database_url"] = "file:/tmp/arbitrary.db"
	if w := request("/api/v1/setup/test", cfg.SetupToken, cfg.PublicURL); w.Code != 400 {
		t.Fatal("unknown field accepted")
	}
	delete(body, "database_url")
	for _, action := range []string{"test", "complete"} {
		if w := request("/api/v1/setup/"+action, cfg.SetupToken, cfg.PublicURL); w.Code != 200 {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body)
		} else if strings.Contains(w.Body.String(), body["password"].(string)) {
			t.Fatal("password leak")
		}
	}
	if w := request("/api/v1/setup/complete", cfg.SetupToken, cfg.PublicURL); w.Code != 409 {
		t.Fatalf("reinitialization accepted: %d", w.Code)
	}
	record, err := readSetupRecord(cfg.SetupFile)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), record.Driver, record.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.Row(context.Background(), "SELECT COUNT(*) FROM users WHERE role='admin'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("admins %d: %v", count, err)
	}
	if err = Bootstrap(context.Background(), db, Config{}); err != nil {
		t.Fatalf("existing administrator not preserved: %v", err)
	}
	t.Setenv("WEB_SETUP", "true")
	t.Setenv("SETUP_CONFIG_FILE", cfg.SetupFile)
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("a", 64))
	t.Setenv("PUBLIC_URL", cfg.PublicURL)
	loaded, err := LoadConfig()
	if err != nil || loaded.SetupPending || loaded.DatabaseURL != record.DatabaseURL {
		t.Fatalf("reload: %+v %v", loaded, err)
	}
}

func TestSetupBehindProxyWithoutConfiguredDomain(t *testing.T) {
	cfg := Config{SetupFile: filepath.Join(t.TempDir(), "setup.json"), SetupToken: strings.Repeat("b", 64), TrustedProxyCIDRs: []string{"127.0.0.1/32"}}
	router := NewSetup(cfg).Router()
	for _, tc := range []struct {
		peer, origin string
		status       int
	}{
		{"127.0.0.1:5000", "https://evil.example", 403},
		{"192.0.2.1:5000", "http://guard.example:8443", 403},
		{"192.0.2.1:5000", "https://guard.example:8443", 403},
		{"127.0.0.1:5000", "https://guard.example:8443", 200},
	} {
		req := httptest.NewRequest("POST", "http://guard.example:8443/api/v1/setup/complete", strings.NewReader(`{"driver":"sqlite","username":"admin","password":"test-password-12345"}`))
		req.RemoteAddr = tc.peer
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Setup-Token", cfg.SetupToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("proxy setup: got %d, want %d: %s", w.Code, tc.status, w.Body)
		}
	}
}

func TestSetupRejectsExistingDatabase(t *testing.T) {
	cfg := Config{SetupFile: filepath.Join(t.TempDir(), "setup.json")}
	record, err := (setupInput{Driver: "sqlite"}).connection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), record.Driver, record.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), "CREATE TABLE keep_me (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if opened, err := NewSetup(cfg).open(context.Background(), record); err == nil {
		opened.Close()
		t.Fatal("existing database accepted")
	}
}

func TestSetupDefaultsToSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	record, err := (setupInput{Username: "admin"}).connection(Config{SetupFile: path})
	if err != nil || record.Driver != "sqlite" || record.DatabaseURL != "file:"+filepath.ToSlash(filepath.Join(filepath.Dir(path), "sshguard.db")) {
		t.Fatal("default storage requires configuration", err)
	}
}

func TestSetupRejectsTrailingJSON(t *testing.T) {
	cfg := Config{PublicURL: "https://guard.example.com", SetupToken: strings.Repeat("a", 64)}
	router := NewSetup(cfg).Router()
	for _, body := range []string{`{} {}`, `{} garbage`, `{} null`, `null`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/test", strings.NewReader(body))
		req.Header.Set("Origin", cfg.PublicURL)
		req.Header.Set("X-Setup-Token", cfg.SetupToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("trailing data accepted: %s (%d)", body, w.Code)
		}
	}
}
