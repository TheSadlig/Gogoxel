package vulkan

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

func (r *Renderer) CaptureFramePNG(record func(*Frame) error, outputPath string) error {
	if r == nil {
		return fmt.Errorf("renderer is not initialized")
	}
	if !r.captureSupported {
		return fmt.Errorf("swapchain capture is not supported by this surface")
	}
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}

	readbackSize := vk.DeviceSize(r.swapchainExtent.Width) * vk.DeviceSize(r.swapchainExtent.Height) * 4
	buffer, memory, err := r.createReadbackBuffer(readbackSize)
	if err != nil {
		return err
	}
	defer func() {
		if !isZeroValue(buffer) {
			vk.DestroyBuffer(r.device, buffer, nil)
		}
		if !isZeroValue(memory) {
			vk.FreeMemory(r.device, memory, nil)
		}
	}()

	if err := r.drawFrame(record, func(frame *Frame) error {
		return r.recordSwapchainReadback(frame, buffer)
	}); err != nil {
		return err
	}
	if err := r.WaitIdle(); err != nil {
		return err
	}
	pixels, err := r.readbackPixels(memory, readbackSize)
	if err != nil {
		return err
	}
	return writeCapturePNG(outputPath, int(r.swapchainExtent.Width), int(r.swapchainExtent.Height), pixels, r.swapchainFormat)
}

func (r *Renderer) createReadbackBuffer(size vk.DeviceSize) (vk.Buffer, vk.DeviceMemory, error) {
	createInfo := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        size,
		Usage:       vk.BufferUsageFlags(vk.BufferUsageTransferDstBit),
		SharingMode: vk.SharingModeExclusive,
	}
	var buffer vk.Buffer
	if err := withPinnedValue(&buffer, func() error {
		return vk.Error(vk.CreateBuffer(r.device, &createInfo, nil, &buffer))
	}); err != nil {
		return vk.NullBuffer, vk.NullDeviceMemory, fmt.Errorf("creating readback buffer: %w", err)
	}

	var requirements vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(r.device, buffer, &requirements)
	requirements.Deref()
	memoryTypeIndex, err := findMemoryTypeIndex(
		r.memoryProperties,
		requirements.MemoryTypeBits,
		vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit|vk.MemoryPropertyHostCoherentBit),
	)
	if err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, fmt.Errorf("finding readback buffer memory type: %w", err)
	}
	var memory vk.DeviceMemory
	if err := withPinnedValue(&memory, func() error {
		return vk.Error(vk.AllocateMemory(r.device, &vk.MemoryAllocateInfo{
			SType:           vk.StructureTypeMemoryAllocateInfo,
			AllocationSize:  requirements.Size,
			MemoryTypeIndex: memoryTypeIndex,
		}, nil, &memory))
	}); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, fmt.Errorf("allocating readback buffer memory: %w", err)
	}
	if err := vk.Error(vk.BindBufferMemory(r.device, buffer, memory, 0)); err != nil {
		vk.DestroyBuffer(r.device, buffer, nil)
		vk.FreeMemory(r.device, memory, nil)
		return vk.NullBuffer, vk.NullDeviceMemory, fmt.Errorf("binding readback buffer memory: %w", err)
	}
	return buffer, memory, nil
}

func (r *Renderer) recordSwapchainReadback(frame *Frame, buffer vk.Buffer) error {
	if frame == nil {
		return fmt.Errorf("frame is required for readback")
	}
	image := r.swapchainImages[frame.ImageIndex]
	r.transitionImageLayout(
		frame.CommandBuffer,
		image,
		vk.ImageLayoutPresentSrc,
		vk.ImageLayoutTransferSrcOptimal,
		vk.AccessFlags(vk.AccessColorAttachmentWriteBit),
		vk.AccessFlags(vk.AccessTransferReadBit),
		vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit),
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
	)
	regions := []vk.BufferImageCopy{{
		BufferOffset:      0,
		BufferRowLength:   0,
		BufferImageHeight: 0,
		ImageSubresource: vk.ImageSubresourceLayers{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			MipLevel:       0,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
		ImageExtent: vk.Extent3D{
			Width:  r.swapchainExtent.Width,
			Height: r.swapchainExtent.Height,
			Depth:  1,
		},
	}}
	vk.CmdCopyImageToBuffer(frame.CommandBuffer, image, vk.ImageLayoutTransferSrcOptimal, buffer, uint32(len(regions)), regions)
	r.transitionImageLayout(
		frame.CommandBuffer,
		image,
		vk.ImageLayoutTransferSrcOptimal,
		vk.ImageLayoutPresentSrc,
		vk.AccessFlags(vk.AccessTransferReadBit),
		0,
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		vk.PipelineStageFlags(vk.PipelineStageBottomOfPipeBit),
	)
	return nil
}

func (r *Renderer) readbackPixels(memory vk.DeviceMemory, size vk.DeviceSize) ([]byte, error) {
	var mapped unsafe.Pointer
	if err := vk.Error(vk.MapMemory(r.device, memory, 0, size, 0, &mapped)); err != nil {
		return nil, fmt.Errorf("mapping readback memory: %w", err)
	}
	defer vk.UnmapMemory(r.device, memory)
	pixels := make([]byte, int(size))
	copy(pixels, unsafe.Slice((*byte)(mapped), int(size)))
	return pixels, nil
}

func writeCapturePNG(path string, width, height int, pixels []byte, format vk.Format) error {
	if format != vk.FormatB8g8r8a8Srgb && format != vk.FormatB8g8r8a8Unorm {
		return fmt.Errorf("swapchain capture only supports BGRA8 formats, got %d", format)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	imageBuffer := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceIndex := (y*width + x) * 4
			destinationIndex := imageBuffer.PixOffset(x, y)
			imageBuffer.Pix[destinationIndex] = pixels[sourceIndex+2]
			imageBuffer.Pix[destinationIndex+1] = pixels[sourceIndex+1]
			imageBuffer.Pix[destinationIndex+2] = pixels[sourceIndex]
			imageBuffer.Pix[destinationIndex+3] = pixels[sourceIndex+3]
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, imageBuffer)
}