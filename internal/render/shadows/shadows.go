// Package shadows is the foundation of ray-traced shadows + sun/sky.
// See issue #11. This first slice owns the sun parameters and a
// directional-light DTO consumable by the renderer.
package shadows

// Vec3 is a 3D vector.
type Vec3 struct{ X, Y, Z float32 }

// Sun describes a directional light source (the sun) with associated
// shadow-pass tuning.
type Sun struct {
	Direction      Vec3    // world-space, unit length, points *from* sun toward scene
	ColorRGB       Vec3    // linear-space radiance
	Intensity      float32 // scalar multiplier
	ShadowBias     float32 // depth bias (world units)
	ShadowMaxDist  float32 // max trace distance for shadow rays
}

// Default returns a reasonable midday sun.
func Default() Sun {
	return Sun{
		Direction:     Vec3{0.3, -0.9, 0.3},
		ColorRGB:      Vec3{1.0, 0.97, 0.92},
		Intensity:     3.0,
		ShadowBias:    0.001,
		ShadowMaxDist: 256.0,
	}
}
