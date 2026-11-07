package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSinkEchoesRequest(t *testing.T) {
	base := newApp(t)
	res := do(t, "POST", base+"/sink/demo/thing?a=1&b=2&b=3", map[string]any{"hello": "world"},
		withHeader("X-Custom", "yes"))
	wantStatus(t, res, 200)
	got := res.JSON(t)
	if got["method"] != "POST" || got["path"] != "/sink/demo/thing" {
		t.Fatalf("method/path = %v %v", got["method"], got["path"])
	}
	args := got["args"].(map[string]any)
	if args["a"] != "1" || len(args["b"].([]any)) != 2 {
		t.Fatalf("args = %v", args)
	}
	if got["body"].(map[string]any)["hello"] != "world" {
		t.Fatalf("body = %v", got["body"])
	}
	if got["headers"].(map[string]any)["x-custom"] != "yes" {
		t.Fatalf("headers = %v", got["headers"])
	}
}

func TestBareSinkAcceptsAnyMethod(t *testing.T) {
	base := newApp(t)
	for _, m := range []string{"GET", "PUT", "PATCH", "DELETE"} {
		wantStatus(t, do(t, m, base+"/sink", nil), 200)
	}
}

func TestMalformedJSONBodyIs400(t *testing.T) {
	base := newApp(t)
	res, err := http.Post(base+"/sink/x", "application/json", strings.NewReader("{bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
}

func TestFormBodyIsParsed(t *testing.T) {
	base := newApp(t)
	res, err := http.Post(base+"/sink/x", "application/x-www-form-urlencoded", strings.NewReader("a=1&b=two"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	body := got["body"].(map[string]any)
	if body["a"] != "1" || body["b"] != "two" {
		t.Fatalf("body = %v", body)
	}
}

func TestSinkTrafficAppearsInStats(t *testing.T) {
	base := newApp(t)
	do(t, "GET", base+"/sink/demo/seen", nil)
	do(t, "GET", base+"/sink/stats", nil) // control plane: must not be recorded

	stats := do(t, "GET", base+"/sink/stats", nil).JSON(t)
	recent := stats["recentRequests"].([]any)
	if len(recent) != 1 {
		t.Fatalf("recentRequests = %d entries, want 1: %v", len(recent), recent)
	}
	first := recent[0].(map[string]any)
	if first["path"] != "/sink/demo/seen" || first["statusCode"] != float64(200) {
		t.Fatalf("recent entry = %v", first)
	}

	wantStatus(t, do(t, "DELETE", base+"/sink/recent", nil), 204)
	stats = do(t, "GET", base+"/sink/stats", nil).JSON(t)
	if n := len(stats["recentRequests"].([]any)); n != 0 {
		t.Fatalf("recentRequests after clear = %d, want 0", n)
	}
}

func TestInjectedFailure(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name + "/boom"
	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": path, "rate": 1, "statusCode": 503}, withHeader("X-Sink-Token", token)), 201)

	res := do(t, "GET", base+path, nil)
	wantStatus(t, res, 503)
	if res.JSON(t)["injectedFailure"] != true {
		t.Fatalf("body = %s", res.Raw)
	}
}

func TestInjectedLatency(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name + "/slow"
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": path, "jitterMs": 5}, withHeader("X-Sink-Token", token)), 201)

	start := time.Now()
	wantStatus(t, do(t, "GET", base+path, nil), 200)
	if time.Since(start) > 2*time.Second {
		t.Fatal("request with 5ms jitter took far too long")
	}
}
