package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLongReportInterval(t *testing.T) {
	for _, seconds := range []int{1, 30, 60, 600} {
		if reportInterval(seconds) != time.Duration(seconds)*time.Second {
			t.Fatalf("interval %d rejected", seconds)
		}
	}
	for _, seconds := range []int{-1, 0, 601} {
		if reportInterval(seconds) != 5*time.Second {
			t.Fatalf("invalid interval %d accepted", seconds)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/heartbeat" {
			t.Errorf("unexpected idle WebSocket: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["report_interval_seconds"] != float64(600) {
			t.Error("long interval not sent", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"received":true}}`))
	}))
	defer server.Close()
	tr := &transport{cfg: Config{Endpoint: server.URL, ReportIntervalSeconds: 600, AllowInsecureHTTP: true}}
	// Avoid unrelated public-IP lookup in this transport test.
	if err := tr.send(context.Background(), "heartbeat", map[string]any{"report_interval_seconds": 600}, nil); err != nil {
		t.Fatal(err)
	}
}
