package server

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Per-request injection. A client can ask for a delay and/or a failure on a
// single request, via query parameters or headers (query wins). The names are
// prefixed so they can't collide with parameters real clients already send.
//
//	sinkDelay    / X-Sink-Delay      250ms | 1.5s | 250 (ms) | 50ms-300ms (random range)
//	sinkStatus   / X-Sink-Status     400-599; with no sinkFailRate the request always fails
//	sinkFailRate / X-Sink-Fail-Rate  0-1; with no sinkStatus the failure is a 500
//
// A request's own settings replace the path config (/sink/config/*) for that request.
const (
	maxInjectedDelay = 10 * time.Second
	// DefaultMaxDelayedRequests bounds how many requests may be sleeping at once.
	DefaultMaxDelayedRequests = 500

	paramDelay, headerDelay       = "sinkDelay", "X-Sink-Delay"
	paramStatus, headerStatus     = "sinkStatus", "X-Sink-Status"
	paramFailRate, headerFailRate = "sinkFailRate", "X-Sink-Fail-Rate"
	defaultInjectedStatus         = http.StatusInternalServerError
	headerInjectedDelay           = "X-Sink-Injected-Delay-Ms"
	errTooManyDelayed             = "too many delayed requests in flight; try again shortly"
	delayHint                     = "must be a duration like 250ms or 1.5s, a number of ms, or a range like 50ms-300ms"
	msPerNs                       = float64(time.Millisecond)
)

// injection is the per-request injection parsed from a request.
type injection struct {
	hasDelay           bool
	delayMin, delayMax time.Duration

	hasFailure bool
	failRate   float64
	failStatus int
}

// delay picks the delay to apply: fixed, or random within the requested range.
func (i injection) delay() time.Duration {
	if i.delayMax <= i.delayMin {
		return i.delayMin
	}
	return i.delayMin + time.Duration(rand.Float64()*float64(i.delayMax-i.delayMin))
}

// failureStatus rolls the dice; it returns 0 when the request should succeed.
func (i injection) failureStatus() int {
	if rand.Float64() < i.failRate {
		return i.failStatus
	}
	return 0
}

// requestValue returns the query parameter, falling back to the header.
func requestValue(r *http.Request, param, header string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(param)); v != "" {
		return v
	}
	return strings.TrimSpace(r.Header.Get(header))
}

// parseInjection reads and validates the injection settings of a request.
func parseInjection(r *http.Request) (injection, error) {
	var inj injection

	if v := requestValue(r, paramDelay, headerDelay); v != "" {
		lo, hi, err := parseDelaySpec(v)
		if err != nil {
			return inj, fmt.Errorf("%s %s (max %s): %v", paramDelay, delayHint, maxInjectedDelay, err)
		}
		inj.hasDelay, inj.delayMin, inj.delayMax = true, lo, hi
	}

	statusStr := requestValue(r, paramStatus, headerStatus)
	rateStr := requestValue(r, paramFailRate, headerFailRate)
	if statusStr != "" || rateStr != "" {
		inj.hasFailure = true
		inj.failStatus, inj.failRate = defaultInjectedStatus, 1
		if statusStr != "" {
			code, err := strconv.Atoi(statusStr)
			if err != nil || code < 400 || code > 599 {
				return inj, fmt.Errorf("%s must be an integer between 400 and 599", paramStatus)
			}
			inj.failStatus = code
		}
		if rateStr != "" {
			rate, err := strconv.ParseFloat(rateStr, 64)
			if err != nil || !isFinite(rate) || rate < 0 || rate > 1 {
				return inj, fmt.Errorf("%s must be a number between 0 and 1", paramFailRate)
			}
			inj.failRate = rate
		}
	}
	return inj, nil
}

// parseDelaySpec parses "250ms", "1.5s", "250" (milliseconds) or a range such
// as "50ms-300ms". For a fixed delay lo == hi.
func parseDelaySpec(s string) (lo, hi time.Duration, err error) {
	loStr, hiStr, isRange := strings.Cut(s, "-")
	if lo, err = parseDelay(loStr); err != nil {
		return 0, 0, err
	}
	hi = lo
	if isRange {
		if hi, err = parseDelay(hiStr); err != nil {
			return 0, 0, err
		}
		if hi < lo {
			return 0, 0, fmt.Errorf("range start %s is after its end %s", lo, hi)
		}
	}
	if hi > maxInjectedDelay {
		return 0, 0, fmt.Errorf("%s exceeds the maximum", hi)
	}
	return lo, hi, nil
}

// parseDelay parses one duration; a bare number means milliseconds.
func parseDelay(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if ms, err := strconv.ParseFloat(s, 64); err == nil {
		if !isFinite(ms) || ms < 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(ms * msPerNs), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

// acquireDelaySlot reserves one of the bounded delayed-request slots.
func (s *Server) acquireDelaySlot() bool {
	select {
	case s.delaySlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseDelaySlot() { <-s.delaySlots }

// formatMs renders a duration as milliseconds for the injected-delay header.
func formatMs(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/msPerNs, 'f', -1, 64)
}
