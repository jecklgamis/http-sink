package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const tenSeconds = 10 * time.Second

func TestParseDelaySpec(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		in      string
		lo, hi  time.Duration
		wantErr bool
	}{
		{"250ms", 250 * ms, 250 * ms, false},
		{"1.5s", 1500 * ms, 1500 * ms, false},
		{"250", 250 * ms, 250 * ms, false}, // bare number = ms
		{"0.5", 500 * time.Microsecond, 500 * time.Microsecond, false},
		{"0", 0, 0, false},
		{"50ms-300ms", 50 * ms, 300 * ms, false},
		{"50-300", 50 * ms, 300 * ms, false},
		{"100ms-1s", 100 * ms, time.Second, false},
		{"10s", tenSeconds, tenSeconds, false}, // the cap itself is allowed
		{"10001ms", 0, 0, true},
		{"11s", 0, 0, true},
		{"50ms-11s", 0, 0, true},
		{"300ms-100ms", 0, 0, true}, // reversed range
		{"-5ms", 0, 0, true},
		{"abc", 0, 0, true},
		{"", 0, 0, true},
		{"ms", 0, 0, true},
		{"NaN", 0, 0, true},
		{"Inf", 0, 0, true},
		{"1-", 0, 0, true},
	}
	for _, tc := range tests {
		lo, hi, err := parseDelaySpec(tc.in, tenSeconds)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDelaySpec(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && (lo != tc.lo || hi != tc.hi) {
			t.Errorf("parseDelaySpec(%q) = %v..%v, want %v..%v", tc.in, lo, hi, tc.lo, tc.hi)
		}
	}
	if _, _, err := parseDelaySpec("2s", time.Second); err == nil {
		t.Error("the maximum should be configurable: 2s must exceed a 1s max")
	}
}

func TestParseStatuses(t *testing.T) {
	got, err := parseStatuses("503:10,429:5")
	if err != nil || len(got) != 2 || got[0].val != 503 || got[0].pct != 10 || got[1].val != 429 || got[1].pct != 5 {
		t.Fatalf("list = %+v, %v", got, err)
	}
	got, err = parseStatuses("503")
	if err != nil || len(got) != 1 || got[0].pct != 100 {
		t.Fatalf("bare status should be 100%%: %+v, %v", got, err)
	}
	if got, err = parseStatuses(" 502 : 12.5 , 504:80 "); err != nil || got[0].pct != 12.5 || got[1].val != 504 {
		t.Fatalf("whitespace/fractions: %+v, %v", got, err)
	}
	if _, err = parseStatuses("503:50,429:50"); err != nil { // exactly 100 is fine
		t.Fatalf("100%% total rejected: %v", err)
	}
	for _, bad := range []string{
		"503:60,429:60", // adds up to 120
		"503:0", "503:-5", "503:101", "503:abc", "503:",
		"200", "399", "600", "abc", "", "503,,429", ",", "503:10,",
	} {
		if _, err := parseStatuses(bad); err == nil {
			t.Errorf("parseStatuses(%q) should fail", bad)
		}
	}
}

func TestParseDelaysList(t *testing.T) {
	got, err := parseDelays("50ms-300ms:90,2s:10", tenSeconds)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
	if got[0].val.lo != 50*time.Millisecond || got[0].val.hi != 300*time.Millisecond || got[0].pct != 90 {
		t.Fatalf("first entry = %+v", got[0])
	}
	if got[1].val.lo != 2*time.Second || got[1].pct != 10 {
		t.Fatalf("second entry = %+v", got[1])
	}
	for _, bad := range []string{"1s:60,2s:60", "1s:0", "abc:10", "20s:10", "1s:10,", "1s-:10"} {
		if _, err := parseDelays(bad, tenSeconds); err == nil {
			t.Errorf("parseDelays(%q) should fail", bad)
		}
	}
}

func TestParseInjection(t *testing.T) {
	req := func(url string, headers ...string) *http.Request {
		r := httptest.NewRequest("GET", url, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		return r
	}

	inj, err := parseInjection(req("/sink/x"), tenSeconds)
	if err != nil || inj.hasDelay() || inj.hasStatus() {
		t.Fatalf("no params: %+v, %v", inj, err)
	}

	// unprefixed names, and the removed sinkFailRate, are ordinary client params
	inj, err = parseInjection(req("/sink/x?delay=5s&status=active&failRate=3&sinkFailRate=1"), tenSeconds)
	if err != nil || inj.hasDelay() || inj.hasStatus() {
		t.Fatalf("other params should be ignored: %+v, %v", inj, err)
	}

	inj, err = parseInjection(req("/sink/x?sinkStatus=503:10,429:5&sinkDelay=50ms:90,2s:10"), tenSeconds)
	if err != nil || len(inj.statuses) != 2 || len(inj.delays) != 2 {
		t.Fatalf("lists: %+v, %v", inj, err)
	}

	// headers work, and the query parameter wins over the header
	inj, err = parseInjection(req("/sink/x", "X-Sink-Delay", "100ms", "X-Sink-Status", "502"), tenSeconds)
	if err != nil || inj.delays[0].val.lo != 100*time.Millisecond || inj.statuses[0].val != 502 {
		t.Fatalf("headers: %+v, %v", inj, err)
	}
	inj, _ = parseInjection(req("/sink/x?sinkDelay=200ms", "X-Sink-Delay", "100ms"), tenSeconds)
	if inj.delays[0].val.lo != 200*time.Millisecond {
		t.Fatalf("query should win over header: %+v", inj)
	}

	for _, bad := range []string{
		"/sink/x?sinkDelay=abc", "/sink/x?sinkDelay=11s", "/sink/x?sinkDelay=1s:60,2s:60",
		"/sink/x?sinkStatus=200", "/sink/x?sinkStatus=600", "/sink/x?sinkStatus=abc", "/sink/x?sinkStatus=503:150",
	} {
		if _, err := parseInjection(req(bad), tenSeconds); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
	if _, err := parseInjection(req("/sink/x?sinkDelay=2s"), time.Second); err == nil {
		t.Error("the configured max delay should apply to requests")
	}
}

func TestPickFollowsPercentages(t *testing.T) {
	const n = 40000
	opts := []weighted[int]{{pct: 10, val: 503}, {pct: 5, val: 429}} // 85% none
	counts := map[int]int{}
	for i := 0; i < n; i++ {
		v, ok := pick(opts)
		if !ok {
			v = 0
		}
		counts[v]++
	}
	within := func(name string, got int, wantPct float64) {
		t.Helper()
		pct := float64(got) / n * 100
		if pct < wantPct-1.5 || pct > wantPct+1.5 {
			t.Errorf("%s = %.2f%%, want ~%.0f%%", name, pct, wantPct)
		}
	}
	within("503", counts[503], 10)
	within("429", counts[429], 5)
	within("none", counts[0], 85)

	always := []weighted[int]{{pct: 100, val: 500}}
	for i := 0; i < 1000; i++ {
		if v, ok := pick(always); !ok || v != 500 {
			t.Fatal("a 100% entry must always be chosen")
		}
	}
	if _, ok := pick[int](nil); ok {
		t.Fatal("an empty list never injects")
	}
}

func TestInjectionDelayWithinRange(t *testing.T) {
	inj := injection{delays: []weighted[delayRange]{{pct: 100, val: delayRange{50 * time.Millisecond, 80 * time.Millisecond}}}}
	for i := 0; i < 500; i++ {
		if d := inj.delay(); d < 50*time.Millisecond || d > 80*time.Millisecond {
			t.Fatalf("delay %v outside [50ms,80ms]", d)
		}
	}
	fixed := injection{delays: []weighted[delayRange]{{pct: 100, val: delayRange{time.Second, time.Second}}}}
	if fixed.delay() != time.Second {
		t.Fatal("fixed delay should be exact")
	}
	if (injection{}).delay() != 0 || (injection{}).failureStatus() != 0 {
		t.Fatal("no injection configured must mean no delay and no failure")
	}
}
