package api

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"path/filepath"
	"sshguard/internal/storage"
	"sync"
	"testing"
	"time"
)

func TestAgentOnlineTransitions(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, "sqlite", filepath.Join(t.TempDir(), "online.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO servers(id,name,group_name) VALUES('server','test','prod')`)
	exec(`INSERT INTO agent_credentials(token_hash,server_id) VALUES('credential','server')`)
	exec(`INSERT INTO notification_channels(id,name,kind,config_ciphertext) VALUES('channel','test','webhook','test')`)
	exec(`UPDATE alert_rules SET enabled=true WHERE kind='agent_online'`)
	app := &App{db: db, hub: newLiveHub()}
	beat := func() {
		t.Helper()
		if err := app.recordHeartbeat(ctx, "server", "credential", Heartbeat{}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int) {
		t.Helper()
		var alerts, jobs int
		if err := db.Row(ctx, `SELECT COUNT(*) FROM alerts WHERE rule_id='00000000-0000-4000-8000-000000000005'`).Scan(&alerts); err != nil {
			t.Fatal(err)
		}
		if err := db.Row(ctx, `SELECT COUNT(*) FROM notification_jobs`).Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if alerts != want || jobs != want {
			t.Fatalf("alerts=%d jobs=%d want=%d", alerts, jobs, want)
		}
	}
	old := time.Now().UTC().Add(-time.Hour)
	beat()
	count(1)
	exec(`UPDATE alert_state SET last_alert_at=?`, old)
	beat()
	count(1) // A normal heartbeat never repeats, even after cooldown.
	exec(`UPDATE servers SET last_seen_at=?`, old)
	beat()
	count(2)
	exec(`UPDATE servers SET last_seen_at=?`, old)
	beat()
	count(2) // Reconnect flapping obeys cooldown.
	exec(`UPDATE alert_state SET last_alert_at=?`, old)
	exec(`UPDATE alert_rules SET server_group='other' WHERE kind='agent_online'`)
	exec(`UPDATE servers SET last_seen_at=?`, old)
	beat()
	count(2)
	exec(`UPDATE alert_rules SET server_group='',enabled=false WHERE kind='agent_online'`)
	exec(`UPDATE servers SET last_seen_at=?`, old)
	beat()
	count(2)
	exec(`UPDATE alert_rules SET enabled=true WHERE kind='agent_online'`)
	exec(`UPDATE servers SET last_seen_at=?`, old)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := app.recordHeartbeat(ctx, "server", "credential", Heartbeat{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	count(3)
	exec(`INSERT INTO server_network(server_id,public_ip) VALUES('server','8.8.8.8')`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/alerts", nil)
	app.alerts(c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Data struct {
			Items []struct {
				IPs []string `json:"server_ip_addresses"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != 3 {
		t.Fatal("alerts missing")
	}
	for _, item := range response.Data.Items {
		if len(item.IPs) != 1 || item.IPs[0] != "8.8.8.8" {
			t.Fatalf("server IP missing: %v", item.IPs)
		}
	}

}
