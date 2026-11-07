package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/jecklgamis/http-sink/internal/sink"
)

func TestRejectsConfigOutsideSinkSubpath(t *testing.T) {
	base := newApp(t)
	for _, path := range []string{"/sink", "/sink/", "/other/foo", "", "sink/foo"} {
		wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": 100}), 400)
		wantStatus(t, do(t, "POST", base+"/sink/config/failure",
			map[string]any{"path": path, "rate": 0.5, "statusCode": 500}), 400)
	}
}

func TestLatencyConfigLifecycle(t *testing.T) {
	base := newApp(t)
	path := "/sink/" + uniqueName("scenario")

	res := do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": 50})
	wantStatus(t, res, 201)
	if got := res.JSON(t); got["path"] != path || got["jitterMs"] != float64(50) {
		t.Fatalf("create response = %v", got)
	}
	if list := do(t, "GET", base+"/sink/config/latency", nil); !contains(string(list.Raw), path) {
		t.Fatalf("config missing from list: %s", list.Raw)
	}

	wantStatus(t, do(t, "DELETE", base+"/sink/config/latency?path="+path, nil), 204)
	if list := do(t, "GET", base+"/sink/config/latency", nil); contains(string(list.Raw), path) {
		t.Fatalf("config still listed after delete: %s", list.Raw)
	}
}

func TestFailureConfigLifecycle(t *testing.T) {
	base := newApp(t)
	path := "/sink/" + uniqueName("scenario")

	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": path, "rate": 0.5, "statusCode": 500}), 201)
	if list := do(t, "GET", base+"/sink/config/failure", nil); !contains(string(list.Raw), path) {
		t.Fatalf("config missing from list: %s", list.Raw)
	}
	wantStatus(t, do(t, "DELETE", base+"/sink/config/failure?path="+path, nil), 204)
	if list := do(t, "GET", base+"/sink/config/failure", nil); contains(string(list.Raw), path) {
		t.Fatalf("config still listed after delete: %s", list.Raw)
	}
}

func TestValidatesConfigValues(t *testing.T) {
	base := newApp(t)
	path := "/sink/" + uniqueName("v")

	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": -1}), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": "abc"}), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path}), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": "25"}), 201)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 1.5, "statusCode": 500}), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 0.5, "statusCode": 200}), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 0.5, "statusCode": 500.5}), 400)
	wantStatus(t, do(t, "DELETE", base+"/sink/config/latency", nil), 400) // path query param required
}

func TestProjectsEndpointIsGone(t *testing.T) {
	base := newApp(t)
	// /sink/projects is now just another echoed sink path
	res := do(t, "POST", base+"/sink/projects", map[string]any{"name": "x"})
	wantStatus(t, res, 200)
	if res.JSON(t)["method"] != "POST" {
		t.Fatalf("expected echo, got %s", res.Raw)
	}
}

func TestConfigCapReturns429(t *testing.T) {
	base := newApp(t)
	for _, endpoint := range []string{"latency", "failure"} {
		payload := func(path string) map[string]any {
			if endpoint == "latency" {
				return map[string]any{"path": path, "jitterMs": 1}
			}
			return map[string]any{"path": path, "rate": 0.1, "statusCode": 500}
		}
		url := base + "/sink/config/" + endpoint
		for i := 0; i < sink.MaxConfigs; i++ {
			wantStatus(t, do(t, "POST", url, payload(fmt.Sprintf("/sink/cap/%d", i))), 201)
		}
		wantStatus(t, do(t, "POST", url, payload("/sink/cap/overflow")), 429)
		wantStatus(t, do(t, "POST", url, payload("/sink/cap/0")), 201) // updating still works
	}
}

func TestConfigResponseIncludesExpiry(t *testing.T) {
	base := newApp(t)
	res := do(t, "POST", base+"/sink/config/latency", map[string]any{"path": "/sink/exp", "jitterMs": 5})
	wantStatus(t, res, 201)
	exp, _ := res.JSON(t)["expiresAt"].(float64)
	if remaining := time.Until(time.UnixMilli(int64(exp))); remaining < 59*time.Minute || remaining > time.Hour {
		t.Fatalf("expiresAt is %v from now, want ~1h", remaining)
	}
}
