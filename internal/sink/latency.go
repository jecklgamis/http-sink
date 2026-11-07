// Package sink holds the in-memory state behind the /sink endpoints:
// traffic stats, latency and failure simulation, and projects.
package sink

import (
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

// LatencyConfig is a per-path jitter setting.
type LatencyConfig struct {
	Path     string  `json:"path"`
	JitterMs float64 `json:"jitterMs"`
}

// Latency maps an exact path to a maximum random delay.
type Latency struct {
	mu      sync.RWMutex
	configs map[string]float64
}

func NewLatency() *Latency { return &Latency{configs: map[string]float64{}} }

func (l *Latency) Set(path string, jitterMs float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configs[path] = jitterMs
}

func (l *Latency) Remove(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.configs, path)
}

// List returns all configs sorted by path (never nil).
func (l *Latency) List() []LatencyConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]LatencyConfig, 0, len(l.configs))
	for p, j := range l.configs {
		out = append(out, LatencyConfig{Path: p, JitterMs: j})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// DelayFor returns a random delay in [0, jitterMs) for the exact path.
func (l *Latency) DelayFor(path string) time.Duration {
	l.mu.RLock()
	jitter := l.configs[path]
	l.mu.RUnlock()
	if jitter <= 0 {
		return 0
	}
	return time.Duration(int64(rand.Float64()*jitter)) * time.Millisecond // whole ms
}
