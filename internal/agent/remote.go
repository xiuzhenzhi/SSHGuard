package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type VNCLog struct {
	Path   string `json:"path"`
	Format string `json:"format"`
}

func validVNCLogs(logs []VNCLog) bool {
	if len(logs) > 32 {
		return false
	}
	for _, source := range logs {
		if !filepath.IsAbs(source.Path) || strings.ContainsAny(source.Path, "\x00\r\n*?[") {
			return false
		}
		switch source.Format {
		case "tigervnc", "tightvnc", "realvnc", "ultravnc", "auto":
		default:
			return false
		}
	}
	return true
}

// Explicit paths take precedence. Only familiar server-log locations are scanned.
func vncLogSources(configured []VNCLog) []VNCLog {
	result := append([]VNCLog{}, configured...)
	patterns := []VNCLog{}
	if runtime.GOOS == "windows" {
		patterns = append(patterns, VNCLog{filepath.Join(os.Getenv("ProgramData"), "TightVNC", "tvnserver*.log"), "tightvnc"})
		// RealVNC and UltraVNC use Application events to avoid duplicate file copies.
	} else {
		for _, pattern := range []string{"/root/.vnc/*.log", "/home/*/.vnc/*.log", "/root/.local/state/tigervnc/*.log", "/home/*/.local/state/tigervnc/*.log"} {
			patterns = append(patterns, VNCLog{pattern, "auto"})
		}
		if !platformJournalAvailable() {
			patterns = append(patterns, VNCLog{"/var/log/vncserver-x11.log", "realvnc"})
		}
	}
	seen := map[string]bool{}
	for _, source := range configured {
		seen[filepath.Clean(source.Path)] = true
	}
	for _, pattern := range patterns {
		paths, _ := filepath.Glob(pattern.Path)
		for _, path := range paths {
			if len(result) >= 32 {
				return result
			}
			if seen[filepath.Clean(path)] {
				continue
			}
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				continue
			}
			seen[filepath.Clean(path)] = true
			result = append(result, VNCLog{path, pattern.Format})
		}
	}
	return result
}

func collectSources(ctx context.Context, s *spool, cfg Config) (time.Time, error) {
	last, baseErr := collectPlatform(ctx, s, cfg.LogFile)
	var remoteErr error
	if cfg.LogFile == "" {
		at, err := collectRemotePlatform(ctx, s)
		remoteErr = err
		if at.After(last) {
			last = at
		}
	}
	errs := []error{baseErr, remoteErr}
	for _, source := range vncLogSources(cfg.VNCLogs) {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		parse := func(message, id string, at time.Time) (SSHEvent, bool) {
			return parseVNC(source.Format, message, id, at)
		}
		at, err := collectFileSource(ctx, s, source.Path, "vnc:"+filepath.Clean(source.Path), parse)
		if err != nil {
			errs = append(errs, fmt.Errorf("VNC %s: %w", source.Format, err))
		}
		if at.After(last) {
			last = at
		}
	}
	return last, errors.Join(errs...)
}
