package agent

import (
	"net/netip"
	"strconv"
	"strings"
	"time"
)

func loginEvent(protocol, outcome, identity string, at time.Time) SSHEvent {
	kind := protocol + ".login"
	if outcome == "failure" {
		kind += "_failed"
	}
	return SSHEvent{ID: sourceEventID(identity), Type: kind, Outcome: outcome, At: at.UTC(), Method: protocol}
}

// Only login's PAM service is a console source. sudo, su, sshd and desktop
// unlock sessions must not be relabelled as provider VNC logins.
func parseLogin(message, identity string, at time.Time) (SSHEvent, bool) {
	if strings.Contains(message, "sshd") {
		return parseSSH(message, identity, at)
	}
	if strings.Contains(message, "pam_unix(login:session): session opened for user ") {
		return loginEvent("console", "success", identity, at), true
	}
	if strings.Contains(message, "pam_unix(login:auth): authentication failure") {
		return loginEvent("console", "failure", identity, at), true
	}
	return SSHEvent{}, false
}

// A named provider or an explicitly configured VNC server log is required.
// Connection acceptance and protocol negotiation alone are never successful logins.
func parseVNC(format, message, identity string, at time.Time) (SSHEvent, bool) {
	m := strings.ToLower(message)
	return parseVNCMessage(format, message, m, identity, at)
}

func parseVNCMessage(format, message, m, identity string, at time.Time) (SSHEvent, bool) {
	outcome := ""
	switch format {
	case "realvnc":
		if strings.Contains(m, "connections: authenticated:") || strings.Contains(m, "connections authenticated:") {
			outcome = "success"
		}
		if strings.Contains(m, "connections: authentication failed:") || strings.Contains(m, "connections authentication failed:") {
			outcome = "failure"
		}
	case "tigervnc":
		if strings.Contains(m, "vncsconnst:") && strings.Contains(m, "server default pixel format") {
			outcome = "success"
		}
		if strings.Contains(m, "sconnection:") && strings.Contains(m, "authfailureexception:") {
			outcome = "failure"
		}
	case "tightvnc":
		if strings.Contains(m, "full-control authentication passed by ") || strings.Contains(m, "view-only authentication passed by ") {
			outcome = "success"
		}
		if strings.Contains(m, "authenticated with view-only password = ") {
			outcome = "success"
		}
		if strings.Contains(m, "rfbvncauthprocessresponse: authentication failed from ") {
			outcome = "failure"
		}
		if strings.Contains(m, "error during rfb initialization: authentication failed") {
			outcome = "failure"
		}
	case "ultravnc":
		if strings.Contains(m, "viewer message:") {
			return SSHEvent{}, false
		}
		if containsVNCWords(m, "authenticated connection") || strings.Contains(m, "authentication accepted from ") {
			outcome = "success"
		}
		if strings.Contains(m, "authentication failed") {
			outcome = "failure"
		}
		if strings.Contains(m, "connection rejected from client ") {
			outcome = "failure"
		}
		if strings.Contains(m, "invalid attempt from client ") {
			outcome = "failure"
		}
	case "auto":
		for _, provider := range []string{"realvnc", "tigervnc", "tightvnc"} {
			if e, ok := parseVNCMessage(provider, message, m, identity, at); ok {
				return e, true
			}
		}
	}
	if outcome == "" {
		return SSHEvent{}, false
	}
	e := loginEvent("vnc", outcome, identity, at)
	e.Method = format
	// Only extract an explicitly identified peer; never pick an arbitrary IP
	// (which can be a local address or user-controlled username).
	e.IP, e.Port = vncPeerAddress(m)
	return e, true
}

func containsVNCWords(message, words string) bool {
	for offset := 0; offset < len(message); {
		i := strings.Index(message[offset:], words)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(words)
		wordByte := func(c byte) bool { return asciiLetter(c) || digit(c) || c == '_' || c >= 128 }
		if (i == 0 || !wordByte(message[i-1])) && (end == len(message) || !wordByte(message[end])) {
			return true
		}
		offset = end
	}
	return false
}

func peerAddress(value string) (string, int) {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap().String(), 0
	}
	if endpoint, err := netip.ParseAddrPort(value); err == nil {
		return endpoint.Addr().Unmap().String(), int(endpoint.Port())
	}
	// RealVNC uses address::port, including bracketed IPv6.
	if i := strings.LastIndex(value, "::"); i >= 0 {
		addr, err := netip.ParseAddr(strings.Trim(value[:i], "[]"))
		port, pe := strconv.Atoi(value[i+2:])
		if err == nil && pe == nil && port > 0 && port <= 65535 {
			return addr.Unmap().String(), port
		}
	}
	return "", 0
}
