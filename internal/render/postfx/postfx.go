// Package postfx is the foundation of HDR + tonemapping + bloom +
// exposure. See issue #14.
package postfx

import "math"

// Settings drives the post-processing chain.
type Settings struct {
	Exposure     float32 // EV, applied after auto-exposure if enabled
	AutoExposure bool
	BloomEnabled bool
	BloomStrength float32
	Tonemap      TonemapOp
}

// TonemapOp selects the tonemapping curve.
type TonemapOp uint8

const (
	TonemapReinhard TonemapOp = iota
	TonemapACES
	TonemapKhronosPBR
)

// Default returns a sensible neutral preset.
func Default() Settings {
	return Settings{Exposure: 0, AutoExposure: true, BloomEnabled: true, BloomStrength: 0.04, Tonemap: TonemapACES}
}

// ApplyReinhard maps HDR luminance to LDR via x/(1+x). Pure for tests.
func ApplyReinhard(x float32) float32 {
	if x < 0 {
		x = 0
	}
	v := float64(x)
	return float32(v / (1 + v))
}

// ApplyExposure multiplies x by 2^stops.
func ApplyExposure(x, stops float32) float32 {
	return x * float32(math.Pow(2, float64(stops)))
}
