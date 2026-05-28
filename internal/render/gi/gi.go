// Package gi is the foundation of voxel ambient occlusion + global
// illumination. See issue #12.
package gi

// Settings drives the AO/GI pass.
type Settings struct {
	AOEnabled      bool
	AORadius       float32 // world units
	AOSamples      int32
	GIEnabled      bool
	GIBounces      int32
	GIIntensity    float32
	IndirectClamp  float32 // clamp on indirect contribution to suppress fireflies
}

// Default returns a balanced AO+GI configuration.
func Default() Settings {
	return Settings{
		AOEnabled:     true,
		AORadius:      2.0,
		AOSamples:     8,
		GIEnabled:     false,
		GIBounces:     1,
		GIIntensity:   1.0,
		IndirectClamp: 4.0,
	}
}
