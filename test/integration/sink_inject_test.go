package integration

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jecklgamis/http-sink/internal/server"
)

func timedGet(t *testing.T, url string, opts ...reqOpt) (response, time.Duration) {
	t.Helper()
	start := time.Now()
	res := do(t, "GET", url, nil, opts...)
	return res, time.Since(start)
}

func TestRequestDelay(t *testing.T) {
	base := newApp(t)

	res, took := timedGet(t, base+"/sink/x?sinkDelay=120ms")
	wantStatus(t, res, 200)
	if took < 120*time.Millisecond {
		t.Fatalf("took %v, want >= 120ms", took)
	}
	if res.Header.Get("X-Sink-Injected-Delay-Ms") != "120" {
		t.Fatalf("injected delay header = %q", res.Header.Get("X-Sink-Injected-Delay-Ms"))
	}

	// header variant, and a bare number meaning milliseconds
	_, took = timedGet(t, base+"/sink/x", withHeader("X-Sink-Delay", "100"))
	if took < 100*time.Millisecond {
		t.Fatalf("header delay took %v, want >= 100ms", took)
	}

	// a range stays within its bounds
	for i := 0; i < 5; i++ {
		_, took = timedGet(t, base+"/sink/x?sinkDelay=40ms-70ms")
		if took < 40*time.Millisecond || took > 500*time.Millisecond {
			t.Fatalf("range delay took %v", took)
		}
	}
}

func TestRequestDelayIsRecordedInResponseTime(t *testing.T) {
	base := newApp(t)
	do(t, "GET", base+"/sink/rt?sinkDelay=100ms", nil)
	stats := do(t, "GET", base+"/sink/stats", nil).JSON(t)
	recent := stats["recentRequests"].([]any)[0].(map[string]any)
	if ms, _ := recent["responseTimeMs"].(float64); ms < 100 {
		t.Fatalf("responseTimeMs = %v, want >= 100", ms)
	}
}

func TestInvalidInjectionIs400(t *testing.T) {
	base := newApp(t)
	for _, q := range []string{
		"sinkDelay=abc", "sinkDelay=11s", "sinkDelay=300ms-100ms", "sinkDelay=-5ms",
		"sinkStatus=200", "sinkStatus=abc", "sinkFailRate=2", "sinkFailRate=abc",
	} {
		res := do(t, "GET", base+"/sink/x?"+q, nil)
		wantStatus(t, res, 400)
		if _, ok := res.JSON(t)["error"]; !ok {
			t.Fatalf("%s: 400 body should have an error field: %s", q, res.Raw)
		}
	}
}

func TestRequestFailure(t *testing.T) {
	base := newApp(t)

	res := do(t, "GET", base+"/sink/x?sinkStatus=503", nil) // rate defaults to 1
	wantStatus(t, res, 503)
	if got := res.JSON(t); got["injectedFailure"] != true || got["statusCode"] != float64(503) {
		t.Fatalf("body = %s", res.Raw)
	}

	wantStatus(t, do(t, "GET", base+"/sink/x?sinkFailRate=1", nil), 500) // status defaults to 500
	wantStatus(t, do(t, "GET", base+"/sink/x?sinkStatus=503&sinkFailRate=0", nil), 200)
	wantStatus(t, do(t, "GET", base+"/sink/x", nil, withHeader("X-Sink-Status", "502")), 502)

	// roughly half of requests fail at rate 0.5
	failed := 0
	for i := 0; i < 200; i++ {
		if do(t, "GET", base+"/sink/x?sinkStatus=500&sinkFailRate=0.5", nil).Status == 500 {
			failed++
		}
	}
	if failed < 50 || failed > 150 {
		t.Fatalf("%d/200 failed at rate 0.5", failed)
	}
}

func TestOrdinaryClientParamsAreNotInterpreted(t *testing.T) {
	base := newApp(t)
	res := do(t, "GET", base+"/sink/webhook?status=active&delay=5s&failRate=9", nil)
	wantStatus(t, res, 200)
	args := res.JSON(t)["args"].(map[string]any)
	if args["status"] != "active" || args["delay"] != "5s" {
		t.Fatalf("args = %v", args)
	}
}

func TestRequestSettingsOverridePathConfig(t *testing.T) {
	base := newApp(t)
	path := "/sink/" + uniqueName("ovr")
	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": path, "rate": 1, "statusCode": 500}), 201)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": path, "jitterMs": 200}), 201)

	wantStatus(t, do(t, "GET", base+path, nil), 500) // path config applies by default

	// the request's own failRate=0 replaces the config's 100% failure, and its
	// fixed 1ms delay replaces the path's random jitter (the header reports it)
	res := do(t, "GET", base+path+"?sinkStatus=503&sinkFailRate=0&sinkDelay=1ms", nil)
	wantStatus(t, res, 200)
	if got := res.Header.Get("X-Sink-Injected-Delay-Ms"); got != "1" {
		t.Fatalf("injected delay = %q, want the request's 1ms", got)
	}
}

func TestDelayedRequestsAreBounded(t *testing.T) {
	ts := httptest.NewServer(server.New(server.Options{MaxDelayedRequests: 1}))
	defer ts.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wantStatusAsync(t, ts.URL+"/sink/x?sinkDelay=400ms", 200)
	}()
	time.Sleep(100 * time.Millisecond) // let the first request take the only slot

	res := do(t, "GET", ts.URL+"/sink/x?sinkDelay=50ms", nil)
	wantStatus(t, res, 429)
	wantStatus(t, do(t, "GET", ts.URL+"/sink/x", nil), 200) // undelayed requests are unaffected

	wg.Wait()
	wantStatus(t, do(t, "GET", ts.URL+"/sink/x?sinkDelay=10ms", nil), 200) // slot released
}

func wantStatusAsync(t *testing.T, url string, want int) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)
		return
	}
	res.Body.Close()
	if res.StatusCode != want {
		t.Errorf("GET %s = %d, want %d", url, res.StatusCode, want)
	}
}
