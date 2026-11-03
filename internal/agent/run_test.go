package agent

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentAndReplacement(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	credential := strings.Repeat("b", 64)
	responseInterval := 600
	fail := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(401)
			return
		}
		var body map[string]string
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["token"] != strings.Repeat("a", 64) || body["distribution"] == "" || body["version"] != version {
			t.Errorf("missing enrollment metadata")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": Config{Token: credential, ServerID: "test-server", ReportIntervalSeconds: responseInterval}})
	}))
	defer server.Close()
	client = server.Client()
	client.Timeout = time.Second
	path := filepath.Join(t.TempDir(), "config", "agent.json")
	state, err := openSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.db.Close()
	previousEnrollment := ""
	for _, value := range []string{strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		credential = value
		if e := enrollAgent(context.Background(), path, server.URL, strings.Repeat("a", 64), false); e != nil {
			t.Fatal(e)
		}
		cfg, e := loadConfig(path)
		if e != nil || cfg.Token != credential || cfg.Endpoint != server.URL || cfg.ReportIntervalSeconds != 600 {
			t.Fatal("config replacement failed", e)
		}
		if cfg.EnrollmentID == "" || cfg.EnrollmentID == previousEnrollment {
			t.Fatal("successful reenrollment did not create a new state generation")
		}
		previousEnrollment = cfg.EnrollmentID
		if e = state.bindIdentity(cfg); e != nil {
			t.Fatal(e)
		}
		if q, _ := state.counts(); q != 0 {
			t.Fatal("same-server reenrollment retained the old queue")
		}
		if e = state.append([]SSHEvent{{ID: "pending-event"}}, cursor{Position: "pending"}); e != nil {
			t.Fatal(e)
		}
	}
	before, _ := os.ReadFile(path)
	responseInterval = 601
	if e := enrollAgent(context.Background(), path, server.URL, strings.Repeat("a", 64), false); e == nil {
		t.Fatal("accepted an unsupported enrollment interval")
	}
	if current, _ := os.ReadFile(path); string(current) != string(before) {
		t.Fatal("invalid enrollment interval destroyed prior config")
	}
	fail = true
	if e := enrollAgent(context.Background(), path, server.URL, strings.Repeat("a", 64), false); e == nil {
		t.Fatal("accepted rejected enrollment")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed enrollment destroyed prior config")
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = state.bindIdentity(cfg); err != nil {
		t.Fatal(err)
	}
	if q, _ := state.counts(); q != 1 {
		t.Fatal("failed enrollment cleared the existing queue")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".agent-*.tmp"))
	if len(files) != 0 {
		t.Fatal("temporary credential files leaked")
	}
}
func TestRejectUntrustedTLS(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	client = &http.Client{Timeout: time.Second}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("request reached untrusted server") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	if e := request(context.Background(), server.URL, "/api/v1/agent/heartbeat", strings.Repeat("a", 64), nil, nil); e == nil {
		t.Fatal("TLS certificate verification bypassed")
	}
}
func TestHeartbeatStopsWithContext(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	started := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("wrong authorization")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	client = server.Client()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		transport := &transport{cfg: Config{Endpoint: server.URL, Token: strings.Repeat("a", 64)}, nextDial: time.Now().Add(time.Hour)}
		defer transport.close()
		_ = transport.send(ctx, "heartbeat", map[string]string{"version": version}, nil)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat not sent")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("service stop did not cancel in-flight HTTP")
	}
}
func TestPlatformsAndValidation(t *testing.T) {
	for _, name := range []string{"Ubuntu 24.04.3 LTS", "Debian GNU/Linux 13 (trixie)", "Alpine Linux v3.22"} {
		if got := parseDistribution("ID=test\nPRETTY_NAME=\"" + name + "\"\n"); got != name {
			t.Fatalf("got %q", got)
		}
	}
	for _, origin := range []string{"http://example.com", "https://example.com/path", "https://user:pass@example.com", "https://example.com?query=1"} {
		if validEndpoint(origin, false) {
			t.Fatalf("accepted %s", origin)
		}
	}
	if !validEndpoint("https://guard.example.com", false) || validToken(strings.Repeat("z", 64)) {
		t.Fatal("invalid origin/token validation")
	}
}

func TestDirectHTTPEnrollmentRequiresExplicitOptIn(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": Config{ServerID: "direct-server", Token: strings.Repeat("b", 64)}})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := enrollAgent(context.Background(), path, server.URL, strings.Repeat("a", 64), false); err == nil || calls != 0 {
		t.Fatal("HTTP sent credentials without opt-in")
	}
	if err := enrollAgent(context.Background(), path, server.URL, strings.Repeat("a", 64), true); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil || !cfg.AllowInsecureHTTP || cfg.Endpoint != server.URL || cfg.ServerID != "direct-server" {
		t.Fatal("HTTP opt-in did not survive configuration reload", err)
	}
	for _, origin := range []string{"http://192.0.2.10:8080", "http://[2001:db8::1]:8080", "https://192.0.2.10:8080"} {
		if !validEndpoint(origin, true) {
			t.Errorf("IP origin rejected: %s", origin)
		}
	}
	for _, origin := range []string{"http://192.0.2.10:0", "http://192.0.2.10:65536", "ftp://192.0.2.10", "http://user:pass@192.0.2.10", "http://192.0.2.10/path", "http://192.0.2.10?"} {
		if validEndpoint(origin, true) {
			t.Errorf("invalid HTTP origin accepted: %s", origin)
		}
	}
}
func TestEnrollmentInputEncoding(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, input := range []string{token, token + "\r\n", "\ufeff" + token + "\r\n"} {
		if got := enrollmentInput([]byte(input)); got != token || !validToken(got) {
			t.Fatal("stdin token encoding rejected")
		}
	}
	if validToken(enrollmentInput([]byte("\ufeffbad"))) {
		t.Fatal("invalid token accepted")
	}
}
func TestCheckPreservesCredentialsAndRejectsOtherOrigin(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/agent/heartbeat" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("unexpected request")
		}
		var body map[string]string
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || body["version"] != "" {
			t.Error("probe advertised a version that is not running")
		}
		w.Write([]byte(`{"data":{"received":true}}`))
	}))
	defer server.Close()
	client = server.Client()
	cfg := Config{Endpoint: server.URL, Token: strings.Repeat("a", 64), ServerID: "same-server"}
	if e := checkAgent(context.Background(), cfg, "https://other.example.com"); e == nil || calls != 0 {
		t.Fatal("sent credential to another origin")
	}
	if e := checkAgent(context.Background(), cfg, server.URL); e != nil || calls != 1 {
		t.Fatal(e)
	}
}
