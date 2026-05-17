package vulkan

import (
	"errors"
	"fmt"
	"math"

	"Gogoxel/internal/vulkan/vkbridge"

	vk "github.com/vulkan-go/vulkan"
)

type Frame struct {
	renderer       *Renderer
	CommandBuffer  vk.CommandBuffer
	ImageIndex     uint32
	Extent         vk.Extent2D
	renderPassOpen bool
}

func (f *Frame) Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32) {
	vk.CmdDraw(f.CommandBuffer, vertexCount, instanceCount, firstVertex, firstInstance)
}

func (f *Frame) BeginRenderPass() {
	if f.renderPassOpen {
		return
	}

	renderPassInfo := vk.RenderPassBeginInfo{
		SType:       vk.StructureTypeRenderPassBeginInfo,
		RenderPass:  f.renderer.renderPass,
		Framebuffer: f.renderer.swapchainFramebuffers[f.ImageIndex],
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

	return nil
}

func (r *Renderer) createSyncObjects() error {
	r.imageAvailableSemaphores = make([]vk.Semaphore, maxFramesInFlight)
	r.inFlightFences = make([]vk.Fence, maxFramesInFlight)
	r.renderFinishedSemaphores = make([]vk.Semaphore, len(r.swapchainImages))
	r.imagesInFlight = make([]vk.Fence, len(r.swapchainImages))
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

	return nil
}

func (r *Renderer) DrawFrame(record func(*Frame) error) error {
	if record == nil {
		return errors.New("draw callback is required")
	}

	currentFrame := r.currentFrame
	fences := []vk.Fence{r.inFlightFences[currentFrame]}
	if err := vk.Error(vk.WaitForFences(r.device, 1, fences, vk.True, math.MaxUint64)); err != nil {
		return fmt.Errorf("waiting for in-flight fence: %w", err)
	}

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

	if err := r.recordCommandBuffer(imageIndex, record); err != nil {
		return err
	}

	waitSemaphores := []vk.Semaphore{r.imageAvailableSemaphores[currentFrame]}
	waitStages := []vk.PipelineStageFlags{vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit)}
	commandBuffers := []vk.CommandBuffer{r.commandBuffers[imageIndex]}
	signalSemaphores := []vk.Semaphore{r.renderFinishedSemaphores[imageIndex]}
	submitInfo := []vk.SubmitInfo{{
		SType:                vk.StructureTypeSubmitInfo,
		WaitSemaphoreCount:   1,
		PWaitSemaphores:      waitSemaphores,
		PWaitDstStageMask:    waitStages,
		CommandBufferCount:   1,
		PCommandBuffers:      commandBuffers,
		SignalSemaphoreCount: 1,
		PSignalSemaphores:    signalSemaphores,
	}}

	if err := vk.Error(vk.QueueSubmit(r.graphicsQueue, 1, submitInfo, r.inFlightFences[currentFrame])); err != nil {
		return fmt.Errorf("submitting draw command: %w", err)
	}

	swapchains := []vk.Swapchain{r.swapchain}
	imageIndices := []uint32{imageIndex}
	presentInfo := vk.PresentInfo{
		SType:              vk.StructureTypePresentInfo,
		WaitSemaphoreCount: 1,
		PWaitSemaphores:    signalSemaphores,
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

func (r *Renderer) recordCommandBuffer(imageIndex uint32, record func(*Frame) error) error {
	commandBuffer := r.commandBuffers[imageIndex]
	if err := vk.Error(vk.ResetCommandBuffer(commandBuffer, 0)); err != nil {
		return fmt.Errorf("resetting command buffer %d: %w", imageIndex, err)
	}

	beginInfo := vk.CommandBufferBeginInfo{
		SType: vk.StructureTypeCommandBufferBeginInfo,
	}
	if err := vk.Error(vk.BeginCommandBuffer(commandBuffer, &beginInfo)); err != nil {
		return fmt.Errorf("beginning command buffer %d: %w", imageIndex, err)
	}

	frame := &Frame{
		renderer:      r,
		CommandBuffer: commandBuffer,
		ImageIndex:    imageIndex,
		Extent:        r.swapchainExtent,
	}

	if err := record(frame); err != nil {
		frame.EndRenderPass()
		return err
	}

	frame.EndRenderPass()

	if err := vk.Error(vk.EndCommandBuffer(commandBuffer)); err != nil {
		return fmt.Errorf("ending command buffer %d: %w", imageIndex, err)
	}

	return nil
}
