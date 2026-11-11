package api

import (
	"context"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testUserLifecycle(t *testing.T, a *App, session string, call func(string, string, any, string, string, bool, int) (map[string]any, *httptest.ResponseRecorder)) {
	ctx := context.Background()
	call("POST", "/rules", map[string]any{"name": "Agent online test", "kind": "agent_online", "enabled": false, "threshold_count": 1, "window_seconds": 300, "cooldown_seconds": 900, "trusted_cidrs": []string{}, "channel_ids": []string{}}, session, "", true, 200)

	for _, tc := range []struct{ username, password, role, want string }{
		{"测试", "valid-password-2026", "viewer", "用户名必须为 3–64 个字符"},
		{"valid-user", "short", "admin", "密码必须为 12–72 字节"},
		{"valid-user", strings.Repeat("密", 25), "operator", "密码必须为 12–72 字节"},
		{"valid-user", "valid-password-2026", "invalid", "请选择有效的用户角色"},
	} {
		result, _ := call("POST", "/users", map[string]string{"username": tc.username, "password": tc.password, "role": tc.role}, session, "", true, 400)
		if result["error"] != tc.want {
			t.Fatalf("validation: %v", result)
		}
	}
	me, _ := call("GET", "/auth/me", nil, session, "", false, 200)
	self := me["data"].(map[string]any)["id"].(string)
	call("DELETE", "/users/"+self, nil, session, "", true, 400)
	for _, role := range []string{"admin", "operator", "viewer"} {
		name := "测试用户" + uuid.NewString()[:8]
		result, _ := call("POST", "/users", map[string]string{"username": " " + name + " ", "password": "密码密码", "role": role}, session, "", true, 200)
		duplicate, _ := call("POST", "/users", map[string]string{"username": name, "password": "valid-password-2026", "role": role}, session, "", true, 409)
		if duplicate["error"] != "用户名已存在" {
			t.Fatal(duplicate)
		}
		user := result["data"].(map[string]any)
		id := user["id"].(string)
		if user["username"] != name {
			t.Fatal("username not normalized")
		}
		_, r := call("POST", "/auth/login", map[string]string{"username": name, "password": "密码密码"}, "", "", true, 200)
		cookie := r.Result().Cookies()[0]
		other := cookie.Name + "=" + cookie.Value
		if role == "viewer" {
			call("POST", "/auth/password", map[string]string{"current_password": "密码密码", "new_password": "新的密码"}, other, "", true, 200)
			call("GET", "/auth/me", nil, other, "", false, 401)
			_, r = call("POST", "/auth/login", map[string]string{"username": name, "password": "新的密码"}, "", "", true, 200)
			cookie = r.Result().Cookies()[0]
			other = cookie.Name + "=" + cookie.Value
		}
		if role != "admin" {
			call("DELETE", "/users/"+self, nil, other, "", true, 403)
		}
		alertID := uuid.NewString()
		if _, err := a.db.Exec(ctx, `INSERT INTO alerts(id,severity,title,message,acknowledged_by,acknowledged_at) VALUES(?,?,?,?,?,?)`, alertID, "info", "test", "test", id, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		call("DELETE", "/users/"+id, nil, session, "", true, 200)
		call("GET", "/auth/me", nil, other, "", false, 401)
		call("DELETE", "/users/"+id, nil, session, "", true, 404)
		var count int
		var actor string
		if err := a.db.Row(ctx, `SELECT x.username FROM audit_actor_snapshots x JOIN audit_logs a ON a.id=x.audit_id WHERE a.target=? AND a.action='auth.login' ORDER BY a.id LIMIT 1`, id).Scan(&actor); err != nil || actor != name {
			t.Fatal("deleted actor attribution lost", actor, err)
		}

		if err := a.db.Row(ctx, `SELECT COUNT(*) FROM alerts WHERE id=? AND acknowledged_by IS NULL AND acknowledged_at IS NOT NULL`, alertID).Scan(&count); err != nil || count != 1 {
			t.Fatal("alert history lost", err)
		}
		if err := a.db.Row(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='user.delete' AND target=?`, id).Scan(&count); err != nil || count != 1 {
			t.Fatal("delete audit missing", err)
		}
	}
}
