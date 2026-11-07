package sink

import (
	"fmt"
	"sync"
	"time"
)

const (
	// MaxConfigs caps how many latency (and, separately, failure) configs can
	// exist at once, so an open instance can't be made to grow without bound.
	MaxConfigs = 200
	// ConfigTTL is how long a config lives after it was last set. Expired
	// configs are dropped lazily, so abandoned rules don't linger on a shared demo.
	ConfigTTL = time.Hour
)

// ErrTooManyConfigs is returned when the config cap is reached.
var ErrTooManyConfigs = fmt.Errorf("too many configs (max %d); remove one or wait for one to expire", MaxConfigs)

type storeEntry[T any] struct {
	val     T
	expires time.Time
}

// configStore is a mutex-guarded path → value map with a size cap and TTL.
type configStore[T any] struct {
	mu    sync.RWMutex
	items map[string]storeEntry[T]
	now   func() time.Time
	max   int
	ttl   time.Duration
	// onChange, if set, is called after every successful set or remove, with
	// no locks held. It must not block (it only signals the persister).
	onChange func()
}

func newConfigStore[T any](max int, ttl time.Duration) *configStore[T] {
	return &configStore[T]{items: map[string]storeEntry[T]{}, now: time.Now, max: max, ttl: ttl}
}

// set stores v for path and returns its expiry. Updating an existing path
// always succeeds and refreshes its TTL; adding a new one fails at the cap.
func (c *configStore[T]) set(path string, v T) (time.Time, error) {
	exp, err := c.setLocked(path, v)
	if err == nil && c.onChange != nil {
		c.onChange()
	}
	return exp, err
}

func (c *configStore[T]) setLocked(path string, v T) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, exists := c.items[path]; !exists {
		for p, e := range c.items { // make room by dropping expired entries
			if !e.expires.After(now) {
				delete(c.items, p)
			}
		}
		if len(c.items) >= c.max {
			return time.Time{}, ErrTooManyConfigs
		}
	}
	exp := now.Add(c.ttl)
	c.items[path] = storeEntry[T]{val: v, expires: exp}
	return exp, nil
}

func (c *configStore[T]) get(path string) (T, bool) {
	c.mu.RLock()
	e, ok := c.items[path]
	c.mu.RUnlock()
	if !ok || !e.expires.After(c.now()) {
		var zero T
		return zero, false
	}
	return e.val, true
}

func (c *configStore[T]) remove(path string) {
	c.mu.Lock()
	_, existed := c.items[path]
	delete(c.items, path)
	c.mu.Unlock()
	if existed && c.onChange != nil {
		c.onChange()
	}
}

// restore loads an entry with a known expiry (used when reading persisted
// state). It skips entries that already expired and respects the size cap.
func (c *configStore[T]) restore(path string, v T, expires time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !expires.After(c.now()) {
		return false
	}
	if _, exists := c.items[path]; !exists && len(c.items) >= c.max {
		return false
	}
	c.items[path] = storeEntry[T]{val: v, expires: expires}
	return true
}

// snapshot returns the live (unexpired) entries and drops expired ones.
func (c *configStore[T]) snapshot() map[string]storeEntry[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	out := make(map[string]storeEntry[T], len(c.items))
	for p, e := range c.items {
		if !e.expires.After(now) {
			delete(c.items, p)
			continue
		}
		out[p] = e
	}
	return out
}
