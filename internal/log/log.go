// Package log is the engine's structured-logging facade built on top of
// log/slog. It exposes a process-wide default logger configured at startup
// and a convenience WithComponent helper for component-scoped child loggers.
//
// All non-main packages should obtain a logger through Logger() or
// WithComponent("name") rather than calling the standard library log or
// fmt.Print* functions directly.
package log

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"
	"sync/atomic"
)

var current atomic.Pointer[slog.Logger]

func init() {
	SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// SetDefault swaps the process-wide logger. Safe for concurrent use.
func SetDefault(l *slog.Logger) {
	if l == nil {
		return
	}
	current.Store(l)
}

// Logger returns the active process-wide logger.
func Logger() *slog.Logger { return current.Load() }

// WithComponent returns a logger with a "component" attribute set.
func WithComponent(name string) *slog.Logger {
	return Logger().With(slog.String("component", name))
}

// Recover is meant to be deferred at the top of every goroutine entry
// point. It logs any panic with the stack trace and swallows it. Returns
// true if a panic was recovered.
//
//	go func() {
//	    defer log.Recover(ctx, "worker-pool")
//	    work()
//	}()
func Recover(ctx context.Context, where string) bool {
	r := recover()
	if r == nil {
		return false
	}
	logger := WithComponent(where)
	logger.LogAttrs(ctx, slog.LevelError, "panic recovered",
		slog.Any("panic", r),
		slog.String("stack", string(debug.Stack())),
	)
	return true
}
