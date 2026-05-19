package generators

import (
	"path/filepath"
	"strings"

	"Gogoxel/internal/world"
)

type Generator interface {
	Name() string
	BuildSVO(svo *world.SVO) error
}

type cachedSVO struct {
	words             []uint32
	occupiedMin       [3]uint32
	occupiedMax       [3]uint32
	hasOccupiedBounds bool
}

func captureCache(svo *world.SVO) cachedSVO {
	minBounds, maxBounds, ok := svo.OccupiedBounds()
	return cachedSVO{
		words:             svo.StorageBufferWords(),
		occupiedMin:       minBounds,
		occupiedMax:       maxBounds,
		hasOccupiedBounds: ok,
	}
}

func (c cachedSVO) apply(target *world.SVO) error {
	return target.LoadStorageBufferWords(c.words, c.occupiedMin, c.occupiedMax, c.hasOccupiedBounds)
}

func DefaultGenerators() []Generator {
	generators := []Generator{
		NewCubeGenerator("Cube", 128, 64, rgbaColor(0xE2, 0x55, 0x4F)),
		NewPerlinGenerator(1, 2),
	}

	for _, path := range []string{
		"third_party/voxel-model/vox/monument/monu1.vox",
		"third_party/voxel-model/vox/monument/monu6-without-water.vox",
		"third_party/voxel-model/vox/monument/monu8-without-water.vox",
		defaultModelPath,
	} {
		generator, err := NewGeneratorFromFile(path)
		if err == nil {
			generators = append(generators, generator)
		}
	}

	return generators
}

func modelDisplayName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if base == "" {
		return "Model"
	}
	return base
}

func sceneSizeForDimension(size int) uint {
	if size <= 1 {
		return 1
	}

	rootSize := 1
	for rootSize < size {
		rootSize <<= 1
	}
	return uint(rootSize)
}
