// Package server wires the HTTP surface of http-sink: middleware, routing,
// the /sink endpoints and the dashboard.
package server

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/jecklgamis/http-sink/internal/buildinfo"
	"github.com/jecklgamis/http-sink/internal/ratelimit"
	"github.com/jecklgamis/http-sink/internal/sink"
	"github.com/jecklgamis/http-sink/internal/statsd"
	"github.com/jecklgamis/http-sink/web"
)

const (
	// DefaultRateLimit is the global request budget per second.
	DefaultRateLimit = 1000
	indexMessage     = "A simple request sink that accepts any method or subpath under /sink, echoes it back, and lets you simulate latency and failures while watching live traffic stats."
)

// Options configures a Server. The zero value is usable.
type Options struct {
	// RateLimiter is the global limiter; defaults to DefaultRateLimit per second.
	RateLimiter *ratelimit.Limiter
	// Statsd receives per-endpoint timing and status counters; nil disables them.
	Statsd *statsd.Client
	// AccessLog receives Apache "combined" format lines; nil disables logging.
	AccessLog io.Writer
	// BuildInfo is served at /build-info.
	BuildInfo buildinfo.Info
	// Development adds error details to error pages.
	Development bool
	// MaxDelayedRequests bounds how many requests may be sleeping on an injected
	// delay at once; defaults to DefaultMaxDelayedRequests.
	MaxDelayedRequests int
}

// Server is the http-sink application; it implements http.Handler.
type Server struct {
	opts    Options
	stats   *sink.Stats
	metrics *sink.Metrics
	latency *sink.Latency
	failure *sink.Failure
	tmpl    *template.Template
	static  fs.FS
	handler http.Handler
	// delaySlots is a counting semaphore for in-flight delayed requests.
	delaySlots  chan struct{}
	persistence *sink.Persistence
}

// New builds a Server with fresh in-memory state.
func New(opts Options) *Server {
	if opts.RateLimiter == nil {
		opts.RateLimiter = ratelimit.New(DefaultRateLimit, time.Second)
	}
	if opts.MaxDelayedRequests <= 0 {
		opts.MaxDelayedRequests = DefaultMaxDelayedRequests
	}
	s := &Server{
		delaySlots: make(chan struct{}, opts.MaxDelayedRequests),
		opts:       opts,
		stats:      sink.NewStats(),
		metrics:    sink.NewMetrics(),
		latency:    sink.NewLatency(),
		failure:    sink.NewFailure(),
		tmpl:       template.Must(template.ParseFS(web.Templates, "templates/*.html")),
		static:     web.Public(),
	}
	// Outermost first: this order mirrors the request flow.
	s.handler = chain(http.HandlerFunc(s.route),
		s.rateLimit,
		s.accessLog,
		s.parseBody,
		s.serveStatic,
		s.statsdMetrics,
		s.recordSinkStats,
	)
	return s
}

// EnablePersistence restores the latency and failure configs from dir and keeps
// the files up to date as they change. Call Close to flush on shutdown.
func (s *Server) EnablePersistence(dir string) error {
	p, err := sink.OpenPersistence(dir, s.latency, s.failure)
	if err != nil {
		return err
	}
	s.persistence = p
	return nil
}

// Close flushes any pending persisted state. It is safe to call without
// EnablePersistence.
func (s *Server) Close() error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.Close()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

type middleware func(http.Handler) http.Handler

// chain wraps h so that mws[0] runs first.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// route dispatches to the endpoint handlers.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	path := rawPath(r)
	p := path
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	isRead := r.Method == http.MethodGet || r.Method == http.MethodHead

	switch {
	case p == "/" && isRead:
		s.handleIndex(w, r)
	case p == "/build-info" && isRead:
		writeJSON(w, http.StatusOK, s.opts.BuildInfo)
	case p == "/metrics" && isRead:
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = s.metrics.WritePrometheus(w)
	case p == "/probe/live" && isRead:
		writeJSON(w, http.StatusOK, probeResponse{OK: true, Message: "I'm alive!"})
	case p == "/probe/ready" && isRead:
		writeJSON(w, http.StatusOK, probeResponse{OK: true, Message: "I'm ready!"})
	case p == "/sink" || strings.HasPrefix(p, "/sink/"):
		s.routeSink(w, r, p, path)
	default:
		s.renderError(w, http.StatusNotFound, "Not Found")
	}
}

type probeResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, http.StatusOK, "index.html", pageData{Title: "http-sink", Message: indexMessage})
}

type pageData struct {
	Title   string
	Message string
	Status  int
	Stack   string
}

func (s *Server) renderPage(w http.ResponseWriter, status int, name string, data pageData) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// renderError renders the HTML error page; details are shown only in development.
func (s *Server) renderError(w http.ResponseWriter, status int, message string) {
	data := pageData{Title: "http-sink", Message: message}
	if s.opts.Development {
		data.Status = status
		data.Stack = http.StatusText(status) + ": " + message
	}
	s.renderPage(w, status, "error.html", data)
}
