package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const agentReleaseAPI = "https://api.github.com/repos/xiuzhenzhi/SSHGuard/releases"

type agentReleaseState struct {
	mu      sync.RWMutex
	version string
}

func stableAgentVersion(value string) ([3]uint64, bool) {
	var numbers [3]uint64
	parts := strings.Split(value, ".")
	if len(parts) != 3 || len(value) > 32 {
		return numbers, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return numbers, false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return numbers, false
			}
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return numbers, false
		}
		numbers[i] = n
	}
	return numbers, true
}

func newerAgentVersion(candidate, current string) bool {
	next, ok := stableAgentVersion(candidate)
	if !ok {
		return false
	}
	old, ok := stableAgentVersion(strings.SplitN(current, "-", 2)[0])
	if !ok {
		return false
	}
	for i := range next {
		if next[i] != old[i] {
			return next[i] > old[i]
		}
	}
	return strings.Contains(current, "-")
}

func (a *App) availableAgentVersion() string {
	a.agentRelease.mu.RLock()
	defer a.agentRelease.mu.RUnlock()
	if a.agentRelease.version != "" {
		return a.agentRelease.version
	}
	return AgentVersion
}

type publishedAgentRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		State string `json:"state"`
	} `json:"assets"`
}

func (r publishedAgentRelease) version() string {
	if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "agent-v") {
		return ""
	}
	version := strings.TrimPrefix(r.Tag, "agent-v")
	if _, ok := stableAgentVersion(version); !ok {
		return ""
	}
	names := map[string]bool{}
	for _, asset := range r.Assets {
		if asset.Size > 0 && asset.State == "uploaded" {
			names[asset.Name] = true
		}
	}
	required := []string{"AGENT_VERSION", "install-agent.sh", "install-agent.ps1", "uninstall-agent.sh", "uninstall-agent.ps1"}
	for _, os := range []string{"linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := "sshguard-agent-" + os + "-" + arch
			if os == "windows" {
				name += ".exe"
			}
			required = append(required, name, name+".sha256")
		}
	}
	for _, name := range required {
		if !names[name] {
			return ""
		}
	}
	return version
}

// Only the fixed repository's complete stable Agent releases are considered.
// Asset URLs from the response are never executed or used to generate commands.
func (a *App) refreshAgentRelease(ctx context.Context, client *http.Client, endpoint string) error {
	best := a.availableAgentVersion()
	for page := 1; page <= 3; page++ {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s?per_page=100&page=%d", endpoint, page), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "SSHGuard/"+Version)
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		response.Body.Close()
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK || len(raw) > 4<<20 {
			return fmt.Errorf("Agent release metadata unavailable: HTTP %d", response.StatusCode)
		}
		var releases []publishedAgentRelease
		if err = json.Unmarshal(raw, &releases); err != nil {
			return err
		}
		for _, release := range releases {
			if next := release.version(); newerAgentVersion(next, best) {
				best = next
			}
		}
		if len(releases) < 100 {
			break
		}
	}
	a.agentRelease.mu.Lock()
	changed := best != a.agentRelease.version
	a.agentRelease.version = best
	a.agentRelease.mu.Unlock()
	if changed && a.hub != nil {
		a.hub.publish()
	}
	return nil
}

func (a *App) agentReleaseLoop(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := a.refreshAgentRelease(check, client, agentReleaseAPI)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Print("Agent release check unavailable; retaining last known version")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
