// Package audio is the foundation of the 3D audio subsystem. See issue
// #28. The first slice defines the spatial data model (Listener,
// Source, Clip) and a no-op driver so the engine can compile and unit-
// test mix logic without an actual audio backend.
package audio

// Vec3 is a 3D position/velocity vector in world space.
type Vec3 struct{ X, Y, Z float32 }

// Clip is an opaque audio asset handle. Concrete loaders live in a
// follow-up slice (Vorbis/WAV).
type Clip struct{ ID uint32 }

// Listener represents the player ear position/orientation. Zero value
// places the listener at world origin facing +X.
type Listener struct {
	Position Vec3
	Forward  Vec3
	Up       Vec3
	Velocity Vec3
}

// Source is one playing/queued sound in the world.
type Source struct {
	Clip     Clip
	Position Vec3
	Velocity Vec3
	Gain     float32
	Pitch    float32
	Looping  bool
}

// Driver is the interface a backend (OpenAL, miniaudio) must satisfy.
type Driver interface {
	Begin(l Listener)
	Submit(s Source)
	End()
}

// NopDriver discards all calls. Useful in headless tests.
type NopDriver struct{}

func (NopDriver) Begin(Listener) {}
func (NopDriver) Submit(Source)  {}
func (NopDriver) End()           {}
