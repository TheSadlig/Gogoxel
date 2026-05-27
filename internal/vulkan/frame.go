package vulkan

import (
	"errors"
	"fmt"
	"math"

	"Gogoxel/internal/vulkan/vkbridge"

	vk "github.com/vulkan-go/vulkan"
)

type Frame struct {
	renderer              *Renderer
	CommandBuffer         vk.CommandBuffer
	TransferCommandBuffer vk.CommandBuffer
	ImageIndex            uint32
	FrameSlot             int
	Extent                vk.Extent2D
	renderPassOpen        bool
	transferRecorded      bool
	transferUploadCount   int
}

func (f *Frame) Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32) {
	vk.CmdDraw(f.CommandBuffer, vertexCount, instanceCount, firstVertex, firstInstance)
}

func (f *Frame) BeginRenderPass() {
	if f.renderPassOpen {
		return
	}

	clearValues := []vk.ClearValue{
		vk.NewClearValue([]float32{0.66, 0.78, 0.93, 1.0}),
	}

	renderPassInfo := vk.RenderPassBeginInfo{
		SType:           vk.StructureTypeRenderPassBeginInfo,
		RenderPass:      f.renderer.renderPass,
		Framebuffer:     f.renderer.swapchainFramebuffers[f.ImageIndex],
		ClearValueCount: uint32(len(clearValues)),
		PClearValues:    clearValues,
		RenderArea: vk.Rect2D{
			Offset: vk.Offset2D{X: 0, Y: 0},
			Extent: f.renderer.swapchainExtent,
		},
	}

	vk.CmdBeginRenderPass(f.CommandBuffer, &renderPassInfo, vk.SubpassContentsInline)
	f.renderPassOpen = true
}

func (f *Frame) EndRenderPass() {
	if !f.renderPassOpen {
		return
	}

	vk.CmdEndRenderPass(f.CommandBuffer)
	f.renderPassOpen = false
}

func (f *Frame) TransferCommands() vk.CommandBuffer {
	if f == nil || isZeroValue(f.TransferCommandBuffer) {
		var zero vk.CommandBuffer
		return zero
	}
	if f.TransferCommandBuffer != f.CommandBuffer {
		f.transferRecorded = true
	}
	return f.TransferCommandBuffer
}

func (f *Frame) RecordTransferUploads(count int) {
	if f == nil || count <= 0 {
		return
	}
	if f.TransferCommandBuffer != f.CommandBuffer {
		f.transferUploadCount += count
	}
}

func (r *Renderer) allocateCommandBuffers() error {
	r.commandBuffers = make([]vk.CommandBuffer, len(r.swapchainFramebuffers))
	allocateInfo := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        r.commandPool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: uint32(len(r.commandBuffers)),
	}
	if err := withPinnedSlice(r.commandBuffers, func() error {
		return vk.Error(vk.AllocateCommandBuffers(r.device, &allocateInfo, r.commandBuffers))
	}); err != nil {
		return fmt.Errorf("allocating command buffers: %w", err)
	}
	if r.hasDedicatedTransferQueue {
		r.transferCommandBuffers = make([]vk.CommandBuffer, maxFramesInFlight)
		transferAllocateInfo := vk.CommandBufferAllocateInfo{
			SType:              vk.StructureTypeCommandBufferAllocateInfo,
			CommandPool:        r.transferCommandPool,
			Level:              vk.CommandBufferLevelPrimary,
			CommandBufferCount: uint32(len(r.transferCommandBuffers)),
		}
		if err := withPinnedSlice(r.transferCommandBuffers, func() error {
			return vk.Error(vk.AllocateCommandBuffers(r.device, &transferAllocateInfo, r.transferCommandBuffers))
		}); err != nil {
			return fmt.Errorf("allocating transfer command buffers: %w", err)
		}
	}

	return nil
}

func (r *Renderer) createSyncObjects() error {
	r.imageAvailableSemaphores = make([]vk.Semaphore, maxFramesInFlight)
	r.inFlightFences = make([]vk.Fence, maxFramesInFlight)
	r.renderFinishedSemaphores = make([]vk.Semaphore, len(r.swapchainImages))
	if r.hasDedicatedTransferQueue {
		r.transferFinishedSemaphores = make([]vk.Semaphore, maxFramesInFlight)
		r.graphicsFinishedSemaphores = make([]vk.Semaphore, maxFramesInFlight)
	}
	r.imagesInFlight = make([]vk.Fence, len(r.swapchainImages))
	r.frameReleases = make([][]func(), maxFramesInFlight)
	r.currentFrame = 0

	for index := range r.imageAvailableSemaphores {
		semaphore, err := vkbridge.CreateSemaphore(r.device)
		if err != nil {
			return fmt.Errorf("creating image-available semaphore %d: %w", index, err)
		}
		r.imageAvailableSemaphores[index] = semaphore

		fence, err := vkbridge.CreateFence(r.device, vk.FenceCreateFlags(vk.FenceCreateSignaledBit))
		if err != nil {
			return fmt.Errorf("creating in-flight fence %d: %w", index, err)
		}
		r.inFlightFences[index] = fence
	}

	for index := range r.renderFinishedSemaphores {
		semaphore, err := vkbridge.CreateSemaphore(r.device)
		if err != nil {
			return fmt.Errorf("creating render-finished semaphore %d: %w", index, err)
		}
		r.renderFinishedSemaphores[index] = semaphore
	}
	for index := range r.transferFinishedSemaphores {
		semaphore, err := vkbridge.CreateSemaphore(r.device)
		if err != nil {
			return fmt.Errorf("creating transfer-finished semaphore %d: %w", index, err)
		}
		r.transferFinishedSemaphores[index] = semaphore
	}
	for index := range r.graphicsFinishedSemaphores {
		semaphore, err := vkbridge.CreateSemaphore(r.device)
		if err != nil {
			return fmt.Errorf("creating graphics-finished semaphore %d: %w", index, err)
		}
		r.graphicsFinishedSemaphores[index] = semaphore
	}

	return nil
}

func (r *Renderer) DrawFrame(record func(*Frame) error) error {
	return r.drawFrame(record, nil)
}

func (r *Renderer) drawFrame(record func(*Frame) error, afterRecord func(*Frame) error) error {
	if record == nil {
		return errors.New("draw callback is required")
	}

	currentFrame := r.currentFrame
	fences := []vk.Fence{r.inFlightFences[currentFrame]}
	if err := vk.Error(vk.WaitForFences(r.device, 1, fences, vk.True, math.MaxUint64)); err != nil {
		return fmt.Errorf("waiting for in-flight fence: %w", err)
	}
	// The fence is signalled → this slot's previous timestamp pair is
	// guaranteed visible. Read it before the command buffer resets the pool.
	if r.gpuTimestamps != nil {
		r.gpuTimestamps.readPrevious(currentFrame)
	}
	r.runDeferredReleases(currentFrame)

	var imageIndex uint32
	var nullFence vk.Fence
	acquireResult := vk.AcquireNextImage(r.device, r.swapchain, math.MaxUint64, r.imageAvailableSemaphores[currentFrame], nullFence, &imageIndex)
	if acquireResult != vk.Success && acquireResult != vk.Suboptimal {
		return fmt.Errorf("acquiring next swapchain image: %w", vk.Error(acquireResult))
	}

	if imageFence := r.imagesInFlight[imageIndex]; !isZeroValue(imageFence) {
		imageFences := []vk.Fence{imageFence}
		if err := vk.Error(vk.WaitForFences(r.device, 1, imageFences, vk.True, math.MaxUint64)); err != nil {
			return fmt.Errorf("waiting for swapchain image %d fence: %w", imageIndex, err)
		}
	}
	r.imagesInFlight[imageIndex] = r.inFlightFences[currentFrame]

	if err := vk.Error(vk.ResetFences(r.device, 1, fences)); err != nil {
		return fmt.Errorf("resetting in-flight fence: %w", err)
	}

	frame, err := r.recordCommandBuffer(currentFrame, imageIndex, record, afterRecord)
	if err != nil {
		return err
	}

	waitSemaphores := []vk.Semaphore{r.imageAvailableSemaphores[currentFrame]}
	waitStages := []vk.PipelineStageFlags{vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit)}
	commandBuffers := []vk.CommandBuffer{r.commandBuffers[imageIndex]}
	signalSemaphores := []vk.Semaphore{r.renderFinishedSemaphores[imageIndex]}
	if r.hasDedicatedTransferQueue {
		if frame.transferRecorded {
			transferCommandBuffers := []vk.CommandBuffer{r.transferCommandBuffers[currentFrame]}
			transferSignalSemaphores := []vk.Semaphore{r.transferFinishedSemaphores[currentFrame]}
			transferSubmit := []vk.SubmitInfo{{
				SType:                vk.StructureTypeSubmitInfo,
				CommandBufferCount:   1,
				PCommandBuffers:      transferCommandBuffers,
				SignalSemaphoreCount: 1,
				PSignalSemaphores:    transferSignalSemaphores,
			}}
			if r.graphicsSubmitCount > 0 {
				previousFrame := (currentFrame + len(r.inFlightFences) - 1) % len(r.inFlightFences)
				transferWaitSemaphores := []vk.Semaphore{r.graphicsFinishedSemaphores[previousFrame]}
				transferWaitStages := []vk.PipelineStageFlags{vk.PipelineStageFlags(vk.PipelineStageTransferBit)}
				transferSubmit[0].WaitSemaphoreCount = 1
				transferSubmit[0].PWaitSemaphores = transferWaitSemaphores
				transferSubmit[0].PWaitDstStageMask = transferWaitStages
			}
			var transferFence vk.Fence
			if err := vk.Error(vk.QueueSubmit(r.transferQueue, 1, transferSubmit, transferFence)); err != nil {
				return fmt.Errorf("submitting transfer command: %w", err)
			}
			waitSemaphores = append(waitSemaphores, r.transferFinishedSemaphores[currentFrame])
			waitStages = append(waitStages, vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit))
		}
		signalSemaphores = append(signalSemaphores, r.graphicsFinishedSemaphores[currentFrame])
	}
	submitInfo := []vk.SubmitInfo{{
		SType:                vk.StructureTypeSubmitInfo,
		WaitSemaphoreCount:   uint32(len(waitSemaphores)),
		PWaitSemaphores:      waitSemaphores,
		PWaitDstStageMask:    waitStages,
		CommandBufferCount:   1,
		PCommandBuffers:      commandBuffers,
		SignalSemaphoreCount: uint32(len(signalSemaphores)),
		PSignalSemaphores:    signalSemaphores,
	}}

	if err := vk.Error(vk.QueueSubmit(r.graphicsQueue, 1, submitInfo, r.inFlightFences[currentFrame])); err != nil {
		return fmt.Errorf("submitting draw command: %w", err)
	}
	if frame.transferRecorded {
		r.lastTransferUploadCount = frame.transferUploadCount
		r.transferQueueUploadCount += uint64(frame.transferUploadCount)
	} else {
		r.lastTransferUploadCount = 0
	}
	r.graphicsSubmitCount++

	swapchains := []vk.Swapchain{r.swapchain}
	imageIndices := []uint32{imageIndex}
	presentWaitSemaphores := []vk.Semaphore{r.renderFinishedSemaphores[imageIndex]}
	presentInfo := vk.PresentInfo{
		SType:              vk.StructureTypePresentInfo,
		WaitSemaphoreCount: 1,
		PWaitSemaphores:    presentWaitSemaphores,
		SwapchainCount:     1,
		PSwapchains:        swapchains,
		PImageIndices:      imageIndices,
	}

	presentResult := vk.QueuePresent(r.presentQueue, &presentInfo)
	if presentResult != vk.Success && presentResult != vk.Suboptimal {
		return fmt.Errorf("presenting frame: %w", vk.Error(presentResult))
	}

	r.currentFrame = (r.currentFrame + 1) % len(r.inFlightFences)

	return nil
}

func (r *Renderer) recordCommandBuffer(frameSlot int, imageIndex uint32, record func(*Frame) error, afterRecord func(*Frame) error) (*Frame, error) {
	commandBuffer := r.commandBuffers[imageIndex]
	if err := vk.Error(vk.ResetCommandBuffer(commandBuffer, 0)); err != nil {
		return nil, fmt.Errorf("resetting command buffer %d: %w", imageIndex, err)
	}

	beginInfo := vk.CommandBufferBeginInfo{
		SType: vk.StructureTypeCommandBufferBeginInfo,
	}
	if err := vk.Error(vk.BeginCommandBuffer(commandBuffer, &beginInfo)); err != nil {
		return nil, fmt.Errorf("beginning command buffer %d: %w", imageIndex, err)
	}
	transferCommandBuffer := commandBuffer
	if r.hasDedicatedTransferQueue {
		transferCommandBuffer = r.transferCommandBuffers[frameSlot]
		if err := vk.Error(vk.ResetCommandBuffer(transferCommandBuffer, 0)); err != nil {
			return nil, fmt.Errorf("resetting transfer command buffer %d: %w", frameSlot, err)
		}
		if err := vk.Error(vk.BeginCommandBuffer(transferCommandBuffer, &beginInfo)); err != nil {
			return nil, fmt.Errorf("beginning transfer command buffer %d: %w", frameSlot, err)
		}
	}

	// Reset and write BEGIN timestamp before any GPU work. The slot is
	// derived from frameSlot (cycled 0..maxFramesInFlight-1) so reads in
	// the next frame line up with this slot's fence.
	if r.gpuTimestamps != nil {
		r.gpuTimestamps.recordReset(commandBuffer, frameSlot)
		r.gpuTimestamps.writeBegin(commandBuffer, frameSlot)
	}

	frame := &Frame{
		renderer:              r,
		CommandBuffer:         commandBuffer,
		TransferCommandBuffer: transferCommandBuffer,
		ImageIndex:            imageIndex,
		FrameSlot:             frameSlot,
		Extent:                r.swapchainExtent,
	}

	if err := record(frame); err != nil {
		frame.EndRenderPass()
		return nil, err
	}

	frame.EndRenderPass()
	if afterRecord != nil {
		if err := afterRecord(frame); err != nil {
			return nil, err
		}
	}

	// END timestamp at bottom of pipe, after all GPU work for this frame.
	if r.gpuTimestamps != nil {
		r.gpuTimestamps.writeEnd(commandBuffer, frameSlot)
	}

	if err := vk.Error(vk.EndCommandBuffer(commandBuffer)); err != nil {
		return nil, fmt.Errorf("ending command buffer %d: %w", imageIndex, err)
	}
	if r.hasDedicatedTransferQueue {
		if err := vk.Error(vk.EndCommandBuffer(transferCommandBuffer)); err != nil {
			return nil, fmt.Errorf("ending transfer command buffer %d: %w", frameSlot, err)
		}
	}

	return frame, nil
}
