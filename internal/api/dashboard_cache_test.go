package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"path/filepath"
	"sshguard/internal/storage"
	"testing"
	"time"
)

func TestSlowOverviewDoesNotBlockCachedSummary(t *testing.T) {
	db, err := storage.Open(context.Background(), "sqlite", filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	app := &App{db: db}
	app.saveDashboard("summary", gin.H{"servers": 42})
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/dashboard/overview", nil).WithContext(ctx)
		app.overview(c)
	}()
	deadline := time.Now().Add(time.Second)
	for db.Stats().WaitCount == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().WaitCount == 0 {
		t.Fatal("overview did not start database query")
	}
	summary := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/dashboard/summary", nil)
		app.summary(c)
		summary <- w.Code
	}()
	select {
	case code := <-summary:
		if code != 200 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Error("cached summary blocked by trend query")
	}
	conn.Close()
	<-finished
	_, _, epoch := app.readDashboard("summary")
	app.dashboardMu.Lock()
	app.dashboardEpoch++
	clear(app.dashboardCache)
	app.dashboardMu.Unlock()
	app.publishDashboard("summary", gin.H{}, epoch)
	if _, valid, _ := app.readDashboard("summary"); valid {
		t.Fatal("invalidated query repopulated cache")
	}
}
