package sink

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// Prometheus histogram bucket upper bounds, in seconds.
var promBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type requestKey struct {
	method string
	class  string
}

// Metrics accumulates lifetime counters and an HDR histogram of response
// times for sink traffic, and renders them in the Prometheus text format.
type Metrics struct {
	mu       sync.Mutex
	hist     *hdrhistogram.Histogram
	sumUs    int64
	requests map[requestKey]int64
}

func NewMetrics() *Metrics {
	return &Metrics{hist: newHistogram(), requests: map[requestKey]int64{}}
}

// normMethod bounds label cardinality: clients can send arbitrary methods.
func normMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "OTHER"
}

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

// Observe records one finished sink request.
func (m *Metrics) Observe(method string, status int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record(m.hist, d)
	m.sumUs += d.Microseconds()
	m.requests[requestKey{normMethod(method), statusClass(status)}]++
}

// WritePrometheus renders the metrics in the Prometheus text exposition format.
func (m *Metrics) WritePrometheus(w io.Writer) error {
	m.mu.Lock()
	reqs := make([]requestKey, 0, len(m.requests))
	counts := make(map[requestKey]int64, len(m.requests))
	for k, v := range m.requests {
		reqs = append(reqs, k)
		counts[k] = v
	}
	bars := m.hist.Distribution()
	total, sumUs := m.hist.TotalCount(), m.sumUs
	m.mu.Unlock()

	sort.Slice(reqs, func(i, j int) bool {
		if reqs[i].method != reqs[j].method {
			return reqs[i].method < reqs[j].method
		}
		return reqs[i].class < reqs[j].class
	})

	// Fold the HDR bars into fixed cumulative buckets, by each bar's upper edge.
	perBucket := make([]int64, len(promBuckets))
	for _, b := range bars {
		if b.Count == 0 {
			continue
		}
		for i, le := range promBuckets {
			if float64(b.To) <= le*1e6 {
				perBucket[i] += b.Count
				break
			}
		}
	}

	var err error
	p := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, args...)
		}
	}
	p("# HELP http_sink_requests_total Sink requests handled, by method and status class.\n")
	p("# TYPE http_sink_requests_total counter\n")
	for _, k := range reqs {
		p("http_sink_requests_total{method=%q,code=%q} %d\n", k.method, k.class, counts[k])
	}
	p("# HELP http_sink_request_duration_seconds Sink response time, including injected latency.\n")
	p("# TYPE http_sink_request_duration_seconds histogram\n")
	var cum int64
	for i, le := range promBuckets {
		cum += perBucket[i]
		p("http_sink_request_duration_seconds_bucket{le=\"%g\"} %d\n", le, cum)
	}
	p("http_sink_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", total)
	p("http_sink_request_duration_seconds_sum %g\n", float64(sumUs)/1e6)
	p("http_sink_request_duration_seconds_count %d\n", total)
	return err
}
