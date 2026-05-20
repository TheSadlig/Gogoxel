package vulkan

import (
	"fmt"

	vk "github.com/vulkan-go/vulkan"
)

const (
	brickSizeVoxels      uint32 = 8
	brickPoolTextureEdge uint32 = 512
	brickPoolGridEdge           = brickPoolTextureEdge / brickSizeVoxels
	brickPoolCapacity           = brickPoolGridEdge * brickPoolGridEdge * brickPoolGridEdge
)

type brickPool struct {
	image       vk.Image
	imageMemory vk.DeviceMemory
	imageView   vk.ImageView
	textureEdge uint32
	gridEdge    uint32
	capacity    uint32

	// allocated[slot] mirrors the free-list for double-free / bounds safety.
	allocated []bool
	// freeList is a LIFO stack of free slot indices. Allocate pops; Free pushes.
	// O(1) for both operations vs. the previous O(brickPoolCapacity) linear scan.
	freeList []uint32

	imageBytes vk.DeviceSize
}

func newBrickPoolState(textureEdge uint32) (*brickPool, error) {
	if textureEdge < brickSizeVoxels || textureEdge%brickSizeVoxels != 0 {
		return nil, fmt.Errorf("brick pool edge %d must be a multiple of brick size %d", textureEdge, brickSizeVoxels)
	}

	gridEdge := textureEdge / brickSizeVoxels
	capacity := gridEdge * gridEdge * gridEdge
	if capacity == 0 {
		return nil, fmt.Errorf("brick pool edge %d yields zero capacity", textureEdge)
	}

	freeListCap := 0
	if capacity > 1 {
		freeListCap = int(capacity - 1)
	}

	pool := &brickPool{
		textureEdge: textureEdge,
		gridEdge:    gridEdge,
		capacity:    capacity,
		allocated:   make([]bool, int(capacity)),
		freeList:    make([]uint32, 0, freeListCap),
	}
	pool.allocated[0] = true
	for slot := capacity; slot > 1; slot-- {
		pool.freeList = append(pool.freeList, slot-1)
	}

	return pool, nil
}

func validateBrickPoolTextureEdge(textureEdge, maxImageDimension3D uint32) error {
	if textureEdge < brickSizeVoxels || textureEdge%brickSizeVoxels != 0 {
		return fmt.Errorf("brick pool edge %d must be a multiple of brick size %d", textureEdge, brickSizeVoxels)
	}
	if maxImageDimension3D > 0 && textureEdge > maxImageDimension3D {
		return fmt.Errorf("brick pool edge %d exceeds device 3D image limit %d", textureEdge, maxImageDimension3D)
	}
	return nil
}

func (r *Renderer) createBrickPool() (*brickPool, error) {
	return r.createBrickPoolWithEdge(brickPoolTextureEdge)
}

func (r *Renderer) createAirOnlyBrickPool() (*brickPool, error) {
	return r.createBrickPoolWithEdge(brickSizeVoxels)
}

func (r *Renderer) createBrickPoolWithEdge(textureEdge uint32) (*brickPool, error) {
	var properties vk.PhysicalDeviceProperties
	vk.GetPhysicalDeviceProperties(r.physicalDevice, &properties)
	properties.Deref()
	if err := validateBrickPoolTextureEdge(textureEdge, properties.Limits.MaxImageDimension3D); err != nil {
		return nil, err
	}

	pool, err := newBrickPoolState(textureEdge)
	if err != nil {
		return nil, err
	}

	imageCreateInfo := vk.ImageCreateInfo{
		SType:     vk.StructureTypeImageCreateInfo,
		ImageType: vk.ImageType3d,
		Format:    vk.FormatR8Uint,
		Extent: vk.Extent3D{
			Width:  pool.textureEdge,
			Height: pool.textureEdge,
			Depth:  pool.textureEdge,
		},
		MipLevels:     1,
		ArrayLayers:   1,
		Samples:       vk.SampleCount1Bit,
		Tiling:        vk.ImageTilingOptimal,
		Usage:         vk.ImageUsageFlags(vk.ImageUsageTransferDstBit | vk.ImageUsageSampledBit),
		SharingMode:   vk.SharingModeExclusive,
		InitialLayout: vk.ImageLayoutUndefined,
	}
	if err := withPinnedValue(&pool.image, func() error {
		return vk.Error(vk.CreateImage(r.device, &imageCreateInfo, nil, &pool.image))
	}); err != nil {
		return nil, fmt.Errorf("creating brick pool image: %w", err)
	}

	var imageRequirements vk.MemoryRequirements
	vk.GetImageMemoryRequirements(r.device, pool.image, &imageRequirements)
	imageRequirements.Deref()

	memoryTypeIndex, err := findMemoryTypeIndex(r.memoryProperties, imageRequirements.MemoryTypeBits, vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit))
	if err != nil {
		// UMA / integrated GPUs may expose only host-visible memory. Mirror the
		// SSBO fallback so the brick pool still allocates on those devices.
		memoryTypeIndex, err = findMemoryTypeIndex(r.memoryProperties, imageRequirements.MemoryTypeBits, vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit))
		if err != nil {
			pool.Close(r.device)
			return nil, fmt.Errorf("finding brick pool image memory type: %w", err)
		}
	}

	allocateInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  imageRequirements.Size,
		MemoryTypeIndex: memoryTypeIndex,
	}
	pool.imageBytes = allocateInfo.AllocationSize
	if err := withPinnedValue(&pool.imageMemory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &allocateInfo, nil, &pool.imageMemory))
	}); err != nil {
		pool.Close(r.device)
		return nil, fmt.Errorf("allocating brick pool image memory: %w", err)
	}

	if err := vk.Error(vk.BindImageMemory(r.device, pool.image, pool.imageMemory, 0)); err != nil {
		pool.Close(r.device)
		return nil, fmt.Errorf("binding brick pool image memory: %w", err)
	}

	viewCreateInfo := vk.ImageViewCreateInfo{
		SType:    vk.StructureTypeImageViewCreateInfo,
		Image:    pool.image,
		ViewType: vk.ImageViewType3d,
		Format:   vk.FormatR8Uint,
		Components: vk.ComponentMapping{
			R: vk.ComponentSwizzleIdentity,
			G: vk.ComponentSwizzleIdentity,
			B: vk.ComponentSwizzleIdentity,
			A: vk.ComponentSwizzleIdentity,
		},
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			BaseMipLevel:   0,
			LevelCount:     1,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
	}
	if err := withPinnedValue(&pool.imageView, func() error {
		return vk.Error(vk.CreateImageView(r.device, &viewCreateInfo, nil, &pool.imageView))
	}); err != nil {
		pool.Close(r.device)
		return nil, fmt.Errorf("creating brick pool image view: %w", err)
	}

	if err := r.clearBrickPoolImage(pool.image); err != nil {
		pool.Close(r.device)
		return nil, err
	}

	return pool, nil
}

func (r *Renderer) clearBrickPoolImage(image vk.Image) error {
	return r.SubmitOneTimeCommands(func(commandBuffer vk.CommandBuffer) error {
		r.transitionImageLayout(
			commandBuffer,
			image,
			vk.ImageLayoutUndefined,
			vk.ImageLayoutTransferDstOptimal,
			0,
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.PipelineStageFlags(vk.PipelineStageTopOfPipeBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		)

		ranges := []vk.ImageSubresourceRange{{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			BaseMipLevel:   0,
			LevelCount:     1,
			BaseArrayLayer: 0,
			LayerCount:     1,
		}}
		var clearColor vk.ClearColorValue
		vk.CmdClearColorImage(commandBuffer, image, vk.ImageLayoutTransferDstOptimal, &clearColor, uint32(len(ranges)), ranges)

		r.transitionImageLayout(
			commandBuffer,
			image,
			vk.ImageLayoutTransferDstOptimal,
			vk.ImageLayoutShaderReadOnlyOptimal,
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.AccessFlags(vk.AccessShaderReadBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
			vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
		)

		return nil
	})
}

func (r *Renderer) transitionImageLayout(commandBuffer vk.CommandBuffer, image vk.Image, oldLayout, newLayout vk.ImageLayout, srcAccessMask, dstAccessMask vk.AccessFlags, srcStageMask, dstStageMask vk.PipelineStageFlags) {
	barriers := []vk.ImageMemoryBarrier{{
		SType:               vk.StructureTypeImageMemoryBarrier,
		SrcAccessMask:       srcAccessMask,
		DstAccessMask:       dstAccessMask,
		OldLayout:           oldLayout,
		NewLayout:           newLayout,
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored,
		DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Image:               image,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			BaseMipLevel:   0,
			LevelCount:     1,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
	}}
	vk.CmdPipelineBarrier(commandBuffer, srcStageMask, dstStageMask, 0, 0, nil, 0, nil, uint32(len(barriers)), barriers)
}

func (pool *brickPool) Allocate() (uint32, error) {
	if pool == nil {
		return 0, fmt.Errorf("brick pool is nil")
	}
	if len(pool.freeList) == 0 {
		return 0, fmt.Errorf("brick pool exhausted")
	}
	slot := pool.freeList[len(pool.freeList)-1]
	pool.freeList = pool.freeList[:len(pool.freeList)-1]
	pool.allocated[slot] = true
	return slot, nil
}

func (pool *brickPool) Free(slot uint32) {
	if pool == nil || slot == 0 || slot >= pool.capacity {
		return
	}
	if !pool.allocated[slot] {
		// Double-free guard: do nothing.
		return
	}
	pool.allocated[slot] = false
	pool.freeList = append(pool.freeList, slot)
}

func (pool *brickPool) slotCoord(slot uint32) (uint32, uint32, uint32, error) {
	if pool == nil {
		return 0, 0, 0, fmt.Errorf("brick pool is nil")
	}
	return brickPoolSlotCoordForGrid(slot, pool.gridEdge)
}

func brickPoolSlotCoord(slot uint32) (uint32, uint32, uint32, error) {
	return brickPoolSlotCoordForGrid(slot, brickPoolGridEdge)
}

func brickPoolSlotCoordForGrid(slot, gridEdge uint32) (uint32, uint32, uint32, error) {
	if gridEdge == 0 {
		return 0, 0, 0, fmt.Errorf("brick pool grid edge must be non-zero")
	}
	capacity := gridEdge * gridEdge * gridEdge
	if slot >= capacity {
		return 0, 0, 0, fmt.Errorf("brick pool slot %d out of range", slot)
	}

	x := slot % gridEdge
	y := (slot / gridEdge) % gridEdge
	z := slot / (gridEdge * gridEdge)
	return x, y, z, nil
}

func brickPoolCoordSlot(x, y, z uint32) (uint32, error) {
	return brickPoolCoordSlotForGrid(x, y, z, brickPoolGridEdge)
}

func brickPoolCoordSlotForGrid(x, y, z, gridEdge uint32) (uint32, error) {
	if gridEdge == 0 {
		return 0, fmt.Errorf("brick pool grid edge must be non-zero")
	}
	if x >= gridEdge || y >= gridEdge || z >= gridEdge {
		return 0, fmt.Errorf("brick pool coordinate (%d,%d,%d) out of range", x, y, z)
	}
	return x + y*gridEdge + z*gridEdge*gridEdge, nil
}

func (pool *brickPool) Close(device vk.Device) {
	if pool == nil {
		return
	}
	if !isZeroValue(pool.imageView) {
		vk.DestroyImageView(device, pool.imageView, nil)
	}
	if !isZeroValue(pool.image) {
		vk.DestroyImage(device, pool.image, nil)
	}
	if !isZeroValue(pool.imageMemory) {
		vk.FreeMemory(device, pool.imageMemory, nil)
	}
	*pool = brickPool{}
}

func (pool *brickPool) GPUBytes() uint64 {
	if pool == nil {
		return 0
	}
	return uint64(pool.imageBytes)
}
