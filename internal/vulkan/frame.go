package vulkan

import (
	"fmt"
	"math"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

type Frame struct {
	renderer      *Renderer
	CommandBuffer vk.CommandBuffer
	ImageIndex    uint32
	Extent        vk.Extent2D
}

func (f *Frame) BindDefaultPipeline() {
	vk.CmdBindPipeline(f.CommandBuffer, vk.PipelineBindPointGraphics, f.renderer.pipeline)
}

func (f *Frame) Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32) {
	vk.CmdDraw(f.CommandBuffer, vertexCount, instanceCount, firstVertex, firstInstance)
}

func (r *Renderer) allocateCommandBuffers() error {
	r.commandBuffers = make([]vk.CommandBuffer, len(r.swapchainFramebuffers))
	allocateInfo := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        r.commandPool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: uint32(len(r.commandBuffers)),
	}
	if err := vk.Error(vk.AllocateCommandBuffers(r.device, &allocateInfo, r.commandBuffers)); err != nil {
		return fmt.Errorf("allocating command buffers: %w", err)
	}

	return nil
}

func (r *Renderer) createSyncObjects() error {
	semaphoreInfo := vk.SemaphoreCreateInfo{SType: vk.StructureTypeSemaphoreCreateInfo}
	fenceInfo := vk.FenceCreateInfo{
		SType: vk.StructureTypeFenceCreateInfo,
		Flags: vk.FenceCreateFlags(vk.FenceCreateSignaledBit),
	}

	if err := vk.Error(vk.CreateSemaphore(r.device, &semaphoreInfo, nil, &r.imageAvailableSemaphore)); err != nil {
		return fmt.Errorf("creating image-available semaphore: %w", err)
	}
	if err := vk.Error(vk.CreateSemaphore(r.device, &semaphoreInfo, nil, &r.renderFinishedSemaphore)); err != nil {
		return fmt.Errorf("creating render-finished semaphore: %w", err)
	}
	if err := vk.Error(vk.CreateFence(r.device, &fenceInfo, nil, &r.inFlightFence)); err != nil {
		return fmt.Errorf("creating in-flight fence: %w", err)
	}

	return nil
}

func (r *Renderer) DrawFrame(record func(*Frame) error) error {
	fences := []vk.Fence{r.inFlightFence}
	if err := vk.Error(vk.WaitForFences(r.device, 1, fences, vk.True, math.MaxUint64)); err != nil {
		return fmt.Errorf("waiting for in-flight fence: %w", err)
	}
	if err := vk.Error(vk.ResetFences(r.device, 1, fences)); err != nil {
		return fmt.Errorf("resetting in-flight fence: %w", err)
	}

	var imageIndex uint32
	var nullFence vk.Fence
	acquireResult := vk.AcquireNextImage(r.device, r.swapchain, math.MaxUint64, r.imageAvailableSemaphore, nullFence, &imageIndex)
	if acquireResult != vk.Success && acquireResult != vk.Suboptimal {
		return fmt.Errorf("acquiring next swapchain image: %w", vk.Error(acquireResult))
	}

	if err := r.recordCommandBuffer(imageIndex, record); err != nil {
		return err
	}

	waitSemaphores := []vk.Semaphore{r.imageAvailableSemaphore}
	waitStages := []vk.PipelineStageFlags{vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit)}
	commandBuffers := []vk.CommandBuffer{r.commandBuffers[imageIndex]}
	signalSemaphores := []vk.Semaphore{r.renderFinishedSemaphore}
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

	if err := vk.Error(vk.QueueSubmit(r.graphicsQueue, 1, submitInfo, r.inFlightFence)); err != nil {
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

	pushData := CameraPushConstant{
		CameraPos: [4]float32{r.RenderingInfo.CameraPosition[0], r.RenderingInfo.CameraPosition[1], r.RenderingInfo.CameraPosition[2], 0},
	}

	// 3. Record the Push Constant assignment into the active Command Buffer
	vk.CmdPushConstants(
		commandBuffer,
		r.pipelineLayout, // The layout initialized in Step 2
		vk.ShaderStageFlags(vk.ShaderStageFragmentBit), // Target stage matches exactly
		0,                               // Offset
		uint32(unsafe.Sizeof(pushData)), // Total byte size (16)
		unsafe.Pointer(&pushData),       // Native pointer to Go data slice
	)

	renderPassInfo := vk.RenderPassBeginInfo{
		SType:       vk.StructureTypeRenderPassBeginInfo,
		RenderPass:  r.renderPass,
		Framebuffer: r.swapchainFramebuffers[imageIndex],
		RenderArea: vk.Rect2D{
			Offset: vk.Offset2D{X: 0, Y: 0},
			Extent: r.swapchainExtent,
		},
	}

	vk.CmdBeginRenderPass(commandBuffer, &renderPassInfo, vk.SubpassContentsInline)

	frame := &Frame{
		renderer:      r,
		CommandBuffer: commandBuffer,
		ImageIndex:    imageIndex,
		Extent:        r.swapchainExtent,
	}

	if record != nil {
		if err := record(frame); err != nil {
			vk.CmdEndRenderPass(commandBuffer)
			return err
		}
	} else {
		frame.BindDefaultPipeline()
		frame.Draw(3, 1, 0, 0)
	}

	vk.CmdEndRenderPass(commandBuffer)

	if err := vk.Error(vk.EndCommandBuffer(commandBuffer)); err != nil {
		return fmt.Errorf("ending command buffer %d: %w", imageIndex, err)
	}

	return nil
}
