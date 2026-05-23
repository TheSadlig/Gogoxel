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
	cache     snapshotCache
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

func (g *cubeGenerator) ChunkSize() uint {
	return g.sceneSize
}

func (g *cubeGenerator) BuildSVO(svo *world.SVO, request BuildRequest) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	request = request.Normalized()
	if restored, err := g.cache.restore(svo, request); restored || err != nil {
		return err
	}

	chunkSize := int(g.ChunkSize())
	requestedSceneSize := request.SceneSize(g.ChunkSize())
	cubeSize := min(int(g.cubeSize), chunkSize)
	start := (chunkSize - cubeSize) / 2
	end := start + cubeSize

	svo.BuildTreeSparseFunc(requestedSceneSize, func(add func(x, y, z uint, color uint32)) {
		request.ForEachChunk(g.ChunkSize(), func(_chunkX, _chunkY int, originX, originY uint) {
			for z := start; z < end; z++ {
				for y := start; y < end; y++ {
					for x := start; x < end; x++ {
						add(originX+uint(x), originY+uint(y), uint(z), g.color)
					}
				}
			}
		})
	})

	g.cache.store(svo, request)
	return nil
}
