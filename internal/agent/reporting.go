package agent

import (
	"context"
	"net"
	"sort"
	"sshguard/internal/ipaddress"
	"time"
)

func reportInterval(seconds int) time.Duration {
	if seconds < 1 || seconds > 600 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func hostAddresses() []string {
	result := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return result
	}
	seen := map[string]bool{}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || !ipaddress.Public(ip.String()) {
			continue
		}
		value := ip.String()
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == 16 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func sendHeartbeat(ctx context.Context, tr *transport, payload map[string]any, interval time.Duration) error {
	payload["report_interval_seconds"] = int(interval / time.Second)
	payload["ip_addresses"] = tr.addresses.addresses(ctx)
	// The reporting interval belongs to the installed config. ACKs carry no commands.
	return tr.send(ctx, "heartbeat", payload, nil)
}
