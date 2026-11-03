package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sshguard/internal/originurl"
	"strings"
	"time"

	"github.com/google/uuid"
)

var version = "0.3.21"

type Config struct {
	Endpoint              string `json:"endpoint"`
	AllowInsecureHTTP     bool   `json:"allow_insecure_http,omitempty"`
	Token                 string `json:"token"`
	ServerID              string `json:"server_id"`
	EnrollmentID          string `json:"enrollment_id,omitempty"`
	LogFile               string `json:"log_file,omitempty"`
	ReportIntervalSeconds int    `json:"report_interval_seconds,omitempty"`
}

var client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func validEndpoint(endpoint string, allowHTTP bool) bool {
	u, e := originurl.Parse(endpoint)
	if e != nil {
		return false
	}
	return u.Scheme == "https" || (allowHTTP && u.Scheme == "http")
}
func validToken(t string) bool { b, e := hex.DecodeString(t); return e == nil && len(b) == 32 }
func enrollmentInput(input []byte) string {
	return strings.TrimSpace(strings.TrimPrefix(string(input), "\ufeff"))
}
func request(ctx context.Context, endpoint, path, token string, data any, out any) error {
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return apiStatusError(res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(out)
	}
	_, e = io.Copy(io.Discard, io.LimitReader(res.Body, 65536))
	return e
}
func distribution() string {
	if runtime.GOOS == "windows" {
		return "Windows (" + runtime.GOARCH + ")"
	}
	if b, e := os.ReadFile("/etc/os-release"); e == nil {
		return parseDistribution(string(b))
	}
	return "Linux"
}
func parseDistribution(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "PRETTY_NAME=")), "\"'")
			if len(name) > 128 {
				name = name[:128]
			}
			return name
		}
	}
	return "Linux"
}
func enrollAgent(ctx context.Context, path, endpoint, t string, allowHTTP bool) error {
	if !validEndpoint(endpoint, allowHTTP) {
		return fmt.Errorf("endpoint must be an HTTPS origin; direct HTTP requires -allow-insecure-http")
	}
	if !validToken(t) {
		return fmt.Errorf("invalid enrollment token")
	}
	host, e := os.Hostname()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".agent-*.tmp")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	var response struct {
		Data Config `json:"data"`
	}
	if e = request(ctx, endpoint, "/api/v1/agent/enroll", "", map[string]string{"token": t, "hostname": host, "distribution": distribution(), "version": version}, &response); e != nil {
		return e
	}
	if !validToken(response.Data.Token) || response.Data.ServerID == "" || response.Data.ReportIntervalSeconds < 0 || response.Data.ReportIntervalSeconds > 600 {
		return fmt.Errorf("invalid enrollment response; generate a new command and retry")
	}
	response.Data.Endpoint = endpoint
	response.Data.AllowInsecureHTTP = allowHTTP && strings.HasPrefix(endpoint, "http://")
	// A successful registration starts fresh local collection even for the same server.
	// Keep this marker stable across restarts and binary updates.
	response.Data.EnrollmentID = uuid.NewString()
	if e = json.NewEncoder(f).Encode(response.Data); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(temp, path)
}
func loadConfig(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	if !validEndpoint(c.Endpoint, c.AllowInsecureHTTP) || !validToken(c.Token) || c.ServerID == "" || c.ReportIntervalSeconds < 0 || c.ReportIntervalSeconds > 600 {
		return c, fmt.Errorf("invalid agent config: valid origin, explicit HTTP opt-in, server_id and token required")
	}
	return c, nil
}
func checkAgent(ctx context.Context, cfg Config, endpoint string) error {
	if endpoint != "" && endpoint != cfg.Endpoint {
		return fmt.Errorf("endpoint differs from installed configuration; refusing update")
	}
	// An installer probe must not advertise the downloaded version as running
	// before the replacement service has actually started.
	return request(ctx, cfg.Endpoint, "/api/v1/agent/heartbeat", cfg.Token, map[string]string{}, nil)
}

// Run starts the Agent command.
func Run() {
	path := flag.String("config", defaultConfigPath(), "configuration path")
	stateDir := flag.String("state-dir", defaultStateDir(), "durable queue and collector cursor directory")
	endpoint := flag.String("endpoint", "", "platform origin (IP or domain)")
	allowHTTP := flag.Bool("allow-insecure-http", false, "allow unencrypted HTTP for direct deployment")
	enroll := flag.Bool("enroll", false, "read one-time enrollment token from stdin")
	showVersion := flag.Bool("version", false, "print agent version")
	check := flag.Bool("check", false, "validate config and send one heartbeat, then exit")
	poll := flag.Bool("poll-update", false, "local scheduled updater: process only a platform Agent-version update")
	serviceAction := flag.String("service", "", "Windows service setup: install or uninstall (administrator)")
	flag.Parse()
	if *showVersion {
		fmt.Printf("SSHGuard %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}
	if *serviceAction != "" {
		if e := configureService(*serviceAction, *path); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *enroll {
		input, e := io.ReadAll(io.LimitReader(os.Stdin, 256))
		if e != nil {
			log.Fatal(e)
		}
		if e = enrollAgent(context.Background(), *path, *endpoint, enrollmentInput(input), *allowHTTP); e != nil {
			log.Fatal(e)
		}
		log.Print("enrolled; start the Agent service to collect SSH logins")
		return
	}
	cfg, e := loadConfig(*path)
	if e != nil {
		log.Fatal(e)
	}
	if *check {
		if e = checkAgent(context.Background(), cfg, *endpoint); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *poll {
		if e := pollUpdate(context.Background(), cfg); e != nil {
			log.Fatal(e)
		}
		return
	}
	if e = runPlatform(func(ctx context.Context) { runAgent(ctx, cfg, *stateDir) }); e != nil {
		log.Fatal(e)
	}
}
