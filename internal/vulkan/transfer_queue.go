package vulkan

import (
	"fmt"

	vk "github.com/vulkan-go/vulkan"
)

// crossQueueSharing returns the sharing-mode triple for a resource that is
// produced on the transfer queue and consumed on the graphics queue. When the
// renderer has a dedicated transfer queue family distinct from the graphics
// family, the resource must be CONCURRENT-shared so cross-queue access does
// not require explicit ownership-transfer barriers. When no dedicated transfer
// queue exists (or the families are the same), EXCLUSIVE sharing is correct
// and slightly faster.
//
// The returned indices slice is retained by Vulkan only for the duration of
// the create call; callers must keep it alive across CreateBuffer/CreateImage.
func (r *Renderer) crossQueueSharing() (vk.SharingMode, uint32, []uint32) {
	if !r.hasDedicatedTransferQueue || r.transferQueueIndex == r.graphicsQueueIndex {
		return vk.SharingModeExclusive, 0, nil
	}
	indices := []uint32{r.graphicsQueueIndex, r.transferQueueIndex}
	return vk.SharingModeConcurrent, uint32(len(indices)), indices
}

// beginOneTimeTransferCommands allocates a primary command buffer from the
// transfer command pool when a dedicated transfer queue is available, falling
// back to the graphics pool otherwise. Pair every successful call with
// endOneTimeTransferCommands or freeOneTimeTransferCommands on failure.
func (r *Renderer) beginOneTimeTransferCommands() (vk.CommandBuffer, bool, error) {
	useTransferPool := r.hasDedicatedTransferQueue && !isZeroValue(r.transferCommandPool)
	pool := r.commandPool
	if useTransferPool {
		pool = r.transferCommandPool
	}

	commandBuffers := make([]vk.CommandBuffer, 1)
	var zero vk.CommandBuffer
	allocateInfo := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        pool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: 1,
	}
	if err := withPinnedSlice(commandBuffers, func() error {
		return vk.Error(vk.AllocateCommandBuffers(r.device, &allocateInfo, commandBuffers))
	}); err != nil {
		return zero, useTransferPool, fmt.Errorf("allocating transfer command buffer: %w", err)
	}

	beginInfo := vk.CommandBufferBeginInfo{
		SType: vk.StructureTypeCommandBufferBeginInfo,
		Flags: vk.CommandBufferUsageFlags(vk.CommandBufferUsageOneTimeSubmitBit),
	}
	if err := vk.Error(vk.BeginCommandBuffer(commandBuffers[0], &beginInfo)); err != nil {
		r.freeOneTimeTransferCommands(commandBuffers[0], useTransferPool)
		return zero, useTransferPool, fmt.Errorf("beginning transfer command buffer: %w", err)
	}

	return commandBuffers[0], useTransferPool, nil
}

func (r *Renderer) endOneTimeTransferCommands(commandBuffer vk.CommandBuffer, usedTransferPool bool) error {
	if err := vk.Error(vk.EndCommandBuffer(commandBuffer)); err != nil {
		r.freeOneTimeTransferCommands(commandBuffer, usedTransferPool)
		return fmt.Errorf("ending transfer command buffer: %w", err)
	}

	queue := r.graphicsQueue
	if usedTransferPool {
		queue = r.transferQueue
	}
	commandBuffers := []vk.CommandBuffer{commandBuffer}
	submitInfos := []vk.SubmitInfo{{
		SType:              vk.StructureTypeSubmitInfo,
		CommandBufferCount: 1,
		PCommandBuffers:    commandBuffers,
	}}
	var fence vk.Fence
	if err := vk.Error(vk.QueueSubmit(queue, 1, submitInfos, fence)); err != nil {
		r.freeOneTimeTransferCommands(commandBuffer, usedTransferPool)
		return fmt.Errorf("submitting transfer command buffer: %w", err)
	}
	if err := vk.Error(vk.QueueWaitIdle(queue)); err != nil {
		r.freeOneTimeTransferCommands(commandBuffer, usedTransferPool)
		return fmt.Errorf("waiting for transfer command buffer: %w", err)
	}

	r.freeOneTimeTransferCommands(commandBuffer, usedTransferPool)
	return nil
}

func (r *Renderer) freeOneTimeTransferCommands(commandBuffer vk.CommandBuffer, usedTransferPool bool) {
	if isZeroValue(commandBuffer) {
		return
	}
	pool := r.commandPool
	if usedTransferPool {
		pool = r.transferCommandPool
	}
	if isZeroValue(pool) {
		return
	}
	vk.FreeCommandBuffers(r.device, pool, 1, []vk.CommandBuffer{commandBuffer})
}

// HasDedicatedTransferQueue reports whether the renderer is using a separate
// transfer-only queue family for bulk async uploads. Useful for tests and for
// surfacing the configuration in diagnostics.
func (r *Renderer) HasDedicatedTransferQueue() bool {
	return r.hasDedicatedTransferQueue
}
