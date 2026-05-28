// Package physics is the foundation of the rigid-body + voxel-bond
// physics subsystem. See issues #32, #33, #34.
//
// The first slice ships the deterministic, fixed-timestep integrator
// and the Body DTO. Voxel-bond constraint solving, broadphase, and
// renderer integration arrive in follow-ups.
package physics

// Vec3 is a 3D vector.
type Vec3 struct{ X, Y, Z float32 }

// Add returns a+b.
func (v Vec3) Add(b Vec3) Vec3 { return Vec3{v.X + b.X, v.Y + b.Y, v.Z + b.Z} }

// MulS returns v*s.
func (v Vec3) MulS(s float32) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }

// Body is a rigid body's deterministic simulation state.
type Body struct {
	Position     Vec3
	Velocity     Vec3
	Acceleration Vec3
	InverseMass  float32 // 0 = static
}

// Step integrates b by dt using semi-implicit Euler.
func (b *Body) Step(dt float32) {
	if b.InverseMass == 0 {
		return
	}
	b.Velocity = b.Velocity.Add(b.Acceleration.MulS(dt))
	b.Position = b.Position.Add(b.Velocity.MulS(dt))
}

// World owns all live Bodies for one simulation island.
type World struct {
	Gravity Vec3
	Bodies  []Body
}

// Tick advances every body by dt, applying World.Gravity.
func (w *World) Tick(dt float32) {
	for i := range w.Bodies {
		b := &w.Bodies[i]
		if b.InverseMass == 0 {
			continue
		}
		b.Acceleration = w.Gravity
		b.Step(dt)
	}
}
