package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"sshguard/internal/storage"
)

func testSettingsAndAllowlist(t *testing.T, db *storage.DB, session string, call func(string, string, any, string, string, bool, int) (map[string]any, *httptest.ResponseRecorder)) {
	t.Helper()
	ctx := context.Background()
	for key, count := range map[string]int{"24h": 24, "7d": 7, "14d": 14, "30d": 30} {
		result, _ := call("GET", "/dashboard/overview?range="+key, nil, session, "", false, 200)
		data := result["data"].(map[string]any)
		if len(data["trend"].([]any)) != count || data["range"] != key {
			t.Fatal("incorrect trend range", key, data)
		}
	}
	call("GET", "/dashboard/overview?range=arbitrary", nil, session, "", false, 400)

	result, _ := call("POST", "/servers", map[string]any{"name": "settings fixture"}, session, "", true, 200)
	id := result["data"].(map[string]any)["id"].(string)
	call("POST", "/servers", map[string]any{"name": "invalid runtime setting", "report_interval_seconds": 1}, session, "", true, 400)
	for _, invalid := range []int{0, 601, -1} {
		call("POST", "/servers/"+id+"/enrollment", map[string]any{"report_interval_seconds": invalid}, session, "", true, 400)
	}
	result, _ = call("POST", "/servers/"+id+"/enrollment", map[string]any{"report_interval_seconds": 600}, session, "", true, 200)
	command := result["data"].(map[string]any)["command"].(string)
	parts := strings.Split(command, "'")
	enrollment := parts[len(parts)-2]
	result, _ = call("POST", "/agent/enroll", map[string]any{"token": enrollment, "hostname": "settings fixture", "version": "test", "report_interval_seconds": 30}, "", "", false, 200)
	if result["data"].(map[string]any)["report_interval_seconds"] != float64(600) {
		t.Fatal("installation interval was not bound to enrollment")
	}
	credential := result["data"].(map[string]any)["token"].(string)
	call("PATCH", "/servers/"+id, map[string]any{"name": "legacy editor"}, session, "", true, 200)
	call("PATCH", "/servers/"+id, map[string]any{"name": "runtime override", "report_interval_seconds": 30}, session, "", true, 400)
	call("PATCH", "/servers/"+id+"/settings", map[string]any{"report_interval_seconds": 30}, session, "", true, 404)
	result, _ = call("GET", "/servers/"+id, nil, session, "", false, 200)
	if result["data"].(map[string]any)["report_interval_seconds"] != float64(600) {
		t.Fatal("metadata editing changed installed interval")
	}
	result, _ = call("POST", "/agent/heartbeat", map[string]any{"collector_state": "healthy", "report_interval_seconds": 600, "ip_addresses": []string{"192.0.2.25", "2001:db8::25", "127.0.0.1", "192.0.2.25"}}, "", credential, false, 200)
	if _, found := result["data"].(map[string]any)["report_interval_seconds"]; found {
		t.Fatal("heartbeat must not reconfigure Agent")
	}
	call("POST", "/servers/"+id+"/enrollment", map[string]any{"report_interval_seconds": 30}, session, "", true, 200)
	result, _ = call("GET", "/servers/"+id, nil, session, "", false, 200)
	if result["data"].(map[string]any)["report_interval_seconds"] != float64(600) {
		t.Fatal("unconsumed installer changed running Agent settings")
	}
	call("POST", "/agent/heartbeat", map[string]any{}, "", credential, false, 200)
	var actual int
	var addresses string
	if e := db.Row(ctx, `SELECT reported_interval_seconds,ip_addresses FROM server_settings WHERE server_id=?`, id).Scan(&actual, &addresses); e != nil || actual != 600 || addresses != `["192.0.2.25","2001:db8::25"]` {
		t.Fatal("observed state lost", actual, addresses, e)
	}
	call("POST", "/agent/heartbeat", map[string]any{"report_interval_seconds": 601}, "", credential, false, 400)
	call("POST", "/agent/heartbeat", map[string]any{"report_interval_seconds": 1, "ip_addresses": []string{"not-an-ip"}}, "", credential, false, 400)
	call("POST", "/ip-allowlist", map[string]any{"cidr": "not an address"}, session, "", true, 400)
	var allowIDs []string
	for _, prefix := range []string{"192.0.2.25", "2001:db8:1::5/64"} {
		result, _ = call("POST", "/ip-allowlist", map[string]any{"cidr": prefix, "notes": "fixture"}, session, "", true, 200)
		allowIDs = append(allowIDs, result["data"].(map[string]any)["id"].(string))
	}
	call("POST", "/ip-allowlist", map[string]any{"cidr": "192.0.2.25/32"}, session, "", true, 409)
	call("POST", "/ip-allowlist", map[string]any{"cidr": "2001:db8:1::/64"}, session, "", true, 409)
	for _, ip := range []string{"192.0.2.25", "2001:db8:1::99"} {
		for i := 0; i < 6; i++ {
			event := map[string]any{"event_id": uuid.NewString(), "event_type": "ssh.login_failed", "username": "root", "source_ip": ip, "source_port": 2222, "outcome": "failure", "occurred_at": time.Now().UTC()}
			call("POST", "/agent/events/batch", map[string]any{"events": []any{event}}, "", credential, false, 200)
		}
	}
	var events, alerts int
	if e := db.Row(ctx, `SELECT COUNT(*) FROM ssh_events WHERE server_id=?`, id).Scan(&events); e != nil || events != 12 {
		t.Fatal("allowlist discarded audit events", events, e)
	}
	if e := db.Row(ctx, `SELECT COUNT(*) FROM alerts WHERE server_id=?`, id).Scan(&alerts); e != nil || alerts != 0 {
		t.Fatal("allowlist did not suppress alerts", alerts, e)
	}
	for _, allowID := range allowIDs {
		call("DELETE", "/ip-allowlist/"+allowID, nil, session, "", true, 200)
	}
	event := map[string]any{"event_id": uuid.NewString(), "event_type": "ssh.login", "username": "root", "source_ip": "192.0.2.25", "source_port": 2222, "outcome": "success", "occurred_at": time.Now().UTC()}
	call("POST", "/agent/events/batch", map[string]any{"events": []any{event}}, "", credential, false, 200)
	if e := db.Row(ctx, `SELECT COUNT(*) FROM alerts WHERE server_id=?`, id).Scan(&alerts); e != nil || alerts != 1 {
		t.Fatal("removed allowlist still suppresses", alerts, e)
	}
	var ip, ua string
	if e := db.Row(ctx, `SELECT x.source_ip,x.user_agent FROM audit_context x JOIN audit_logs a ON a.id=x.audit_id WHERE a.target=? AND a.action='server.create'`, id).Scan(&ip, &ua); e != nil || ip != "192.0.2.1" || ua != "SSHGuard integration browser" {
		t.Fatal("audit context missing or forged", ip, ua, e)
	}
	monitor := NewApp(db, Config{})
	rules, err := monitor.readRules(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		age    time.Duration
		status string
	}{{20 * time.Minute, "online"}, {1801 * time.Second, "offline"}} {
		if _, err := db.Exec(ctx, `UPDATE servers SET last_seen_at=? WHERE id=?`, time.Now().UTC().Add(-check.age), id); err != nil {
			t.Fatal(err)
		}
		result, _ = call("GET", "/servers/"+id, nil, session, "", false, 200)
		if result["data"].(map[string]any)["status"] != check.status {
			t.Fatal("long report interval freshness", result)
		}
		if err := monitor.monitorServer(ctx, id, rules); err != nil {
			t.Fatal(err)
		}
		var incidents int
		if err := db.Row(ctx, `SELECT COUNT(*) FROM alerts WHERE server_id=? AND rule_id='00000000-0000-4000-8000-000000000003' AND resolved_at IS NULL`, id).Scan(&incidents); err != nil {
			t.Fatal(err)
		}
		if (incidents > 0) != (check.status == "offline") {
			t.Fatal("long interval offline alert mismatch", check.status, incidents)
		}
	}
	call("POST", "/agent/heartbeat", map[string]any{}, "", credential, false, 200)

	testReportFreshness(t, db, id)
}
