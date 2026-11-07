package sink

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStatsWindowAndHistory(t *testing.T) {
	now := time.Unix(10_000, 0)
	s := NewStats()
	s.now = func() time.Time { return now }

	s.Record(Entry{Method: "GET", Path: "/sink/a"})
	s.Record(Entry{Method: "POST", Path: "/sink/a"})
	now = now.Add(500 * time.Millisecond)
	s.Record(Entry{Method: "GET", Path: "/sink/b"})
	now = now.Add(time.Millisecond) // a sample stamped exactly "now" is not in a completed bucket yet

	snap := s.Compute()
	w := snap.Windows["1s"]
	if w.Total != 3 || w.Rps != 3 {
		t.Fatalf("total/rps = %d/%v, want 3/3", w.Total, w.Rps)
	}
	if w.ByMethod["GET"] != 2 || w.ByPath["/sink/a"] != 2 || w.ByPathAndMethod["/sink/a"]["POST"] != 1 {
		t.Fatalf("unexpected breakdown: %+v", w)
	}
	if len(snap.RpsHistory) != 30 {
		t.Fatalf("history len = %d, want 30", len(snap.RpsHistory))
	}
	// both groups fall in the newest completed one-second bucket
	if got := snap.RpsHistory[29]; got != 3 {
		t.Fatalf("newest bucket = %d, want 3", got)
	}

	// 2s later the 1s window is empty but history still sees the requests
	now = now.Add(2 * time.Second)
	snap = s.Compute()
	if snap.Windows["1s"].Total != 0 {
		t.Fatalf("1s total = %d, want 0", snap.Windows["1s"].Total)
	}
	sum := 0
	for _, c := range snap.RpsHistory {
		sum += c
	}
	if sum != 3 {
		t.Fatalf("history sum = %d, want 3", sum)
	}

	// everything ages out after 30s
	now = now.Add(31 * time.Second)
	for _, c := range s.Compute().RpsHistory {
		if c != 0 {
			t.Fatal("expected empty history after max age")
		}
	}
}

func TestStatsRecentIsCappedNewestFirstAndClearable(t *testing.T) {
	s := NewStats()
	for i := 0; i < 15; i++ {
		s.Record(Entry{Method: "GET", Path: "/sink/x", StatusCode: 200 + i})
	}
	recent := s.Compute().RecentRequests
	if len(recent) != 10 {
		t.Fatalf("recent len = %d, want 10", len(recent))
	}
	if recent[0].StatusCode != 214 || recent[9].StatusCode != 205 {
		t.Fatalf("recent not newest-first: first=%d last=%d", recent[0].StatusCode, recent[9].StatusCode)
	}
	s.ClearRecent()
	if got := s.Compute().RecentRequests; got == nil || len(got) != 0 {
		t.Fatalf("recent after clear = %#v, want empty non-nil", got)
	}
}

func TestLatencyDelayFor(t *testing.T) {
	l := NewLatency()
	if d := l.DelayFor("/sink/p/x"); d != 0 {
		t.Fatalf("unconfigured delay = %v, want 0", d)
	}
	l.Set("/sink/p/x", 50)
	for i := 0; i < 200; i++ {
		if d := l.DelayFor("/sink/p/x"); d < 0 || d >= 50*time.Millisecond {
			t.Fatalf("delay %v outside [0,50ms)", d)
		}
	}
	if d := l.DelayFor("/sink/p/other"); d != 0 {
		t.Fatalf("other path delay = %v, want 0", d)
	}
	l.Remove("/sink/p/x")
	if len(l.List()) != 0 {
		t.Fatal("expected empty list after remove")
	}
}

func TestFailureStatusCodeFor(t *testing.T) {
	f := NewFailure()
	f.Set("/sink/p/always", 1, 503)
	f.Set("/sink/p/never", 0, 500)
	for i := 0; i < 100; i++ {
		if got := f.StatusCodeFor("/sink/p/always"); got != 503 {
			t.Fatalf("rate 1 gave %d, want 503", got)
		}
		if got := f.StatusCodeFor("/sink/p/never"); got != 0 {
			t.Fatalf("rate 0 gave %d, want 0", got)
		}
	}
	if got := f.StatusCodeFor("/sink/p/unset"); got != 0 {
		t.Fatalf("unset path gave %d, want 0", got)
	}
	if len(f.List()) != 2 {
		t.Fatalf("list len = %d, want 2", len(f.List()))
	}
}

func TestStatsLatencyPercentiles(t *testing.T) {
	now := time.Unix(10_000, 0)
	s := NewStats()
	s.now = func() time.Time { return now }

	// 99 fast requests on /sink/fast, one slow outlier on /sink/slow
	for i := 0; i < 99; i++ {
		s.Record(Entry{Method: "GET", Path: "/sink/fast", Duration: 2 * time.Millisecond})
	}
	s.Record(Entry{Method: "GET", Path: "/sink/slow", Duration: 500 * time.Millisecond})

	lat := s.Compute().Latency
	if lat.WindowSeconds != 30 || lat.All.Count != 100 {
		t.Fatalf("window/count = %d/%d, want 30/100", lat.WindowSeconds, lat.All.Count)
	}
	near := func(name string, got, want float64) {
		t.Helper()
		if got < want*0.98 || got > want*1.02 { // histogram is 1% precise
			t.Fatalf("%s = %v, want ~%v", name, got, want)
		}
	}
	near("p50", lat.All.P50Ms, 2)
	near("max", lat.All.MaxMs, 500)
	if lat.All.P99Ms > 3 { // 99th of 100 samples is still a fast one
		t.Fatalf("p99 = %v, want ~2", lat.All.P99Ms)
	}
	near("fast p50", lat.ByPath["/sink/fast"].P50Ms, 2)
	near("slow max", lat.ByPath["/sink/slow"].MaxMs, 500)
}

func TestStatsLatencyByPathIsCappedToBusiest(t *testing.T) {
	s := NewStats()
	for i := 0; i < 25; i++ {
		for n := 0; n <= i; n++ {
			s.Record(Entry{Method: "GET", Path: fmt.Sprintf("/sink/p%02d", i), Duration: time.Millisecond})
		}
	}
	lat := s.Compute().Latency
	if len(lat.ByPath) != 10 {
		t.Fatalf("byPath len = %d, want 10", len(lat.ByPath))
	}
	if _, ok := lat.ByPath["/sink/p24"]; !ok {
		t.Fatal("busiest path missing")
	}
	if _, ok := lat.ByPath["/sink/p00"]; ok {
		t.Fatal("quietest path should have been dropped")
	}
}

func TestStatsLatencyEmpty(t *testing.T) {
	lat := NewStats().Compute().Latency
	if lat.All.Count != 0 || lat.ByPath == nil {
		t.Fatalf("empty latency = %+v", lat)
	}
}

func TestMetricsPrometheusOutput(t *testing.T) {
	m := NewMetrics()
	m.Observe("GET", 200, 300*time.Microsecond) // <= 0.0005
	m.Observe("GET", 200, 3*time.Millisecond)   // <= 0.005
	m.Observe("POST", 503, 2*time.Second)       // <= 2.5
	m.Observe("BREW", 200, time.Millisecond)    // unknown method -> OTHER

	var buf strings.Builder
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`http_sink_requests_total{method="GET",code="2xx"} 2`,
		`http_sink_requests_total{method="POST",code="5xx"} 1`,
		`http_sink_requests_total{method="OTHER",code="2xx"} 1`,
		`http_sink_request_duration_seconds_bucket{le="0.0005"} 1`,
		`http_sink_request_duration_seconds_bucket{le="0.005"} 3`,
		`http_sink_request_duration_seconds_bucket{le="2.5"} 4`,
		`http_sink_request_duration_seconds_bucket{le="+Inf"} 4`,
		`http_sink_request_duration_seconds_count 4`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestConfigExpiresAfterTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLatency()
	l.store.now = func() time.Time { return now }

	cfg, err := l.Set("/sink/a", 50)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(ConfigTTL).UnixMilli(); cfg.ExpiresAt != want {
		t.Fatalf("expiresAt = %d, want %d", cfg.ExpiresAt, want)
	}

	now = now.Add(ConfigTTL - time.Second)
	if len(l.List()) != 1 || l.store.items["/sink/a"].val != 50 {
		t.Fatal("config should still be live just before expiry")
	}

	now = now.Add(2 * time.Second)
	if len(l.List()) != 0 {
		t.Fatal("config should be gone after TTL")
	}
	if d := l.DelayFor("/sink/a"); d != 0 {
		t.Fatalf("expired config still delaying: %v", d)
	}
}

func TestSetRefreshesTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	f := NewFailure()
	f.store.now = func() time.Time { return now }

	f.Set("/sink/a", 1, 500)
	now = now.Add(ConfigTTL - time.Minute)
	f.Set("/sink/a", 1, 503) // refresh + update
	now = now.Add(ConfigTTL - time.Minute)
	if got := f.StatusCodeFor("/sink/a"); got != 503 {
		t.Fatalf("status = %d, want 503 (TTL should have been refreshed)", got)
	}
}

func TestConfigCap(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLatency()
	l.store.now = func() time.Time { return now }

	for i := 0; i < MaxConfigs; i++ {
		if _, err := l.Set(fmt.Sprintf("/sink/p%d", i), 10); err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
	}
	if _, err := l.Set("/sink/overflow", 10); err != ErrTooManyConfigs {
		t.Fatalf("err = %v, want ErrTooManyConfigs", err)
	}
	// updating an existing path is still allowed at the cap
	if _, err := l.Set("/sink/p0", 99); err != nil {
		t.Fatalf("update at cap: %v", err)
	}
	// removing frees a slot
	l.Remove("/sink/p1")
	if _, err := l.Set("/sink/overflow", 10); err != nil {
		t.Fatalf("set after remove: %v", err)
	}
	// expiry frees slots too
	now = now.Add(ConfigTTL + time.Second)
	if _, err := l.Set("/sink/after-expiry", 10); err != nil {
		t.Fatalf("set after expiry: %v", err)
	}
	if n := len(l.List()); n != 1 {
		t.Fatalf("live configs = %d, want 1", n)
	}
}
