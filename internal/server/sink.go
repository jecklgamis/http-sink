package server

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jecklgamis/http-sink/internal/sink"
)

var projectNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const configPathHint = "path must be a subpath under /sink/<project>/, e.g. /sink/my-project/my-scenario"

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
// path used for routing; path is the exact path used for latency/failure lookups.
func (s *Server) routeSink(w http.ResponseWriter, r *http.Request, p, path string) {
	m := r.Method
	switch {
	case p == "/sink/stats" && (m == http.MethodGet || m == http.MethodHead):
		writeJSON(w, http.StatusOK, s.stats.Compute())
	case p == "/sink/recent" && m == http.MethodDelete:
		s.stats.ClearRecent()
		noContent(w)

	case p == "/sink/projects" && m == http.MethodPost:
		s.createProject(w, r)
	case p == "/sink/projects" && (m == http.MethodGet || m == http.MethodHead):
		writeJSON(w, http.StatusOK, s.projects.List())
	case strings.HasPrefix(p, "/sink/projects/") && m == http.MethodDelete && !strings.Contains(p[len("/sink/projects/"):], "/"):
		s.deleteProject(w, r, unescape(p[len("/sink/projects/"):]))

	case p == "/sink/config/latency" && (m == http.MethodGet || m == http.MethodHead):
		writeJSON(w, http.StatusOK, s.latency.List())
	case p == "/sink/config/latency" && m == http.MethodPost:
		s.setLatency(w, r)
	case p == "/sink/config/latency" && m == http.MethodDelete:
		s.deleteConfig(w, r, s.latency.Remove)

	case p == "/sink/config/failure" && (m == http.MethodGet || m == http.MethodHead):
		writeJSON(w, http.StatusOK, s.failure.List())
	case p == "/sink/config/failure" && m == http.MethodPost:
		s.setFailure(w, r)
	case p == "/sink/config/failure" && m == http.MethodDelete:
		s.deleteConfig(w, r, s.failure.Remove)

	default:
		s.echo(w, r, path)
	}
}

// unescape percent-decodes a path segment, leaving undecodable input as-is.
func unescape(seg string) string {
	if dec, err := url.PathUnescape(seg); err == nil {
		return dec
	}
	return seg
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	name, _ := bodyObject(r)["name"].(string)
	if !projectNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, "name must be 1-64 chars of letters, digits, - or _")
		return
	}
	token, ok := s.projects.Create(name)
	if !ok {
		writeError(w, http.StatusConflict, "project '"+name+"' already exists")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}{name, token})
}

type authError struct {
	status int
	msg    string
}

// authorizeProject checks that the project exists and the request carries its token.
func (s *Server) authorizeProject(r *http.Request, name string) *authError {
	if !s.projects.Exists(name) {
		return &authError{http.StatusNotFound,
			"project '" + name + "' does not exist -- create it first via POST /sink/projects"}
	}
	token := r.Header.Get("X-Sink-Token")
	if token == "" || !s.projects.Verify(name, token) {
		return &authError{http.StatusForbidden, "invalid or missing token for project '" + name + "'"}
	}
	return nil
}

// authorizeConfigPath validates that path lives under /sink/<project> and the
// caller owns that project.
func (s *Server) authorizeConfigPath(r *http.Request, path string) *authError {
	if !strings.HasPrefix(path, "/sink/") {
		return &authError{http.StatusBadRequest, configPathHint}
	}
	project, _, _ := strings.Cut(path[len("/sink/"):], "/")
	if project == "" {
		return &authError{http.StatusBadRequest, configPathHint}
	}
	return s.authorizeProject(r, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, name string) {
	if e := s.authorizeProject(r, name); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	prefix := "/sink/" + name
	under := func(p string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }
	for _, c := range s.latency.List() {
		if under(c.Path) {
			s.latency.Remove(c.Path)
		}
	}
	for _, c := range s.failure.List() {
		if under(c.Path) {
			s.failure.Remove(c.Path)
		}
	}
	s.projects.Remove(name)
	noContent(w)
}

func (s *Server) setLatency(w http.ResponseWriter, r *http.Request) {
	body := bodyObject(r)
	path, _ := body["path"].(string)
	if e := s.authorizeConfigPath(r, path); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	jitter, ok := toNumber(body, "jitterMs")
	if !ok || !isFinite(jitter) || jitter < 0 {
		writeError(w, http.StatusBadRequest, "jitterMs must be a non-negative number")
		return
	}
	s.latency.Set(path, jitter)
	writeJSON(w, http.StatusCreated, sink.LatencyConfig{Path: path, JitterMs: jitter})
}

func (s *Server) setFailure(w http.ResponseWriter, r *http.Request) {
	body := bodyObject(r)
	path, _ := body["path"].(string)
	if e := s.authorizeConfigPath(r, path); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	rate, ok := toNumber(body, "rate")
	if !ok || !isFinite(rate) || rate < 0 || rate > 1 {
		writeError(w, http.StatusBadRequest, "rate must be a number between 0 and 1")
		return
	}
	code, ok := toNumber(body, "statusCode")
	if !ok || !isFinite(code) || code != math.Trunc(code) || code < 400 || code > 599 {
		writeError(w, http.StatusBadRequest, "statusCode must be an integer between 400 and 599")
		return
	}
	s.failure.Set(path, rate, int(code))
	writeJSON(w, http.StatusCreated, sink.FailureConfig{Path: path, Rate: rate, StatusCode: int(code)})
}

// deleteConfig removes a latency/failure config selected by the ?path= query param.
func (s *Server) deleteConfig(w http.ResponseWriter, r *http.Request, remove func(string)) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path query param is required")
		return
	}
	if e := s.authorizeConfigPath(r, path); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	remove(path)
	noContent(w)
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

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

// echo is the sink itself: after any configured delay and failure roll for
// the exact path, it reflects the request back.
func (s *Server) echo(w http.ResponseWriter, r *http.Request, path string) {
	if delay := s.latency.DelayFor(path); delay > 0 {
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return // client went away
		}
	}
	if code := s.failure.StatusCodeFor(path); code != 0 {
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
	"/sink/stats":          true,
	"/sink/recent":         true,
	"/sink/config/latency": true,
	"/sink/config/failure": true,
}

func isSinkTraffic(p string) bool { return p == "/sink" || strings.HasPrefix(p, "/sink/") }

func isControlPlane(p string) bool {
	return excludedFromStats[p] || p == "/sink/projects" || strings.HasPrefix(p, "/sink/projects/")
}

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
