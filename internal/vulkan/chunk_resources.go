package vulkan

import (
	"errors"
	"fmt"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

type ChunkResources struct {
	DescriptorSet vk.DescriptorSet
	Width         uint32
	Height        uint32
	Depth         uint32

	device         vk.Device
	descriptorPool vk.DescriptorPool
	buffer         vk.Buffer
	bufferMemory   vk.DeviceMemory
	image          vk.Image
	imageView      vk.ImageView
	imageMemory    vk.DeviceMemory
	imageLayout    vk.ImageLayout
}

func (r *Renderer) CreateChunkResourcesFromData(chunkBindings *ChunkBindings, data []uint32, width, height, depth uint32) (*ChunkResources, error) {
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
		Width:       width,
		Height:      height,
		Depth:       depth,
		device:      r.device,
		imageLayout: vk.ImageLayoutUndefined,
	}

	if err := r.createChunkStorageBuffer(chunk, data); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkStorageImage(chunk); err != nil {
		chunk.Close()
		return nil, err
	}
	if err := r.createChunkDescriptorSet(chunkBindings, chunk); err != nil {
		chunk.Close()
		return nil, err
	}

	return chunk, nil
}

func (r *Renderer) createChunkStorageBuffer(chunk *ChunkResources, data []uint32) error {
	createInfo := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        vk.DeviceSize(len(data) * 4),
		Usage:       vk.BufferUsageFlags(vk.BufferUsageStorageBufferBit),
		SharingMode: vk.SharingModeExclusive,
	}
	if err := withPinnedValue(&chunk.buffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &createInfo, nil, &chunk.buffer))
	}); err != nil {
		return fmt.Errorf("creating chunk storage buffer: %w", err)
	}

	var memoryRequirements vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, chunk.buffer, &memoryRequirements)
	memoryRequirements.Deref()

	var memoryProperties vk.PhysicalDeviceMemoryProperties
	vk.GetPhysicalDeviceMemoryProperties(r.physicalDevice, &memoryProperties)
	memoryProperties.Deref()

	memoryTypeIndex, err := findMemoryTypeIndex(
		memoryProperties,
		memoryRequirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
	)
	if err != nil {
		return fmt.Errorf("finding chunk buffer memory type: %w", err)
	}

	allocateInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  memoryRequirements.Size,
		MemoryTypeIndex: memoryTypeIndex,
	}
	if err := withPinnedValue(&chunk.bufferMemory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &allocateInfo, nil, &chunk.bufferMemory))
	}); err != nil {
		return fmt.Errorf("allocating chunk buffer memory: %w", err)
	}
	if err := vk.Error(vk.BindBufferMemory(r.device, chunk.buffer, chunk.bufferMemory, 0)); err != nil {
		return fmt.Errorf("binding chunk buffer memory: %w", err)
	}

	var mapped unsafe.Pointer
	if err := vk.Error(vk.MapMemory(r.device, chunk.bufferMemory, 0, createInfo.Size, 0, &mapped)); err != nil {
		return fmt.Errorf("mapping chunk buffer memory: %w", err)
	}

	mappedData := unsafe.Slice((*uint32)(mapped), len(data))
	copy(mappedData, data)
	vk.UnmapMemory(r.device, chunk.bufferMemory)

	return nil
}

func (r *Renderer) createChunkStorageImage(chunk *ChunkResources) error {
	createInfo := vk.ImageCreateInfo{
		SType:         vk.StructureTypeImageCreateInfo,
		ImageType:     vk.ImageType3d,
		Format:        vk.FormatR32Uint,
		Extent:        vk.Extent3D{Width: chunk.Width, Height: chunk.Height, Depth: chunk.Depth},
		MipLevels:     1,
		ArrayLayers:   1,
		Samples:       vk.SampleCount1Bit,
		Tiling:        vk.ImageTilingOptimal,
		Usage:         vk.ImageUsageFlags(vk.ImageUsageStorageBit),
		SharingMode:   vk.SharingModeExclusive,
		InitialLayout: vk.ImageLayoutUndefined,
	}
	if err := withPinnedValue(&chunk.image, func() error {
		return vk.Error(vk.CreateImage(r.device, &createInfo, nil, &chunk.image))
	}); err != nil {
		return fmt.Errorf("creating chunk image: %w", err)
	}

	var memoryRequirements vk.MemoryRequirements
	vk.GetImageMemoryRequirements(r.device, chunk.image, &memoryRequirements)
	memoryRequirements.Deref()

	var memoryProperties vk.PhysicalDeviceMemoryProperties
	vk.GetPhysicalDeviceMemoryProperties(r.physicalDevice, &memoryProperties)
	memoryProperties.Deref()

	memoryTypeIndex, err := findMemoryTypeIndex(
		memoryProperties,
		memoryRequirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit),
	)
	if err != nil {
		memoryTypeIndex, err = findMemoryTypeIndex(memoryProperties, memoryRequirements.MemoryTypeBits, 0)
		if err != nil {
			return fmt.Errorf("finding chunk image memory type: %w", err)
		}
	}

	allocateInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  memoryRequirements.Size,
		MemoryTypeIndex: memoryTypeIndex,
	}
	if err := withPinnedValue(&chunk.imageMemory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &allocateInfo, nil, &chunk.imageMemory))
	}); err != nil {
		return fmt.Errorf("allocating chunk image memory: %w", err)
	}
	if err := vk.Error(vk.BindImageMemory(r.device, chunk.image, chunk.imageMemory, 0)); err != nil {
		return fmt.Errorf("binding chunk image memory: %w", err)
	}

	viewCreateInfo := vk.ImageViewCreateInfo{
		SType:    vk.StructureTypeImageViewCreateInfo,
		Image:    chunk.image,
		ViewType: vk.ImageViewType3d,
		Format:   vk.FormatR32Uint,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			BaseMipLevel:   0,
			LevelCount:     1,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
	}
	if err := withPinnedValue(&chunk.imageView, func() error {
		return vk.Error(vk.CreateImageView(r.device, &viewCreateInfo, nil, &chunk.imageView))
	}); err != nil {
		return fmt.Errorf("creating chunk image view: %w", err)
	}

	return nil
}

func (r *Renderer) createChunkDescriptorSet(chunkBindings *ChunkBindings, chunk *ChunkResources) error {
	poolSizes := []vk.DescriptorPoolSize{
		{Type: vk.DescriptorTypeStorageBuffer, DescriptorCount: 1},
		{Type: vk.DescriptorTypeStorageImage, DescriptorCount: 1},
	}
	poolCreateInfo := vk.DescriptorPoolCreateInfo{
		SType:         vk.StructureTypeDescriptorPoolCreateInfo,
		MaxSets:       1,
		PoolSizeCount: uint32(len(poolSizes)),
		PPoolSizes:    poolSizes,
	}
	if err := withPinnedValue(&chunk.descriptorPool, func() error {
		return vk.Error(vk.CreateDescriptorPool(r.device, &poolCreateInfo, nil, &chunk.descriptorPool))
	}); err != nil {
		return fmt.Errorf("creating chunk descriptor pool: %w", err)
	}

	setLayouts := []vk.DescriptorSetLayout{chunkBindings.Layout()}
	allocateInfo := vk.DescriptorSetAllocateInfo{
		SType:              vk.StructureTypeDescriptorSetAllocateInfo,
		DescriptorPool:     chunk.descriptorPool,
		DescriptorSetCount: 1,
		PSetLayouts:        setLayouts,
	}
	if err := withPinnedValue(&chunk.DescriptorSet, func() error {
		return vk.Error(vk.AllocateDescriptorSets(r.device, &allocateInfo, &chunk.DescriptorSet))
	}); err != nil {
		return fmt.Errorf("allocating chunk descriptor set: %w", err)
	}

	bufferInfos := []vk.DescriptorBufferInfo{{
		Buffer: chunk.buffer,
		Offset: 0,
		Range:  vk.DeviceSize(chunk.Width * chunk.Height * chunk.Depth * 4),
	}}
	imageInfos := []vk.DescriptorImageInfo{{
		ImageView:   chunk.imageView,
		ImageLayout: vk.ImageLayoutGeneral,
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
			DescriptorType:  vk.DescriptorTypeStorageImage,
			PImageInfo:      imageInfos,
		},
	}
	vk.UpdateDescriptorSets(r.device, uint32(len(writeDescriptorSets)), writeDescriptorSets, 0, nil)

	return nil
}

func (r *Renderer) SubmitOneTimeCommands(record func(vk.CommandBuffer) error) error {
	if record == nil {
		return errors.New("one-time command recorder is required")
	}

	commandBuffer, err := r.beginOneTimeCommands()
	if err != nil {
		return err
	}

	if err := record(commandBuffer); err != nil {
		r.freeOneTimeCommands(commandBuffer)
		return err
	}

	return r.endOneTimeCommands(commandBuffer)
}

func (r *Renderer) beginOneTimeCommands() (vk.CommandBuffer, error) {
	commandBuffers := make([]vk.CommandBuffer, 1)
	var zeroCommandBuffer vk.CommandBuffer
	allocateInfo := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        r.commandPool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: 1,
	}
	if err := withPinnedSlice(commandBuffers, func() error {
		return vk.Error(vk.AllocateCommandBuffers(r.device, &allocateInfo, commandBuffers))
	}); err != nil {
		return zeroCommandBuffer, fmt.Errorf("allocating one-time command buffer: %w", err)
	}

	beginInfo := vk.CommandBufferBeginInfo{
		SType: vk.StructureTypeCommandBufferBeginInfo,
		Flags: vk.CommandBufferUsageFlags(vk.CommandBufferUsageOneTimeSubmitBit),
	}
	if err := vk.Error(vk.BeginCommandBuffer(commandBuffers[0], &beginInfo)); err != nil {
		r.freeOneTimeCommands(commandBuffers[0])
		return zeroCommandBuffer, fmt.Errorf("beginning one-time command buffer: %w", err)
	}

	return commandBuffers[0], nil
}

func (r *Renderer) endOneTimeCommands(commandBuffer vk.CommandBuffer) error {
	if err := vk.Error(vk.EndCommandBuffer(commandBuffer)); err != nil {
		r.freeOneTimeCommands(commandBuffer)
		return fmt.Errorf("ending one-time command buffer: %w", err)
	}

	commandBuffers := []vk.CommandBuffer{commandBuffer}
	submitInfos := []vk.SubmitInfo{{
		SType:              vk.StructureTypeSubmitInfo,
		CommandBufferCount: 1,
		PCommandBuffers:    commandBuffers,
	}}
	var fence vk.Fence
	if err := vk.Error(vk.QueueSubmit(r.graphicsQueue, 1, submitInfos, fence)); err != nil {
		r.freeOneTimeCommands(commandBuffer)
		return fmt.Errorf("submitting one-time command buffer: %w", err)
	}
	if err := vk.Error(vk.QueueWaitIdle(r.graphicsQueue)); err != nil {
		r.freeOneTimeCommands(commandBuffer)
		return fmt.Errorf("waiting for one-time command buffer: %w", err)
	}

	r.freeOneTimeCommands(commandBuffer)
	return nil
}

func (r *Renderer) freeOneTimeCommands(commandBuffer vk.CommandBuffer) {
	if isZeroValue(commandBuffer) || isZeroValue(r.commandPool) {
		return
	}

	vk.FreeCommandBuffers(r.device, r.commandPool, 1, []vk.CommandBuffer{commandBuffer})
}

func (chunk *ChunkResources) Close() {
	if chunk == nil || isZeroValue(chunk.device) {
		return
	}

	if !isZeroValue(chunk.descriptorPool) {
		vk.DestroyDescriptorPool(chunk.device, chunk.descriptorPool, nil)
	}
	if !isZeroValue(chunk.imageView) {
		vk.DestroyImageView(chunk.device, chunk.imageView, nil)
	}
	if !isZeroValue(chunk.image) {
		vk.DestroyImage(chunk.device, chunk.image, nil)
	}
	if !isZeroValue(chunk.imageMemory) {
		vk.FreeMemory(chunk.device, chunk.imageMemory, nil)
	}
	if !isZeroValue(chunk.buffer) {
		vk.DestroyBuffer(chunk.device, chunk.buffer, nil)
	}
	if !isZeroValue(chunk.bufferMemory) {
		vk.FreeMemory(chunk.device, chunk.bufferMemory, nil)
	}

	*chunk = ChunkResources{}
}
