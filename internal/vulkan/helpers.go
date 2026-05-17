package vulkan

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"

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

func choosePresentMode(presentModes []vk.PresentMode) (vk.PresentMode, error) {
	if requested := strings.TrimSpace(strings.ToLower(os.Getenv("GOGOXEL_PRESENT_MODE"))); requested != "" {
		overrideMode, ok := parsePresentMode(requested)
		if !ok {
			return vk.PresentModeFifo, fmt.Errorf("unknown GOGOXEL_PRESENT_MODE %q", requested)
		}
		for _, mode := range presentModes {
			if mode == overrideMode {
				return mode, nil
			}
		}
		return vk.PresentModeFifo, fmt.Errorf("requested present mode %q is unavailable; available modes: %s", requested, strings.Join(presentModeNames(presentModes), ", "))
	}

	for _, mode := range presentModes {
		if mode == vk.PresentModeMailbox {
			return mode, nil
		}
	}
	return vk.PresentModeFifo, nil
}

func parsePresentMode(name string) (vk.PresentMode, bool) {
	switch name {
	case "immediate":
		return vk.PresentModeImmediate, true
	case "mailbox":
		return vk.PresentModeMailbox, true
	case "fifo":
		return vk.PresentModeFifo, true
	case "fifo_relaxed", "fifo-relaxed":
		return vk.PresentModeFifoRelaxed, true
	default:
		return vk.PresentModeFifo, false
	}
}

func presentModeName(mode vk.PresentMode) string {
	switch mode {
	case vk.PresentModeImmediate:
		return "immediate"
	case vk.PresentModeMailbox:
		return "mailbox"
	case vk.PresentModeFifo:
		return "fifo"
	case vk.PresentModeFifoRelaxed:
		return "fifo_relaxed"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

func presentModeNames(modes []vk.PresentMode) []string {
	names := make([]string, 0, len(modes))
	seen := make(map[vk.PresentMode]struct{}, len(modes))
	for _, mode := range modes {
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		names = append(names, presentModeName(mode))
	}
	return names
}

func physicalDeviceName(device vk.PhysicalDevice) string {
	var properties vk.PhysicalDeviceProperties
	vk.GetPhysicalDeviceProperties(device, &properties)
	properties.Deref()
	return string(bytes.TrimRight(properties.DeviceName[:], "\x00"))
}

func CreateShaderModule(device vk.Device, code []byte) (vk.ShaderModule, error) {
	var shaderModule vk.ShaderModule
	createInfo := vk.ShaderModuleCreateInfo{
		SType:    vk.StructureTypeShaderModuleCreateInfo,
		CodeSize: uint(len(code)),
		PCode:    repackUint32(code),
	}
	if err := withPinnedValue(&shaderModule, func() error {
		return vk.Error(vk.CreateShaderModule(device, &createInfo, nil, &shaderModule))
	}); err != nil {
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

func findMemoryTypeIndex(memoryProperties vk.PhysicalDeviceMemoryProperties, typeBits uint32, required vk.MemoryPropertyFlags) (uint32, error) {
	for index := uint32(0); index < memoryProperties.MemoryTypeCount; index++ {
		memoryProperties.MemoryTypes[index].Deref()
		memoryType := memoryProperties.MemoryTypes[index]
		if typeBits&(1<<index) == 0 {
			continue
		}
		if memoryType.PropertyFlags&required == required {
			return index, nil
		}
	}

	return 0, fmt.Errorf("no compatible Vulkan memory type for flags %#x", required)
}

func isZeroValue[T comparable](value T) bool {
	var zero T
	return value == zero
}
