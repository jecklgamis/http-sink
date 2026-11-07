package integration

import (
	"net/http/httptest"
	"testing"

	"github.com/jecklgamis/http-sink/internal/server"
)

// startPersistent starts an app backed by dir and returns its URL and a stop
// function that flushes and shuts it down, like a real restart.
func startPersistent(t *testing.T, dir string) (string, func()) {
	t.Helper()
	app := server.New(server.Options{})
	if err := app.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app)
	return ts.URL, func() { ts.Close(); app.Close() }
}

func TestConfigsSurviveRestart(t *testing.T) {
	dir := t.TempDir()

	base, stop := startPersistent(t, dir)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": "/sink/p/boom", "rate": 1, "statusCode": 503}), 201)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": "/sink/p/slow", "jitterMs": 7}), 201)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": "/sink/p/removed", "jitterMs": 7}), 201)
	wantStatus(t, do(t, "DELETE", base+"/sink/config/latency?path=/sink/p/removed", nil), 204)
	stop()

	base, stop = startPersistent(t, dir)
	defer stop()
	if list := do(t, "GET", base+"/sink/config/latency", nil); !contains(string(list.Raw), "/sink/p/slow") ||
		contains(string(list.Raw), "/sink/p/removed") {
		t.Fatalf("latency configs after restart: %s", list.Raw)
	}
	if list := do(t, "GET", base+"/sink/config/failure", nil); !contains(string(list.Raw), "/sink/p/boom") {
		t.Fatalf("failure configs after restart: %s", list.Raw)
	}
	// the restored rule actually applies
	wantStatus(t, do(t, "GET", base+"/sink/p/boom", nil), 503)
}

func TestStatsDoNotSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	base, stop := startPersistent(t, dir)
	do(t, "GET", base+"/sink/seen", nil)
	stop()

	base, stop = startPersistent(t, dir)
	defer stop()
	stats := do(t, "GET", base+"/sink/stats", nil).JSON(t)
	if n := len(stats["recentRequests"].([]any)); n != 0 {
		t.Fatalf("recent requests should be ephemeral, got %d after restart", n)
	}
}
