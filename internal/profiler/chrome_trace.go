package profiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// chromeTraceBackend records events in memory and writes them out in the
// Chrome "trace_event" JSON Array Format on close. View at
// https://ui.perfetto.dev/ or chrome://tracing .
type chromeTraceBackend struct {
	outputPath string
	maxEvents  int

	startWall time.Time

	mu     sync.Mutex
	events []chromeEvent
}

type chromeEvent struct {
	Name string         `json:"name"`
	Cat  string         `json:"cat,omitempty"`
	Ph   string         `json:"ph"`
	Ts   int64          `json:"ts"`
	Pid  int            `json:"pid"`
	Tid  int            `json:"tid"`
	Args map[string]any `json:"args,omitempty"`
	S    string         `json:"s,omitempty"`
}

func newChromeTraceBackend(opts Options) (*chromeTraceBackend, error) {
	if strings.TrimSpace(opts.OutputPath) == "" {
		return nil, ErrMissingOutput
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return nil, fmt.Errorf("profiler: prepare output dir: %w", err)
	}
	initial := 4096
	if opts.BufferEvents > 0 && opts.BufferEvents < initial {
		initial = opts.BufferEvents
	}
	return &chromeTraceBackend{
		outputPath: opts.OutputPath,
		maxEvents:  opts.BufferEvents,
		startWall:  time.Now(),
		events:     make([]chromeEvent, 0, initial),
	}, nil
}

func (b *chromeTraceBackend) appendLocked(ev chromeEvent) {
	if b.maxEvents > 0 && len(b.events) >= b.maxEvents {
		copy(b.events, b.events[1:])
		b.events = b.events[:len(b.events)-1]
	}
	b.events = append(b.events, ev)
}

func (b *chromeTraceBackend) tsMicros(ts time.Time) int64 {
	return ts.Sub(b.startWall).Microseconds()
}

func (b *chromeTraceBackend) beginZone(name string, ts time.Time) {
	b.mu.Lock()
	b.appendLocked(chromeEvent{Name: name, Ph: "B", Ts: b.tsMicros(ts), Pid: 1, Tid: 1})
	b.mu.Unlock()
}

func (b *chromeTraceBackend) endZone(name string, ts time.Time) {
	b.mu.Lock()
	b.appendLocked(chromeEvent{Name: name, Ph: "E", Ts: b.tsMicros(ts), Pid: 1, Tid: 1})
	b.mu.Unlock()
}

func (b *chromeTraceBackend) plot(name string, ts time.Time, value float64) {
	b.mu.Lock()
	b.appendLocked(chromeEvent{
		Name: name, Ph: "C", Ts: b.tsMicros(ts), Pid: 1, Tid: 1,
		Args: map[string]any{name: value},
	})
	b.mu.Unlock()
}

func (b *chromeTraceBackend) frameMark(ts time.Time) {
	b.mu.Lock()
	b.appendLocked(chromeEvent{Name: "frame", Ph: "i", Ts: b.tsMicros(ts), Pid: 1, Tid: 1, S: "g"})
	b.mu.Unlock()
}

func (b *chromeTraceBackend) close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, err := os.Create(b.outputPath)
	if err != nil {
		return fmt.Errorf("profiler: create trace: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(b.events); err != nil {
		return fmt.Errorf("profiler: encode trace: %w", err)
	}
	return nil
}

func (b *chromeTraceBackend) snapshotEvents() []chromeEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]chromeEvent, len(b.events))
	copy(out, b.events)
	return out
}
