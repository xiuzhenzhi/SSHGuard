package api

import (
	"strings"
	"testing"
)

func TestInstallCommandsPlatforms(t *testing.T) {
	token := strings.Repeat("a", 64)
	commands := installCommands("https://guard.example.com", token)
	for _, os := range []string{"windows", "linux", "ubuntu", "debian", "alpine"} {
		if !strings.Contains(commands[os], token) {
			t.Fatalf("%s missing enrollment", os)
		}
	}
	if strings.Contains(commands["alpine"], "sudo") || strings.Contains(commands["alpine"], "bash") {
		t.Fatal("Alpine must not require sudo or bash")
	}
	if !strings.Contains(commands["windows"], "powershell.exe") || !strings.Contains(commands["windows"], "install-agent.ps1") {
		t.Fatal("Windows installer unavailable")
	}
	if !strings.Contains(commands["windows"], "--tlsv1.2") || !strings.Contains(commands["windows"], "$LASTEXITCODE -ne 0") {
		t.Fatal("Windows bootstrap must enable TLS and propagate failure")
	}
	if strings.Contains(commands["windows"], "ExecutionPolicy") {
		t.Fatal("generated command must respect the current execution policy")
	}
	if powershellQuote("a'b") != "'a''b'" {
		t.Fatal("PowerShell quote escaping")
	}
}
func TestUpdateDoesNotEnroll(t *testing.T) {
	commands := updateCommands("https://guard.example.com")
	for _, platform := range []string{"linux", "ubuntu", "debian", "alpine"} {
		if !strings.Contains(commands[platform], "'--update'") {
			t.Fatal("update flag missing", platform)
		}
	}
	if strings.Contains(commands["windows"], "-EnrollmentToken") || !strings.Contains(commands["windows"], "-Update") {
		t.Fatal("Windows update would enroll")
	}
}

func TestUninstallCommands(t *testing.T) {
	for platform, command := range uninstallCommands("https://guard.example.com") {
		if !strings.Contains(command, "uninstall-agent.") || strings.Contains(command, "EnrollmentToken") || strings.Contains(command, "--update") {
			t.Fatal("invalid uninstall command", platform)
		}
		if platform == "alpine" && strings.Contains(command, "sudo") {
			t.Fatal("Alpine requires root directly")
		}
	}
}

func TestIPCommandsMatchTransport(t *testing.T) {
	for _, origin := range []string{"http://192.0.2.10:8080", "http://[2001:db8::1]:8080", "https://192.0.2.10:8443"} {
		insecure := strings.HasPrefix(origin, "http://")
		for _, commands := range []map[string]string{installCommands(origin, strings.Repeat("a", 64)), updateCommands(origin)} {
			for platform, command := range commands {
				if !strings.Contains(command, origin) {
					t.Fatal("origin lost", platform)
				}
				option := "--allow-insecure-http"
				if platform == "windows" {
					option = "-AllowInsecureHttp"
				}
				if strings.Contains(command, option) != insecure {
					t.Fatal("HTTP opt-in mismatch", platform, origin)
				}
				if !strings.Contains(command, "--proto '=https'") || !strings.Contains(command, "--proto-redir '=https'") || !strings.Contains(command, agentAsset("install-agent.")) || strings.Contains(command, origin+"/downloads/") {
					t.Fatal("download transport mismatch")
				}
			}
		}
		for platform, command := range uninstallCommands(origin) {
			if !strings.Contains(command, agentAsset("uninstall-agent.")) {
				t.Fatal("invalid uninstall transport", platform)
			}
		}
	}
}
