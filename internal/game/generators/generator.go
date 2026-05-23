package generators

import (
	"path/filepath"
	"strings"

	"Gogoxel/internal/world"
)

type BuildRequest struct {
	ChunkX     int
	ChunkY     int
	ChunkRange int
}

type Generator interface {
	Name() string
	ChunkSize() uint
	BuildSVO(svo *world.SVO, request BuildRequest) error
}

type CameraDrivenGenerator interface {
	Generator
	CameraDriven() bool
}

type ChunkStreamGenerator interface {
	CameraDrivenGenerator
	ChunkPaletteColors() []uint32
	BuildChunkSVO(svo *world.SVO, chunkX, chunkY int) error
}

type svoSnapshot struct {
	snapshot world.Snapshot
}

type snapshotCache struct {
	request  BuildRequest
	snapshot *svoSnapshot
}

func (r BuildRequest) Normalized() BuildRequest {
	if r.ChunkRange < 0 {
		r.ChunkRange = 0
	}
	return r
}

func (r BuildRequest) ChunkSpan() int {
	r = r.Normalized()
	return r.ChunkRange*2 + 1
}

func (r BuildRequest) MinChunk() (x, y int) {
	r = r.Normalized()
	return r.ChunkX - r.ChunkRange, r.ChunkY - r.ChunkRange
}

func (r BuildRequest) ExactSceneSize(chunkSize uint) uint {
	if chunkSize == 0 {
		return 1
	}
	return uint(r.ChunkSpan()) * chunkSize
}

func (r BuildRequest) SceneSize(chunkSize uint) uint {
	return sceneSizeForDimension(int(r.ExactSceneSize(chunkSize)))
}

func (r BuildRequest) WorldOrigin(chunkSize uint) (x, y float32) {
	if chunkSize == 0 {
		return 0, 0
	}
	minChunkX, minChunkY := r.MinChunk()
	return float32(minChunkX) * float32(chunkSize), float32(minChunkY) * float32(chunkSize)
}

func (r BuildRequest) LocalChunkCenter(chunkSize uint) (x, y float32) {
	r = r.Normalized()
	center := float32(r.ChunkRange)*float32(chunkSize) + float32(chunkSize)*0.5
	return center, center
}

func (r BuildRequest) ForEachChunk(chunkSize uint, emit func(chunkX, chunkY int, originX, originY uint)) {
	if emit == nil || chunkSize == 0 {
		return
	}
	r = r.Normalized()
	minChunkX, minChunkY := r.MinChunk()
	for chunkYOffset := 0; chunkYOffset < r.ChunkSpan(); chunkYOffset++ {
		chunkY := minChunkY + chunkYOffset
		originY := uint(chunkYOffset) * chunkSize
		for chunkXOffset := 0; chunkXOffset < r.ChunkSpan(); chunkXOffset++ {
			chunkX := minChunkX + chunkXOffset
			originX := uint(chunkXOffset) * chunkSize
			emit(chunkX, chunkY, originX, originY)
		}
	}
}

func snapshot(svo *world.SVO) svoSnapshot {
	return svoSnapshot{snapshot: svo.Snapshot()}
}

func (c svoSnapshot) restore(target *world.SVO) error {
	return target.LoadSnapshot(c.snapshot)
}

func (c *snapshotCache) restore(target *world.SVO, request BuildRequest) (bool, error) {
	if c == nil || c.snapshot == nil {
		return false, nil
	}
	if c.request != request.Normalized() {
		return false, nil
	}
	return true, c.snapshot.restore(target)
}

func (c *snapshotCache) store(source *world.SVO, request BuildRequest) {
	if c == nil || source == nil {
		return
	}
	value := snapshot(source)
	c.request = request.Normalized()
	c.snapshot = &value
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
