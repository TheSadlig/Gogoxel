// Package material is the foundation of the PBR material model. See
// issue #18. Materials live in a fixed-size palette indexed by voxel
// material id (1..255); slot 0 is reserved for "empty".
package material

// PBR is the per-material parameter block.
type PBR struct {
	BaseColorRGB [3]float32 // linear sRGB
	Metallic     float32
	Roughness    float32
	Emissive     [3]float32 // linear sRGB radiance scale
	IOR          float32
	Transmission float32
}

// Palette holds one PBR entry per voxel material id.
type Palette [256]PBR

// Default returns a non-metal, fully-rough dielectric for every slot.
func Default() Palette {
	var p Palette
	for i := range p {
		p[i] = PBR{
			BaseColorRGB: [3]float32{0.7, 0.7, 0.7},
			Metallic:     0,
			Roughness:    1,
			IOR:          1.5,
		}
	}
	return p
}
