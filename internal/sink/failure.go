package sink

import (
	"math/rand/v2"
	"sort"
	"sync"
)

// FailureConfig is a per-path failure injection setting.
type FailureConfig struct {
	Path       string  `json:"path"`
	Rate       float64 `json:"rate"`
	StatusCode int     `json:"statusCode"`
}

// Failure maps an exact path to a failure rate and status code.
type Failure struct {
	mu      sync.RWMutex
	configs map[string]FailureConfig
}

func NewFailure() *Failure { return &Failure{configs: map[string]FailureConfig{}} }

func (f *Failure) Set(path string, rate float64, statusCode int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs[path] = FailureConfig{Path: path, Rate: rate, StatusCode: statusCode}
}

func (f *Failure) Remove(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.configs, path)
}

// List returns all configs sorted by path (never nil).
func (f *Failure) List() []FailureConfig {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FailureConfig, 0, len(f.configs))
	for _, c := range f.configs {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// StatusCodeFor rolls the dice for the exact path. It returns the injected
// status code, or 0 when the request should proceed normally.
func (f *Failure) StatusCodeFor(path string) int {
	f.mu.RLock()
	c, ok := f.configs[path]
	f.mu.RUnlock()
	if !ok {
		return 0
	}
	if rand.Float64() < c.Rate {
		return c.StatusCode
	}
	return 0
}
