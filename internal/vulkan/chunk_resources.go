package vulkan

import (
	"errors"
	"fmt"
	"unsafe"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"

	vk "github.com/vulkan-go/vulkan"
)

type ChunkResources struct {
	DescriptorSet vk.DescriptorSet

	device               vk.Device
	descriptorPool       vk.DescriptorPool
	ownsBuffer           bool
	buffer               vk.Buffer
	bufferMemory         vk.DeviceMemory
	bufferOffset         vk.DeviceSize
	bufferBytes          vk.DeviceSize
	gigabuffer           *chunkGigabuffer
	brickPool            *brickPool
	ownsBrickPool        bool
	streamer             *brickStreamer
	pendingWords         []uint32
	pendingBricks        []world.Brick
	pendingBrickIndices  []int
	pendingPaletteWords  []uint32
	pendingPaletteOffset vk.DeviceSize
	pendingWordPatches   []bufferWordPatch
	pendingSceneOrigin   [3]int32
	sceneOrigin          [3]int32
	camera               platform.Camera
	cameraEverSet        bool
}

const minChunkStorageBufferBytes vk.DeviceSize = 64 * 1024

type bufferWordPatch struct {
	dstOffset vk.DeviceSize
	value     uint32
}

func (r *Renderer) CreateChunkResourcesFromData(chunkBindings *ChunkBindings, data []uint8, palette [255]uint32, width, height, depth uint32) (*ChunkResources, error) {
	if len(data) == 0 {
		return nil, errors.New("chunk data cannot be empty")
	}
	if chunkBindings == nil || isZeroValue(chunkBindings.Layout()) {
		return nil, errors.New("chunk bindings are required")
	}
	if width == 0 || height == 0 || depth == 0 {
		return nil, errors.New("chunk dimensions must be non-zero")
	}

	expectedSize := int(width * height * depth)
	if len(data) != expectedSize {
		return nil, fmt.Errorf("chunk data length %d does not match dimensions %dx%dx%d", len(data), width, height, depth)
	}

	chunk := &ChunkResources{
		device: r.device,
	}

	if err := r.createChunkStorageBuffer(chunk, data, palette); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkBrickPool(chunk, false); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkDescriptorSet(chunkBindings, chunk); err != nil {
		chunk.Close()
		return nil, err
	}

	return chunk, nil
}

func (r *Renderer) CreateChunkResourcesFromSVO(chunkBindings *ChunkBindings, svo *world.SVO, sceneOrigin [3]int32) (*ChunkResources, error) {
	if svo == nil {
		return nil, errors.New("svo is required")
	}
	if chunkBindings == nil || isZeroValue(chunkBindings.Layout()) {
		return nil, errors.New("chunk bindings are required")
	}

	words := svo.StorageBufferWordsRef()
	if len(words) < 3 {
		return nil, errors.New("svo must contain at least one node")
	}

	chunk := &ChunkResources{device: r.device}
	if err := r.createChunkStorageBufferWords(chunk, words); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkBrickPool(chunk, true); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkDescriptorSet(chunkBindings, chunk); err != nil {
		chunk.Close()
		return nil, err
	}
	chunk.streamer = newBrickStreamer(svo.BricksRef())
	if chunk.streamer != nil {
		chunk.streamer.setSceneOrigin(sceneOrigin)
	}
	chunk.sceneOrigin = sceneOrigin

	return chunk, nil
}

func (r *Renderer) createChunkStorageBuffer(chunk *ChunkResources, data []uint8, palette [255]uint32) error {
	return r.createChunkStorageBufferWords(chunk, packChunkData(data, palette))
}

func (r *Renderer) createChunkBrickPool(chunk *ChunkResources, dedicated bool) error {
	if chunk == nil {
		return errors.New("chunk resources are required")
	}

	var (
		pool *brickPool
		err  error
	)
	if dedicated {
		pool, err = r.createBrickPool()
		chunk.ownsBrickPool = true
	} else {
		pool, err = r.sharedAirOnlyBrickPool()
		chunk.ownsBrickPool = false
	}
	if err != nil {
		return err
	}
	chunk.brickPool = pool
	return nil
}

func (r *Renderer) sharedAirOnlyBrickPool() (*brickPool, error) {
	if r.sharedAirPool != nil {
		return r.sharedAirPool, nil
	}

	pool, err := r.createAirOnlyBrickPool()
	if err != nil {
		return nil, err
	}
	r.sharedAirPool = pool
	return pool, nil
}

func (r *Renderer) createChunkStorageBufferWords(chunk *ChunkResources, words []uint32) error {
	if len(words) == 0 {
		return errors.New("storage buffer data cannot be empty")
	}
	if chunk == nil {
		return errors.New("chunk resources are required")
	}
	if isZeroValue(r.chunkGigabuffer.buffer) {
		return errors.New("chunk gigabuffer is not initialized")
	}

	copySize := vk.DeviceSize(len(words) * 4)
	bufferSize := growChunkStorageBufferSize(copySize)
	bufferOffset, allocSize, err := r.chunkGigabuffer.Allocate(bufferSize)
	if err != nil {
		return err
	}
	chunk.ownsBuffer = false
	chunk.buffer = r.chunkGigabuffer.buffer
	chunk.bufferOffset = bufferOffset
	chunk.bufferBytes = allocSize
	chunk.gigabuffer = &r.chunkGigabuffer

	// ── Staging buffer (CPU-writable) ────────────────────────────────────────
	stagingCreateInfo := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        copySize,
		Usage:       vk.BufferUsageFlags(vk.BufferUsageTransferSrcBit),
		SharingMode: vk.SharingModeExclusive,
	}
	var stagingBuffer vk.Buffer
	if err := withPinnedValue(&stagingBuffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &stagingCreateInfo, nil, &stagingBuffer))
	}); err != nil {
		return fmt.Errorf("creating staging buffer: %w", err)
	}
	defer vk.DestroyBuffer(r.device, stagingBuffer, nil)

	var stagingReqs vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, stagingBuffer, &stagingReqs)
	stagingReqs.Deref()

	stagingTypeIdx, err := findMemoryTypeIndex(
		r.memoryProperties,
		stagingReqs.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
	)
	if err != nil {
		return fmt.Errorf("finding staging buffer memory type: %w", err)
	}
	var stagingMemory vk.DeviceMemory
	if err := withPinnedValue(&stagingMemory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &vk.MemoryAllocateInfo{
			SType:           vk.StructureTypeMemoryAllocateInfo,
			AllocationSize:  stagingReqs.Size,
			MemoryTypeIndex: stagingTypeIdx,
		}, nil, &stagingMemory))
	}); err != nil {
		return fmt.Errorf("allocating staging memory: %w", err)
	}
	defer vk.FreeMemory(r.device, stagingMemory, nil)

	if err := vk.Error(vk.BindBufferMemory(r.device, stagingBuffer, stagingMemory, 0)); err != nil {
		return fmt.Errorf("binding staging memory: %w", err)
	}
	var mapped unsafe.Pointer
	if err := vk.Error(vk.MapMemory(r.device, stagingMemory, 0, copySize, 0, &mapped)); err != nil {
		return fmt.Errorf("mapping staging memory: %w", err)
	}
	copy(unsafe.Slice((*uint32)(mapped), len(words)), words)
	vk.UnmapMemory(r.device, stagingMemory)

	// ── Copy staging → device-local ──────────────────────────────────────────
	return r.SubmitOneTimeCommands(func(cb vk.CommandBuffer) error {
		regions := []vk.BufferCopy{{
			SrcOffset: 0,
			DstOffset: chunk.bufferOffset,
			Size:      copySize,
		}}
		vk.CmdCopyBuffer(cb, stagingBuffer, chunk.buffer, 1, regions)
		return nil
	})
}

func growChunkStorageBufferSize(size vk.DeviceSize) vk.DeviceSize {
	if size <= minChunkStorageBufferBytes {
		return minChunkStorageBufferBytes
	}
	capacity := minChunkStorageBufferBytes
	for capacity < size {
		capacity <<= 1
	}
	return capacity
}

func (chunk *ChunkResources) QueueSceneUpdate(svo *world.SVO, sceneOrigin [3]int32) bool {
	if chunk == nil || svo == nil {
		return false
	}
	words := svo.StorageBufferWordsRef()
	if len(words) == 0 {
		return false
	}
	size := vk.DeviceSize(len(words) * 4)
	if size > chunk.bufferBytes {
		return false
	}
	if sceneOrigin == chunk.sceneOrigin && chunk.streamer != nil {
		brickIndices := svo.LastEditTouchedBrickIndices()
		if len(brickIndices) > 0 && chunk.queueIncrementalSceneUpdate(svo, words, brickIndices) {
			chunk.pendingSceneOrigin = sceneOrigin
			return true
		}
	}
	chunk.pendingWords = words
	chunk.pendingBricks = svo.BricksRef()
	chunk.pendingSceneOrigin = sceneOrigin
	chunk.pendingBrickIndices = nil
	chunk.pendingPaletteWords = nil
	chunk.pendingPaletteOffset = 0
	chunk.pendingWordPatches = nil
	return true
}

func (chunk *ChunkResources) RecordSceneUpdate(frame *Frame) error {
	if chunk == nil || frame == nil {
		return nil
	}
	if len(chunk.pendingWords) == 0 && len(chunk.pendingWordPatches) == 0 && len(chunk.pendingPaletteWords) == 0 {
		return nil
	}

	if len(chunk.pendingWords) > 0 {
		size := vk.DeviceSize(len(chunk.pendingWords) * 4)
		stagingBuffer, stagingOffset, dst, release, err := frame.renderer.stagingAllocForUpload(frame.FrameSlot, size, 4)
		if err != nil {
			return fmt.Errorf("allocating staging space for scene update: %w", err)
		}
		if release != nil {
			frame.renderer.deferFrameRelease(frame.FrameSlot, release)
		}
		copy(unsafe.Slice((*uint32)(dst), len(chunk.pendingWords)), chunk.pendingWords)
		regions := []vk.BufferCopy{{SrcOffset: stagingOffset, DstOffset: chunk.bufferOffset, Size: size}}
		vk.CmdCopyBuffer(frame.TransferCommands(), stagingBuffer, chunk.buffer, 1, regions)
		frame.RecordTransferUploads(1)
		// The scene upload resets every brick-leaf childPtr in chunk.buffer to the
		// CPU snapshot value (usually 0). Any later vkCmdFillBuffer pointer patch in
		// this command buffer, including patches emitted by RecordStreaming for new
		// bricks, must execute after this copy or edited brick regions can render as
		// missing/stale 8x8 patches.
		frame.renderer.recordBufferTransferBarrier(frame.TransferCommands(), chunk.buffer, chunk.bufferOffset, chunk.bufferBytes)
	}

	if err := chunk.recordBufferWordPatches(frame, chunk.pendingWordPatches); err != nil {
		return err
	}
	if err := chunk.recordBufferWordSlice(frame, chunk.pendingPaletteOffset, chunk.pendingPaletteWords); err != nil {
		return err
	}

	plan := sceneUpdatePlan{}
	if len(chunk.pendingWords) > 0 && chunk.streamer != nil {
		plan = chunk.streamer.replaceSceneBricks(chunk.brickPool, chunk.pendingSceneOrigin, chunk.pendingBricks)
	} else if len(chunk.pendingWords) > 0 && len(chunk.pendingBricks) > 0 {
		chunk.streamer = newBrickStreamer(chunk.pendingBricks)
		chunk.streamer.setSceneOrigin(chunk.pendingSceneOrigin)
		if chunk.streamer != nil && chunk.cameraEverSet {
			chunk.streamer.primeCamera(chunk.camera)
		}
	} else if chunk.streamer != nil && len(chunk.pendingBrickIndices) > 0 {
		plan.uploads = chunk.streamer.updateSceneBricks(chunk.pendingBricks, chunk.pendingBrickIndices)
	}

	if len(plan.uploads) > 0 {
		frame.renderer.transitionChunkTransferDst(frame, chunk.brickPool.image)
		for _, upload := range plan.uploads {
			if err := chunk.recordBrickUpload(frame, upload.logicalIndex, upload.slot); err != nil {
				return err
			}
		}
		frame.renderer.transitionChunkShaderRead(frame, chunk.brickPool.image)
	}
	for _, patch := range plan.pointerPatches {
		chunk.patchNodePointer(frame, patch.nodeIndex, patch.slot)
	}
	if frame.TransferCommandBuffer == frame.CommandBuffer {
		frame.renderer.recordBufferShaderBarrier(frame.CommandBuffer, chunk.buffer, chunk.bufferOffset, chunk.bufferBytes)
	}

	chunk.sceneOrigin = chunk.pendingSceneOrigin
	chunk.pendingWords = nil
	chunk.pendingBricks = nil
	chunk.pendingBrickIndices = nil
	chunk.pendingPaletteWords = nil
	chunk.pendingPaletteOffset = 0
	chunk.pendingWordPatches = nil
	return nil
}

func (chunk *ChunkResources) queueIncrementalSceneUpdate(svo *world.SVO, words []uint32, brickIndices []int) bool {
	if chunk == nil || svo == nil || len(words) == 0 || len(brickIndices) == 0 {
		return false
	}
	nodeCount := svo.NodeCount()
	paletteOffset := svoHeaderWordCount + nodeCount*2
	if paletteOffset+world.PaletteSize > len(words) {
		return false
	}
	bricks := svo.BricksRef()
	patches := make([]bufferWordPatch, 0, len(brickIndices))
	for _, brickIndex := range brickIndices {
		if brickIndex < 0 || brickIndex >= len(bricks) {
			return false
		}
		nodeIndex := bricks[brickIndex].NodeIndex
		wordIndex := svoHeaderWordCount + int(nodeIndex)*2
		if wordIndex < 0 || wordIndex >= len(words) {
			return false
		}
		patches = append(patches, bufferWordPatch{
			dstOffset: vk.DeviceSize(wordIndex * 4),
			value:     words[wordIndex],
		})
	}

	chunk.pendingWords = nil
	chunk.pendingBricks = bricks
	chunk.pendingBrickIndices = append(chunk.pendingBrickIndices[:0], brickIndices...)
	chunk.pendingPaletteWords = append(chunk.pendingPaletteWords[:0], words[paletteOffset:paletteOffset+world.PaletteSize]...)
	chunk.pendingPaletteOffset = vk.DeviceSize(paletteOffset * 4)
	chunk.pendingWordPatches = append(chunk.pendingWordPatches[:0], patches...)
	return true
}

func (chunk *ChunkResources) recordBufferWordPatches(frame *Frame, patches []bufferWordPatch) error {
	if chunk == nil || frame == nil || len(patches) == 0 {
		return nil
	}
	size := vk.DeviceSize(len(patches) * 4)
	stagingBuffer, stagingOffset, dst, release, err := frame.renderer.stagingAllocForUpload(frame.FrameSlot, size, 4)
	if err != nil {
		return fmt.Errorf("allocating staging space for scene patches: %w", err)
	}
	if release != nil {
		frame.renderer.deferFrameRelease(frame.FrameSlot, release)
	}
	values := unsafe.Slice((*uint32)(dst), len(patches))
	regions := make([]vk.BufferCopy, len(patches))
	for index, patch := range patches {
		values[index] = patch.value
		regions[index] = vk.BufferCopy{
			SrcOffset: stagingOffset + vk.DeviceSize(index*4),
			DstOffset: chunk.bufferOffset + patch.dstOffset,
			Size:      4,
		}
	}
	vk.CmdCopyBuffer(frame.TransferCommands(), stagingBuffer, chunk.buffer, uint32(len(regions)), regions)
	frame.RecordTransferUploads(len(regions))
	return nil
}

func (chunk *ChunkResources) recordBufferWordSlice(frame *Frame, dstOffset vk.DeviceSize, words []uint32) error {
	if chunk == nil || frame == nil || len(words) == 0 {
		return nil
	}
	size := vk.DeviceSize(len(words) * 4)
	stagingBuffer, stagingOffset, dst, release, err := frame.renderer.stagingAllocForUpload(frame.FrameSlot, size, 4)
	if err != nil {
		return fmt.Errorf("allocating staging space for scene palette update: %w", err)
	}
	if release != nil {
		frame.renderer.deferFrameRelease(frame.FrameSlot, release)
	}
	copy(unsafe.Slice((*uint32)(dst), len(words)), words)
	regions := []vk.BufferCopy{{SrcOffset: stagingOffset, DstOffset: chunk.bufferOffset + dstOffset, Size: size}}
	vk.CmdCopyBuffer(frame.TransferCommands(), stagingBuffer, chunk.buffer, 1, regions)
	frame.RecordTransferUploads(1)
	return nil
}

func packChunkData(data []uint8, palette [255]uint32) []uint32 {
	packedVoxelWords := (len(data) + 3) / 4
	packed := make([]uint32, packedVoxelWords+len(palette))
	for index, voxel := range data {
		packed[index/4] |= uint32(voxel) << ((index % 4) * 8)
	}
	copy(packed[packedVoxelWords:], palette[:])
	return packed
}

func (r *Renderer) createChunkDescriptorSet(chunkBindings *ChunkBindings, chunk *ChunkResources) error {
	if chunk == nil {
		return errors.New("chunk resources are required")
	}
	if chunk.brickPool == nil {
		return errors.New("brick pool is required")
	}

	poolSizes := []vk.DescriptorPoolSize{
		{Type: vk.DescriptorTypeStorageBuffer, DescriptorCount: 1},
		{Type: vk.DescriptorTypeSampledImage, DescriptorCount: 1},
	}
	poolCreateInfo := vk.DescriptorPoolCreateInfo{
		SType:         vk.StructureTypeDescriptorPoolCreateInfo,
		MaxSets:       1,
		PoolSizeCount: uint32(len(poolSizes)),
		PPoolSizes:    poolSizes,
	}
	var descriptorPool vk.DescriptorPool
	if err := withPinnedValue(&descriptorPool, func() error {
		return vk.Error(vk.CreateDescriptorPool(r.device, &poolCreateInfo, nil, &descriptorPool))
	}); err != nil {
		return fmt.Errorf("creating chunk descriptor pool: %w", err)
	}
	chunk.descriptorPool = descriptorPool

	setLayouts := []vk.DescriptorSetLayout{chunkBindings.Layout()}
	allocateInfo := vk.DescriptorSetAllocateInfo{
		SType:              vk.StructureTypeDescriptorSetAllocateInfo,
		DescriptorPool:     chunk.descriptorPool,
		DescriptorSetCount: 1,
		PSetLayouts:        setLayouts,
	}
	var descriptorSet vk.DescriptorSet
	if err := withPinnedValue(&descriptorSet, func() error {
		return vk.Error(vk.AllocateDescriptorSets(r.device, &allocateInfo, &descriptorSet))
	}); err != nil {
		return fmt.Errorf("allocating chunk descriptor set: %w", err)
	}
	chunk.DescriptorSet = descriptorSet

	bufferInfos := []vk.DescriptorBufferInfo{{
		Buffer: chunk.buffer,
		Offset: chunk.bufferOffset,
		Range:  chunk.bufferBytes,
	}}
	imageInfos := []vk.DescriptorImageInfo{{
		ImageView:   chunk.brickPool.imageView,
		ImageLayout: vk.ImageLayoutShaderReadOnlyOptimal,
	}}
	writeDescriptorSets := []vk.WriteDescriptorSet{
		{
			SType:           vk.StructureTypeWriteDescriptorSet,
			DstSet:          chunk.DescriptorSet,
			DstBinding:      0,
			DescriptorCount: 1,
			DescriptorType:  vk.DescriptorTypeStorageBuffer,
			PBufferInfo:     bufferInfos,
		},
		{
			SType:           vk.StructureTypeWriteDescriptorSet,
			DstSet:          chunk.DescriptorSet,
			DstBinding:      1,
			DescriptorCount: 1,
			DescriptorType:  vk.DescriptorTypeSampledImage,
			PImageInfo:      imageInfos,
		},
	}
	vk.UpdateDescriptorSets(r.device, uint32(len(writeDescriptorSets)), writeDescriptorSets, 0, nil)

	return nil
}

func (chunk *ChunkResources) RAMBytes() uint64 {
	if chunk == nil {
		return 0
	}
	return uint64(chunk.bufferBytes)
}

func (chunk *ChunkResources) VRAMBytes() uint64 {
	return chunk.GPUBytes()
}

func (r *Renderer) SubmitOneTimeCommands(record func(vk.CommandBuffer) error) error {
	if record == nil {
		return errors.New("one-time command recorder is required")
	}

	commandBuffer, usedTransferPool, err := r.beginOneTimeTransferCommands()
	if err != nil {
		return err
	}

	if err := record(commandBuffer); err != nil {
		r.freeOneTimeTransferCommands(commandBuffer, usedTransferPool)
		return err
	}

	return r.endOneTimeTransferCommands(commandBuffer, usedTransferPool)
}

func (chunk *ChunkResources) Close() {
	if chunk == nil || isZeroValue(chunk.device) {
		return
	}

	if !isZeroValue(chunk.descriptorPool) {
		vk.DestroyDescriptorPool(chunk.device, chunk.descriptorPool, nil)
	}
	if !isZeroValue(chunk.buffer) {
		if chunk.ownsBuffer {
			vk.DestroyBuffer(chunk.device, chunk.buffer, nil)
		} else if chunk.gigabuffer != nil {
			chunk.gigabuffer.Free(chunk.bufferOffset, chunk.bufferBytes)
		}
	}
	if chunk.ownsBuffer && !isZeroValue(chunk.bufferMemory) {
		vk.FreeMemory(chunk.device, chunk.bufferMemory, nil)
	}
	if chunk.streamer != nil {
		chunk.streamer.Close()
	}
	if chunk.ownsBrickPool && chunk.brickPool != nil {
		chunk.brickPool.Close(chunk.device)
	}

	*chunk = ChunkResources{}
}

func (chunk *ChunkResources) GPUBytes() uint64 {
	if chunk == nil {
		return 0
	}
	brickPoolBytes := uint64(0)
	if chunk.ownsBrickPool && chunk.brickPool != nil {
		brickPoolBytes = chunk.brickPool.GPUBytes()
	}
	return uint64(chunk.bufferBytes) + brickPoolBytes
}
