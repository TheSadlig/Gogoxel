package vulkan

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"
	"unsafe"

	"Gogoxel/internal/world"

	vk "github.com/vulkan-go/vulkan"
)

const (
	defaultResidentBrickLimit = 4096
	defaultBrickUploadBudget  = 16
	brickPlannerInterval      = 50 * time.Millisecond
	svoHeaderWordCount        = 2
)

type streamBrick struct {
	nodeIndex uint32
	origin    [3]uint32
	center    [3]float32
	voxels    [world.BrickVoxelCount]uint8
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

type brickStreamer struct {
	mu sync.Mutex

	bricks []streamBrick

	cameraPosition [3]float32
	desired        []int
	desiredReady   bool
	resident       map[int]residentBrick

	residentLimit int
	uploadBudget  int
	lruTick       uint64

	stopCh chan struct{}
	doneCh chan struct{}
}

func newBrickStreamer(bricks []world.Brick) *brickStreamer {
	if len(bricks) == 0 {
		return nil
	}

	residentLimit := minInt(defaultResidentBrickLimit, len(bricks))
	if residentLimit <= 0 {
		residentLimit = 1
	}

	streamer := &brickStreamer{
		bricks:        make([]streamBrick, len(bricks)),
		resident:      make(map[int]residentBrick, residentLimit),
		residentLimit: residentLimit,
		uploadBudget:  defaultBrickUploadBudget,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
	for index, brick := range bricks {
		streamer.bricks[index] = streamBrick{
			nodeIndex: brick.NodeIndex,
			origin:    brick.Origin,
			center: [3]float32{
				float32(brick.Origin[0]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[1]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[2]) + float32(brickSizeVoxels)*0.5,
			},
			voxels: brick.Voxels,
		}
	}

	go streamer.run()
	return streamer
}

func (s *brickStreamer) run() {
	ticker := time.NewTicker(brickPlannerInterval)
	defer ticker.Stop()
	defer close(s.doneCh)

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.recomputeDesired()
		}
	}
}

func (s *brickStreamer) Close() {
	if s == nil {
		return
	}
	close(s.stopCh)
	<-s.doneCh
}

func (s *brickStreamer) SetCameraPosition(position [3]float32) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.cameraPosition = position
	needsPrime := !s.desiredReady
	s.mu.Unlock()

	if !needsPrime {
		return
	}

	desired := s.computeDesired(position)
	s.mu.Lock()
	if !s.desiredReady {
		s.desired = desired
		s.desiredReady = true
	}
	s.mu.Unlock()
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
	s.mu.Unlock()
}

func (s *brickStreamer) computeDesired(cameraPosition [3]float32) []int {
	indices := make([]int, len(s.bricks))
	for index := range indices {
		indices[index] = index
	}

	sort.Slice(indices, func(i, j int) bool {
		left := squaredDistance(cameraPosition, s.bricks[indices[i]].center)
		right := squaredDistance(cameraPosition, s.bricks[indices[j]].center)
		if left == right {
			return indices[i] < indices[j]
		}
		return left < right
	})

	limit := minInt(s.residentLimit, len(indices))
	return append([]int(nil), indices[:limit]...)
}

func squaredDistance(a, b [3]float32) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return dx*dx + dy*dy + dz*dz
}

func (s *brickStreamer) planOps(pool *brickPool) ([]brickUploadOp, []brickEvictOp) {
	if s == nil || pool == nil {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.bricks) == 0 || !s.desiredReady {
		return nil, nil
	}

	desiredSet := make(map[int]struct{}, len(s.desired))
	for _, logicalIndex := range s.desired {
		desiredSet[logicalIndex] = struct{}{}
		if resident, ok := s.resident[logicalIndex]; ok {
			s.lruTick++
			resident.lru = s.lruTick
			s.resident[logicalIndex] = resident
		}
	}

	uploads := make([]brickUploadOp, 0, minInt(len(s.desired), s.uploadBudget))
	for _, logicalIndex := range s.desired {
		if len(uploads) >= s.uploadBudget {
			break
		}
		if _, ok := s.resident[logicalIndex]; ok {
			continue
		}

		slot, evictedLogicalIdx, ok := s.reserveSlot(pool, desiredSet)
		if !ok {
			break
		}
		if evictedLogicalIdx >= 0 {
			delete(s.resident, evictedLogicalIdx)
		}

		s.lruTick++
		s.resident[logicalIndex] = residentBrick{slot: slot, lru: s.lruTick}
		uploads = append(uploads, brickUploadOp{
			logicalIndex:      logicalIndex,
			slot:              slot,
			evictedLogicalIdx: evictedLogicalIdx,
		})
	}

	remainingEvictionBudget := s.uploadBudget - len(uploads)
	if remainingEvictionBudget <= 0 || len(s.resident) <= len(desiredSet) {
		return uploads, nil
	}

	candidates := make([]brickEvictOp, 0, len(s.resident))
	for logicalIndex, resident := range s.resident {
		if _, ok := desiredSet[logicalIndex]; ok {
			continue
		}
		candidates = append(candidates, brickEvictOp{logicalIndex: logicalIndex, slot: resident.slot})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return s.resident[candidates[i].logicalIndex].lru < s.resident[candidates[j].logicalIndex].lru
	})

	evictions := make([]brickEvictOp, 0, minInt(len(candidates), remainingEvictionBudget))
	for _, eviction := range candidates {
		if remainingEvictionBudget == 0 || len(s.resident) <= len(desiredSet) {
			break
		}
		delete(s.resident, eviction.logicalIndex)
		pool.Free(eviction.slot)
		evictions = append(evictions, eviction)
		remainingEvictionBudget--
	}

	return uploads, evictions
}

func (s *brickStreamer) reserveSlot(pool *brickPool, desiredSet map[int]struct{}) (uint32, int, bool) {
	if len(s.resident) < s.residentLimit {
		slot, err := pool.Allocate()
		if err == nil {
			return slot, -1, true
		}
	}

	victim := s.pickVictim(desiredSet)
	if victim < 0 {
		slot, err := pool.Allocate()
		if err != nil {
			return 0, -1, false
		}
		return slot, -1, true
	}

	return s.resident[victim].slot, victim, true
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

	uploads, evictions := chunk.streamer.planOps(chunk.brickPool)
	if len(uploads) == 0 && len(evictions) == 0 {
		return nil
	}

	cleanups := make([]func(), 0, len(uploads)*3+len(evictions))
	cleanupImmediate := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}

	if len(uploads) > 0 {
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
	for _, upload := range uploads {
		if upload.evictedLogicalIdx >= 0 {
			cleanup, err := chunk.recordNodePointerPatch(frame, chunk.streamer.bricks[upload.evictedLogicalIdx].nodeIndex, 0)
			if err != nil {
				cleanupImmediate()
				return err
			}
			cleanups = append(cleanups, cleanup)
			bufferTouched = true
		}

		cleanup, err := chunk.recordBrickUpload(frame, upload.logicalIndex, upload.slot)
		if err != nil {
			cleanupImmediate()
			return err
		}
		cleanups = append(cleanups, cleanup)

		cleanup, err = chunk.recordNodePointerPatch(frame, chunk.streamer.bricks[upload.logicalIndex].nodeIndex, upload.slot)
		if err != nil {
			cleanupImmediate()
			return err
		}
		cleanups = append(cleanups, cleanup)
		bufferTouched = true
	}

	for _, eviction := range evictions {
		cleanup, err := chunk.recordNodePointerPatch(frame, chunk.streamer.bricks[eviction.logicalIndex].nodeIndex, 0)
		if err != nil {
			cleanupImmediate()
			return err
		}
		cleanups = append(cleanups, cleanup)
		bufferTouched = true
	}

	if len(uploads) > 0 {
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

	for _, cleanup := range cleanups {
		frame.renderer.deferFrameRelease(frame.FrameSlot, cleanup)
	}
	return nil
}

func (chunk *ChunkResources) recordNodePointerPatch(frame *Frame, nodeIndex, value uint32) (func(), error) {
	patchBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(patchBytes, value)

	stagingBuffer, stagingMemory, _, err := frame.renderer.createFilledStagingBufferBytes(patchBytes)
	if err != nil {
		return nil, fmt.Errorf("creating node pointer patch staging buffer: %w", err)
	}

	regions := []vk.BufferCopy{{
		SrcOffset: 0,
		DstOffset: nodeChildPointerByteOffset(nodeIndex),
		Size:      4,
	}}
	vk.CmdCopyBuffer(frame.CommandBuffer, stagingBuffer, chunk.buffer, uint32(len(regions)), regions)

	return func() {
		vk.DestroyBuffer(chunk.device, stagingBuffer, nil)
		vk.FreeMemory(chunk.device, stagingMemory, nil)
	}, nil
}

func (chunk *ChunkResources) recordBrickUpload(frame *Frame, logicalIndex int, slot uint32) (func(), error) {
	brickX, brickY, brickZ, err := brickPoolSlotCoord(slot)
	if err != nil {
		return nil, err
	}

	stagingBuffer, stagingMemory, _, err := frame.renderer.createFilledStagingBufferBytes(chunk.streamer.bricks[logicalIndex].voxels[:])
	if err != nil {
		return nil, fmt.Errorf("creating brick upload staging buffer: %w", err)
	}

	regions := []vk.BufferImageCopy{{
		BufferOffset:      0,
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

	return func() {
		vk.DestroyBuffer(chunk.device, stagingBuffer, nil)
		vk.FreeMemory(chunk.device, stagingMemory, nil)
	}, nil
}

func nodeChildPointerByteOffset(nodeIndex uint32) vk.DeviceSize {
	return vk.DeviceSize((svoHeaderWordCount+int(nodeIndex)*2+1) * 4)
}

func (r *Renderer) createFilledStagingBufferBytes(data []byte) (vk.Buffer, vk.DeviceMemory, vk.DeviceSize, error) {
	if len(data) == 0 {
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("staging buffer data cannot be empty")
	}

	size := vk.DeviceSize(len(data))
	createInfo := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        size,
		Usage:       vk.BufferUsageFlags(vk.BufferUsageTransferSrcBit),
		SharingMode: vk.SharingModeExclusive,
	}

	var buffer vk.Buffer
	if err := withPinnedValue(&buffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &createInfo, nil, &buffer))
	}); err != nil {
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("creating staging buffer: %w", err)
	}

	var requirements vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, buffer, &requirements)
	requirements.Deref()

	var memoryProperties vk.PhysicalDeviceMemoryProperties
	vk.GetPhysicalDeviceMemoryProperties(r.physicalDevice, &memoryProperties)
	memoryProperties.Deref()

	memoryTypeIndex, err := findMemoryTypeIndex(
		memoryProperties,
		requirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
	)
	if err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("finding staging buffer memory type: %w", err)
	}

	allocateInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  requirements.Size,
		MemoryTypeIndex: memoryTypeIndex,
	}
	var memory vk.DeviceMemory
	if err := withPinnedValue(&memory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &allocateInfo, nil, &memory))
	}); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("allocating staging buffer memory: %w", err)
	}

	if err := vk.Error(vk.BindBufferMemory(r.device, buffer, memory, 0)); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		vk.FreeMemory(r.device, memory, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("binding staging buffer memory: %w", err)
	}

	var mapped unsafe.Pointer
	if err := vk.Error(vk.MapMemory(r.device, memory, 0, size, 0, &mapped)); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		vk.FreeMemory(r.device, memory, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, 0, fmt.Errorf("mapping staging buffer memory: %w", err)
	}
	copy(unsafe.Slice((*byte)(mapped), len(data)), data)
	vk.UnmapMemory(r.device, memory)

	return buffer, memory, size, nil
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}