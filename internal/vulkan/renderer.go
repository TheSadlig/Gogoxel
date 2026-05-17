package vulkan

import (
	"errors"
	"fmt"
	"runtime"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan/vkbridge"

	vk "github.com/vulkan-go/vulkan"
)

const (
	DefaultWindowWidth  = 1920
	DefaultWindowHeight = 1080
)

func init() {
	runtime.LockOSThread()
}

type Renderer struct {
	window *platform.Window

	instance       vk.Instance
	surface        vk.Surface
	physicalDevice vk.PhysicalDevice
	device         vk.Device

	graphicsQueue      vk.Queue
	presentQueue       vk.Queue
	graphicsQueueIndex uint32
	presentQueueIndex  uint32

	swapchain             vk.Swapchain
	swapchainImages       []vk.Image
	swapchainImageViews   []vk.ImageView
	swapchainFramebuffers []vk.Framebuffer
	swapchainFormat       vk.Format
	swapchainExtent       vk.Extent2D

	renderPass vk.RenderPass

	commandPool    vk.CommandPool
	commandBuffers []vk.CommandBuffer

	imageAvailableSemaphore vk.Semaphore
	renderFinishedSemaphore vk.Semaphore
	inFlightFence           vk.Fence

	instanceExtensions []string
}

func New(window *platform.Window) (*Renderer, error) {
	if window == nil {
		return nil, errors.New("renderer requires a window")
	}

	renderer := &Renderer{window: window}
	renderer.instanceExtensions = window.RequiredInstanceExtensions()
	if len(renderer.instanceExtensions) == 0 {
		return nil, errors.New("GLFW did not provide required Vulkan instance extensions")
	}

	if err := renderer.initVulkan(); err != nil {
		renderer.cleanupVulkan()
		return nil, err
	}

	return renderer, nil
}

func (r *Renderer) Close() {
	r.cleanupVulkan()
}

func (r *Renderer) WaitIdle() error {
	if isZeroValue(r.device) {
		return nil
	}
	if err := vk.Error(vk.DeviceWaitIdle(r.device)); err != nil {
		return fmt.Errorf("waiting for device idle: %w", err)
	}
	return nil
}

func (r *Renderer) initVulkan() error {
	if err := r.createInstance(); err != nil {
		return err
	}
	if err := r.createSurface(); err != nil {
		return err
	}
	if err := r.pickPhysicalDevice(); err != nil {
		return err
	}
	if err := r.createLogicalDevice(); err != nil {
		return err
	}
	if err := r.createSwapchain(); err != nil {
		return err
	}
	if err := r.createImageViews(); err != nil {
		return err
	}
	if err := r.createRenderPass(); err != nil {
		return err
	}
	if err := r.createFramebuffers(); err != nil {
		return err
	}
	if err := r.createCommandPool(); err != nil {
		return err
	}
	if err := r.allocateCommandBuffers(); err != nil {
		return err
	}
	if err := r.createSyncObjects(); err != nil {
		return err
	}

	return nil
}

func (r *Renderer) createInstance() error {
	instance, err := vkbridge.CreateInstance("Gogoxel", "Gogoxel", r.instanceExtensions)
	if err != nil {
		return err
	}
	r.instance = instance
	vk.InitInstance(r.instance)
	return nil
}

func (r *Renderer) createSurface() error {
	surface, err := r.window.CreateSurface(r.instance)
	if err != nil {
		return err
	}
	r.surface = surface
	return nil
}

func (r *Renderer) pickPhysicalDevice() error {
	var deviceCount uint32
	if err := vk.Error(vk.EnumeratePhysicalDevices(r.instance, &deviceCount, nil)); err != nil {
		return fmt.Errorf("enumerating physical devices: %w", err)
	}
	if deviceCount == 0 {
		return errors.New("no Vulkan-capable physical devices found")
	}

	devices := make([]vk.PhysicalDevice, deviceCount)
	if err := withPinnedSlice(devices, func() error {
		return vk.Error(vk.EnumeratePhysicalDevices(r.instance, &deviceCount, devices))
	}); err != nil {
		return fmt.Errorf("loading physical devices: %w", err)
	}

	for _, device := range devices {
		indices, ok := r.findQueueFamilies(device)
		if !ok {
			continue
		}

		support, err := r.querySwapchainSupport(device)
		if err != nil {
			continue
		}
		if len(support.formats) == 0 || len(support.presentModes) == 0 {
			continue
		}

		r.physicalDevice = device
		r.graphicsQueueIndex = indices.graphics
		r.presentQueueIndex = indices.present
		return nil
	}

	return errors.New("no physical device supports graphics, presentation, and swapchains")
}

func (r *Renderer) createLogicalDevice() error {
	indices, ok := r.findQueueFamilies(r.physicalDevice)
	if !ok {
		return errors.New("selected physical device lost required queue families")
	}

	uniqueFamilies := []uint32{indices.graphics}
	if indices.graphics != indices.present {
		uniqueFamilies = append(uniqueFamilies, indices.present)
	}

	device, err := vkbridge.CreateDevice(r.physicalDevice, uniqueFamilies, []string{"VK_KHR_swapchain"})
	if err != nil {
		return err
	}
	r.device = device

	r.graphicsQueue = vkbridge.GetDeviceQueue(r.device, indices.graphics, 0)
	r.presentQueue = vkbridge.GetDeviceQueue(r.device, indices.present, 0)
	return nil
}

func (r *Renderer) createSwapchain() error {
	support, err := r.querySwapchainSupport(r.physicalDevice)
	if err != nil {
		return err
	}

	surfaceFormat := chooseSurfaceFormat(support.formats)
	presentMode := choosePresentMode(support.presentModes)
	extent := r.chooseExtent(support.capabilities)

	imageCount := support.capabilities.MinImageCount + 1
	if support.capabilities.MaxImageCount > 0 && imageCount > support.capabilities.MaxImageCount {
		imageCount = support.capabilities.MaxImageCount
	}

	queueFamilyIndices := []uint32{r.graphicsQueueIndex, r.presentQueueIndex}
	sharingMode := vk.SharingModeExclusive
	if r.graphicsQueueIndex != r.presentQueueIndex {
		sharingMode = vk.SharingModeConcurrent
	}

	createInfo := vk.SwapchainCreateInfo{
		SType:            vk.StructureTypeSwapchainCreateInfo,
		Surface:          r.surface,
		MinImageCount:    imageCount,
		ImageFormat:      surfaceFormat.Format,
		ImageColorSpace:  surfaceFormat.ColorSpace,
		ImageExtent:      extent,
		ImageArrayLayers: 1,
		ImageUsage:       vk.ImageUsageFlags(vk.ImageUsageColorAttachmentBit),
		ImageSharingMode: sharingMode,
		PreTransform:     support.capabilities.CurrentTransform,
		CompositeAlpha:   vk.CompositeAlphaOpaqueBit,
		PresentMode:      presentMode,
		Clipped:          vk.True,
	}
	if sharingMode == vk.SharingModeConcurrent {
		createInfo.QueueFamilyIndexCount = uint32(len(queueFamilyIndices))
		createInfo.PQueueFamilyIndices = queueFamilyIndices
	}

	swapchain, err := vkbridge.CreateSwapchain(r.device, &createInfo)
	if err != nil {
		return err
	}
	r.swapchain = swapchain

	r.swapchainFormat = surfaceFormat.Format
	r.swapchainExtent = extent

	r.swapchainImages, err = vkbridge.GetSwapchainImages(r.device, r.swapchain)
	if err != nil {
		return err
	}

	return nil
}

func (r *Renderer) createImageViews() error {
	r.swapchainImageViews = make([]vk.ImageView, len(r.swapchainImages))
	for index, image := range r.swapchainImages {
		createInfo := vk.ImageViewCreateInfo{
			SType:    vk.StructureTypeImageViewCreateInfo,
			Image:    image,
			ViewType: vk.ImageViewType2d,
			Format:   r.swapchainFormat,
			Components: vk.ComponentMapping{
				R: vk.ComponentSwizzleIdentity,
				G: vk.ComponentSwizzleIdentity,
				B: vk.ComponentSwizzleIdentity,
				A: vk.ComponentSwizzleIdentity,
			},
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
				BaseMipLevel:   0,
				LevelCount:     1,
				BaseArrayLayer: 0,
				LayerCount:     1,
			},
		}

		if err := withPinnedValue(&r.swapchainImageViews[index], func() error {
			return vk.Error(vk.CreateImageView(r.device, &createInfo, nil, &r.swapchainImageViews[index]))
		}); err != nil {
			return fmt.Errorf("creating image view %d: %w", index, err)
		}
	}
	return nil
}

func (r *Renderer) createRenderPass() error {
	renderPass, err := vkbridge.CreateRenderPass(r.device, r.swapchainFormat)
	if err != nil {
		return err
	}
	r.renderPass = renderPass
	return nil
}

type CameraPushConstant struct {
	CameraPos [4]float32
	Forward   [4]float32
	Right     [4]float32
	Up        [4]float32
	Aspect    float32
	FovScale  float32
}

func (r *Renderer) createFramebuffers() error {
	r.swapchainFramebuffers = make([]vk.Framebuffer, len(r.swapchainImageViews))
	for index, imageView := range r.swapchainImageViews {
		attachments := []vk.ImageView{imageView}
		createInfo := vk.FramebufferCreateInfo{
			SType:           vk.StructureTypeFramebufferCreateInfo,
			RenderPass:      r.renderPass,
			AttachmentCount: 1,
			PAttachments:    attachments,
			Width:           r.swapchainExtent.Width,
			Height:          r.swapchainExtent.Height,
			Layers:          1,
		}
		if err := withPinnedValue(&r.swapchainFramebuffers[index], func() error {
			return vk.Error(vk.CreateFramebuffer(r.device, &createInfo, nil, &r.swapchainFramebuffers[index]))
		}); err != nil {
			return fmt.Errorf("creating framebuffer %d: %w", index, err)
		}
	}
	return nil
}

func (r *Renderer) createCommandPool() error {
	commandPool, err := vkbridge.CreateCommandPool(
		r.device,
		r.graphicsQueueIndex,
		vk.CommandPoolCreateFlags(vk.CommandPoolCreateResetCommandBufferBit),
	)
	if err != nil {
		return err
	}
	r.commandPool = commandPool
	return nil
}

func (r *Renderer) cleanupVulkan() {
	if !isZeroValue(r.device) {
		_ = vk.Error(vk.DeviceWaitIdle(r.device))
	}

	if !isZeroValue(r.imageAvailableSemaphore) {
		vk.DestroySemaphore(r.device, r.imageAvailableSemaphore, nil)
	}
	if !isZeroValue(r.renderFinishedSemaphore) {
		vk.DestroySemaphore(r.device, r.renderFinishedSemaphore, nil)
	}
	if !isZeroValue(r.inFlightFence) {
		vk.DestroyFence(r.device, r.inFlightFence, nil)
	}
	if !isZeroValue(r.commandPool) {
		vk.DestroyCommandPool(r.device, r.commandPool, nil)
	}
	for _, framebuffer := range r.swapchainFramebuffers {
		vk.DestroyFramebuffer(r.device, framebuffer, nil)
	}
	if !isZeroValue(r.renderPass) {
		vk.DestroyRenderPass(r.device, r.renderPass, nil)
	}
	for _, imageView := range r.swapchainImageViews {
		vk.DestroyImageView(r.device, imageView, nil)
	}
	if !isZeroValue(r.swapchain) {
		vk.DestroySwapchain(r.device, r.swapchain, nil)
	}
	if !isZeroValue(r.device) {
		vk.DestroyDevice(r.device, nil)
	}
	if !isZeroValue(r.surface) {
		vk.DestroySurface(r.instance, r.surface, nil)
	}
	if !isZeroValue(r.instance) {
		vk.DestroyInstance(r.instance, nil)
	}
}
