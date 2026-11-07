package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// recorder captures the response status and size for logging and stats.
type recorder struct {
	http.ResponseWriter
	status int
	wrote  bool
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status, r.wrote = http.StatusOK, true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// asRecorder returns the recorder installed by accessLog.
func asRecorder(w http.ResponseWriter) *recorder {
	if rec, ok := w.(*recorder); ok {
		return rec
	}
	return &recorder{ResponseWriter: w, status: http.StatusOK}
}

// rateLimit applies the global fixed-window limit.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.opts.RateLimiter.Allow() {
			writeJSON(w, http.StatusTooManyRequests, errorBody{
				Error: fmt.Sprintf("rate limit exceeded: max %d requests per second", s.opts.RateLimiter.Limit()),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// accessLog installs the status recorder and writes a combined-format line.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		if s.opts.AccessLog == nil {
			return
		}
		size := "-"
		if rec.bytes > 0 {
			size = fmt.Sprint(rec.bytes)
		}
		dash := func(v string) string {
			if v == "" {
				return "-"
			}
			return v
		}
		fmt.Fprintf(s.opts.AccessLog, "%s - - [%s] \"%s %s %s\" %d %s \"%s\" \"%s\"\n",
			clientIP(r), start.Format("02/Jan/2006:15:04:05 -0700"),
			r.Method, requestTarget(r), r.Proto, rec.status, size,
			dash(r.Referer()), dash(r.UserAgent()))
	})
}

// serveStatic serves the embedded dashboard assets before any routing,
// so asset requests are not counted in the endpoint metrics or sink stats.
func (s *Server) serveStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if name != "" && name != "." {
				if info, err := fs.Stat(s.static, name); err == nil && !info.IsDir() {
					http.ServeFileFS(w, r, s.static, name)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// statsdMetrics emits <url>.duration, <url>.hits and <url>.<N>xx metrics,
// where <url> is the request URL with "/" replaced by ".".
func (s *Server) statsdMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Statsd == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := asRecorder(w)
		next.ServeHTTP(rec, r)
		name := metricName(requestTarget(r))
		s.opts.Statsd.Timing(name+".duration", time.Since(start))
		s.opts.Statsd.Increment(name+".hits", 1)
		s.opts.Statsd.Increment(name+"."+statusClass(rec.status), 1)
	})
}

var metricReplacer = strings.NewReplacer("/", ".", ":", "_", "|", "_", "@", "_", "\n", "_", "\r", "_")

func metricName(target string) string { return metricReplacer.Replace(target) }

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
