package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func testServerGroup(t *testing.T, app *App, session string, call func(string, string, any, string, string, bool, int) (map[string]any, *httptest.ResponseRecorder)) {
	t.Helper()
	result, _ := call("POST", "/servers", map[string]string{"name": "group fixture", "notes": "preserve notes", "group_name": "old"}, session, "", true, 200)
	id := result["data"].(map[string]any)["id"].(string)
	path := "/servers/" + id + "/group"
	call("PATCH", path, map[string]string{"group_name": "new"}, "", "", true, 401)
	call("PATCH", path, map[string]string{"group_name": "new"}, session, "", false, 403)
	call("PATCH", path, map[string]string{}, session, "", true, 400)
	call("PATCH", path, map[string]string{"group_name": strings.Repeat("x", 65)}, session, "", true, 400)
	for _, group := range []string{"new", ""} {
		call("PATCH", path, map[string]string{"group_name": group}, session, "", true, 200)
		row, _ := call("GET", "/servers/"+id, nil, session, "", false, 200)
		data := row["data"].(map[string]any)
		if data["group_name"] != group || data["name"] != "group fixture" || data["notes"] != "preserve notes" {
			t.Fatal("group update changed unrelated fields", data)
		}
	}
	var count int
	if err := app.db.Row(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action='server.group.update' AND target=?`, id).Scan(&count); err != nil || count != 2 {
		t.Fatal("missing group audit", count, err)
	}
}

func testServerDeletion(t *testing.T, app *App, session string, call func(string, string, any, string, string, bool, int) (map[string]any, *httptest.ResponseRecorder)) {
	t.Helper()
	ctx := context.Background()
	db := app.db
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, want int, args ...any) {
		t.Helper()
		var got int
		if err := db.Row(ctx, query, args...).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d, want %d: %v", query, got, want, err)
		}
	}
	channelID := uuid.NewString()
	exec(`INSERT INTO notification_channels(id,name,kind,config_ciphertext,enabled) VALUES(?,'deletion fixture','webhook','fixture',false)`, channelID)
	type fixture struct{ id, credential, enrollment, alert string }
	makeFixture := func(name string) fixture {
		t.Helper()
		result, _ := call("POST", "/servers", map[string]string{"name": name}, session, "", true, 200)
		f := fixture{result["data"].(map[string]any)["id"].(string), token(), token(), uuid.NewString()}
		expires := time.Now().UTC().Add(time.Hour)
		exec(`UPDATE servers SET last_seen_at=? WHERE id=?`, time.Now().UTC(), f.id)
		exec(`INSERT INTO agent_credentials(token_hash,server_id) VALUES(?,?)`, hash(f.credential), f.id)
		exec(`INSERT INTO agent_enrollments(token_hash,server_id,expires_at) VALUES(?,?,?)`, hash(f.enrollment), f.id, expires)
		exec(`INSERT INTO agent_install_settings(token_hash,report_interval_seconds,expires_at) VALUES(?,5,?)`, hash(f.enrollment), expires)
		exec(`INSERT INTO agent_health(server_id,collector_state) VALUES(?,'healthy')`, f.id)
		exec(`INSERT INTO agent_updates(server_id,status) VALUES(?,'pending')`, f.id)
		exec(`INSERT INTO server_settings(server_id) VALUES(?)`, f.id)
		exec(`INSERT INTO server_network(server_id,public_ip) VALUES(?,'8.8.8.8')`, f.id)
		exec(`INSERT INTO ssh_events(event_id,server_id,event_type,username,source_ip,outcome,occurred_at) VALUES(?,?,'ssh.login','root','192.0.2.1','success',?)`, uuid.NewString(), f.id, time.Now().UTC())
		exec(`INSERT INTO alerts(id,server_id,severity,title,message) VALUES(?,?,'info','deletion fixture','fixture')`, f.alert, f.id)
		for _, status := range []string{"pending", "sending", "sent", "dead"} {
			exec(`INSERT INTO notification_jobs(id,alert_id,channel_id,status,next_attempt_at) VALUES(?,?,?,?,?)`, uuid.NewString(), f.alert, channelID, status, expires)
		}
		return f
	}
	deleted, kept := makeFixture("delete this server"), makeFixture("keep this server")
	// A failed audit write must roll back the entire deletion, including dependents.
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("DELETE", "/api/v1/servers/"+kept.id, nil)
	c.Params = gin.Params{{Key: "id", Value: kept.id}}
	c.Set("user", User{ID: uuid.NewString()})
	app.deleteServer(c)
	if recorder.Code != 500 {
		t.Fatalf("missing audit actor: got %d, want 500", recorder.Code)
	}
	call("DELETE", "/servers/"+deleted.id, nil, session, "", false, 403)
	count(`SELECT COUNT(*) FROM servers WHERE id=?`, 1, deleted.id)
	count(`SELECT COUNT(*) FROM notification_jobs WHERE alert_id=?`, 4, deleted.alert)
	call("POST", "/agent/heartbeat", map[string]any{}, "", deleted.credential, false, 200)
	app.dashboardMu.Lock()
	clear(app.dashboardCache)
	app.dashboardMu.Unlock()
	before, _ := call("GET", "/dashboard/summary", nil, session, "", false, 200)
	_, _ = call("GET", "/dashboard/overview", nil, session, "", false, 200)
	_, _ = call("GET", "/dashboard/country-ranking", nil, session, "", false, 200)
	app.dashboardMu.Lock()
	epoch := app.countryEpoch
	app.dashboardMu.Unlock()
	result, _ := call("DELETE", "/servers/"+deleted.id, nil, session, "", true, 200)
	if result["data"].(map[string]any)["deleted"] != true {
		t.Fatal("missing deletion confirmation", result)
	}
	app.dashboardMu.Lock()
	invalidated := len(app.dashboardCache) == 0 && app.countryEpoch > epoch
	app.dashboardMu.Unlock()
	if !invalidated {
		t.Fatal("deletion retained stale dashboard caches")
	}
	after, _ := call("GET", "/dashboard/summary", nil, session, "", false, 200)
	if after["data"].(map[string]any)["servers"].(float64) != before["data"].(map[string]any)["servers"].(float64)-1 {
		t.Fatal("summary did not reflect deletion")
	}
	for _, table := range []string{"servers", "agent_credentials", "agent_enrollments", "agent_health", "agent_updates", "server_settings", "server_network", "ssh_events", "alerts"} {
		key := "server_id"
		if table == "servers" {
			key = "id"
		}
		count("SELECT COUNT(*) FROM "+table+" WHERE "+key+"=?", 0, deleted.id)
		count("SELECT COUNT(*) FROM "+table+" WHERE "+key+"=?", 1, kept.id)
	}
	count(`SELECT COUNT(*) FROM agent_install_settings WHERE token_hash=?`, 0, hash(deleted.enrollment))
	count(`SELECT COUNT(*) FROM agent_install_settings WHERE token_hash=?`, 1, hash(kept.enrollment))
	count(`SELECT COUNT(*) FROM notification_jobs WHERE alert_id=?`, 0, deleted.alert)
	count(`SELECT COUNT(*) FROM notification_jobs WHERE alert_id=?`, 4, kept.alert)
	count(`SELECT COUNT(*) FROM notification_channels WHERE id=?`, 1, channelID)
	count(`SELECT COUNT(*) FROM audit_logs WHERE action='server.create' AND target=?`, 1, deleted.id)
	count(`SELECT COUNT(*) FROM audit_logs WHERE action='server.delete' AND target=?`, 1, deleted.id)
	count(`SELECT COUNT(*) FROM audit_context x JOIN audit_logs a ON a.id=x.audit_id WHERE a.action='server.delete' AND a.target=? AND x.source_ip='192.0.2.1' AND x.user_agent='SSHGuard integration browser'`, 1, deleted.id)
	call("GET", "/servers/"+deleted.id, nil, session, "", false, 404)
	result, _ = call("GET", "/servers?q=delete+this+server", nil, session, "", false, 200)
	if len(result["data"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatal("deleted server remains listed")
	}
	call("DELETE", "/servers/"+deleted.id, nil, session, "", true, 404)
	call("DELETE", "/servers/missing", nil, session, "", true, 404)
	count(`SELECT COUNT(*) FROM audit_logs WHERE action='server.delete' AND target=?`, 1, deleted.id)
	for _, path := range []string{"/agent/heartbeat", "/agent/events/batch", "/agent/update/poll", "/agent/update/result"} {
		call("POST", path, map[string]any{}, "", deleted.credential, false, 401)
	}
	call("POST", "/agent/enroll", map[string]string{"token": deleted.enrollment, "hostname": "deleted", "version": "test"}, "", "", false, 401)
	call("POST", "/agent/heartbeat", map[string]any{}, "", kept.credential, false, 200)
	call("DELETE", "/servers/"+kept.id, nil, session, "", true, 200)
	exec(`DELETE FROM notification_channels WHERE id=?`, channelID)
	for _, revoked := range []bool{false, true} {
		result, _ := call("POST", "/servers", map[string]string{"name": "empty server"}, session, "", true, 200)
		id := result["data"].(map[string]any)["id"].(string)
		if revoked {
			call("POST", "/servers/"+id+"/revoke", map[string]any{}, session, "", true, 200)
		}
		call("DELETE", "/servers/"+id, nil, session, "", true, 200)
		count(`SELECT COUNT(*) FROM servers WHERE id=?`, 0, id)
	}
}
