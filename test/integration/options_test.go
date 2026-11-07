package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jecklgamis/http-sink/internal/server"
)

func serve(t *testing.T, app *server.Server) string {
	t.Helper()
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestInstanceNameIsReportedInStats(t *testing.T) {
	base := serve(t, server.New(server.Options{Instance: "pod-a"}))
	if got := do(t, "GET", base+"/sink/stats", nil).JSON(t)["instance"]; got != "pod-a" {
		t.Fatalf("instance = %v, want pod-a", got)
	}
	// defaults to the hostname rather than being empty
	if got := do(t, "GET", newApp(t)+"/sink/stats", nil).JSON(t)["instance"]; got == nil || got == "" {
		t.Fatalf("instance should default to the hostname, got %v", got)
	}
}

func TestMaxDelayOption(t *testing.T) {
	base := serve(t, server.New(server.Options{MaxDelay: time.Second}))
	wantStatus(t, do(t, "GET", base+"/sink/x?sinkDelay=1500ms", nil), 400)
	wantStatus(t, do(t, "GET", base+"/sink/x?sinkDelay=20ms", nil), 200)

	// a larger maximum allows longer delays than the 10s default
	app := server.New(server.Options{MaxDelay: time.Minute})
	if err := app.SetRules([]server.Rule{{Path: "/sink/long", Delay: "30s"}}); err != nil {
		t.Fatalf("a 30s rule should be valid under a 1m max: %v", err)
	}
	// and a smaller one rejects rules the default would accept
	if err := server.New(server.Options{MaxDelay: time.Second}).SetRules([]server.Rule{{Path: "/sink/long", Delay: "5s"}}); err == nil {
		t.Fatal("a 5s rule should be invalid under a 1s max")
	}
}

func TestMaxBodyBytesOption(t *testing.T) {
	base := serve(t, server.New(server.Options{MaxBodyBytes: 32}))
	small, big := `{"a":1}`, `{"payload":"`+strings.Repeat("x", 100)+`"}`
	post := func(body string) int {
		res, err := http.Post(base+"/sink/x", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := post(small); got != 200 {
		t.Fatalf("small body = %d, want 200", got)
	}
	if got := post(big); got != 413 {
		t.Fatalf("oversized body = %d, want 413", got)
	}
}

func TestNoRateLimitOption(t *testing.T) {
	base := serve(t, server.New(server.Options{NoRateLimit: true}))
	for i := 0; i < 1500; i++ { // more than the 1000 req/s default would allow in a window
		if res := do(t, "GET", base+"/probe/live", nil); res.Status != 200 {
			t.Fatalf("request %d = %d with the rate limit disabled", i+1, res.Status)
		}
	}
}

// syncBuffer lets the test read the access log while the server writes it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForLog(t *testing.T, b *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := b.String(); strings.HasSuffix(s, "\n") {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no access log line written")
	return ""
}

func TestJSONAccessLog(t *testing.T) {
	logs := &syncBuffer{}
	base := serve(t, server.New(server.Options{AccessLog: logs, AccessLogJSON: true}))
	do(t, "GET", base+"/sink/logged?a=1", nil, withHeader("User-Agent", "test-agent"))

	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(waitForLog(t, logs))), &line); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, logs.String())
	}
	if line["method"] != "GET" || line["target"] != "/sink/logged?a=1" || line["status"] != float64(200) ||
		line["user_agent"] != "test-agent" {
		t.Fatalf("log line = %v", line)
	}
	for _, k := range []string{"time", "remote", "proto", "bytes", "duration_ms"} {
		if _, ok := line[k]; !ok {
			t.Fatalf("log line missing %q: %v", k, line)
		}
	}
}

func TestCombinedAccessLogIsTheDefaultFormat(t *testing.T) {
	logs := &syncBuffer{}
	base := serve(t, server.New(server.Options{AccessLog: logs}))
	do(t, "GET", base+"/sink/logged", nil)
	if line := waitForLog(t, logs); !strings.Contains(line, `"GET /sink/logged HTTP/1.1" 200`) || strings.HasPrefix(line, "{") {
		t.Fatalf("expected combined format, got %q", line)
	}
}
