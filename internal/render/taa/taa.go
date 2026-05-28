// Package taa is the foundation of temporal anti-aliasing. See issue #13.
package taa

// Settings drives the TAA resolve pass.
type Settings struct {
	Enabled      bool
	BlendFactor  float32 // history vs current weight (0..1)
	JitterScale  float32 // sub-pixel jitter scale (typically 1.0)
	ClampMode    ClampMode
}

// ClampMode selects history clamp strategy.
type ClampMode uint8

const (
	ClampAABB ClampMode = iota
	ClampVariance
)

// Default returns a stable, mildly-blurry preset.
func Default() Settings {
	return Settings{Enabled: true, BlendFactor: 0.9, JitterScale: 1.0, ClampMode: ClampAABB}
}

// HaltonJitter returns the (x,y) sub-pixel offset for the given frame
// index, using a Halton(2,3) sequence over [-0.5, 0.5).
func HaltonJitter(frameIndex int) (float32, float32) {
	return float32(halton(frameIndex+1, 2)) - 0.5, float32(halton(frameIndex+1, 3)) - 0.5
}

func halton(i, b int) float64 {
	f := 1.0
	r := 0.0
	for i > 0 {
		f /= float64(b)
		r += f * float64(i%b)
		i /= b
	}
	return r
}
