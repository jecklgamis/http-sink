package sink

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	persistFileName = "configs.json"
	persistVersion  = 1
	// persistDebounce coalesces a burst of config edits into one disk write.
	persistDebounce = 250 * time.Millisecond
)

// persistedState is the on-disk format. Only the latency and failure configs
// are persisted; traffic stats and metrics are deliberately ephemeral.
type persistedState struct {
	Version int             `json:"version"`
	Latency []LatencyConfig `json:"latency"`
	Failure []FailureConfig `json:"failure"`
}

// Persistence saves the latency and failure configs to a JSON file so they
// survive restarts. Writes are atomic (temp file + rename) and debounced, and
// Close flushes any pending change.
type Persistence struct {
	path     string
	latency  *Latency
	failure  *Failure
	debounce time.Duration

	kick     chan struct{} // capacity 1: "state changed"
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// OpenPersistence restores configs from dir (creating it if needed), starts
// the background writer, and wires it to the stores. A missing file means a
// fresh start; a corrupt file is moved aside and logged, never fatal. Expired
// entries are dropped on load.
func OpenPersistence(dir string, l *Latency, f *Failure) (*Persistence, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	p := &Persistence{
		path:     filepath.Join(dir, persistFileName),
		latency:  l,
		failure:  f,
		debounce: persistDebounce,
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	// Verify the directory is writable now, so a bad volume fails at startup
	// rather than silently on the first config change.
	if err := p.write(); err != nil {
		return nil, fmt.Errorf("data dir is not writable: %w", err)
	}
	l.store.onChange = p.notify
	f.store.onChange = p.notify
	go p.run()
	return p, nil
}

func (p *Persistence) load() error {
	raw, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", p.path, err)
	}
	var st persistedState
	if err := json.Unmarshal(raw, &st); err != nil {
		aside := p.path + ".corrupt"
		log.Printf("persistence: %s is corrupt (%v); moving it to %s and starting empty", p.path, err, aside)
		_ = os.Rename(p.path, aside)
		return nil
	}
	var restored, dropped int
	for _, c := range st.Latency {
		if p.latency.restore(c) {
			restored++
		} else {
			dropped++
		}
	}
	for _, c := range st.Failure {
		if p.failure.restore(c) {
			restored++
		} else {
			dropped++
		}
	}
	log.Printf("persistence: restored %d config(s) from %s (%d expired or skipped)", restored, p.path, dropped)
	return nil
}

// notify records that state changed; it never blocks.
func (p *Persistence) notify() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Persistence) run() {
	defer close(p.done)
	for {
		select {
		case <-p.kick:
			select {
			case <-time.After(p.debounce):
			case <-p.stop:
				p.flush()
				return
			}
			p.flush()
		case <-p.stop:
			p.flush()
			return
		}
	}
}

func (p *Persistence) flush() {
	if err := p.write(); err != nil {
		log.Printf("persistence: write failed: %v", err)
	}
}

// write atomically replaces the file with the current live configs.
func (p *Persistence) write() error {
	data, err := json.Marshal(persistedState{
		Version: persistVersion,
		Latency: p.latency.List(),
		Failure: p.failure.List(),
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.path), persistFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmpName)
		return werr
	}
	if err := os.Rename(tmpName, p.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// Close stops the writer after flushing any pending change. It is safe to
// call more than once.
func (p *Persistence) Close() error {
	p.stopOnce.Do(func() { close(p.stop) })
	<-p.done
	return nil
}
