package server

import (
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fault injection is stateless: a client asks for a delay and/or a failure on
// a single request, via query parameters or headers (the query wins). The names
// are prefixed so they can't collide with parameters real clients already send.
//
//	sinkDelay  / X-Sink-Delay    250ms | 1.5s | 250 (ms) | 50ms-300ms (random range)
//	sinkStatus / X-Sink-Status   503 | 404
//
// Either can be a probability list, as in Fortio: "value:percent,value:percent".
// Each entry is the chance (0-100) that this value is used; the percentages must
// add up to at most 100 and the remainder means "no injection". An entry with no
// percentage is 100%.
//
//	sinkStatus=503:10,429:5          10% get a 503, 5% get a 429, the rest succeed
//	sinkDelay=50ms-300ms:90,2s:10    90% wait 50-300ms, 10% wait 2s
//
// The same syntax is used by the static rules file (see rules.go). A request's
// own settings replace the rule for its path.
const (
	// DefaultMaxDelay is the longest delay a request or rule may ask for.
	DefaultMaxDelay = 10 * time.Second
	// DefaultMaxDelayedRequests bounds how many requests may be sleeping at once.
	DefaultMaxDelayedRequests = 500

	paramDelay, headerDelay   = "sinkDelay", "X-Sink-Delay"
	paramStatus, headerStatus = "sinkStatus", "X-Sink-Status"
	headerInjectedDelay       = "X-Sink-Injected-Delay-Ms"
	errTooManyDelayed         = "too many delayed requests in flight; try again shortly"
	delayHint                 = "must be a duration like 250ms or 1.5s, a number of ms, or a range like 50ms-300ms, optionally as a list with percentages like 50ms:90,2s:10"
	statusHint                = "must be a status code between 400 and 599, optionally as a list with percentages like 503:10,429:5"
	msPerNs                   = float64(time.Millisecond)
)

type delayRange struct{ lo, hi time.Duration }

// weighted is one entry of a probability list; pct is in (0, 100].
type weighted[T any] struct {
	pct float64
	val T
}

// injection holds parsed delay and status distributions. A nil slice means the
// setting was not specified.
type injection struct {
	delays   []weighted[delayRange]
	statuses []weighted[int]
}

func (i injection) hasDelay() bool  { return len(i.delays) > 0 }
func (i injection) hasStatus() bool { return len(i.statuses) > 0 }

// pick rolls the dice over a probability list; ok is false when the roll
// lands in the unassigned remainder (no injection).
func pick[T any](opts []weighted[T]) (v T, ok bool) {
	r := rand.Float64() * 100
	var cum float64
	for _, o := range opts {
		cum += o.pct
		if r < cum {
			return o.val, true
		}
	}
	return v, false
}

// delay returns the delay to apply, or 0 for none.
func (i injection) delay() time.Duration {
	d, ok := pick(i.delays)
	if !ok {
		return 0
	}
	if d.hi <= d.lo {
		return d.lo
	}
	return d.lo + time.Duration(rand.Float64()*float64(d.hi-d.lo))
}

// failureStatus returns the status to fail with, or 0 for none.
func (i injection) failureStatus() int {
	code, _ := pick(i.statuses)
	return code
}

// requestValue returns the query parameter, falling back to the header.
func requestValue(r *http.Request, param, header string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(param)); v != "" {
		return v
	}
	return strings.TrimSpace(r.Header.Get(header))
}

// parseInjection reads and validates the injection settings of a request.
func parseInjection(r *http.Request, maxDelay time.Duration) (injection, error) {
	var inj injection
	var err error
	if v := requestValue(r, paramDelay, headerDelay); v != "" {
		if inj.delays, err = parseDelays(v, maxDelay); err != nil {
			return injection{}, fmt.Errorf("%s %s (max %s): %v", paramDelay, delayHint, maxDelay, err)
		}
	}
	if v := requestValue(r, paramStatus, headerStatus); v != "" {
		if inj.statuses, err = parseStatuses(v); err != nil {
			return injection{}, fmt.Errorf("%s %s: %v", paramStatus, statusHint, err)
		}
	}
	return inj, nil
}

// parseWeighted parses "value[:percent],value[:percent],...".
func parseWeighted[T any](s string, parseVal func(string) (T, error)) ([]weighted[T], error) {
	var out []weighted[T]
	var total float64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty entry")
		}
		spec, pct := part, 100.0
		if i := strings.LastIndex(part, ":"); i >= 0 {
			spec = part[:i]
			p, err := strconv.ParseFloat(strings.TrimSpace(part[i+1:]), 64)
			if err != nil || !isFinite(p) || p <= 0 || p > 100 {
				return nil, fmt.Errorf("invalid percentage in %q (must be above 0 and at most 100)", part)
			}
			pct = p
		}
		v, err := parseVal(strings.TrimSpace(spec))
		if err != nil {
			return nil, err
		}
		total += pct
		out = append(out, weighted[T]{pct: pct, val: v})
	}
	if total > 100+1e-9 {
		return nil, fmt.Errorf("percentages add up to %g, must be at most 100", total)
	}
	return out, nil
}

func parseDelays(s string, maxDelay time.Duration) ([]weighted[delayRange], error) {
	return parseWeighted(s, func(spec string) (delayRange, error) {
		lo, hi, err := parseDelaySpec(spec, maxDelay)
		return delayRange{lo, hi}, err
	})
}

func parseStatuses(s string) ([]weighted[int], error) {
	return parseWeighted(s, func(spec string) (int, error) {
		code, err := strconv.Atoi(spec)
		if err != nil || code < 400 || code > 599 {
			return 0, fmt.Errorf("invalid status %q", spec)
		}
		return code, nil
	})
}

// parseDelaySpec parses "250ms", "1.5s", "250" (milliseconds) or a range such
// as "50ms-300ms". For a fixed delay lo == hi.
func parseDelaySpec(s string, maxDelay time.Duration) (lo, hi time.Duration, err error) {
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
	if hi > maxDelay {
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

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

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
