package vulkan

import (
	"encoding/binary"
	"fmt"
	"math"

	vk "github.com/vulkan-go/vulkan"
)

type queueFamilyIndices struct {
	graphics uint32
	present  uint32
	hasGraph bool
	hasPres  bool
}

type swapchainSupport struct {
	capabilities vk.SurfaceCapabilities
	formats      []vk.SurfaceFormat
	presentModes []vk.PresentMode
}

func (r *Renderer) querySwapchainSupport(device vk.PhysicalDevice) (swapchainSupport, error) {
	var support swapchainSupport

	if err := vk.Error(vk.GetPhysicalDeviceSurfaceCapabilities(device, r.surface, &support.capabilities)); err != nil {
		return support, fmt.Errorf("querying surface capabilities: %w", err)
	}
	support.capabilities.Deref()
	support.capabilities.CurrentExtent.Deref()
	support.capabilities.MinImageExtent.Deref()
	support.capabilities.MaxImageExtent.Deref()

	var formatCount uint32
	if err := vk.Error(vk.GetPhysicalDeviceSurfaceFormats(device, r.surface, &formatCount, nil)); err != nil {
		return support, fmt.Errorf("counting surface formats: %w", err)
	}
	if formatCount > 0 {
		support.formats = make([]vk.SurfaceFormat, formatCount)
		if err := vk.Error(vk.GetPhysicalDeviceSurfaceFormats(device, r.surface, &formatCount, support.formats)); err != nil {
			return support, fmt.Errorf("loading surface formats: %w", err)
		}
		for index := range support.formats {
			support.formats[index].Deref()
		}
	}

	var presentModeCount uint32
	if err := vk.Error(vk.GetPhysicalDeviceSurfacePresentModes(device, r.surface, &presentModeCount, nil)); err != nil {
		return support, fmt.Errorf("counting present modes: %w", err)
	}
	if presentModeCount > 0 {
		support.presentModes = make([]vk.PresentMode, presentModeCount)
		if err := vk.Error(vk.GetPhysicalDeviceSurfacePresentModes(device, r.surface, &presentModeCount, support.presentModes)); err != nil {
			return support, fmt.Errorf("loading present modes: %w", err)
		}
	}

	return support, nil
}

func (r *Renderer) findQueueFamilies(device vk.PhysicalDevice) (queueFamilyIndices, bool) {
	var queueFamilyCount uint32
	vk.GetPhysicalDeviceQueueFamilyProperties(device, &queueFamilyCount, nil)
	if queueFamilyCount == 0 {
		return queueFamilyIndices{}, false
	}

	queueFamilies := make([]vk.QueueFamilyProperties, queueFamilyCount)
	vk.GetPhysicalDeviceQueueFamilyProperties(device, &queueFamilyCount, queueFamilies)

	indices := queueFamilyIndices{}
	for index, family := range queueFamilies {
		queueFamilies[index].Deref()
		family = queueFamilies[index]
		if family.QueueFlags&vk.QueueFlags(vk.QueueGraphicsBit) != 0 {
			indices.graphics = uint32(index)
			indices.hasGraph = true
		}

		var presentSupported vk.Bool32
		_ = vk.GetPhysicalDeviceSurfaceSupport(device, uint32(index), r.surface, &presentSupported)
		if presentSupported == vk.True {
			indices.present = uint32(index)
			indices.hasPres = true
		}

		if indices.hasGraph && indices.hasPres {
			return indices, true
		}
	}

	return indices, false
}

func (r *Renderer) chooseExtent(capabilities vk.SurfaceCapabilities) vk.Extent2D {
	if capabilities.CurrentExtent.Width != uint32(math.MaxUint32) {
		return capabilities.CurrentExtent
	}

	width, height := r.window.FramebufferSize()
	extent := vk.Extent2D{
		Width:  clampUint32(uint32(width), capabilities.MinImageExtent.Width, capabilities.MaxImageExtent.Width),
		Height: clampUint32(uint32(height), capabilities.MinImageExtent.Height, capabilities.MaxImageExtent.Height),
	}

	return extent
}

func chooseSurfaceFormat(formats []vk.SurfaceFormat) vk.SurfaceFormat {
	for _, format := range formats {
		if format.Format == vk.FormatB8g8r8a8Srgb && format.ColorSpace == vk.ColorSpaceSrgbNonlinear {
			return format
		}
	}
	return formats[0]
}

func choosePresentMode(presentModes []vk.PresentMode) vk.PresentMode {
	for _, mode := range presentModes {
		if mode == vk.PresentModeMailbox {
			return mode
		}
	}
	return vk.PresentModeFifo
}

func createShaderModule(device vk.Device, code []byte) (vk.ShaderModule, error) {
	var shaderModule vk.ShaderModule
	createInfo := vk.ShaderModuleCreateInfo{
		SType:    vk.StructureTypeShaderModuleCreateInfo,
		CodeSize: uint(len(code)),
		PCode:    repackUint32(code),
	}
	if err := vk.Error(vk.CreateShaderModule(device, &createInfo, nil, &shaderModule)); err != nil {
		return shaderModule, err
	}
	return shaderModule, nil
}

func repackUint32(data []byte) []uint32 {
	packed := make([]uint32, len(data)/4)
	for index := range packed {
		packed[index] = binary.LittleEndian.Uint32(data[index*4 : index*4+4])
	}
	return packed
}

func clampUint32(value, minValue, maxValue uint32) uint32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func isZeroValue[T comparable](value T) bool {
	var zero T
	return value == zero
}
