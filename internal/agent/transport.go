package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

// transport has one owner: the upload loop. Never send concurrently on it.
type transport struct {
	cfg       Config
	conn      *websocket.Conn
	nextDial  time.Time
	failures  int
	addresses addressResolver
}
type apiStatusError int

func (e apiStatusError) Error() string { return fmt.Sprintf("API status %d", int(e)) }

func (t *transport) close() {
	if t.conn != nil {
		t.conn.CloseNow()
		t.conn = nil
	}
}
func (t *transport) disconnected() {
	t.close()
	t.failures = min(t.failures+1, 7)
	delay := time.Second * time.Duration(1<<t.failures)
	t.nextDial = time.Now().Add(delay/2 + time.Duration(rand.Int64N(int64(delay/2))))
}
func (t *transport) connect(ctx context.Context) {
	// Long reporting intervals exceed common reverse-proxy WebSocket idle
	// timeouts. Use the same acknowledged HTTP transport without an idle socket.
	if t.cfg.ReportIntervalSeconds > 30 {
		return
	}
	if t.conn != nil || time.Now().Before(t.nextDial) {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(t.cfg.Endpoint, "http") + "/api/v1/agent/stream"
	conn, resp, e := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + t.cfg.Token}}, CompressionMode: websocket.CompressionDisabled,
	})
	if e != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		t.disconnected()
		return
	}
	conn.SetReadLimit(65536)
	t.conn = conn
	// Reset backoff only after a matching application ACK, not merely a successful upgrade.
}
func (t *transport) send(ctx context.Context, kind string, data, out any) error {
	t.connect(ctx)
	if t.conn != nil {
		requestID := uuid.NewString()
		exchangeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		e := wsjson.Write(exchangeCtx, t.conn, map[string]any{"type": kind, "request_id": requestID, "data": data})
		var ack struct {
			Type      string          `json:"type"`
			RequestID string          `json:"request_id"`
			Status    int             `json:"status"`
			Data      json.RawMessage `json:"data"`
		}
		if e == nil {
			e = wsjson.Read(exchangeCtx, t.conn, &ack)
		}
		cancel()
		if e == nil && ack.Type == "ack" && ack.RequestID == requestID {
			t.failures = 0
			if ack.Status == http.StatusOK {
				if out == nil {
					return nil
				}
				if e = json.Unmarshal(ack.Data, out); e == nil {
					return nil
				}
			} else if ack.Status == 400 || ack.Status == 401 {
				if ack.Status == 401 {
					t.disconnected()
				}
				return apiStatusError(ack.Status)
			}
		}
		// Commit may already have happened. HTTPS repeats exactly the same event IDs.
		t.disconnected()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	path := "/api/v1/agent/heartbeat"
	if kind == "events" {
		path = "/api/v1/agent/events/batch"
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if e := request(ctx, t.cfg.Endpoint, path, t.cfg.Token, data, &envelope); e != nil {
		return e
	}
	if out != nil {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

// The server must positively acknowledge the whole batch before durable deletion.
// Any invalid response or transport error leaves every queued event in place.
func uploadBatch(ctx context.Context, t *transport, s *spool) (bool, error) {
	items, e := s.batch()
	if e != nil || len(items) == 0 {
		return false, e
	}
	events := make([]SSHEvent, len(items))
	for i, item := range items {
		events[i] = item.event
	}
	var result struct {
		Accepted   int `json:"accepted"`
		Inserted   int `json:"inserted"`
		Duplicates int `json:"duplicates"`
	}
	e = t.send(ctx, "events", map[string]any{"events": events}, &result)
	if e != nil {
		var status apiStatusError
		if errors.As(e, &status) && status == 400 {
			// Quarantine each invalid record individually, preserving valid peers in a mixed batch.
			if len(items) == 1 {
				return false, s.acknowledge(items, true)
			}
			for _, item := range items {
				single := map[string]any{"events": []SSHEvent{item.event}}
				result.Accepted, result.Inserted, result.Duplicates = 0, 0, 0
				err := t.send(ctx, "events", single, &result)
				if errors.As(err, &status) && status == 400 {
					if err = s.acknowledge([]queued{item}, true); err != nil {
						return false, err
					}
				} else if err != nil {
					return false, err
				} else if result.Accepted != 1 || result.Inserted < 0 || result.Duplicates < 0 || result.Inserted+result.Duplicates != 1 {
					return false, errors.New("invalid event acknowledgement")
				} else if err = s.acknowledge([]queued{item}, false); err != nil {
					return false, err
				}
			}
			return true, nil
		}
		return false, e
	}
	if result.Accepted != len(items) || result.Inserted < 0 || result.Duplicates < 0 || result.Inserted+result.Duplicates != len(items) {
		return false, errors.New("invalid event acknowledgement")
	}
	return true, s.acknowledge(items, false)
}
