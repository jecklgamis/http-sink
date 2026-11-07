package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
		{"10s", 10 * time.Second, 10 * time.Second, false}, // the cap itself is allowed
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
		lo, hi, err := parseDelaySpec(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDelaySpec(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && (lo != tc.lo || hi != tc.hi) {
			t.Errorf("parseDelaySpec(%q) = %v..%v, want %v..%v", tc.in, lo, hi, tc.lo, tc.hi)
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

	inj, err := parseInjection(req("/sink/x"))
	if err != nil || inj.hasDelay || inj.hasFailure {
		t.Fatalf("no params: %+v, %v", inj, err)
	}

	// unprefixed names are ordinary client params and must be ignored
	inj, err = parseInjection(req("/sink/x?delay=5s&status=active&failRate=3"))
	if err != nil || inj.hasDelay || inj.hasFailure {
		t.Fatalf("unprefixed params should be ignored: %+v, %v", inj, err)
	}

	inj, err = parseInjection(req("/sink/x?sinkStatus=503"))
	if err != nil || !inj.hasFailure || inj.failStatus != 503 || inj.failRate != 1 {
		t.Fatalf("status only: %+v, %v", inj, err)
	}

	inj, err = parseInjection(req("/sink/x?sinkFailRate=0.25"))
	if err != nil || !inj.hasFailure || inj.failStatus != 500 || inj.failRate != 0.25 {
		t.Fatalf("rate only: %+v, %v", inj, err)
	}

	// headers work, and the query parameter wins over the header
	inj, err = parseInjection(req("/sink/x", "X-Sink-Delay", "100ms", "X-Sink-Status", "502"))
	if err != nil || inj.delayMin != 100*time.Millisecond || inj.failStatus != 502 {
		t.Fatalf("headers: %+v, %v", inj, err)
	}
	inj, _ = parseInjection(req("/sink/x?sinkDelay=200ms", "X-Sink-Delay", "100ms"))
	if inj.delayMin != 200*time.Millisecond {
		t.Fatalf("query should win over header: %+v", inj)
	}

	for _, bad := range []string{
		"/sink/x?sinkDelay=abc", "/sink/x?sinkDelay=11s",
		"/sink/x?sinkStatus=200", "/sink/x?sinkStatus=600", "/sink/x?sinkStatus=abc",
		"/sink/x?sinkFailRate=1.5", "/sink/x?sinkFailRate=-0.1", "/sink/x?sinkFailRate=abc", "/sink/x?sinkFailRate=NaN",
	} {
		if _, err := parseInjection(req(bad)); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func TestInjectionDelayWithinRange(t *testing.T) {
	inj := injection{hasDelay: true, delayMin: 50 * time.Millisecond, delayMax: 80 * time.Millisecond}
	for i := 0; i < 500; i++ {
		if d := inj.delay(); d < inj.delayMin || d > inj.delayMax {
			t.Fatalf("delay %v outside [%v,%v]", d, inj.delayMin, inj.delayMax)
		}
	}
	fixed := injection{hasDelay: true, delayMin: time.Second, delayMax: time.Second}
	if fixed.delay() != time.Second {
		t.Fatal("fixed delay should be exact")
	}
}

func TestInjectionFailureRate(t *testing.T) {
	always := injection{hasFailure: true, failRate: 1, failStatus: 503}
	never := injection{hasFailure: true, failRate: 0, failStatus: 503}
	for i := 0; i < 200; i++ {
		if always.failureStatus() != 503 || never.failureStatus() != 0 {
			t.Fatal("rate 1 must always fail and rate 0 must never fail")
		}
	}
}
