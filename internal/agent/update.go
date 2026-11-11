package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func validUpdateVersion(version string) bool {
	if len(version) == 0 || len(version) > 32 {
		return false
	}
	core, suffix, prerelease := strings.Cut(version, "-")
	dots, digits := 0, 0
	for i := 0; i < len(core); i++ {
		if core[i] >= '0' && core[i] <= '9' {
			digits++
		} else if core[i] == '.' && digits > 0 {
			dots++
			digits = 0
		} else {
			return false
		}
	}
	if dots != 2 || digits == 0 || prerelease && suffix == "" {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if !asciiLetter(c) && !(c >= '0' && c <= '9') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

func validUpdateID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= 'a' && c <= 'f') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

type updateTask struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func updateCommand(cfg Config, task updateTask) (string, []string, error) {
	if !validEndpoint(cfg.Endpoint, cfg.AllowInsecureHTTP) || !validUpdateID(task.ID) || !validUpdateVersion(task.Version) {
		return "", nil, fmt.Errorf("invalid fixed update request")
	}
	if runtime.GOOS == "windows" {
		args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-File", filepath.Join(os.Getenv("ProgramData"), "SSHGuard", "Agent", "sshguard", "update-agent.ps1"), "-Endpoint", cfg.Endpoint, "-Update"}
		if cfg.AllowInsecureHTTP {
			args = append(args, "-AllowInsecureHttp")
		}
		return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), args, nil
	}
	args := []string{"/opt/SSHGuard/Agent/sshguard/update-agent.sh", cfg.Endpoint, "--update"}
	if cfg.AllowInsecureHTTP {
		args = append(args, "--allow-insecure-http")
	}
	return "/bin/sh", args, nil
}

// Invoked by a privileged local scheduler, never by the collector. The platform
// can only request the fixed locally-installed updater; it cannot supply code.
func pollUpdate(ctx context.Context, cfg Config) error {
	var response struct {
		Data struct {
			Update *updateTask `json:"update"`
		} `json:"data"`
	}
	if e := request(ctx, cfg.Endpoint, "/api/v1/agent/update/poll", cfg.Token, struct{}{}, &response); e != nil {
		return e
	}
	task := response.Data.Update
	if task == nil {
		return nil
	}
	exe, args, e := updateCommand(cfg, *task)
	if e != nil {
		return e
	}
	runCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	command := exec.CommandContext(runCtx, exe, args...)
	command.Env = append(os.Environ(), "SSHGUARD_EXPECTED_VERSION="+task.Version)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	e = command.Run()
	status := "success"
	if e != nil {
		status = "failed"
	}
	reportCtx, done := context.WithTimeout(context.Background(), 45*time.Second)
	defer done()
	reportErr := request(reportCtx, cfg.Endpoint, "/api/v1/agent/update/result", cfg.Token, map[string]string{"id": task.ID, "status": status}, nil)
	if e != nil {
		return fmt.Errorf("fixed Agent update failed; inspect updater logs")
	}
	return reportErr
}
