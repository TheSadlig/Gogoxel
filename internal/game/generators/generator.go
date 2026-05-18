package generators

// Generator produces chunk voxel data using a width/height/depth.
type Generator interface {
	Generate(width, height, depth uint32) ([]uint8, [255]uint32)
}

// NewDefault returns the default Generator implementation.
func NewDefault() Generator {
	if generator, err := NewGeneratorFromFile(defaultModelPath); err == nil {
		return generator
	}

	return NewHouseGenerator(defaultHouseScale)
}
