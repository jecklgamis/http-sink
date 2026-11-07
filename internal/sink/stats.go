package sink

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const (
	rpsHistorySeconds = 30
	recentLimit       = 10
	maxAge            = rpsHistorySeconds * time.Second
	latencyTopPaths   = 10 // per-path percentiles are reported for the busiest paths only
)

// Entry is one recorded request, as shown in the "Packet Inspection" table.
type Entry struct {
	Timestamp      int64             `json:"timestamp"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	RemoteAddress  string            `json:"remoteAddress"`
	UserAgent      string            `json:"userAgent,omitempty"`
	Headers        map[string]string `json:"headers"`
	ResponseTimeMs int64             `json:"responseTimeMs"`
	StatusCode     int               `json:"statusCode"`
	Body           any               `json:"body,omitempty"`

	// Duration is the precise response time; it feeds the latency histograms.
	Duration time.Duration `json:"-"`
}

// WindowStats is the traffic summary for one trailing time window.
type WindowStats struct {
	Seconds            int                           `json:"seconds"`
	Total              int                           `json:"total"`
	ByMethod           map[string]int                `json:"byMethod"`
	ByPath             map[string]int                `json:"byPath"`
	ByPathAndMethod    map[string]map[string]int     `json:"byPathAndMethod"`
	RpsByPathAndMethod map[string]map[string]float64 `json:"rpsByPathAndMethod"`
	Rps                float64                       `json:"rps"`
}

// Snapshot is the payload served at GET /sink/stats.
type Snapshot struct {
	Now            int64                  `json:"now"`
	Windows        map[string]WindowStats `json:"windows"`
	RecentRequests []Entry                `json:"recentRequests"`
	RpsHistory     []int                  `json:"rpsHistory"`
	Latency        LatencyStats           `json:"latency"`
}

// sample is the minimal per-request record kept for rate computation, so
// that headers and bodies are only retained for the small "recent" buffer.
type sample struct {
	ts     int64
	method string
	path   string
	us     int64 // response time in microseconds
}

// Stats is the in-memory traffic recorder.
type Stats struct {
	mu      sync.Mutex
	now     func() time.Time
	samples []sample
	recent  []Entry
}

func NewStats() *Stats { return &Stats{now: time.Now} }

// Record stores a finished request. e.Timestamp is set here.
func (s *Stats) Record(e Entry) {
	e.Timestamp = s.now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sample{ts: e.Timestamp, method: e.Method, path: e.Path, us: e.Duration.Microseconds()})
	s.recent = append(s.recent, e)
	if len(s.recent) > recentLimit {
		s.recent = s.recent[len(s.recent)-recentLimit:]
	}
}

// ClearRecent empties the "Packet Inspection" buffer.
func (s *Stats) ClearRecent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = nil
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// Compute builds the current stats snapshot.
func (s *Stats) Compute() Snapshot {
	now := s.now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()

	// prune samples older than the longest window we report
	cutoff := now - maxAge.Milliseconds()
	i := 0
	for i < len(s.samples) && s.samples[i].ts < cutoff {
		i++
	}
	if i > 0 {
		s.samples = append([]sample(nil), s.samples[i:]...)
	}

	const seconds = 1
	w := WindowStats{
		Seconds:            seconds,
		ByMethod:           map[string]int{},
		ByPath:             map[string]int{},
		ByPathAndMethod:    map[string]map[string]int{},
		RpsByPathAndMethod: map[string]map[string]float64{},
	}
	history := make([]int, rpsHistorySeconds)
	windowCutoff := now - seconds*1000
	for _, r := range s.samples {
		if r.ts >= windowCutoff {
			w.ByMethod[r.method]++
			w.ByPath[r.path]++
			if w.ByPathAndMethod[r.path] == nil {
				w.ByPathAndMethod[r.path] = map[string]int{}
			}
			w.ByPathAndMethod[r.path][r.method]++
			w.Total++
		}
		// bucket i (0 = newest) covers [now-(i+1)s, now-i s)
		if d := now - r.ts; d >= 1 {
			if b := int((d - 1) / 1000); b < rpsHistorySeconds {
				history[rpsHistorySeconds-1-b]++
			}
		}
	}
	for p, byMethod := range w.ByPathAndMethod {
		w.RpsByPathAndMethod[p] = map[string]float64{}
		for m, n := range byMethod {
			w.RpsByPathAndMethod[p][m] = round3(float64(n) / seconds)
		}
	}
	w.Rps = round3(float64(w.Total) / seconds)

	recent := make([]Entry, 0, len(s.recent))
	for i := len(s.recent) - 1; i >= 0; i-- {
		recent = append(recent, s.recent[i])
	}

	return Snapshot{
		Now:            now,
		Windows:        map[string]WindowStats{"1s": w},
		RecentRequests: recent,
		RpsHistory:     history,
		Latency:        latencyStats(s.samples),
	}
}

// latencyStats summarises response times over all retained samples (the
// trailing 30s). Histograms are built on demand, so nothing is kept per path
// between polls and path cardinality can't grow memory.
func latencyStats(samples []sample) LatencyStats {
	out := LatencyStats{
		WindowSeconds: rpsHistorySeconds,
		ByPath:        map[string]LatencySummary{},
	}
	counts := map[string]int{}
	for _, r := range samples {
		counts[r.path]++
	}
	paths := make([]string, 0, len(counts))
	for p := range counts {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if counts[paths[i]] != counts[paths[j]] {
			return counts[paths[i]] > counts[paths[j]]
		}
		return paths[i] < paths[j]
	})
	if len(paths) > latencyTopPaths {
		paths = paths[:latencyTopPaths]
	}

	all := newHistogram()
	byPath := make(map[string]*hdrhistogram.Histogram, len(paths))
	for _, p := range paths {
		byPath[p] = newHistogram()
	}
	for _, r := range samples {
		d := time.Duration(r.us) * time.Microsecond
		record(all, d)
		if h, ok := byPath[r.path]; ok {
			record(h, d)
		}
	}
	out.All = summarize(all)
	for p, h := range byPath {
		out.ByPath[p] = summarize(h)
	}
	return out
}
