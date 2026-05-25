package engine

import (
	"fmt"
	"sort"
	"sync"

	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/world"
)

const (
	chunkSceneComposeBatchSize = 4
	chunkSceneCacheMargin      = 2
)

type chunkCoord struct {
	x int
	y int
}

type chunkSceneUpdate struct {
	request generators.BuildRequest
	scene   *world.SVO
	err     error
}

type translatedChunkScene struct {
	originX uint
	originY uint
	scene   *world.SVO
}

type chunkSceneManager struct {
	loader        generators.ChunkLoader
	chunkSize     uint
	paletteColors []uint32

	mu                sync.Mutex
	desiredRequest    generators.BuildRequest
	hasDesiredRequest bool
	cachedChunks      map[chunkCoord]*world.SVO
	pendingUpdate     *chunkSceneUpdate

	dirty    chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

func newChunkSceneManager(loader generators.ChunkLoader) *chunkSceneManager {
	paletteColors := loader.ChunkPaletteColors()
	copiedPalette := make([]uint32, len(paletteColors))
	copy(copiedPalette, paletteColors)

	manager := &chunkSceneManager{
		loader:        loader,
		chunkSize:     loader.ChunkSize(),
		paletteColors: copiedPalette,
		cachedChunks:  make(map[chunkCoord]*world.SVO),
		dirty:         make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
	go manager.run()
	return manager
}

func (m *chunkSceneManager) LoadInitial(request generators.BuildRequest) (*world.SVO, error) {
	if m == nil {
		return nil, fmt.Errorf("chunk scene manager is not initialized")
	}
	request = request.Normalized()
	if _, err := m.ensureMissingChunks(request, 0); err != nil {
		return nil, err
	}
	scene := m.composeScene(request)
	m.trimCache(request)

	m.mu.Lock()
	m.desiredRequest = request
	m.hasDesiredRequest = true
	m.pendingUpdate = nil
	m.mu.Unlock()

	return scene, nil
}

func (m *chunkSceneManager) Close() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
		<-m.doneCh
	})
}

func (m *chunkSceneManager) SetRequest(request generators.BuildRequest) {
	if m == nil {
		return
	}
	request = request.Normalized()

	m.mu.Lock()
	if m.hasDesiredRequest && request == m.desiredRequest {
		m.mu.Unlock()
		return
	}
	m.desiredRequest = request
	m.hasDesiredRequest = true
	m.mu.Unlock()

	m.markDirty()
}

func (m *chunkSceneManager) TakeUpdate() (*world.SVO, generators.BuildRequest, error, bool) {
	if m == nil {
		return nil, generators.BuildRequest{}, nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendingUpdate == nil {
		return nil, generators.BuildRequest{}, nil, false
	}
	update := m.pendingUpdate
	m.pendingUpdate = nil
	return update.scene, update.request, update.err, true
}

func (m *chunkSceneManager) markDirty() {
	select {
	case m.dirty <- struct{}{}:
	default:
	}
}

func (m *chunkSceneManager) run() {
	defer close(m.doneCh)

	publishedRequest := generators.BuildRequest{}
	hasPublishedRequest := false

	for {
		select {
		case <-m.stopCh:
			return
		case <-m.dirty:
		}

		for {
			request, ok := m.requestSnapshot()
			if !ok {
				hasPublishedRequest = false
				break
			}
			if m.hasAllChunks(request) {
				if !hasPublishedRequest || request != publishedRequest {
					m.publishScene(request)
					publishedRequest = request
					hasPublishedRequest = true
				}
				m.trimCache(request)

				latest, ok := m.requestSnapshot()
				if !ok {
					hasPublishedRequest = false
					break
				}
				if latest != request {
					hasPublishedRequest = false
					continue
				}
				break
			}

			built, err := m.ensureMissingChunks(request, chunkSceneComposeBatchSize)
			if err != nil {
				m.publishError(request, err)
				break
			}

			latest, ok := m.requestSnapshot()
			if !ok {
				hasPublishedRequest = false
				break
			}
			if latest != request {
				hasPublishedRequest = false
				continue
			}
			if built == 0 {
				// Nothing new was built and the request still is not ready. Avoid a
				// tight spin and wait for a newer request signal.
				break
			}
		}
	}
}

func (m *chunkSceneManager) hasAllChunks(request generators.BuildRequest) bool {
	if m == nil {
		return false
	}
	request = request.Normalized()
	ready := true
	m.mu.Lock()
	defer m.mu.Unlock()
	request.ForEachChunk(m.chunkSize, func(chunkX, chunkY int, _originX, _originY uint) {
		if !ready {
			return
		}
		if _, ok := m.cachedChunks[chunkCoord{x: chunkX, y: chunkY}]; !ok {
			ready = false
		}
	})
	return ready
}

func (m *chunkSceneManager) requestSnapshot() (generators.BuildRequest, bool) {
	if m == nil {
		return generators.BuildRequest{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasDesiredRequest {
		return generators.BuildRequest{}, false
	}
	return m.desiredRequest, true
}

func (m *chunkSceneManager) publishScene(request generators.BuildRequest) {
	scene := m.composeScene(request)
	m.mu.Lock()
	m.pendingUpdate = &chunkSceneUpdate{request: request, scene: scene}
	m.mu.Unlock()
}

func (m *chunkSceneManager) publishError(request generators.BuildRequest, err error) {
	m.mu.Lock()
	m.pendingUpdate = &chunkSceneUpdate{request: request, err: err}
	m.mu.Unlock()
}

func (m *chunkSceneManager) composeScene(request generators.BuildRequest) *world.SVO {
	parts := m.sceneParts(request)
	scene := world.NewSVO()
	scene.BuildTreeSparseVolumesWithMaterialBricks(request.SceneSize(m.chunkSize), m.paletteColors, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[world.BrickVoxelCount]uint8)) {
		for _, part := range parts {
			part.scene.EmitTranslatedMaterialVolumes(part.originX, part.originY, 0, addCube, addBrick)
		}
	})
	return scene
}

func (m *chunkSceneManager) sceneParts(request generators.BuildRequest) []translatedChunkScene {
	if m == nil {
		return nil
	}
	parts := make([]translatedChunkScene, 0, request.ChunkSpan()*request.ChunkSpan())
	m.mu.Lock()
	defer m.mu.Unlock()
	request.ForEachChunk(m.chunkSize, func(chunkX, chunkY int, originX, originY uint) {
		chunk := m.cachedChunks[chunkCoord{x: chunkX, y: chunkY}]
		if chunk == nil {
			return
		}
		parts = append(parts, translatedChunkScene{originX: originX, originY: originY, scene: chunk})
	})
	return parts
}

func (m *chunkSceneManager) ensureMissingChunks(request generators.BuildRequest, limit int) (int, error) {
	if m == nil {
		return 0, nil
	}
	missing := m.missingChunkCoords(request)
	if limit > 0 && len(missing) > limit {
		missing = missing[:limit]
	}

	built := 0
	for _, coord := range missing {
		select {
		case <-m.stopCh:
			return built, nil
		default:
		}

		chunk, err := m.buildChunk(coord)
		if err != nil {
			return built, err
		}

		m.mu.Lock()
		if _, exists := m.cachedChunks[coord]; !exists {
			m.cachedChunks[coord] = chunk
			built++
		}
		m.mu.Unlock()

		latest, ok := m.requestSnapshot()
		if ok && latest != request {
			break
		}
	}

	return built, nil
}

func (m *chunkSceneManager) missingChunkCoords(request generators.BuildRequest) []chunkCoord {
	request = request.Normalized()
	coords := make([]chunkCoord, 0, request.ChunkSpan()*request.ChunkSpan())

	m.mu.Lock()
	cached := make(map[chunkCoord]struct{}, len(m.cachedChunks))
	for coord := range m.cachedChunks {
		cached[coord] = struct{}{}
	}
	m.mu.Unlock()

	request.ForEachChunk(m.chunkSize, func(chunkX, chunkY int, _originX, _originY uint) {
		coord := chunkCoord{x: chunkX, y: chunkY}
		if _, ok := cached[coord]; ok {
			return
		}
		coords = append(coords, coord)
	})

	sort.Slice(coords, func(i, j int) bool {
		distanceI := chunkCoordDistanceSq(coords[i], request.ChunkX, request.ChunkY)
		distanceJ := chunkCoordDistanceSq(coords[j], request.ChunkX, request.ChunkY)
		if distanceI != distanceJ {
			return distanceI < distanceJ
		}
		if coords[i].y != coords[j].y {
			return coords[i].y < coords[j].y
		}
		return coords[i].x < coords[j].x
	})

	return coords
}

func chunkCoordDistanceSq(coord chunkCoord, centerX, centerY int) int {
	deltaX := coord.x - centerX
	deltaY := coord.y - centerY
	return deltaX*deltaX + deltaY*deltaY
}

func (m *chunkSceneManager) buildChunk(coord chunkCoord) (*world.SVO, error) {
	chunk := world.NewSVO()
	if err := m.loader.LoadChunkSVO(chunk, coord.x, coord.y); err != nil {
		return nil, fmt.Errorf("loading chunk (%d,%d): %w", coord.x, coord.y, err)
	}
	return chunk, nil
}

func (m *chunkSceneManager) trimCache(request generators.BuildRequest) {
	if m == nil {
		return
	}
	request = request.Normalized()
	minChunkX, minChunkY := request.MinChunk()
	maxChunkX := request.ChunkX + request.ChunkRange
	maxChunkY := request.ChunkY + request.ChunkRange
	keepMinX := minChunkX - chunkSceneCacheMargin
	keepMinY := minChunkY - chunkSceneCacheMargin
	keepMaxX := maxChunkX + chunkSceneCacheMargin
	keepMaxY := maxChunkY + chunkSceneCacheMargin

	m.mu.Lock()
	defer m.mu.Unlock()
	for coord := range m.cachedChunks {
		if coord.x < keepMinX || coord.x > keepMaxX || coord.y < keepMinY || coord.y > keepMaxY {
			delete(m.cachedChunks, coord)
		}
	}
}
