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
	sampler     vk.Sampler
	allocated   []bool
	imageBytes  vk.DeviceSize
}

func (r *Renderer) createBrickPool() (*brickPool, error) {
	pool := &brickPool{allocated: make([]bool, brickPoolCapacity)}
	pool.allocated[0] = true

	imageCreateInfo := vk.ImageCreateInfo{
		SType:     vk.StructureTypeImageCreateInfo,
		ImageType: vk.ImageType3d,
		Format:    vk.FormatR8Uint,
		Extent: vk.Extent3D{
			Width:  brickPoolTextureEdge,
			Height: brickPoolTextureEdge,
			Depth:  brickPoolTextureEdge,
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

	var memoryProperties vk.PhysicalDeviceMemoryProperties
	vk.GetPhysicalDeviceMemoryProperties(r.physicalDevice, &memoryProperties)
	memoryProperties.Deref()

	memoryTypeIndex, err := findMemoryTypeIndex(memoryProperties, imageRequirements.MemoryTypeBits, vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit))
	if err != nil {
		pool.Close(r.device)
		return nil, fmt.Errorf("finding brick pool image memory type: %w", err)
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

	samplerCreateInfo := vk.SamplerCreateInfo{
		SType:                   vk.StructureTypeSamplerCreateInfo,
		MagFilter:               vk.FilterNearest,
		MinFilter:               vk.FilterNearest,
		MipmapMode:              vk.SamplerMipmapModeNearest,
		AddressModeU:            vk.SamplerAddressModeClampToEdge,
		AddressModeV:            vk.SamplerAddressModeClampToEdge,
		AddressModeW:            vk.SamplerAddressModeClampToEdge,
		MipLodBias:              0,
		AnisotropyEnable:        vk.False,
		MaxAnisotropy:           1,
		CompareEnable:           vk.False,
		CompareOp:               vk.CompareOpAlways,
		MinLod:                  0,
		MaxLod:                  0,
		BorderColor:             vk.BorderColorIntOpaqueBlack,
		UnnormalizedCoordinates: vk.False,
	}
	if err := withPinnedValue(&pool.sampler, func() error {
		return vk.Error(vk.CreateSampler(r.device, &samplerCreateInfo, nil, &pool.sampler))
	}); err != nil {
		pool.Close(r.device)
		return nil, fmt.Errorf("creating brick pool sampler: %w", err)
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

	for slot := uint32(1); slot < brickPoolCapacity; slot++ {
		if pool.allocated[slot] {
			continue
		}
		pool.allocated[slot] = true
		return slot, nil
	}

	return 0, fmt.Errorf("brick pool exhausted")
}

func (pool *brickPool) Free(slot uint32) {
	if pool == nil || slot == 0 || slot >= brickPoolCapacity {
		return
	}
	pool.allocated[slot] = false
}

func brickPoolSlotCoord(slot uint32) (uint32, uint32, uint32, error) {
	if slot >= brickPoolCapacity {
		return 0, 0, 0, fmt.Errorf("brick pool slot %d out of range", slot)
	}

	x := slot % brickPoolGridEdge
	y := (slot / brickPoolGridEdge) % brickPoolGridEdge
	z := slot / (brickPoolGridEdge * brickPoolGridEdge)
	return x, y, z, nil
}

func brickPoolCoordSlot(x, y, z uint32) (uint32, error) {
	if x >= brickPoolGridEdge || y >= brickPoolGridEdge || z >= brickPoolGridEdge {
		return 0, fmt.Errorf("brick pool coordinate (%d,%d,%d) out of range", x, y, z)
	}
	return x + y*brickPoolGridEdge + z*brickPoolGridEdge*brickPoolGridEdge, nil
}

func (pool *brickPool) Close(device vk.Device) {
	if pool == nil {
		return
	}
	if !isZeroValue(pool.sampler) {
		vk.DestroySampler(device, pool.sampler, nil)
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