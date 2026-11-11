package agent

import (
	"runtime"
	"strings"
	"testing"
)

func TestFixedUpdateCommand(t *testing.T) {
	cfg := Config{Endpoint: "https://guard.example.com"}
	task := updateTask{ID: "12345678-1234-1234-1234-123456789abc", Version: "0.3.2"}
	executable, args, err := updateCommand(cfg, task)
	if err != nil || executable == "" {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "update-agent.") {
		t.Fatal("not a fixed local updater")
	}
	if strings.Contains(strings.ToLower(strings.Join(args, " ")), "bypass") {
		t.Fatal("updater overrides the administrator's execution policy")
	}
	if runtime.GOOS == "windows" && !strings.Contains(strings.Join(args, " "), "-ExecutionPolicy RemoteSigned -File") {
		t.Fatal("Windows updater needs process-scoped script policy")
	}
	for _, invalid := range []string{";whoami", "$(id)", "https://evil.test/payload", "../payload", "0.3.2\ncommand", "1.2.3-" + strings.Repeat("a", 32)} {
		task.Version = invalid
		if _, _, err := updateCommand(cfg, task); err == nil {
			t.Fatal("accepted invalid version", invalid)
		}
	}
	task.Version = "0.3.2"
	for _, invalid := range []string{strings.Repeat("-", 36), strings.Repeat("a", 36), "12345678-1234-1234-1234-123456789ab!"} {
		task.ID = invalid
		if _, _, err := updateCommand(cfg, task); err == nil {
			t.Fatal("accepted malformed update task ID", invalid)
		}
	}
	task.ID = "12345678-1234-1234-1234-123456789abc"
	cfg.Endpoint = "http://guard.example.com"
	if _, _, err := updateCommand(cfg, task); err == nil {
		t.Fatal("HTTP update allowed")
	}
	cfg.AllowInsecureHTTP = true
	_, args, err = updateCommand(cfg, task)
	if err != nil || !strings.Contains(strings.ToLower(strings.Join(args, " ")), "insecure") {
		t.Fatal("direct HTTP updater lost explicit opt-in", err)
	}
}
