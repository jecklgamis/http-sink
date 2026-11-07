package integration

import (
	"strings"
	"testing"
)

func TestGetIndex(t *testing.T) {
	base := newApp(t)
	res := do(t, "GET", base+"/", nil)
	wantStatus(t, res, 200)
	if !strings.Contains(string(res.Raw), "Live Traffic Stats") {
		t.Fatalf("dashboard HTML missing expected content:\n%s", res.Raw)
	}
}

func TestProbes(t *testing.T) {
	base := newApp(t)
	for path, msg := range map[string]string{"/probe/live": "I'm alive!", "/probe/ready": "I'm ready!"} {
		res := do(t, "GET", base+path, nil)
		wantStatus(t, res, 200)
		body := res.JSON(t)
		if body["ok"] != true || body["message"] != msg {
			t.Fatalf("%s = %v", path, body)
		}
	}
}

func TestBuildInfo(t *testing.T) {
	base := newApp(t)
	res := do(t, "GET", base+"/build-info", nil)
	wantStatus(t, res, 200)
	for _, k := range []string{"branch", "version", "build_time"} {
		if _, ok := res.JSON(t)[k]; !ok {
			t.Fatalf("build-info missing %q: %s", k, res.Raw)
		}
	}
}

func TestStaticAssets(t *testing.T) {
	base := newApp(t)
	wantStatus(t, do(t, "GET", base+"/javascripts/sink-stats.js", nil), 200)
	wantStatus(t, do(t, "GET", base+"/stylesheets/style.css", nil), 200)
}

func TestUnknownRouteIs404(t *testing.T) {
	base := newApp(t)
	wantStatus(t, do(t, "GET", base+"/nope", nil), 404)
}

func TestMetricsEndpointAndStatsLatency(t *testing.T) {
	base := newApp(t)
	for i := 0; i < 5; i++ {
		do(t, "GET", base+"/sink/demo/m", nil)
	}

	res := do(t, "GET", base+"/metrics", nil)
	wantStatus(t, res, 200)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
	for _, want := range []string{
		`http_sink_requests_total{method="GET",code="2xx"} 5`,
		`http_sink_request_duration_seconds_count 5`,
	} {
		if !strings.Contains(string(res.Raw), want) {
			t.Fatalf("missing %q in:\n%s", want, res.Raw)
		}
	}

	stats := do(t, "GET", base+"/sink/stats", nil).JSON(t)
	lat := stats["latency"].(map[string]any)
	if lat["all"].(map[string]any)["count"] != float64(5) {
		t.Fatalf("latency.all = %v", lat["all"])
	}
	if _, ok := lat["byPath"].(map[string]any)["/sink/demo/m"]; !ok {
		t.Fatalf("latency.byPath = %v", lat["byPath"])
	}
}
