package vulkan

import (
	"fmt"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

// stagingRing is a single persistently-mapped host-coherent buffer used as a
// bump allocator for one in-flight frame slot. The fence wait at the start of
// each frame guarantees the GPU is done reading any prior data from this
// slot's ring, so the offset can be reset and the space reused.
type stagingRing struct {
	buffer vk.Buffer
	memory vk.DeviceMemory
	mapped unsafe.Pointer
	size   vk.DeviceSize
	offset vk.DeviceSize
}

// stagingRingBytesPerSlot sizes each frame slot's staging ring. It must be
// large enough to hold one frame's worth of streaming uploads plus scene-copy
// headroom on camera-driven reload frames. A 1 MiB slot keeps this buffer tiny
// relative to scene memory while leaving room for higher high-speed budgets.
const stagingRingBytesPerSlot vk.DeviceSize = 1024 * 1024

func alignDeviceSize(value, alignment vk.DeviceSize) vk.DeviceSize {
	if alignment <= 1 {
		return value
	}
	remainder := value % alignment
	if remainder == 0 {
		return value
	}
	return value + alignment - remainder
}

func (r *Renderer) initStagingRings() error {
	for slot := 0; slot < maxFramesInFlight; slot++ {
		ring, err := r.createStagingRing(stagingRingBytesPerSlot)
		if err != nil {
			r.destroyStagingRings()
			return fmt.Errorf("creating staging ring %d: %w", slot, err)
		}
		r.stagingRings[slot] = ring
	}
	return nil
}

func (r *Renderer) createStagingRing(size vk.DeviceSize) (*stagingRing, error) {
	ring := &stagingRing{size: size}

	createInfo := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        size,
		Usage:       vk.BufferUsageFlags(vk.BufferUsageTransferSrcBit),
		SharingMode: vk.SharingModeExclusive,
	}
	if err := withPinnedValue(&ring.buffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &createInfo, nil, &ring.buffer))
	}); err != nil {
		return nil, fmt.Errorf("creating staging buffer: %w", err)
	}

	var requirements vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, ring.buffer, &requirements)
	requirements.Deref()

	typeIndex, err := findMemoryTypeIndex(
		r.memoryProperties,
		requirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
	)
	if err != nil {
		vk.DestroyBuffer(r.device, ring.buffer, nil)
		return nil, fmt.Errorf("finding staging memory type: %w", err)
	}

	allocateInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  requirements.Size,
		MemoryTypeIndex: typeIndex,
	}
	if err := withPinnedValue(&ring.memory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &allocateInfo, nil, &ring.memory))
	}); err != nil {
		vk.DestroyBuffer(r.device, ring.buffer, nil)
		return nil, fmt.Errorf("allocating staging memory: %w", err)
	}

	if err := vk.Error(vk.BindBufferMemory(r.device, ring.buffer, ring.memory, 0)); err != nil {
		vk.FreeMemory(r.device, ring.memory, nil)
		vk.DestroyBuffer(r.device, ring.buffer, nil)
		return nil, fmt.Errorf("binding staging memory: %w", err)
	}

	if err := vk.Error(vk.MapMemory(r.device, ring.memory, 0, size, 0, &ring.mapped)); err != nil {
		vk.FreeMemory(r.device, ring.memory, nil)
		vk.DestroyBuffer(r.device, ring.buffer, nil)
		return nil, fmt.Errorf("mapping staging memory: %w", err)
	}

	return ring, nil
}

func (r *Renderer) destroyStagingRings() {
	for slot, ring := range r.stagingRings {
		if ring == nil {
			continue
		}
		ring.destroy(r.device)
		r.stagingRings[slot] = nil
	}
}

func (ring *stagingRing) destroy(device vk.Device) {
	if ring == nil {
		return
	}
	if !isZeroValue(ring.memory) {
		vk.UnmapMemory(device, ring.memory)
		vk.FreeMemory(device, ring.memory, nil)
	}
	if !isZeroValue(ring.buffer) {
		vk.DestroyBuffer(device, ring.buffer, nil)
	}
	*ring = stagingRing{}
}

// stagingAlloc reserves `size` bytes of host-coherent staging memory inside
// the ring for the given frame slot. The returned (buffer, offset, dst) lets
// the caller `copy` into `dst` and then reference the GPU buffer/offset in a
// command. Returns an error if the ring is exhausted for this frame.
func (r *Renderer) stagingAlloc(frameSlot int, size vk.DeviceSize, alignment vk.DeviceSize) (vk.Buffer, vk.DeviceSize, unsafe.Pointer, error) {
	if frameSlot < 0 || frameSlot >= len(r.stagingRings) {
		return vk.NullBuffer, 0, nil, fmt.Errorf("frame slot %d out of range", frameSlot)
	}
	ring := r.stagingRings[frameSlot]
	if ring == nil {
		return vk.NullBuffer, 0, nil, fmt.Errorf("staging ring not initialized for slot %d", frameSlot)
	}
	if alignment < 1 {
		alignment = 1
	}

	aligned := alignDeviceSize(ring.offset, alignment)
	if aligned+size > ring.size {
		return vk.NullBuffer, 0, nil, fmt.Errorf("staging ring slot %d exhausted (need %d, have %d)", frameSlot, size, ring.size-aligned)
	}
	dst := unsafe.Pointer(uintptr(ring.mapped) + uintptr(aligned))
	ring.offset = aligned + size
	return ring.buffer, aligned, dst, nil
}

func stagingRingCanAllocate(ring *stagingRing, size vk.DeviceSize, alignment vk.DeviceSize) bool {
	if ring == nil {
		return false
	}
	if alignment < 1 {
		alignment = 1
	}
	aligned := alignDeviceSize(ring.offset, alignment)
	return aligned+size <= ring.size
}

func (r *Renderer) stagingAllocForUpload(frameSlot int, size vk.DeviceSize, alignment vk.DeviceSize) (vk.Buffer, vk.DeviceSize, unsafe.Pointer, func(), error) {
	if frameSlot < 0 || frameSlot >= len(r.stagingRings) {
		return vk.NullBuffer, 0, nil, nil, fmt.Errorf("frame slot %d out of range", frameSlot)
	}
	ring := r.stagingRings[frameSlot]
	if ring == nil {
		return vk.NullBuffer, 0, nil, nil, fmt.Errorf("staging ring not initialized for slot %d", frameSlot)
	}
	if size <= stagingRingBytesPerSlot && stagingRingCanAllocate(ring, size, alignment) {
		buffer, offset, dst, err := r.stagingAlloc(frameSlot, size, alignment)
		return buffer, offset, dst, nil, err
	}
	ring, err := r.createStagingRing(size)
	if err != nil {
		return vk.NullBuffer, 0, nil, nil, err
	}
	return ring.buffer, 0, ring.mapped, func() { ring.destroy(r.device) }, nil
}

func (r *Renderer) resetStagingRing(frameSlot int) {
	if frameSlot < 0 || frameSlot >= len(r.stagingRings) {
		return
	}
	if ring := r.stagingRings[frameSlot]; ring != nil {
		ring.offset = 0
	}
}
