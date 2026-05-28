// Package profiler provides a zero-allocation, low-overhead profiling façade
// for the engine. The default backend writes Chrome Catapult ("trace_event")
// JSON when the GOGOXEL_TRACE env var is set or when Start is called
// explicitly. A Tracy backend lives behind the `tracy` build tag.
//
// Usage:
//
//	defer profiler.Zone("scene-upload")()
//	profiler.Plot("resident_bricks", float64(n))
//	profiler.FrameMark()
//
// When the profiler is disabled (the common case), Zone, Plot and FrameMark
// perform a single atomic load and a branch — no allocation, no syscall.
package profiler

import (
	"sync/atomic"
	"time"
)

var enabled atomic.Bool
var active atomic.Pointer[backend]

type backend interface {
	beginZone(name string, ts time.Time)
	endZone(name string, ts time.Time)
	plot(name string, ts time.Time, value float64)
	frameMark(ts time.Time)
	close() error
}

// IsEnabled reports whether profiling is currently active.
func IsEnabled() bool { return enabled.Load() }

// Zone marks the start of a scope. The returned function should be deferred
// to mark the end. When profiling is off, both operations are a single
// atomic load — safe to call in hot loops.
//
//	defer profiler.Zone("brick-upload")()
func Zone(name string) func() {
	if !enabled.Load() {
		return noopEnd
	}
	b := active.Load()
	if b == nil {
		return noopEnd
	}
	start := time.Now()
	(*b).beginZone(name, start)
	return func() {
		bk := active.Load()
		if bk == nil {
			return
		}
		(*bk).endZone(name, time.Now())
	}
}

// Plot records a numeric value at the current instant.
func Plot(name string, value float64) {
	if !enabled.Load() {
		return
	}
	b := active.Load()
	if b == nil {
		return
	}
	(*b).plot(name, time.Now(), value)
}

// FrameMark records a frame boundary. Call once per rendered frame.
func FrameMark() {
	if !enabled.Load() {
		return
	}
	b := active.Load()
	if b == nil {
		return
	}
	(*b).frameMark(time.Now())
}

func noopEnd() {}

// Options configure Start.
type Options struct {
	OutputPath   string
	BufferEvents int
}

// Start activates profiling. Returns ErrAlreadyStarted if a session is
// already running.
func Start(opts Options) error {
	if enabled.Load() {
		return ErrAlreadyStarted
	}
	b, err := newChromeTraceBackend(opts)
	if err != nil {
		return err
	}
	var iface backend = b
	active.Store(&iface)
	enabled.Store(true)
	return nil
}

// Stop flushes any pending events and disables profiling.
func Stop() error {
	if !enabled.Load() {
		return nil
	}
	enabled.Store(false)
	b := active.Swap(nil)
	if b == nil {
		return nil
	}
	return (*b).close()
}
