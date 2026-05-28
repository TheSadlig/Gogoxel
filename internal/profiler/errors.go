package profiler

import "errors"

// ErrAlreadyStarted is returned by Start when the profiler is already active.
var ErrAlreadyStarted = errors.New("profiler: already started")

// ErrMissingOutput is returned by Start when no OutputPath is supplied for
// the default chrome-trace backend.
var ErrMissingOutput = errors.New("profiler: OutputPath required")
