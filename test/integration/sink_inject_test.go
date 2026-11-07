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

func TestRequestDelayList(t *testing.T) {
	base := newApp(t)
	const n = 300
	delayed := 0
	for i := 0; i < n; i++ {
		res := do(t, "GET", base+"/sink/x?sinkDelay=1ms:30", nil) // 30% wait 1ms, the rest don't
		if res.Header.Get("X-Sink-Injected-Delay-Ms") != "" {
			delayed++
		}
	}
	if delayed < n*15/100 || delayed > n*45/100 {
		t.Fatalf("%d/%d delayed at 30%%", delayed, n)
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
		"sinkDelay=abc", "sinkDelay=11s", "sinkDelay=300ms-100ms", "sinkDelay=-5ms", "sinkDelay=1s:60,2s:60",
		"sinkStatus=200", "sinkStatus=abc", "sinkStatus=503:150", "sinkStatus=503:60,429:60",
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

	res := do(t, "GET", base+"/sink/x?sinkStatus=503", nil) // a bare status always fails
	wantStatus(t, res, 503)
	if got := res.JSON(t); got["injectedFailure"] != true || got["statusCode"] != float64(503) {
		t.Fatalf("body = %s", res.Raw)
	}
	wantStatus(t, do(t, "GET", base+"/sink/x?sinkStatus=504:100", nil), 504)
	wantStatus(t, do(t, "GET", base+"/sink/x", nil, withHeader("X-Sink-Status", "502")), 502)
}

func TestRequestFailureProbabilityList(t *testing.T) {
	base := newApp(t)
	const n = 600
	counts := map[int]int{}
	for i := 0; i < n; i++ {
		counts[do(t, "GET", base+"/sink/x?sinkStatus=503:20,429:20", nil).Status]++
	}
	// expected 20% / 20% / 60% (sd ~ 3% of n); bounds are wide enough to never flake
	for status, want := range map[int]float64{503: 0.20, 429: 0.20, 200: 0.60} {
		got := float64(counts[status]) / n
		if got < want-0.10 || got > want+0.10 {
			t.Fatalf("status %d = %.0f%%, want ~%.0f%% (counts %v)", status, got*100, want*100, counts)
		}
	}
	if len(counts) != 3 {
		t.Fatalf("unexpected statuses: %v", counts)
	}
}

func TestOrdinaryClientParamsAreNotInterpreted(t *testing.T) {
	base := newApp(t)
	// includes the removed sinkFailRate: no longer special, so it is just echoed
	res := do(t, "GET", base+"/sink/webhook?status=active&delay=5s&failRate=9&sinkFailRate=1", nil)
	wantStatus(t, res, 200)
	args := res.JSON(t)["args"].(map[string]any)
	if args["status"] != "active" || args["delay"] != "5s" || args["sinkFailRate"] != "1" {
		t.Fatalf("args = %v", args)
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

	wantStatus(t, do(t, "GET", ts.URL+"/sink/x?sinkDelay=50ms", nil), 429)
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
