// Package log is the public structured-logging facade. It wraps log/slog
// with a small component-tagging helper so engine consumers can attach
// per-subsystem context without depending on internal/.
package log

import "log/slog"

// WithComponent returns the default slog.Logger tagged with the given
// component name. Use for per-subsystem logs in consumer code.
func WithComponent(name string) *slog.Logger {
return slog.Default().With("component", name)
}
