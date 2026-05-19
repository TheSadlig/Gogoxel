package generators

import (
	"fmt"
	"path/filepath"
	"strings"

	"Gogoxel/internal/world"
)

type Generator interface {
	Name() string
	BuildSVO(svo *world.SVO) error
}

type cachedSVO struct {
	size              uint
	nodes             []world.PackedNode
	occupiedMin       [3]uint32
	occupiedMax       [3]uint32
	hasOccupiedBounds bool
}

func captureCache(svo *world.SVO) cachedSVO {
	minBounds, maxBounds, ok := svo.OccupiedBounds()
	return cachedSVO{
		size:              svo.Size(),
		nodes:             svo.PackedNodes(),
		occupiedMin:       minBounds,
		occupiedMax:       maxBounds,
		hasOccupiedBounds: ok,
	}
}

func (c cachedSVO) apply(target *world.SVO) {
	target.LoadPackedNodes(c.size, c.nodes, c.occupiedMin, c.occupiedMax, c.hasOccupiedBounds)
}

type cubeGenerator struct {
	name      string
	sceneSize uint
	cubeSize  uint
	color     uint32
	cache     *cachedSVO
}

func NewCubeGenerator(name string, sceneSize, cubeSize uint, color uint32) Generator {
	if name == "" {
		name = "Cube"
	}
	if sceneSize == 0 {
		sceneSize = 128
	}
	if cubeSize == 0 {
		cubeSize = sceneSize / 2
	}
	return &cubeGenerator{name: name, sceneSize: sceneSizeForDimension(int(sceneSize)), cubeSize: cubeSize, color: color}
}

func (g *cubeGenerator) Name() string {
	return g.name
}

func (g *cubeGenerator) BuildSVO(svo *world.SVO) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	if g.cache != nil {
		g.cache.apply(svo)
		return nil
	}

	sceneSize := int(g.sceneSize)
	cubeSize := minInt(int(g.cubeSize), sceneSize)
	start := (sceneSize - cubeSize) / 2
	end := start + cubeSize

	svo.BuildTreeSparseFunc(g.sceneSize, func(add func(world.VoxelPoint)) {
		for z := start; z < end; z++ {
			for y := start; y < end; y++ {
				for x := start; x < end; x++ {
					add(world.VoxelPoint{X: uint(x), Y: uint(y), Z: uint(z), Color: g.color})
				}
			}
		}
	})

	cache := captureCache(svo)
	g.cache = &cache
	return nil
}

func DefaultGenerators() []Generator {
	generators := []Generator{
		NewCubeGenerator("Cube", 128, 64, rgbaColor(0xE2, 0x55, 0x4F)),
		NewHouseGenerator(defaultHouseScale),
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
