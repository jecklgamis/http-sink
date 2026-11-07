package integration

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jecklgamis/http-sink/internal/ratelimit"
	"github.com/jecklgamis/http-sink/internal/server"
)

func TestGlobalRateLimitReturns429(t *testing.T) {
	ts := httptest.NewServer(server.New(server.Options{RateLimiter: ratelimit.New(3, time.Hour)}))
	defer ts.Close()

	for i := 0; i < 3; i++ {
		wantStatus(t, do(t, "GET", ts.URL+"/probe/live", nil), 200)
	}
	res := do(t, "GET", ts.URL+"/probe/live", nil)
	wantStatus(t, res, 429)
	if _, ok := res.JSON(t)["error"]; !ok {
		t.Fatalf("429 body should have an error field: %s", res.Raw)
	}
}
