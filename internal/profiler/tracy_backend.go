//go:build tracy

package profiler

// The Tracy backend is gated behind the `tracy` build tag and requires cgo
// bindings to the Tracy client. This stub keeps the API surface intact so
// callers compile under -tags=tracy; integrators can swap in a concrete
// backend (e.g. github.com/wminshew/tracy-go) by replacing newTracyBackend
// here.
//
// To use: `go build -tags=tracy ./cmd/gogoxel` after providing a Tracy
// client implementation.

import "errors"

// ErrTracyUnavailable is returned when the tracy backend is selected but no
// concrete client has been wired in this build.
var ErrTracyUnavailable = errors.New("profiler: tracy backend not wired into this build")
