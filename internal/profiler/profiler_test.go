package profiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestZoneDisabledIsNoop(t *testing.T) {
	if IsEnabled() {
		t.Fatal("profiler should start disabled")
	}
	end := Zone("foo")
	if end == nil {
		t.Fatal("Zone must return a non-nil end func even when disabled")
	}
	end()
	Plot("metric", 1.0)
	FrameMark()
}

func TestStartStopRecordsEvents(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "trace.json")
	if err := Start(Options{OutputPath: out}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Stop() })

	end := Zone("scene-upload")
	Plot("resident_bricks", 42)
	FrameMark()
	end()

	if err := Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if IsEnabled() {
		t.Fatal("expected disabled after Stop")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var events []map[string]any
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("trace must be a JSON array of objects: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events; got %d", len(events))
	}
	phases := map[string]int{}
	for _, e := range events {
		phases[e["ph"].(string)]++
	}
	for _, ph := range []string{"B", "E", "C", "i"} {
		if phases[ph] == 0 {
			t.Errorf("missing phase %q in trace; phases=%v", ph, phases)
		}
	}
}

func TestStartRejectsDoubleStart(t *testing.T) {
	tmp := t.TempDir()
	if err := Start(Options{OutputPath: filepath.Join(tmp, "a.json")}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Stop() })
	if err := Start(Options{OutputPath: filepath.Join(tmp, "b.json")}); err != ErrAlreadyStarted {
		t.Fatalf("expected ErrAlreadyStarted, got %v", err)
	}
}

func TestStartRequiresOutput(t *testing.T) {
	if err := Start(Options{}); err != ErrMissingOutput {
		t.Fatalf("expected ErrMissingOutput, got %v", err)
	}
	if IsEnabled() {
		t.Fatal("expected disabled after failed Start")
	}
}

func TestBufferCap(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "cap.json")
	if err := Start(Options{OutputPath: out, BufferEvents: 4}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Stop() })
	for i := 0; i < 20; i++ {
		FrameMark()
	}
	cb := backendForTest(t)
	if got := len(cb.snapshotEvents()); got != 4 {
		t.Fatalf("expected buffer capped at 4, got %d", got)
	}
}

func backendForTest(t *testing.T) *chromeTraceBackend {
	t.Helper()
	b := active.Load()
	if b == nil {
		t.Fatal("no active backend")
	}
	cb, ok := (*b).(*chromeTraceBackend)
	if !ok {
		t.Fatalf("active backend is not chromeTraceBackend: %T", *b)
	}
	return cb
}

// BenchmarkDisabledZone measures Zone overhead with profiling off.
func BenchmarkDisabledZone(b *testing.B) {
	if IsEnabled() {
		b.Fatal("benchmark requires disabled profiler")
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		end := Zone("bench")
		end()
	}
}
