package ratelimit

import (
	"testing"
	"time"
)

func TestAllowsLimitThenRejects(t *testing.T) {
	l := New(1000, time.Second)
	for i := 0; i < 1000; i++ {
		if !l.Allow() {
			t.Fatalf("request %d rejected, want allowed", i+1)
		}
	}
	if l.Allow() {
		t.Fatal("request 1001 allowed, want rejected")
	}
}

func TestWindowRollsOver(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newWithClock(2, time.Second, func() time.Time { return now })
	l.Allow()
	l.Allow()
	if l.Allow() {
		t.Fatal("third request in window allowed")
	}
	now = now.Add(time.Second)
	if !l.Allow() {
		t.Fatal("request in new window rejected")
	}
}

func TestReset(t *testing.T) {
	l := New(1, time.Hour)
	l.Allow()
	if l.Allow() {
		t.Fatal("expected rejection before reset")
	}
	l.Reset()
	if !l.Allow() {
		t.Fatal("expected allow after reset")
	}
}
