// Package server wires the HTTP surface of http-sink: middleware, routing,
// the /sink endpoints and the dashboard.
package server

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
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
	// MaxDelay is the longest delay a request or rule may ask for; defaults to DefaultMaxDelay.
	MaxDelay time.Duration
	// MaxBodyBytes caps parsed (JSON and form) request bodies; defaults to DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// NoRateLimit disables the global rate limit.
	NoRateLimit bool
	// AccessLogJSON writes the access log as one JSON object per line instead of combined format.
	AccessLogJSON bool
	// Instance names this replica in the stats view; defaults to the hostname.
	Instance string
}

// Server is the http-sink application; it implements http.Handler.
type Server struct {
	opts    Options
	stats   *sink.Stats
	metrics *sink.Metrics
	tmpl    *template.Template
	static  fs.FS
	handler http.Handler
	// delaySlots is a counting semaphore for in-flight delayed requests.
	delaySlots chan struct{}
	// rules are the static per-path injections; set once before serving, read-only after.
	rules    map[string]ruleEntry
	ruleList []Rule
}

// New builds a Server with fresh in-memory state.
func New(opts Options) *Server {
	if opts.RateLimiter == nil && !opts.NoRateLimit {
		opts.RateLimiter = ratelimit.New(DefaultRateLimit, time.Second)
	}
	if opts.MaxDelayedRequests <= 0 {
		opts.MaxDelayedRequests = DefaultMaxDelayedRequests
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = DefaultMaxDelay
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if opts.Instance == "" {
		opts.Instance, _ = os.Hostname()
	}
	s := &Server{
		delaySlots: make(chan struct{}, opts.MaxDelayedRequests),
		opts:       opts,
		stats:      sink.NewStats(),
		metrics:    sink.NewMetrics(),
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
	s.renderPage(w, http.StatusOK, "index.html", pageData{Title: "http-sink", Message: indexMessage, MaxDelay: s.opts.MaxDelay.String()})
}

type pageData struct {
	Title   string
	Message string
	Status  int
	Stack   string
	// MaxDelay is shown in the dashboard's injection help.
	MaxDelay string
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
