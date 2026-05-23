package engine

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/world"
)

type chunkStreamTestGenerator struct {
	name      string
	chunkSize uint
	color     uint32

	mu    sync.Mutex
	calls map[chunkCoord]int
}

func newChunkStreamTestGenerator(name string, chunkSize uint, color uint32) *chunkStreamTestGenerator {
	return &chunkStreamTestGenerator{
		name:      name,
		chunkSize: chunkSize,
		color:     color,
		calls:     make(map[chunkCoord]int),
	}
}

func (g *chunkStreamTestGenerator) Name() string {
	return g.name
}

func (g *chunkStreamTestGenerator) ChunkSize() uint {
	return g.chunkSize
}

func (g *chunkStreamTestGenerator) CameraDriven() bool {
	return true
}

func (g *chunkStreamTestGenerator) ChunkPaletteColors() []uint32 {
	return []uint32{g.color}
}

func (g *chunkStreamTestGenerator) BuildChunkSVO(svo *world.SVO, chunkX, chunkY int) error {
	return g.BuildSVO(svo, generators.BuildRequest{ChunkX: chunkX, ChunkY: chunkY, ChunkRange: 0})
}

func (g *chunkStreamTestGenerator) BuildSVO(svo *world.SVO, request generators.BuildRequest) error {
	if request.ChunkRange != 0 {
		return fmt.Errorf("expected per-chunk build request, got range %d", request.ChunkRange)
	}
	coord := chunkCoord{x: request.ChunkX, y: request.ChunkY}
	g.mu.Lock()
	g.calls[coord]++
	g.mu.Unlock()

	svo.BuildTreeSparseVolumes(request.SceneSize(g.chunkSize), func(_addVoxel func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(0, 0, 0, g.chunkSize, g.color)
	})
	return nil
}

func (g *chunkStreamTestGenerator) buildCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, count := range g.calls {
		total += count
	}
	return total
}

type blockingChunkStreamTestGenerator struct {
	base         *chunkStreamTestGenerator
	blockCoord   chunkCoord
	buildStarted chan struct{}
	releaseBuild chan struct{}
}

func newBlockingChunkStreamTestGenerator(name string, chunkSize uint, color uint32, blockCoord chunkCoord) *blockingChunkStreamTestGenerator {
	return &blockingChunkStreamTestGenerator{
		base:         newChunkStreamTestGenerator(name, chunkSize, color),
		blockCoord:   blockCoord,
		buildStarted: make(chan struct{}, 1),
		releaseBuild: make(chan struct{}),
	}
}

func (g *blockingChunkStreamTestGenerator) Name() string {
	return g.base.Name()
}

func (g *blockingChunkStreamTestGenerator) ChunkSize() uint {
	return g.base.ChunkSize()
}

func (g *blockingChunkStreamTestGenerator) CameraDriven() bool {
	return g.base.CameraDriven()
}

func (g *blockingChunkStreamTestGenerator) ChunkPaletteColors() []uint32 {
	return g.base.ChunkPaletteColors()
}

func (g *blockingChunkStreamTestGenerator) BuildChunkSVO(svo *world.SVO, chunkX, chunkY int) error {
	return g.BuildSVO(svo, generators.BuildRequest{ChunkX: chunkX, ChunkY: chunkY, ChunkRange: 0})
}

func (g *blockingChunkStreamTestGenerator) BuildSVO(svo *world.SVO, request generators.BuildRequest) error {
	if request.ChunkX == g.blockCoord.x && request.ChunkY == g.blockCoord.y {
		select {
		case g.buildStarted <- struct{}{}:
		default:
		}
		<-g.releaseBuild
	}
	return g.base.BuildSVO(svo, request)
}

func (g *blockingChunkStreamTestGenerator) buildCount() int {
	return g.base.buildCount()
}

func TestChunkSceneManagerBuildsOnlyMissingChunksAcrossOverlap(t *testing.T) {
	generator := newChunkStreamTestGenerator("Chunk Stream Terrain", 16, 0xFF00FF00)
	manager := newChunkSceneManager(generator)
	defer manager.Close()

	initialRequest := generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}
	scene, err := manager.LoadInitial(initialRequest)
	if err != nil {
		t.Fatalf("LoadInitial returned error: %v", err)
	}
	if scene == nil || scene.NodeCount() == 0 {
		t.Fatal("expected initial scene to be built")
	}
	if got, want := generator.buildCount(), 9; got != want {
		t.Fatalf("initial chunk build count = %d, want %d", got, want)
	}

	nextRequest := generators.BuildRequest{ChunkX: 1, ChunkY: 0, ChunkRange: 1}
	manager.SetRequest(nextRequest)
	waitForChunkBuildCount(t, generator, 12)
	update := waitForChunkSceneUpdate(t, manager, func(update chunkSceneUpdate) bool {
		return update.err == nil && update.request == nextRequest && generator.buildCount() == 12
	})
	if update.scene == nil || update.scene.NodeCount() == 0 {
		t.Fatal("expected overlapping scene update after building missing chunks")
	}
	if got, want := generator.buildCount(), 12; got != want {
		t.Fatalf("overlap chunk build count = %d, want %d", got, want)
	}

	manager.SetRequest(initialRequest)
	update = waitForChunkSceneUpdate(t, manager, func(update chunkSceneUpdate) bool {
		return update.err == nil && update.request == initialRequest
	})
	if update.scene == nil || update.scene.NodeCount() == 0 {
		t.Fatal("expected scene update when returning to cached chunks")
	}
	if got, want := generator.buildCount(), 12; got != want {
		t.Fatalf("return-to-cache chunk build count = %d, want %d", got, want)
	}
}

func TestChunkSceneManagerDoesNotPublishIncompleteScene(t *testing.T) {
	generator := newBlockingChunkStreamTestGenerator("Chunk Stream Terrain", 16, 0xFF00FF00, chunkCoord{x: 2, y: 0})
	manager := newChunkSceneManager(generator)
	defer manager.Close()

	initialRequest := generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}
	if _, err := manager.LoadInitial(initialRequest); err != nil {
		t.Fatalf("LoadInitial returned error: %v", err)
	}

	nextRequest := generators.BuildRequest{ChunkX: 1, ChunkY: 0, ChunkRange: 1}
	manager.SetRequest(nextRequest)
	select {
	case <-generator.buildStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked chunk build to start")
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if scene, request, err, ok := manager.TakeUpdate(); ok {
			t.Fatalf("unexpected partial update while chunk build was blocked: request=%+v err=%v scene_nil=%t", request, err, scene == nil)
		}
		runtime.Gosched()
	}

	close(generator.releaseBuild)
	waitForChunkBuildCount(t, generator.base, 12)
	update := waitForChunkSceneUpdate(t, manager, func(update chunkSceneUpdate) bool {
		return update.err == nil && update.request == nextRequest && update.scene != nil
	})
	if update.scene == nil || update.scene.NodeCount() == 0 {
		t.Fatal("expected completed scene update after releasing blocked chunk build")
	}
}

func TestChunkSceneManagerKeepsPerlinMixedBricks(t *testing.T) {
	manager := newChunkSceneManager(generators.NewPerlinGenerator(1, 2))
	defer manager.Close()

	scene, err := manager.LoadInitial(generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1})
	if err != nil {
		t.Fatalf("LoadInitial returned error: %v", err)
	}
	if scene == nil {
		t.Fatal("expected scene to be built")
	}
	if scene.NodeCount() == 0 {
		t.Fatal("expected perlin scene nodes")
	}
	if scene.BrickCount() == 0 {
		t.Fatal("expected perlin chunk manager scene to retain mixed bricks")
	}
}

func TestChunkSceneManagerMatchesDirectPerlinBuild(t *testing.T) {
	generator := generators.NewPerlinGenerator(1, 2)
	request := generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}

	direct := world.NewSVO()
	if err := generator.BuildSVO(direct, request); err != nil {
		t.Fatalf("direct BuildSVO returned error: %v", err)
	}

	manager := newChunkSceneManager(generator)
	defer manager.Close()
	composed, err := manager.LoadInitial(request)
	if err != nil {
		t.Fatalf("LoadInitial returned error: %v", err)
	}
	if composed == nil {
		t.Fatal("expected composed scene")
	}

	gotMin, gotMax, gotOK := composed.OccupiedBounds()
	wantMin, wantMax, wantOK := direct.OccupiedBounds()
	if gotOK != wantOK || gotMin != wantMin || gotMax != wantMax {
		t.Fatalf("composed occupied bounds = (%v, %v, %t), want (%v, %v, %t)", gotMin, gotMax, gotOK, wantMin, wantMax, wantOK)
	}
	for sampleY := uint32(16); sampleY < uint32(composed.Size()); sampleY += 64 {
		for sampleX := uint32(16); sampleX < uint32(composed.Size()); sampleX += 64 {
			directHit, directOK := direct.Raycast(world.Ray{
				Origin:    [3]float32{float32(sampleX) + 0.5, float32(sampleY) + 0.5, float32(direct.Size()) + 64},
				Direction: [3]float32{0, 0, -1},
			}, float32(direct.Size())+128)
			composedHit, composedOK := composed.Raycast(world.Ray{
				Origin:    [3]float32{float32(sampleX) + 0.5, float32(sampleY) + 0.5, float32(composed.Size()) + 64},
				Direction: [3]float32{0, 0, -1},
			}, float32(composed.Size())+128)
			if directOK != composedOK {
				t.Fatalf("ray hit presence mismatch at sample (%d,%d): composed=%t direct=%t", sampleX, sampleY, composedOK, directOK)
			}
			if !directOK {
				continue
			}
			if directHit.Voxel != composedHit.Voxel || directHit.MaterialID != composedHit.MaterialID {
				t.Fatalf("ray hit mismatch at sample (%d,%d): composed voxel=%v material=%d, want voxel=%v material=%d", sampleX, sampleY, composedHit.Voxel, composedHit.MaterialID, directHit.Voxel, directHit.MaterialID)
			}
		}
	}
}

func waitForChunkBuildCount(t *testing.T, generator *chunkStreamTestGenerator, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if generator.buildCount() >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("chunk build count did not reach %d (got %d)", want, generator.buildCount())
}

func waitForChunkSceneUpdate(t *testing.T, manager *chunkSceneManager, accept func(update chunkSceneUpdate) bool) chunkSceneUpdate {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		scene, request, err, ok := manager.TakeUpdate()
		if ok {
			update := chunkSceneUpdate{scene: scene, request: request, err: err}
			if accept(update) {
				return update
			}
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for chunk scene update")
	return chunkSceneUpdate{}
}
