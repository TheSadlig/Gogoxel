package vulkan

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan/vkbridge"

	vk "github.com/vulkan-go/vulkan"
)

const (
	DefaultWindowWidth  = 1920
	DefaultWindowHeight = 1080
	maxFramesInFlight   = 2
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
	transferQueue      vk.Queue
	graphicsQueueIndex uint32
	presentQueueIndex  uint32
	transferQueueIndex uint32
	// hasDedicatedTransferQueue is true when the device exposes a transfer-only
	// queue family separate from the graphics family. In that case bulk setup
	// uploads run on a parallel queue and chunk-resource buffers/images that
	// participate in transfer + graphics use SHARING_MODE_CONCURRENT to
	// sidestep cross-queue ownership-transfer barriers.
	hasDedicatedTransferQueue bool

	swapchain             vk.Swapchain
	swapchainImages       []vk.Image
	swapchainImageViews   []vk.ImageView
	swapchainFramebuffers []vk.Framebuffer
	swapchainFormat       vk.Format
	swapchainExtent       vk.Extent2D

	renderPass vk.RenderPass

	commandPool            vk.CommandPool
	transferCommandPool    vk.CommandPool
	commandBuffers         []vk.CommandBuffer
	transferCommandBuffers []vk.CommandBuffer

	imageAvailableSemaphores   []vk.Semaphore
	renderFinishedSemaphores   []vk.Semaphore
	transferFinishedSemaphores []vk.Semaphore
	graphicsFinishedSemaphores []vk.Semaphore
	inFlightFences             []vk.Fence
	imagesInFlight             []vk.Fence
	currentFrame               int
	frameReleases              [][]func()
	graphicsSubmitCount        uint64

	// memoryProperties is cached once after physical-device selection so
	// hot paths (staging allocation) don't re-query Vulkan every call.
	memoryProperties             vk.PhysicalDeviceMemoryProperties
	storageBufferOffsetAlignment vk.DeviceSize

	// stagingRings provides one persistent host-coherent staging buffer per
	// in-flight frame slot. Sub-allocations are bump-pointer; the offset is
	// reset whenever the slot's fence has been waited on.
	stagingRings    [maxFramesInFlight]*stagingRing
	sharedAirPool   *brickPool
	chunkGigabuffer chunkGigabuffer

	lastTransferUploadCount  int
	transferQueueUploadCount uint64

	// gpuTimestamps measures wall-clock GPU work per frame slot via Vulkan
	// timestamp queries. nil when the device has no usable timestamp period.
	gpuTimestamps *gpuTimestamps

	instanceExtensions []string
	instanceLayers     []string
	validationEnabled  bool
	physicalDeviceName string
	timestampPeriodNs  float32
	presentMode        vk.PresentMode
	captureSupported   bool
}

// ValidationConfig is the cross-package signal for enabling Vulkan
// validation layers. It is intentionally a process-wide value because the
// renderer is created without an Options argument today.
var (
	validationOnce    sync.Once
	validationDesired bool
)

// EnableValidationLayers requests VK_LAYER_KHRONOS_validation +
// VK_EXT_debug_utils on subsequent renderer instances. Must be called
// before vulkan.New().
func EnableValidationLayers() {
	validationOnce.Do(func() {})
	validationDesired = true
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
	if validationDesired {
		renderer.validationEnabled = true
		renderer.instanceLayers = append(renderer.instanceLayers, "VK_LAYER_KHRONOS_validation")
		// VK_EXT_debug_utils is the modern unified messenger extension.
		// The loader silently no-ops it if the layer isn't installed,
		// which keeps --validate a soft request.
		renderer.instanceExtensions = append(renderer.instanceExtensions, "VK_EXT_debug_utils")
		validationLogger().LogAttrs(nil, slog.LevelInfo, "vulkan validation layers requested",
			slog.String("layer", "VK_LAYER_KHRONOS_validation"),
			slog.String("extension", "VK_EXT_debug_utils"),
		)
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
	if err := r.createChunkGigabuffer(); err != nil {
		return err
	}
	if err := r.initStagingRings(); err != nil {
		return err
	}
	timestamps, err := newGPUTimestamps(r.device, r.timestampPeriodNs, maxFramesInFlight)
	if err != nil {
		return err
	}
	r.gpuTimestamps = timestamps

	return nil
}

func (r *Renderer) createInstance() error {
	instance, err := vkbridge.CreateInstance("Gogoxel", "Gogoxel", r.instanceExtensions, r.instanceLayers)
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
		r.physicalDeviceName = physicalDeviceName(device)
		var props vk.PhysicalDeviceProperties
		vk.GetPhysicalDeviceProperties(device, &props)
		props.Deref()
		props.Limits.Deref()
		// TimestampPeriod is 0 → device cannot report GPU timestamps.
		if props.Limits.TimestampComputeAndGraphics != vk.False {
			r.timestampPeriodNs = props.Limits.TimestampPeriod
		}
		r.storageBufferOffsetAlignment = vk.DeviceSize(props.Limits.MinStorageBufferOffsetAlignment)
		vk.GetPhysicalDeviceMemoryProperties(device, &r.memoryProperties)
		r.memoryProperties.Deref()
		fmt.Printf("[vulkan] physical device: %s (timestamp period: %.2f ns)\n", r.physicalDeviceName, r.timestampPeriodNs)
		return nil
	}

	return errors.New("no physical device supports graphics, presentation, and swapchains")
}

// validationLogger returns a stderr slog logger tagged for vulkan
// validation messages. It is intentionally self-contained so this branch
// doesn't depend on internal/log landing first.
func validationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).With(slog.String("component", "vulkan"))
}

func (r *Renderer) createLogicalDevice() error {
	indices, ok := r.findQueueFamilies(r.physicalDevice)
	if !ok {
		return errors.New("selected physical device lost required queue families")
	}

	uniqueFamilies := []uint32{indices.graphics}
	addUnique := func(family uint32) {
		for _, existing := range uniqueFamilies {
			if existing == family {
				return
			}
		}
		uniqueFamilies = append(uniqueFamilies, family)
	}
	if indices.graphics != indices.present {
		addUnique(indices.present)
	}
	// Only request a separate queue if the transfer family is actually
	// distinct from the graphics family — otherwise the dedicated path
	// degenerates to graphics anyway.
	if indices.hasTransfer && indices.transfer != indices.graphics {
		addUnique(indices.transfer)
	}

	device, err := vkbridge.CreateDevice(r.physicalDevice, uniqueFamilies, []string{"VK_KHR_swapchain"})
	if err != nil {
		return err
	}
	r.device = device

	r.graphicsQueue = vkbridge.GetDeviceQueue(r.device, indices.graphics, 0)
	r.presentQueue = vkbridge.GetDeviceQueue(r.device, indices.present, 0)
	r.graphicsQueueIndex = indices.graphics
	r.presentQueueIndex = indices.present
	if indices.hasTransfer && indices.transfer != indices.graphics {
		r.transferQueue = vkbridge.GetDeviceQueue(r.device, indices.transfer, 0)
		r.transferQueueIndex = indices.transfer
		r.hasDedicatedTransferQueue = true
	} else {
		r.transferQueue = r.graphicsQueue
		r.transferQueueIndex = indices.graphics
		r.hasDedicatedTransferQueue = false
	}
	return nil
}

func (r *Renderer) createSwapchain() error {
	support, err := r.querySwapchainSupport(r.physicalDevice)
	if err != nil {
		return err
	}

	surfaceFormat := chooseSurfaceFormat(support.formats)
	presentMode, err := choosePresentMode(support.presentModes)
	if err != nil {
		return err
	}
	extent := r.chooseExtent(support.capabilities)
	fmt.Printf("[vulkan] present mode: %s (available: %s)\n", presentModeName(presentMode), strings.Join(presentModeNames(support.presentModes), ", "))

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
	if support.capabilities.SupportedUsageFlags&vk.ImageUsageFlags(vk.ImageUsageTransferSrcBit) != 0 {
		createInfo.ImageUsage |= vk.ImageUsageFlags(vk.ImageUsageTransferSrcBit)
		r.captureSupported = true
	} else {
		r.captureSupported = false
	}

	swapchain, err := vkbridge.CreateSwapchain(r.device, &createInfo)
	if err != nil {
		return err
	}
	r.swapchain = swapchain
	r.presentMode = presentMode

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
	CameraPos   [4]float32
	Forward     [4]float32
	Right       [4]float32
	Up          [4]float32
	OccupiedMin [4]float32
	OccupiedMax [4]float32
	Aspect      float32
	FovScale    float32
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
	if r.hasDedicatedTransferQueue {
		transferPool, err := vkbridge.CreateCommandPool(
			r.device,
			r.transferQueueIndex,
			vk.CommandPoolCreateFlags(vk.CommandPoolCreateResetCommandBufferBit),
		)
		if err != nil {
			return fmt.Errorf("creating transfer command pool: %w", err)
		}
		r.transferCommandPool = transferPool
	}
	return nil
}

func (r *Renderer) cleanupVulkan() {
	if !isZeroValue(r.device) {
		_ = vk.Error(vk.DeviceWaitIdle(r.device))
	}
	for frameSlot := range r.frameReleases {
		r.runDeferredReleases(frameSlot)
	}
	r.destroyStagingRings()
	if r.gpuTimestamps != nil {
		r.gpuTimestamps.destroy()
		r.gpuTimestamps = nil
	}
	if r.sharedAirPool != nil {
		r.sharedAirPool.Close(r.device)
		r.sharedAirPool = nil
	}
	r.chunkGigabuffer.Close()

	for _, semaphore := range r.imageAvailableSemaphores {
		if !isZeroValue(semaphore) {
			vk.DestroySemaphore(r.device, semaphore, nil)
		}
	}
	for _, semaphore := range r.renderFinishedSemaphores {
		if !isZeroValue(semaphore) {
			vk.DestroySemaphore(r.device, semaphore, nil)
		}
	}
	for _, semaphore := range r.transferFinishedSemaphores {
		if !isZeroValue(semaphore) {
			vk.DestroySemaphore(r.device, semaphore, nil)
		}
	}
	for _, semaphore := range r.graphicsFinishedSemaphores {
		if !isZeroValue(semaphore) {
			vk.DestroySemaphore(r.device, semaphore, nil)
		}
	}
	for _, fence := range r.inFlightFences {
		if !isZeroValue(fence) {
			vk.DestroyFence(r.device, fence, nil)
		}
	}
	if !isZeroValue(r.commandPool) {
		vk.DestroyCommandPool(r.device, r.commandPool, nil)
	}
	if !isZeroValue(r.transferCommandPool) {
		vk.DestroyCommandPool(r.device, r.transferCommandPool, nil)
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

func (r *Renderer) deferFrameRelease(frameSlot int, release func()) {
	if r == nil || release == nil || frameSlot < 0 || frameSlot >= len(r.frameReleases) {
		return
	}
	r.frameReleases[frameSlot] = append(r.frameReleases[frameSlot], release)
}

func (r *Renderer) runDeferredReleases(frameSlot int) {
	if r == nil || frameSlot < 0 || frameSlot >= len(r.frameReleases) {
		return
	}
	for _, release := range r.frameReleases[frameSlot] {
		release()
	}
	r.frameReleases[frameSlot] = r.frameReleases[frameSlot][:0]
}

func (r *Renderer) TransferQueueUploadsLastFrame() int {
	if r == nil {
		return 0
	}
	return r.lastTransferUploadCount
}

func (r *Renderer) TransferQueueUploadsWindowTotal() uint64 {
	if r == nil {
		return 0
	}
	return r.transferQueueUploadCount
}

func (r *Renderer) TransferUploadPath() string {
	if r == nil {
		return ""
	}
	if r.hasDedicatedTransferQueue {
		return "dedicated-transfer-queue"
	}
	return "graphics-fallback"
}

func (r *Renderer) ResetTransferQueueUploadCounter() {
	if r == nil {
		return
	}
	r.transferQueueUploadCount = 0
}

func (r *Renderer) ChunkStorageStrategy() string {
	if r == nil || isZeroValue(r.chunkGigabuffer.buffer) {
		return ""
	}
	return "gigabuffer"
}

func (r *Renderer) TraversalAlgorithm() string {
	if r == nil {
		return ""
	}
	return "explicit-stack"
}

func (r *Renderer) DeviceName() string {
	if r == nil {
		return ""
	}
	return r.physicalDeviceName
}

func (r *Renderer) PresentModeName() string {
	if r == nil {
		return ""
	}
	return presentModeName(r.presentMode)
}

// LastGPUFrameMs returns the GPU wall-clock time for the most recently
// completed frame in milliseconds. Returns 0 when the device does not
// expose timestamp queries or no frame has been measured yet.
func (r *Renderer) LastGPUFrameMs() float64 {
	if r == nil || r.gpuTimestamps == nil {
		return 0
	}
	return r.gpuTimestamps.lastFrameMs()
}
