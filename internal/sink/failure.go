package sink

import (
	"math/rand/v2"
	"sort"
)

// FailureConfig is a per-path failure injection setting.
type FailureConfig struct {
	Path       string  `json:"path"`
	Rate       float64 `json:"rate"`
	StatusCode int     `json:"statusCode"`
	ExpiresAt  int64   `json:"expiresAt"` // unix ms
}

// failureRule is the stored value; the path is the store key.
type failureRule struct {
	rate       float64
	statusCode int
}

// Failure maps an exact path to a failure rate and status code.
type Failure struct{ store *configStore[failureRule] }

func NewFailure() *Failure {
	return &Failure{store: newConfigStore[failureRule](MaxConfigs, ConfigTTL)}
}

// Set configures path, or returns ErrTooManyConfigs at the cap.
func (f *Failure) Set(path string, rate float64, statusCode int) (FailureConfig, error) {
	exp, err := f.store.set(path, failureRule{rate, statusCode})
	if err != nil {
		return FailureConfig{}, err
	}
	return FailureConfig{Path: path, Rate: rate, StatusCode: statusCode, ExpiresAt: exp.UnixMilli()}, nil
}

func (f *Failure) Remove(path string) { f.store.remove(path) }

// List returns all live configs sorted by path (never nil).
func (f *Failure) List() []FailureConfig {
	snap := f.store.snapshot()
	out := make([]FailureConfig, 0, len(snap))
	for p, e := range snap {
		out = append(out, FailureConfig{Path: p, Rate: e.val.rate, StatusCode: e.val.statusCode, ExpiresAt: e.expires.UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// StatusCodeFor rolls the dice for the exact path. It returns the injected
// status code, or 0 when the request should proceed normally.
func (f *Failure) StatusCodeFor(path string) int {
	rule, ok := f.store.get(path)
	if !ok {
		return 0
	}
	if rand.Float64() < rule.rate {
		return rule.statusCode
	}
	return 0
}
