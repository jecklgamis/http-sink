package sink

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T, dir string) (*Latency, *Failure, *Persistence) {
	t.Helper()
	l, f := NewLatency(), NewFailure()
	p, err := OpenPersistence(dir, l, f)
	if err != nil {
		t.Fatalf("OpenPersistence: %v", err)
	}
	return l, f, p
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, f, p := open(t, dir)
	latCfg, _ := l.Set("/sink/a", 120)
	failCfg, _ := f.Set("/sink/b", 0.5, 503)
	l.Set("/sink/gone", 10)
	l.Remove("/sink/gone")
	if err := p.Close(); err != nil { // Close flushes pending changes
		t.Fatal(err)
	}

	l2, f2, p2 := open(t, dir)
	defer p2.Close()
	gotL, gotF := l2.List(), f2.List()
	if len(gotL) != 1 || gotL[0] != latCfg {
		t.Fatalf("latency restored = %+v, want [%+v]", gotL, latCfg)
	}
	if len(gotF) != 1 || gotF[0] != failCfg {
		t.Fatalf("failure restored = %+v, want [%+v]", gotF, failCfg)
	}
	if d := l2.DelayFor("/sink/a"); d < 0 || d >= 120*time.Millisecond {
		t.Fatalf("restored latency not applied: %v", d)
	}
}

func TestPersistenceKeepsOriginalExpiry(t *testing.T) {
	dir := t.TempDir()
	l, _, p := open(t, dir)
	cfg, _ := l.Set("/sink/a", 5)
	p.Close()

	l2, _, p2 := open(t, dir)
	defer p2.Close()
	if got := l2.List(); len(got) != 1 || got[0].ExpiresAt != cfg.ExpiresAt {
		t.Fatalf("expiry changed across restart: %+v vs %d", got, cfg.ExpiresAt)
	}
}

func TestPersistenceDropsExpiredOnLoad(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().Add(-time.Minute).UnixMilli()
	future := time.Now().Add(time.Hour).UnixMilli()
	file := `{"version":1,
	  "latency":[{"path":"/sink/old","jitterMs":5,"expiresAt":` + itoa(past) + `},
	             {"path":"/sink/new","jitterMs":5,"expiresAt":` + itoa(future) + `}],
	  "failure":[{"path":"/sink/oldf","rate":1,"statusCode":500,"expiresAt":` + itoa(past) + `}]}`
	if err := os.WriteFile(filepath.Join(dir, persistFileName), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}

	l, f, p := open(t, dir)
	defer p.Close()
	if got := l.List(); len(got) != 1 || got[0].Path != "/sink/new" {
		t.Fatalf("latency = %+v, want only /sink/new", got)
	}
	if len(f.List()) != 0 {
		t.Fatalf("expired failure config was restored: %+v", f.List())
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestPersistenceCorruptFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, persistFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, f, p := open(t, dir)
	defer p.Close()
	if len(l.List())+len(f.List()) != 0 {
		t.Fatal("expected an empty start after a corrupt file")
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("corrupt file should have been moved aside: %v", err)
	}
	// and it keeps working: new changes are persisted over a fresh file
	l.Set("/sink/ok", 1)
	p.Close()
	l2, _, p2 := open(t, dir)
	defer p2.Close()
	if len(l2.List()) != 1 {
		t.Fatal("changes after recovery were not persisted")
	}
}

func TestPersistenceWritesWithoutClose(t *testing.T) {
	dir := t.TempDir()
	l, _, p := open(t, dir)
	defer p.Close()
	l.Set("/sink/a", 1)

	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, _ := os.ReadFile(filepath.Join(dir, persistFileName))
		if strings.Contains(string(raw), "/sink/a") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("change was not written within the debounce window; file: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPersistenceLeavesNoTempFilesAndCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	l, _, p := open(t, dir)
	for i := 0; i < 20; i++ {
		l.Set("/sink/a", float64(i))
	}
	p.Close()
	p.Close()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != persistFileName {
			t.Fatalf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestPersistenceFailsFastOnUnwritableDir(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	if _, err := OpenPersistence(filepath.Join(blocker, "sub"), NewLatency(), NewFailure()); err == nil {
		t.Fatal("expected an error when the data dir cannot be created")
	}
}
