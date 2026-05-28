package vkbridge

/*
#cgo CFLAGS: -I${SRCDIR}/../../../third_party/vulkan-headers
#cgo LDFLAGS: -lvulkan
#include <stdlib.h>
#include "vulkan/vulkan.h"
*/
import "C"

import (
	"fmt"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

func CreateInstance(appName, engineName string, extensions []string, layers []string) (vk.Instance, error) {
	appNamePtr := C.CString(appName)
	defer C.free(unsafe.Pointer(appNamePtr))

	engineNamePtr := C.CString(engineName)
	defer C.free(unsafe.Pointer(engineNamePtr))

	var extensionNames **C.char
	if len(extensions) > 0 {
		arraySize := C.size_t(len(extensions)) * C.size_t(unsafe.Sizeof(uintptr(0)))
		extensionNames = (**C.char)(C.malloc(arraySize))
		defer C.free(unsafe.Pointer(extensionNames))

		extensionSlice := unsafe.Slice(extensionNames, len(extensions))
		for index, extension := range extensions {
			extensionSlice[index] = C.CString(extension)
			defer C.free(unsafe.Pointer(extensionSlice[index]))
		}
	}

	var layerNames **C.char
	if len(layers) > 0 {
		arraySize := C.size_t(len(layers)) * C.size_t(unsafe.Sizeof(uintptr(0)))
		layerNames = (**C.char)(C.malloc(arraySize))
		defer C.free(unsafe.Pointer(layerNames))

		layerSlice := unsafe.Slice(layerNames, len(layers))
		for index, layer := range layers {
			layerSlice[index] = C.CString(layer)
			defer C.free(unsafe.Pointer(layerSlice[index]))
		}
	}

	appInfo := (*C.VkApplicationInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkApplicationInfo{}))))
	defer C.free(unsafe.Pointer(appInfo))
	appInfo.sType = C.VK_STRUCTURE_TYPE_APPLICATION_INFO
	appInfo.pApplicationName = appNamePtr
	appInfo.applicationVersion = C.uint32_t(vk.MakeVersion(1, 0, 0))
	appInfo.pEngineName = engineNamePtr
	appInfo.engineVersion = C.uint32_t(vk.MakeVersion(1, 0, 0))
	appInfo.apiVersion = C.uint32_t(vk.MakeVersion(1, 0, 0))

	createInfo := (*C.VkInstanceCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkInstanceCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO
	createInfo.pApplicationInfo = appInfo
	createInfo.enabledExtensionCount = C.uint32_t(len(extensions))
	createInfo.ppEnabledExtensionNames = extensionNames
	createInfo.enabledLayerCount = C.uint32_t(len(layers))
	createInfo.ppEnabledLayerNames = layerNames

	var instance C.VkInstance
	result := C.vkCreateInstance(createInfo, nil, &instance)
	if result != C.VK_SUCCESS {
		return *(*vk.Instance)(unsafe.Pointer(&instance)), fmt.Errorf("creating Vulkan instance: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.Instance)(unsafe.Pointer(&instance)), nil
}

func CreateDevice(physicalDevice vk.PhysicalDevice, queueFamilyIndices []uint32, extensionNames []string) (vk.Device, error) {
	var queueInfos *C.VkDeviceQueueCreateInfo
	var priorityPtr *C.float
	if len(queueFamilyIndices) > 0 {
		priorityPtr = (*C.float)(C.malloc(C.size_t(unsafe.Sizeof(C.float(0)))))
		defer C.free(unsafe.Pointer(priorityPtr))
		*priorityPtr = C.float(1.0)

		queueInfos = (*C.VkDeviceQueueCreateInfo)(C.calloc(C.size_t(len(queueFamilyIndices)), C.size_t(unsafe.Sizeof(C.VkDeviceQueueCreateInfo{}))))
		defer C.free(unsafe.Pointer(queueInfos))

		queueInfoSlice := unsafe.Slice(queueInfos, len(queueFamilyIndices))
		for index, familyIndex := range queueFamilyIndices {
			queueInfoSlice[index].sType = C.VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO
			queueInfoSlice[index].queueFamilyIndex = C.uint32_t(familyIndex)
			queueInfoSlice[index].queueCount = 1
			queueInfoSlice[index].pQueuePriorities = priorityPtr
		}
	}

	var extensionNamePtrs **C.char
	if len(extensionNames) > 0 {
		arraySize := C.size_t(len(extensionNames)) * C.size_t(unsafe.Sizeof(uintptr(0)))
		extensionNamePtrs = (**C.char)(C.malloc(arraySize))
		defer C.free(unsafe.Pointer(extensionNamePtrs))

		extensionSlice := unsafe.Slice(extensionNamePtrs, len(extensionNames))
		for index, extensionName := range extensionNames {
			extensionSlice[index] = C.CString(extensionName)
			defer C.free(unsafe.Pointer(extensionSlice[index]))
		}
	}

	createInfo := (*C.VkDeviceCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkDeviceCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO
	createInfo.queueCreateInfoCount = C.uint32_t(len(queueFamilyIndices))
	createInfo.pQueueCreateInfos = queueInfos
	createInfo.enabledExtensionCount = C.uint32_t(len(extensionNames))
	createInfo.ppEnabledExtensionNames = extensionNamePtrs

	cPhysicalDevice := *(*C.VkPhysicalDevice)(unsafe.Pointer(&physicalDevice))
	var device C.VkDevice
	result := C.vkCreateDevice(cPhysicalDevice, createInfo, nil, &device)
	if result != C.VK_SUCCESS {
		return *(*vk.Device)(unsafe.Pointer(&device)), fmt.Errorf("creating logical device: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.Device)(unsafe.Pointer(&device)), nil
}

func GetDeviceQueue(device vk.Device, queueFamilyIndex, queueIndex uint32) vk.Queue {
	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var queue C.VkQueue
	C.vkGetDeviceQueue(cDevice, C.uint32_t(queueFamilyIndex), C.uint32_t(queueIndex), &queue)
	return *(*vk.Queue)(unsafe.Pointer(&queue))
}

func CreateSwapchain(device vk.Device, createInfo *vk.SwapchainCreateInfo) (vk.Swapchain, error) {
	if createInfo == nil {
		var zero vk.Swapchain
		return zero, fmt.Errorf("creating swapchain: nil create info")
	}

	cCreateInfo := (*C.VkSwapchainCreateInfoKHR)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkSwapchainCreateInfoKHR{}))))
	defer C.free(unsafe.Pointer(cCreateInfo))

	cCreateInfo.sType = C.VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR
	cCreateInfo.flags = C.VkSwapchainCreateFlagsKHR(createInfo.Flags)
	cCreateInfo.surface = *(*C.VkSurfaceKHR)(unsafe.Pointer(&createInfo.Surface))
	cCreateInfo.minImageCount = C.uint32_t(createInfo.MinImageCount)
	cCreateInfo.imageFormat = C.VkFormat(createInfo.ImageFormat)
	cCreateInfo.imageColorSpace = C.VkColorSpaceKHR(createInfo.ImageColorSpace)
	cCreateInfo.imageExtent.width = C.uint32_t(createInfo.ImageExtent.Width)
	cCreateInfo.imageExtent.height = C.uint32_t(createInfo.ImageExtent.Height)
	cCreateInfo.imageArrayLayers = C.uint32_t(createInfo.ImageArrayLayers)
	cCreateInfo.imageUsage = C.VkImageUsageFlags(createInfo.ImageUsage)
	cCreateInfo.imageSharingMode = C.VkSharingMode(createInfo.ImageSharingMode)
	cCreateInfo.preTransform = C.VkSurfaceTransformFlagBitsKHR(createInfo.PreTransform)
	cCreateInfo.compositeAlpha = C.VkCompositeAlphaFlagBitsKHR(createInfo.CompositeAlpha)
	cCreateInfo.presentMode = C.VkPresentModeKHR(createInfo.PresentMode)
	cCreateInfo.clipped = C.VkBool32(createInfo.Clipped)
	cCreateInfo.oldSwapchain = *(*C.VkSwapchainKHR)(unsafe.Pointer(&createInfo.OldSwapchain))

	if createInfo.QueueFamilyIndexCount > 0 {
		cIndices := (*C.uint32_t)(C.calloc(C.size_t(createInfo.QueueFamilyIndexCount), C.size_t(unsafe.Sizeof(C.uint32_t(0)))))
		defer C.free(unsafe.Pointer(cIndices))

		indices := unsafe.Slice(cIndices, int(createInfo.QueueFamilyIndexCount))
		for index, familyIndex := range createInfo.PQueueFamilyIndices {
			indices[index] = C.uint32_t(familyIndex)
		}

		cCreateInfo.queueFamilyIndexCount = C.uint32_t(createInfo.QueueFamilyIndexCount)
		cCreateInfo.pQueueFamilyIndices = cIndices
	}

	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var swapchain C.VkSwapchainKHR
	result := C.vkCreateSwapchainKHR(cDevice, cCreateInfo, nil, &swapchain)
	if result != C.VK_SUCCESS {
		var zero vk.Swapchain
		return zero, fmt.Errorf("creating swapchain: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.Swapchain)(unsafe.Pointer(&swapchain)), nil
}

func GetSwapchainImages(device vk.Device, swapchain vk.Swapchain) ([]vk.Image, error) {
	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	cSwapchain := *(*C.VkSwapchainKHR)(unsafe.Pointer(&swapchain))

	var imageCount C.uint32_t
	result := C.vkGetSwapchainImagesKHR(cDevice, cSwapchain, &imageCount, nil)
	if result != C.VK_SUCCESS {
		return nil, fmt.Errorf("counting swapchain images: %w", vk.Error(vk.Result(result)))
	}

	if imageCount == 0 {
		return nil, nil
	}

	cImages := (*C.VkImage)(C.calloc(C.size_t(imageCount), C.size_t(unsafe.Sizeof(C.VkImage(nil)))))
	defer C.free(unsafe.Pointer(cImages))

	result = C.vkGetSwapchainImagesKHR(cDevice, cSwapchain, &imageCount, cImages)
	if result != C.VK_SUCCESS {
		return nil, fmt.Errorf("loading swapchain images: %w", vk.Error(vk.Result(result)))
	}

	images := make([]vk.Image, int(imageCount))
	for index, image := range unsafe.Slice(cImages, int(imageCount)) {
		images[index] = *(*vk.Image)(unsafe.Pointer(&image))
	}

	return images, nil
}

func CreateRenderPass(device vk.Device, format vk.Format) (vk.RenderPass, error) {
	attachment := (*C.VkAttachmentDescription)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkAttachmentDescription{}))))
	defer C.free(unsafe.Pointer(attachment))
	attachment.format = C.VkFormat(format)
	attachment.samples = C.VK_SAMPLE_COUNT_1_BIT
	attachment.loadOp = C.VK_ATTACHMENT_LOAD_OP_CLEAR
	attachment.storeOp = C.VK_ATTACHMENT_STORE_OP_STORE
	attachment.stencilLoadOp = C.VK_ATTACHMENT_LOAD_OP_DONT_CARE
	attachment.stencilStoreOp = C.VK_ATTACHMENT_STORE_OP_DONT_CARE
	attachment.initialLayout = C.VK_IMAGE_LAYOUT_UNDEFINED
	attachment.finalLayout = C.VK_IMAGE_LAYOUT_PRESENT_SRC_KHR

	colorAttachment := (*C.VkAttachmentReference)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkAttachmentReference{}))))
	defer C.free(unsafe.Pointer(colorAttachment))
	colorAttachment.attachment = 0
	colorAttachment.layout = C.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL

	subpass := (*C.VkSubpassDescription)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkSubpassDescription{}))))
	defer C.free(unsafe.Pointer(subpass))
	subpass.pipelineBindPoint = C.VK_PIPELINE_BIND_POINT_GRAPHICS
	subpass.colorAttachmentCount = 1
	subpass.pColorAttachments = colorAttachment

	dependency := (*C.VkSubpassDependency)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkSubpassDependency{}))))
	defer C.free(unsafe.Pointer(dependency))
	dependency.srcSubpass = C.VK_SUBPASS_EXTERNAL
	dependency.dstSubpass = 0
	dependency.srcStageMask = C.VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT
	dependency.dstStageMask = C.VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT
	dependency.dstAccessMask = C.VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT

	createInfo := (*C.VkRenderPassCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkRenderPassCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO
	createInfo.attachmentCount = 1
	createInfo.pAttachments = attachment
	createInfo.subpassCount = 1
	createInfo.pSubpasses = subpass
	createInfo.dependencyCount = 1
	createInfo.pDependencies = dependency

	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var renderPass C.VkRenderPass
	result := C.vkCreateRenderPass(cDevice, createInfo, nil, &renderPass)
	if result != C.VK_SUCCESS {
		var zero vk.RenderPass
		return zero, fmt.Errorf("creating render pass: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.RenderPass)(unsafe.Pointer(&renderPass)), nil
}

func CreateCommandPool(device vk.Device, queueFamilyIndex uint32, flags vk.CommandPoolCreateFlags) (vk.CommandPool, error) {
	createInfo := (*C.VkCommandPoolCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkCommandPoolCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO
	createInfo.flags = C.VkCommandPoolCreateFlags(flags)
	createInfo.queueFamilyIndex = C.uint32_t(queueFamilyIndex)

	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var commandPool C.VkCommandPool
	result := C.vkCreateCommandPool(cDevice, createInfo, nil, &commandPool)
	if result != C.VK_SUCCESS {
		var zero vk.CommandPool
		return zero, fmt.Errorf("creating command pool: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.CommandPool)(unsafe.Pointer(&commandPool)), nil
}

func CreateSemaphore(device vk.Device) (vk.Semaphore, error) {
	createInfo := (*C.VkSemaphoreCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkSemaphoreCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO

	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var semaphore C.VkSemaphore
	result := C.vkCreateSemaphore(cDevice, createInfo, nil, &semaphore)
	if result != C.VK_SUCCESS {
		var zero vk.Semaphore
		return zero, fmt.Errorf("creating semaphore: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.Semaphore)(unsafe.Pointer(&semaphore)), nil
}

func CreateFence(device vk.Device, flags vk.FenceCreateFlags) (vk.Fence, error) {
	createInfo := (*C.VkFenceCreateInfo)(C.calloc(1, C.size_t(unsafe.Sizeof(C.VkFenceCreateInfo{}))))
	defer C.free(unsafe.Pointer(createInfo))
	createInfo.sType = C.VK_STRUCTURE_TYPE_FENCE_CREATE_INFO
	createInfo.flags = C.VkFenceCreateFlags(flags)

	cDevice := *(*C.VkDevice)(unsafe.Pointer(&device))
	var fence C.VkFence
	result := C.vkCreateFence(cDevice, createInfo, nil, &fence)
	if result != C.VK_SUCCESS {
		var zero vk.Fence
		return zero, fmt.Errorf("creating fence: %w", vk.Error(vk.Result(result)))
	}

	return *(*vk.Fence)(unsafe.Pointer(&fence)), nil
}
