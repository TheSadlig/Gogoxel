package vulkan

import (
	"container/heap"
	"fmt"
	"sync"
	"unsafe"

	"Gogoxel/internal/world"

	vk "github.com/vulkan-go/vulkan"
)

const (
	defaultBrickUploadBudget = 128
	svoHeaderWordCount       = 2
	// halfBrickWorldUnits is the camera-movement threshold that triggers a
	// streaming replan: half a brick edge ensures we never miss a brick
	// crossing between replans, while avoiding redundant planner work for
	// sub-brick motion.
	halfBrickWorldUnits float32 = float32(brickSizeVoxels) * 0.5
)

type streamBrick struct {
	nodeIndex uint32
	origin    [3]uint32
	center    [3]float32
	// voxels references the SVO's backing voxel array directly to avoid a
	// 512B copy per brick at streamer construction. The streamer keeps a
	// reference to the source slice (`sourceBricks`) to keep the backing
	// data alive for its lifetime.
	voxels *[world.BrickVoxelCount]uint8
}

type residentBrick struct {
	slot uint32
	lru  uint64
}

type brickUploadOp struct {
	logicalIndex      int
	slot              uint32
	evictedLogicalIdx int
}

type brickEvictOp struct {
	logicalIndex int
	slot         uint32
}

// streamPlan is the immutable result of planning. Recording consumes it and
// only commits the state changes (residency-map mutation, pool frees) after
// the plan has been successfully transformed into GPU commands.
type streamPlan struct {
	uploads        []brickUploadOp
	evictions      []brickEvictOp
	residencyDelta []residencyDelta
	allocations    []uint32 // fresh slots; freed on rollback only
	frees          []uint32 // victim slots freed on commit only
}

type residencyDelta struct {
	logicalIndex int
	add          bool
	slot         uint32
	lru          uint64
}

type brickStreamer struct {
	mu sync.Mutex

	// sourceBricks keeps the underlying brick slice alive so the voxel
	// pointers in `bricks` remain valid.
	sourceBricks []world.Brick
	bricks       []streamBrick

	cameraPosition  [3]float32
	cameraEverSet   bool
	lastPlannedPos  [3]float32
	lastPlannedOnce bool
	desired         []int
	desiredReady    bool
	resident        map[int]residentBrick

	residentLimit int
	uploadBudget  int
	lruTick       uint64

	// Reused planner scratch.
	desiredHeap brickDistanceHeap

	stopCh   chan struct{}
	doneCh   chan struct{}
	dirty    chan struct{}
	stopOnce sync.Once
}

// brickStreamerConfig customises the streaming budget. Zero-valued fields
// fall back to defaults derived from brick-pool capacity.
type brickStreamerConfig struct {
	ResidentLimit int
	UploadBudget  int
}

func newBrickStreamer(bricks []world.Brick) *brickStreamer {
	return newBrickStreamerWithConfig(bricks, brickStreamerConfig{})
}

func newBrickStreamerWithConfig(bricks []world.Brick, cfg brickStreamerConfig) *brickStreamer {
	if len(bricks) == 0 {
		return nil
	}

	residentLimit := cfg.ResidentLimit
	if residentLimit <= 0 {
		// Use the full brick-pool capacity (minus slot 0 reserved for air).
		residentLimit = int(brickPoolCapacity) - 1
	}
	if residentLimit > len(bricks) {
		residentLimit = len(bricks)
	}
	if residentLimit <= 0 {
		residentLimit = 1
	}

	uploadBudget := cfg.UploadBudget
	if uploadBudget <= 0 {
		uploadBudget = defaultBrickUploadBudget
	}

	streamer := &brickStreamer{
		sourceBricks:  bricks,
		bricks:        make([]streamBrick, len(bricks)),
		resident:      make(map[int]residentBrick, residentLimit),
		residentLimit: residentLimit,
		uploadBudget:  uploadBudget,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		// dirty is 1-buffered so SetCameraPosition can signal without blocking
		// and signals coalesce until the planner consumes one.
		dirty: make(chan struct{}, 1),
	}
	for index := range bricks {
		brick := &bricks[index]
		streamer.bricks[index] = streamBrick{
			nodeIndex: brick.NodeIndex,
			origin:    brick.Origin,
			center: [3]float32{
				float32(brick.Origin[0]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[1]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[2]) + float32(brickSizeVoxels)*0.5,
			},
			voxels: &brick.Voxels,
		}
	}

	go streamer.run()
	return streamer
}

func (s *brickStreamer) run() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.dirty:
			s.recomputeDesired()
		}
	}
}

func (s *brickStreamer) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

// markDirty signals the planner without blocking. Extra signals coalesce.
func (s *brickStreamer) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *brickStreamer) SetCameraPosition(position [3]float32) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.cameraPosition = position
	firstSet := !s.cameraEverSet
	s.cameraEverSet = true

	movedFarEnough := !s.lastPlannedOnce
	if s.lastPlannedOnce {
		dx := position[0] - s.lastPlannedPos[0]
		dy := position[1] - s.lastPlannedPos[1]
		dz := position[2] - s.lastPlannedPos[2]
		movedFarEnough = dx*dx+dy*dy+dz*dz >= halfBrickWorldUnits*halfBrickWorldUnits
	}
	s.mu.Unlock()

	if firstSet || movedFarEnough {
		s.markDirty()
	}
}

func (s *brickStreamer) ResidentCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.resident)
}

func (s *brickStreamer) recomputeDesired() {
	if s == nil {
		return
	}

	s.mu.Lock()
	cameraPosition := s.cameraPosition
	s.mu.Unlock()

	desired := s.computeDesired(cameraPosition)

	s.mu.Lock()
	s.desired = desired
	s.desiredReady = true
	s.lastPlannedPos = cameraPosition
	s.lastPlannedOnce = true
	s.mu.Unlock()
}

// brickDistance is a (logicalIndex, distance²) pair used by the top-K planner.
type brickDistance struct {
	logicalIndex int
	distSq       float32
}

// brickDistanceHeap is a max-heap over distSq, used to maintain the K
// closest bricks in O(N log K) instead of sorting all N.
type brickDistanceHeap []brickDistance

func (h brickDistanceHeap) Len() int { return len(h) }
func (h brickDistanceHeap) Less(i, j int) bool {
	if h[i].distSq == h[j].distSq {
		return h[i].logicalIndex > h[j].logicalIndex
	}
	return h[i].distSq > h[j].distSq
}
func (h brickDistanceHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *brickDistanceHeap) Push(x any)   { *h = append(*h, x.(brickDistance)) }
func (h *brickDistanceHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func (s *brickStreamer) computeDesired(cameraPosition [3]float32) []int {
	limit := min(s.residentLimit, len(s.bricks))
	if limit <= 0 {
		return nil
	}

	// Reuse the heap's backing array across planner ticks.
	heapSlice := s.desiredHeap[:0]

	for index, brick := range s.bricks {
		distSq := squaredDistance(cameraPosition, brick.center)
		if len(heapSlice) < limit {
			heapSlice = append(heapSlice, brickDistance{logicalIndex: index, distSq: distSq})
			if len(heapSlice) == limit {
				s.desiredHeap = heapSlice
				heap.Init(&s.desiredHeap)
				heapSlice = s.desiredHeap
			}
			continue
		}
		// Heap is full: replace root if this brick is closer than the farthest.
		if distSq < heapSlice[0].distSq {
			heapSlice[0] = brickDistance{logicalIndex: index, distSq: distSq}
			s.desiredHeap = heapSlice
			heap.Fix(&s.desiredHeap, 0)
			heapSlice = s.desiredHeap
		}
	}
	s.desiredHeap = heapSlice

	// Drain the max-heap to get farthest-first order, then reverse for
	// closest-first deterministic planning.
	out := make([]int, len(s.desiredHeap))
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&s.desiredHeap).(brickDistance).logicalIndex
	}
	return out
}

func squaredDistance(a, b [3]float32) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return dx*dx + dy*dy + dz*dz
}

// planOps produces an immutable plan and pre-reserves brick-pool slots. The
// reservations are tracked in plan.allocations so commitPlan / rollbackPlan
// can keep pool state consistent with what was actually recorded.
func (s *brickStreamer) planOps(pool *brickPool) streamPlan {
	if s == nil || pool == nil {
		return streamPlan{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.bricks) == 0 || !s.desiredReady {
		return streamPlan{}
	}

	plan := streamPlan{}

	desiredSet := make(map[int]struct{}, len(s.desired))
	for _, logicalIndex := range s.desired {
		desiredSet[logicalIndex] = struct{}{}
	}

	// Touch LRU for already-resident desired bricks. Slightly-stale LRU on
	// recording failure is harmless.
	for _, logicalIndex := range s.desired {
		if resident, ok := s.resident[logicalIndex]; ok {
			s.lruTick++
			resident.lru = s.lruTick
			s.resident[logicalIndex] = resident
		}
	}

	plan.uploads = make([]brickUploadOp, 0, min(len(s.desired), s.uploadBudget))
	plan.residencyDelta = make([]residencyDelta, 0, s.uploadBudget*2)

	for _, logicalIndex := range s.desired {
		if len(plan.uploads) >= s.uploadBudget {
			break
		}
		if _, ok := s.resident[logicalIndex]; ok {
			continue
		}

		slot, evictedLogicalIdx, allocatedFresh, ok := s.reserveSlot(pool, desiredSet)
		if !ok {
			break
		}
		if allocatedFresh {
			plan.allocations = append(plan.allocations, slot)
		}
		if evictedLogicalIdx >= 0 {
			plan.residencyDelta = append(plan.residencyDelta, residencyDelta{
				logicalIndex: evictedLogicalIdx,
				add:          false,
			})
		}

		s.lruTick++
		plan.residencyDelta = append(plan.residencyDelta, residencyDelta{
			logicalIndex: logicalIndex,
			add:          true,
			slot:         slot,
			lru:          s.lruTick,
		})
		plan.uploads = append(plan.uploads, brickUploadOp{
			logicalIndex:      logicalIndex,
			slot:              slot,
			evictedLogicalIdx: evictedLogicalIdx,
		})
	}

	remainingEvictionBudget := s.uploadBudget - len(plan.uploads)
	if remainingEvictionBudget > 0 && len(s.resident) > len(desiredSet) {
		type lruEntry struct {
			logicalIndex int
			slot         uint32
			lru          uint64
		}
		candidates := make([]lruEntry, 0, len(s.resident))
		for logicalIndex, resident := range s.resident {
			if _, ok := desiredSet[logicalIndex]; ok {
				continue
			}
			candidates = append(candidates, lruEntry{logicalIndex: logicalIndex, slot: resident.slot, lru: resident.lru})
		}

		// Resident set evictions are typically small; insertion sort is fine.
		for i := 1; i < len(candidates); i++ {
			for j := i; j > 0 && candidates[j-1].lru > candidates[j].lru; j-- {
				candidates[j-1], candidates[j] = candidates[j], candidates[j-1]
			}
		}

		plan.evictions = make([]brickEvictOp, 0, min(len(candidates), remainingEvictionBudget))
		for _, eviction := range candidates {
			if len(plan.evictions) >= remainingEvictionBudget {
				break
			}
			plan.evictions = append(plan.evictions, brickEvictOp{
				logicalIndex: eviction.logicalIndex,
				slot:         eviction.slot,
			})
			plan.frees = append(plan.frees, eviction.slot)
			plan.residencyDelta = append(plan.residencyDelta, residencyDelta{
				logicalIndex: eviction.logicalIndex,
				add:          false,
			})
		}
	}

	return plan
}

// commitPlan applies a successfully-recorded plan to the streamer's residency
// map and frees evicted pool slots.
func (s *brickStreamer) commitPlan(pool *brickPool, plan streamPlan) {
	if s == nil {
		return
	}
	s.mu.Lock()
	for _, delta := range plan.residencyDelta {
		if delta.add {
			s.resident[delta.logicalIndex] = residentBrick{slot: delta.slot, lru: delta.lru}
		} else {
			delete(s.resident, delta.logicalIndex)
		}
	}
	s.mu.Unlock()
	for _, slot := range plan.frees {
		pool.Free(slot)
	}
}

// rollbackPlan undoes the fresh pool-slot allocations done by planOps after a
// recording failure. The residency map is left untouched.
func (s *brickStreamer) rollbackPlan(pool *brickPool, plan streamPlan) {
	for _, slot := range plan.allocations {
		pool.Free(slot)
	}
}

// reserveSlot returns (slot, evictedLogicalIndex, allocatedFresh, ok).
//   - allocatedFresh = true when the slot came from pool.Allocate (must be
//     freed on rollback). When evicting we reuse the victim's slot without
//     touching the pool.
func (s *brickStreamer) reserveSlot(pool *brickPool, desiredSet map[int]struct{}) (uint32, int, bool, bool) {
	if len(s.resident) < s.residentLimit {
		slot, err := pool.Allocate()
		if err == nil {
			return slot, -1, true, true
		}
	}

	victim := s.pickVictim(desiredSet)
	if victim < 0 {
		slot, err := pool.Allocate()
		if err != nil {
			return 0, -1, false, false
		}
		return slot, -1, true, true
	}

	return s.resident[victim].slot, victim, false, true
}

func (s *brickStreamer) pickVictim(desiredSet map[int]struct{}) int {
	victim := -1
	victimLRU := uint64(0)
	for logicalIndex, resident := range s.resident {
		if _, ok := desiredSet[logicalIndex]; ok {
			continue
		}
		if victim == -1 || resident.lru < victimLRU {
			victim = logicalIndex
			victimLRU = resident.lru
		}
	}
	return victim
}

func (chunk *ChunkResources) SetCameraPosition(position [3]float32) {
	if chunk == nil || chunk.streamer == nil {
		return
	}
	chunk.streamer.SetCameraPosition(position)
}

func (chunk *ChunkResources) ResidentBrickCount() int {
	if chunk == nil || chunk.streamer == nil {
		return 0
	}
	return chunk.streamer.ResidentCount()
}

func (chunk *ChunkResources) RecordStreaming(frame *Frame) error {
	if chunk == nil || frame == nil || chunk.streamer == nil || chunk.brickPool == nil {
		return nil
	}

	plan := chunk.streamer.planOps(chunk.brickPool)
	if len(plan.uploads) == 0 && len(plan.evictions) == 0 {
		return nil
	}

	if err := chunk.recordPlan(frame, plan); err != nil {
		chunk.streamer.rollbackPlan(chunk.brickPool, plan)
		return err
	}

	chunk.streamer.commitPlan(chunk.brickPool, plan)
	return nil
}

func (chunk *ChunkResources) recordPlan(frame *Frame, plan streamPlan) error {
	if len(plan.uploads) > 0 {
		frame.renderer.transitionImageLayout(
			frame.CommandBuffer,
			chunk.brickPool.image,
			vk.ImageLayoutShaderReadOnlyOptimal,
			vk.ImageLayoutTransferDstOptimal,
			vk.AccessFlags(vk.AccessShaderReadBit),
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		)
	}

	bufferTouched := false
	for _, upload := range plan.uploads {
		if upload.evictedLogicalIdx >= 0 {
			chunk.patchNodePointer(frame, chunk.streamer.bricks[upload.evictedLogicalIdx].nodeIndex, 0)
			bufferTouched = true
		}

		if err := chunk.recordBrickUpload(frame, upload.logicalIndex, upload.slot); err != nil {
			return err
		}

		chunk.patchNodePointer(frame, chunk.streamer.bricks[upload.logicalIndex].nodeIndex, upload.slot)
		bufferTouched = true
	}

	for _, eviction := range plan.evictions {
		chunk.patchNodePointer(frame, chunk.streamer.bricks[eviction.logicalIndex].nodeIndex, 0)
		bufferTouched = true
	}

	if len(plan.uploads) > 0 {
		frame.renderer.transitionImageLayout(
			frame.CommandBuffer,
			chunk.brickPool.image,
			vk.ImageLayoutTransferDstOptimal,
			vk.ImageLayoutShaderReadOnlyOptimal,
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.AccessFlags(vk.AccessShaderReadBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
			vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
		)
	}

	if bufferTouched {
		frame.renderer.recordBufferShaderBarrier(frame.CommandBuffer, chunk.buffer, chunk.bufferBytes)
	}

	return nil
}

// patchNodePointer issues a 4-byte vkCmdFillBuffer to update the node's
// childPointer in place — no staging buffer or cleanup required.
func (chunk *ChunkResources) patchNodePointer(frame *Frame, nodeIndex, value uint32) {
	vk.CmdFillBuffer(
		frame.CommandBuffer,
		chunk.buffer,
		nodeChildPointerByteOffset(nodeIndex),
		vk.DeviceSize(4),
		value,
	)
}

func (chunk *ChunkResources) recordBrickUpload(frame *Frame, logicalIndex int, slot uint32) error {
	brickX, brickY, brickZ, err := brickPoolSlotCoord(slot)
	if err != nil {
		return err
	}

	const brickBytes = vk.DeviceSize(world.BrickVoxelCount)
	stagingBuffer, stagingOffset, dst, err := frame.renderer.stagingAlloc(frame.FrameSlot, brickBytes, 4)
	if err != nil {
		return fmt.Errorf("staging-alloc for brick upload: %w", err)
	}
	src := chunk.streamer.bricks[logicalIndex].voxels[:]
	copy(unsafe.Slice((*byte)(dst), len(src)), src)

	regions := []vk.BufferImageCopy{{
		BufferOffset:      stagingOffset,
		BufferRowLength:   0,
		BufferImageHeight: 0,
		ImageSubresource: vk.ImageSubresourceLayers{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			MipLevel:       0,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
		ImageOffset: vk.Offset3D{
			X: int32(brickX * brickSizeVoxels),
			Y: int32(brickY * brickSizeVoxels),
			Z: int32(brickZ * brickSizeVoxels),
		},
		ImageExtent: vk.Extent3D{
			Width:  brickSizeVoxels,
			Height: brickSizeVoxels,
			Depth:  brickSizeVoxels,
		},
	}}
	vk.CmdCopyBufferToImage(frame.CommandBuffer, stagingBuffer, chunk.brickPool.image, vk.ImageLayoutTransferDstOptimal, uint32(len(regions)), regions)
	return nil
}

func nodeChildPointerByteOffset(nodeIndex uint32) vk.DeviceSize {
	return vk.DeviceSize((svoHeaderWordCount+int(nodeIndex)*2+1) * 4)
}

func (r *Renderer) recordBufferShaderBarrier(commandBuffer vk.CommandBuffer, buffer vk.Buffer, size vk.DeviceSize) {
	barriers := []vk.BufferMemoryBarrier{{
		SType:               vk.StructureTypeBufferMemoryBarrier,
		SrcAccessMask:       vk.AccessFlags(vk.AccessTransferWriteBit),
		DstAccessMask:       vk.AccessFlags(vk.AccessShaderReadBit),
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored,
		DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Buffer:              buffer,
		Offset:              0,
		Size:                size,
	}}
	vk.CmdPipelineBarrier(
		commandBuffer,
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
		0,
		0,
		nil,
		uint32(len(barriers)),
		barriers,
		0,
		nil,
	)
}
