package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jecklgamis/http-sink/internal/sink"
)

type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, `{"error":"encoding failure"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// routeSink handles everything under /sink. p is the trailing-slash-trimmed
// path used for routing; path is the exact path used for rule lookups.
func (s *Server) routeSink(w http.ResponseWriter, r *http.Request, p, path string) {
	m := r.Method
	isRead := m == http.MethodGet || m == http.MethodHead
	switch {
	case p == "/sink/stats" && isRead:
		snap := s.stats.Compute()
		snap.Instance = s.opts.Instance
		writeJSON(w, http.StatusOK, snap)
	case p == "/sink/recent" && m == http.MethodDelete:
		s.stats.ClearRecent()
		noContent(w)
	case p == "/sink/rules" && isRead:
		writeJSON(w, http.StatusOK, s.rulesOrEmpty())
	default:
		s.echo(w, r, path)
	}
}

func (s *Server) rulesOrEmpty() []Rule {
	if s.ruleList == nil {
		return []Rule{}
	}
	return s.ruleList
}

type echoResponse struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	URL     string            `json:"url"`
	Args    map[string]any    `json:"args"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body,omitempty"`
	Origin  string            `json:"origin"`
}

type injectedFailureResponse struct {
	Method          string `json:"method"`
	Path            string `json:"path"`
	InjectedFailure bool   `json:"injectedFailure"`
	StatusCode      int    `json:"statusCode"`
}

// echo is the sink itself: after any requested (or rule-configured) delay and
// failure, it reflects the request back. A request's own settings replace the
// rule for its path, delay and status independently.
func (s *Server) echo(w http.ResponseWriter, r *http.Request, path string) {
	inj, err := parseInjection(r, s.opts.MaxDelay)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule := s.rules[path] // zero value (no injection) when the path has no rule

	var delay time.Duration
	switch {
	case inj.hasDelay():
		delay = inj.delay()
	case rule.inj.hasDelay():
		delay = rule.inj.delay()
	}
	if delay > 0 {
		if !s.acquireDelaySlot() {
			writeError(w, http.StatusTooManyRequests, errTooManyDelayed)
			return
		}
		defer s.releaseDelaySlot()
		w.Header().Set(headerInjectedDelay, formatMs(delay))
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return // client went away
		}
	}

	var code int
	switch {
	case inj.hasStatus():
		code = inj.failureStatus()
	case rule.inj.hasStatus():
		code = rule.inj.failureStatus()
	}
	if code != 0 {
		writeJSON(w, code, injectedFailureResponse{
			Method: r.Method, Path: path, InjectedFailure: true, StatusCode: code,
		})
		return
	}
	writeJSON(w, http.StatusOK, echoResponse{
		Method:  r.Method,
		Path:    path,
		URL:     scheme(r) + "://" + r.Host + requestTarget(r),
		Args:    queryArgs(r),
		Headers: headerMap(r),
		Body:    requestBody(r),
		Origin:  peerAddr(r),
	})
}

// excludedFromStats are control-plane paths that never count as sink traffic.
var excludedFromStats = map[string]bool{
	"/sink/stats":  true,
	"/sink/recent": true,
	"/sink/rules":  true,
}

func isSinkTraffic(p string) bool { return p == "/sink" || strings.HasPrefix(p, "/sink/") }

func isControlPlane(p string) bool { return excludedFromStats[p] }

// recordSinkStats records every non-control-plane /sink request once it finishes.
func (s *Server) recordSinkStats(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := rawPath(r)
		if !isSinkTraffic(path) || isControlPlane(path) {
			next.ServeHTTP(w, r)
			return
		}
		rec := asRecorder(w)
		start := time.Now()
		next.ServeHTTP(rec, r)
		if !rec.wrote {
			return // aborted before responding
		}
		elapsed := time.Since(start)
		s.metrics.Observe(r.Method, rec.status, elapsed)
		entry := sink.Entry{
			Method:         r.Method,
			Path:           path,
			RemoteAddress:  clientIP(r),
			UserAgent:      r.UserAgent(),
			Headers:        headerMap(r),
			ResponseTimeMs: elapsed.Milliseconds(),
			Duration:       elapsed,
			StatusCode:     rec.status,
		}
		if body := requestBody(r); hasBody(body) {
			entry.Body = body
		}
		s.stats.Record(entry)
	})
}
