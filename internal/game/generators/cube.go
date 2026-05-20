package generators

import (
	"fmt"

	"Gogoxel/internal/world"
)

type cubeGenerator struct {
	name      string
	sceneSize uint
	cubeSize  uint
	color     uint32
	cache     *svoSnapshot
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
		return g.cache.restore(svo)
	}

	sceneSize := int(g.sceneSize)
	cubeSize := min(int(g.cubeSize), sceneSize)
	start := (sceneSize - cubeSize) / 2
	end := start + cubeSize

	svo.BuildTreeSparseFunc(g.sceneSize, func(add func(x, y, z uint, color uint32)) {
		for z := start; z < end; z++ {
			for y := start; y < end; y++ {
				for x := start; x < end; x++ {
					add(uint(x), uint(y), uint(z), g.color)
				}
			}
		}
	})

	cache := snapshot(svo)
	g.cache = &cache
	return nil
}
