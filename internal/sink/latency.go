// Package sink holds the in-memory state behind the /sink endpoints:
// traffic stats and latency and failure simulation.
package sink

import (
	"math/rand/v2"
	"sort"
	"time"
)

// LatencyConfig is a per-path jitter setting.
type LatencyConfig struct {
	Path      string  `json:"path"`
	JitterMs  float64 `json:"jitterMs"`
	ExpiresAt int64   `json:"expiresAt"` // unix ms
}

// Latency maps an exact path to a maximum random delay.
type Latency struct{ store *configStore[float64] }

func NewLatency() *Latency {
	return &Latency{store: newConfigStore[float64](MaxConfigs, ConfigTTL)}
}

// Set configures path, or returns ErrTooManyConfigs at the cap.
func (l *Latency) Set(path string, jitterMs float64) (LatencyConfig, error) {
	exp, err := l.store.set(path, jitterMs)
	if err != nil {
		return LatencyConfig{}, err
	}
	return LatencyConfig{Path: path, JitterMs: jitterMs, ExpiresAt: exp.UnixMilli()}, nil
}

func (l *Latency) Remove(path string) { l.store.remove(path) }

// List returns all live configs sorted by path (never nil).
func (l *Latency) List() []LatencyConfig {
	snap := l.store.snapshot()
	out := make([]LatencyConfig, 0, len(snap))
	for p, e := range snap {
		out = append(out, LatencyConfig{Path: p, JitterMs: e.val, ExpiresAt: e.expires.UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// DelayFor returns a random delay in [0, jitterMs) for the exact path.
func (l *Latency) DelayFor(path string) time.Duration {
	jitter, ok := l.store.get(path)
	if !ok || jitter <= 0 {
		return 0
	}
	return time.Duration(int64(rand.Float64()*jitter)) * time.Millisecond // whole ms
}
