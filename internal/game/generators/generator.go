package generators

// Generator produces chunk voxel data using a width/height/depth.
type Generator interface {
	Generate(width, height, depth uint32) []uint32
}

// NewDefault returns the default Generator implementation.
func NewDefault() Generator {
	return NewPerlinGenerator(1, 2)
}
