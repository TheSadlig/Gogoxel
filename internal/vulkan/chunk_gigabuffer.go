package vulkan

import (
	"fmt"

	vk "github.com/vulkan-go/vulkan"
)

const defaultChunkGigabufferBytes = 256 * 1024 * 1024

type chunkGigabufferSpan struct {
	offset vk.DeviceSize
	size   vk.DeviceSize
}

type chunkGigabuffer struct {
	device    vk.Device
	buffer    vk.Buffer
	memory    vk.DeviceMemory
	capacity  vk.DeviceSize
	alignment vk.DeviceSize
	freeList  []chunkGigabufferSpan
}

func (r *Renderer) createChunkGigabuffer() error {
	if r == nil || !isZeroValue(r.chunkGigabuffer.buffer) {
		return nil
	}

	gigabuffer, err := newChunkGigabuffer(r, defaultChunkGigabufferBytes)
	if err != nil {
		return err
	}
	r.chunkGigabuffer = gigabuffer
	return nil
}

func newChunkGigabuffer(r *Renderer, capacity vk.DeviceSize) (chunkGigabuffer, error) {
	if r == nil {
		return chunkGigabuffer{}, fmt.Errorf("renderer is required")
	}
	if capacity <= 0 {
		capacity = defaultChunkGigabufferBytes
	}

	alignment := r.storageBufferOffsetAlignment
	if alignment == 0 {
		alignment = 256
	}
	if alignment < 256 {
		alignment = 256
	}
	capacity = alignDeviceSize(capacity, alignment)

	sharingMode, sharingCount, sharingIndices := r.crossQueueSharing()
	createInfo := vk.BufferCreateInfo{
		SType:                 vk.StructureTypeBufferCreateInfo,
		Size:                  capacity,
		Usage:                 vk.BufferUsageFlags(vk.BufferUsageStorageBufferBit | vk.BufferUsageTransferDstBit),
		SharingMode:           sharingMode,
		QueueFamilyIndexCount: sharingCount,
		PQueueFamilyIndices:   sharingIndices,
	}

	var buffer vk.Buffer
	if err := withPinnedValue(&buffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &createInfo, nil, &buffer))
	}); err != nil {
		return chunkGigabuffer{}, fmt.Errorf("creating chunk gigabuffer: %w", err)
	}

	var requirements vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, buffer, &requirements)
	requirements.Deref()

	memoryTypeIdx, err := findMemoryTypeIndex(
		r.memoryProperties,
		requirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit),
	)
	if err != nil {
		memoryTypeIdx, err = findMemoryTypeIndex(
			r.memoryProperties,
			requirements.MemoryTypeBits,
			vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
		)
		if err != nil {
			vk.DestroyBuffer(r.device, buffer, nil)
			return chunkGigabuffer{}, fmt.Errorf("finding chunk gigabuffer memory type: %w", err)
		}
	}

	var memory vk.DeviceMemory
	if err := withPinnedValue(&memory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &vk.MemoryAllocateInfo{
			SType:           vk.StructureTypeMemoryAllocateInfo,
			AllocationSize:  requirements.Size,
			MemoryTypeIndex: memoryTypeIdx,
		}, nil, &memory))
	}); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		return chunkGigabuffer{}, fmt.Errorf("allocating chunk gigabuffer memory: %w", err)
	}
	if err := vk.Error(vk.BindBufferMemory(r.device, buffer, memory, 0)); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		vk.FreeMemory(r.device, memory, nil)
		return chunkGigabuffer{}, fmt.Errorf("binding chunk gigabuffer memory: %w", err)
	}

	return chunkGigabuffer{
		device:    r.device,
		buffer:    buffer,
		memory:    memory,
		capacity:  capacity,
		alignment: alignment,
		freeList:  []chunkGigabufferSpan{{offset: 0, size: capacity}},
	}, nil
}

func (g *chunkGigabuffer) Allocate(size vk.DeviceSize) (vk.DeviceSize, vk.DeviceSize, error) {
	if g == nil || g.capacity == 0 {
		return 0, 0, fmt.Errorf("chunk gigabuffer is not initialized")
	}
	if size <= 0 {
		return 0, 0, fmt.Errorf("gigabuffer allocation must be non-zero")
	}

	alignedSize := alignDeviceSize(size, g.alignment)
	for index, span := range g.freeList {
		if span.size < alignedSize {
			continue
		}
		offset := span.offset
		if span.size == alignedSize {
			g.freeList = append(g.freeList[:index], g.freeList[index+1:]...)
		} else {
			g.freeList[index].offset += alignedSize
			g.freeList[index].size -= alignedSize
		}
		return offset, alignedSize, nil
	}
	return 0, 0, fmt.Errorf("chunk gigabuffer exhausted: need %d bytes, have %d bytes free", alignedSize, g.freeBytes())
}

func (g *chunkGigabuffer) Free(offset, size vk.DeviceSize) {
	if g == nil || size <= 0 {
		return
	}

	size = alignDeviceSize(size, g.alignment)
	span := chunkGigabufferSpan{offset: offset, size: size}
	insertAt := len(g.freeList)
	for index, existing := range g.freeList {
		if offset < existing.offset {
			insertAt = index
			break
		}
	}
	g.freeList = append(g.freeList, chunkGigabufferSpan{})
	copy(g.freeList[insertAt+1:], g.freeList[insertAt:])
	g.freeList[insertAt] = span

	merged := g.freeList[:0]
	for _, current := range g.freeList {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		if last.offset+last.size >= current.offset {
			end := current.offset + current.size
			if last.offset+last.size < end {
				last.size = end - last.offset
			}
			continue
		}
		merged = append(merged, current)
	}
	g.freeList = merged
}

func (g *chunkGigabuffer) Close() {
	if g == nil || isZeroValue(g.device) {
		return
	}
	if !isZeroValue(g.buffer) {
		vk.DestroyBuffer(g.device, g.buffer, nil)
	}
	if !isZeroValue(g.memory) {
		vk.FreeMemory(g.device, g.memory, nil)
	}
	*g = chunkGigabuffer{}
}

func (g *chunkGigabuffer) freeBytes() vk.DeviceSize {
	if g == nil {
		return 0
	}
	free := vk.DeviceSize(0)
	for _, span := range g.freeList {
		free += span.size
	}
	return free
}
