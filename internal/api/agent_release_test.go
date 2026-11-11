package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func releaseFixture(tag string) publishedAgentRelease {
	r := publishedAgentRelease{Tag: tag}
	for _, name := range []string{"AGENT_VERSION", "install-agent.sh", "install-agent.ps1", "uninstall-agent.sh", "uninstall-agent.ps1", "sshguard-agent-linux-amd64", "sshguard-agent-linux-amd64.sha256", "sshguard-agent-linux-arm64", "sshguard-agent-linux-arm64.sha256", "sshguard-agent-windows-amd64.exe", "sshguard-agent-windows-amd64.exe.sha256", "sshguard-agent-windows-arm64.exe", "sshguard-agent-windows-arm64.exe.sha256"} {
		r.Assets = append(r.Assets, struct {
			Name  string `json:"name"`
			Size  int64  `json:"size"`
			State string `json:"state"`
		}{name, 100, "uploaded"})
	}
	return r
}

func TestDiscoverIndependentAgentRelease(t *testing.T) {
	draft := releaseFixture("agent-v99.0.30")
	draft.Draft = true
	pre := releaseFixture("agent-v99.0.29")
	pre.Prerelease = true
	incomplete := releaseFixture("agent-v99.0.28")
	incomplete.Assets = incomplete.Assets[:12]
	response := []publishedAgentRelease{releaseFixture("v999.0.0"), draft, pre, incomplete, releaseFixture("agent-v99.0.27-rc.1"), releaseFixture("agent-v99.0.9"), releaseFixture("agent-v99.0.10")}
	status, calls := http.StatusOK, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("per_page") != "100" || r.Header.Get("User-Agent") == "" {
			t.Error("missing bounded query or user agent")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	a := &App{}
	if a.availableAgentVersion() != AgentVersion {
		t.Fatal("missing offline bootstrap version")
	}
	if err := a.refreshAgentRelease(context.Background(), server.Client(), server.URL); err != nil {
		t.Fatal(err)
	}
	if got := a.availableAgentVersion(); got != "99.0.10" {
		t.Fatalf("selected %q", got)
	}
	for i := 0; i < 100; i++ {
		_ = a.availableAgentVersion()
	}
	if calls != 1 {
		t.Fatal("cached reads triggered network")
	}
	for _, commands := range []map[string]string{installCommands("https://guard.example.com", "token", a.availableAgentVersion()), updateCommands("https://guard.example.com", a.availableAgentVersion()), uninstallCommands("https://guard.example.com", a.availableAgentVersion())} {
		for platform, command := range commands {
			if !strings.Contains(command, "/agent-v99.0.10/") {
				t.Fatal("stale download version", platform, command)
			}
		}
	}
	status = http.StatusForbidden // GitHub rate limit/network failure keeps last success.
	if err := a.refreshAgentRelease(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("ignored HTTP failure")
	}
	if a.availableAgentVersion() != "99.0.10" {
		t.Fatal("failure discarded cache")
	}
	status = http.StatusOK
	response = []publishedAgentRelease{releaseFixture("agent-v99.0.8")}
	if err := a.refreshAgentRelease(context.Background(), server.Client(), server.URL); err != nil {
		t.Fatal(err)
	}
	if a.availableAgentVersion() != "99.0.10" {
		t.Fatal("release refresh downgraded version")
	}
}

func TestAgentReleaseVersions(t *testing.T) {
	for _, invalid := range []string{"", "v0.4.1", "0.4", "0.4.1/../x", "0.4.01", "0.4.+1", "0.4.1-rc.1", "999999999999999999999.0.0"} {
		if _, ok := stableAgentVersion(invalid); ok {
			t.Fatal("accepted invalid stable version", invalid)
		}
	}
	if !newerAgentVersion("0.4.10", "0.4.9") || newerAgentVersion("0.4.1", "0.4.2") || newerAgentVersion("0.4.1", "0.4.1") || !newerAgentVersion("0.4.1", "0.4.1-rc.1") {
		t.Fatal("incorrect version comparison")
	}
}
