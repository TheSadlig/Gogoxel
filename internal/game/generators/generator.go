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
	snapshot world.Snapshot
}

func captureCache(svo *world.SVO) cachedSVO {
	return cachedSVO{snapshot: svo.Snapshot()}
}

func (c cachedSVO) apply(target *world.SVO) error {
	return target.LoadSnapshot(c.snapshot)
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
