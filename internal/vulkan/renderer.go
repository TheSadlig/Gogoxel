package vulkan

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan/shaders"

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

	renderPass     vk.RenderPass
	pipelineLayout vk.PipelineLayout
	pipeline       vk.Pipeline

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
	if err := r.createGraphicsPipeline(); err != nil {
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
	appInfo := vk.ApplicationInfo{
		SType:              vk.StructureTypeApplicationInfo,
		PApplicationName:   "Gogoxel\x00",
		ApplicationVersion: vk.MakeVersion(1, 0, 0),
		PEngineName:        "Gogoxel\x00",
		EngineVersion:      vk.MakeVersion(1, 0, 0),
		ApiVersion:         vk.MakeVersion(1, 0, 0),
	}

	createInfo := vk.InstanceCreateInfo{
		SType:                   vk.StructureTypeInstanceCreateInfo,
		PApplicationInfo:        &appInfo,
		EnabledExtensionCount:   uint32(len(r.instanceExtensions)),
		PpEnabledExtensionNames: r.instanceExtensions,
	}

	if err := vk.Error(vk.CreateInstance(&createInfo, nil, &r.instance)); err != nil {
		return fmt.Errorf("creating Vulkan instance: %w", err)
	}
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
	if err := vk.Error(vk.EnumeratePhysicalDevices(r.instance, &deviceCount, devices)); err != nil {
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

	queuePriority := []float32{1.0}
	queueCreateInfos := make([]vk.DeviceQueueCreateInfo, 0, len(uniqueFamilies))
	for _, familyIndex := range uniqueFamilies {
		queueCreateInfos = append(queueCreateInfos, vk.DeviceQueueCreateInfo{
			SType:            vk.StructureTypeDeviceQueueCreateInfo,
			QueueFamilyIndex: familyIndex,
			QueueCount:       1,
			PQueuePriorities: queuePriority,
		})
	}

	deviceExtensions := []string{"VK_KHR_swapchain\x00"}
	deviceCreateInfo := vk.DeviceCreateInfo{
		SType:                   vk.StructureTypeDeviceCreateInfo,
		QueueCreateInfoCount:    uint32(len(queueCreateInfos)),
		PQueueCreateInfos:       queueCreateInfos,
		EnabledExtensionCount:   uint32(len(deviceExtensions)),
		PpEnabledExtensionNames: deviceExtensions,
	}

	if err := vk.Error(vk.CreateDevice(r.physicalDevice, &deviceCreateInfo, nil, &r.device)); err != nil {
		return fmt.Errorf("creating logical device: %w", err)
	}

	vk.GetDeviceQueue(r.device, indices.graphics, 0, &r.graphicsQueue)
	vk.GetDeviceQueue(r.device, indices.present, 0, &r.presentQueue)
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

	if err := vk.Error(vk.CreateSwapchain(r.device, &createInfo, nil, &r.swapchain)); err != nil {
		return fmt.Errorf("creating swapchain: %w", err)
	}

	r.swapchainFormat = surfaceFormat.Format
	r.swapchainExtent = extent

	var swapchainImageCount uint32
	if err := vk.Error(vk.GetSwapchainImages(r.device, r.swapchain, &swapchainImageCount, nil)); err != nil {
		return fmt.Errorf("counting swapchain images: %w", err)
	}
	r.swapchainImages = make([]vk.Image, swapchainImageCount)
	if err := vk.Error(vk.GetSwapchainImages(r.device, r.swapchain, &swapchainImageCount, r.swapchainImages)); err != nil {
		return fmt.Errorf("loading swapchain images: %w", err)
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

		if err := vk.Error(vk.CreateImageView(r.device, &createInfo, nil, &r.swapchainImageViews[index])); err != nil {
			return fmt.Errorf("creating image view %d: %w", index, err)
		}
	}
	return nil
}

func (r *Renderer) createRenderPass() error {
	attachments := []vk.AttachmentDescription{{
		Format:         r.swapchainFormat,
		Samples:        vk.SampleCount1Bit,
		LoadOp:         vk.AttachmentLoadOpDontCare,
		StoreOp:        vk.AttachmentStoreOpStore,
		StencilLoadOp:  vk.AttachmentLoadOpDontCare,
		StencilStoreOp: vk.AttachmentStoreOpDontCare,
		InitialLayout:  vk.ImageLayoutUndefined,
		FinalLayout:    vk.ImageLayoutPresentSrc,
	}}
	colorAttachments := []vk.AttachmentReference{{
		Attachment: 0,
		Layout:     vk.ImageLayoutColorAttachmentOptimal,
	}}
	subpasses := []vk.SubpassDescription{{
		PipelineBindPoint:    vk.PipelineBindPointGraphics,
		ColorAttachmentCount: 1,
		PColorAttachments:    colorAttachments,
	}}
	dependencies := []vk.SubpassDependency{{
		SrcSubpass:    vk.SubpassExternal,
		DstSubpass:    0,
		SrcStageMask:  vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit),
		DstStageMask:  vk.PipelineStageFlags(vk.PipelineStageColorAttachmentOutputBit),
		DstAccessMask: vk.AccessFlags(vk.AccessColorAttachmentWriteBit),
	}}

	createInfo := vk.RenderPassCreateInfo{
		SType:           vk.StructureTypeRenderPassCreateInfo,
		AttachmentCount: uint32(len(attachments)),
		PAttachments:    attachments,
		SubpassCount:    uint32(len(subpasses)),
		PSubpasses:      subpasses,
		DependencyCount: uint32(len(dependencies)),
		PDependencies:   dependencies,
	}

	if err := vk.Error(vk.CreateRenderPass(r.device, &createInfo, nil, &r.renderPass)); err != nil {
		return fmt.Errorf("creating render pass: %w", err)
	}
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

func (r *Renderer) createGraphicsPipeline() error {
	// Define the range of the push constant block
	pushConstantRange := vk.PushConstantRange{
		StageFlags: vk.ShaderStageFlags(vk.ShaderStageVertexBit | vk.ShaderStageFragmentBit), // Vertex + Fragment
		Offset:     0,
		Size:       uint32(unsafe.Sizeof(CameraPushConstant{})),
	}

	if err := vk.Error(vk.CreatePipelineLayout(r.device, &vk.PipelineLayoutCreateInfo{
		SType:                  vk.StructureTypePipelineLayoutCreateInfo,
		PushConstantRangeCount: 1,
		PPushConstantRanges:    []vk.PushConstantRange{pushConstantRange},
		SetLayoutCount:         0, // Assuming no other descriptor sets for now
	}, nil, &r.pipelineLayout)); err != nil {
		return fmt.Errorf("creating pipeline layout: %w", err)
	}

	vertexShader, err := createShaderModule(r.device, shaders.RaytracerVertexSPV)
	if err != nil {
		return fmt.Errorf("creating vertex shader module: %w", err)
	}
	defer vk.DestroyShaderModule(r.device, vertexShader, nil)

	fragmentShader, err := createShaderModule(r.device, shaders.RaytracerSPV)
	if err != nil {
		return fmt.Errorf("creating fragment shader module: %w", err)
	}
	defer vk.DestroyShaderModule(r.device, fragmentShader, nil)

	shaderStages := []vk.PipelineShaderStageCreateInfo{
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageVertexBit,
			Module: vertexShader,
			PName:  "main\x00",
		},
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageFragmentBit,
			Module: fragmentShader,
			PName:  "main\x00",
		},
	}

	attributes := []vk.VertexInputAttributeDescription{{
		Location: 0,
		Binding:  0,
		Format:   vk.FormatR32g32Sfloat, // vec2
		Offset:   0,
	}}

	// 3. Setup binding description
	bindingDescriptions := []vk.VertexInputBindingDescription{
		{
			Binding:   0,
			Stride:    12, // Total size of Vertex struct in bytes (3 * 4 bytes)
			InputRate: vk.VertexInputRateVertex,
		},
	}

	viewport := []vk.Viewport{{
		X:        0,
		Y:        0,
		Width:    float32(r.swapchainExtent.Width),
		Height:   float32(r.swapchainExtent.Height),
		MinDepth: 0,
		MaxDepth: 1,
	}}
	scissor := []vk.Rect2D{{
		Offset: vk.Offset2D{X: 0, Y: 0},
		Extent: r.swapchainExtent,
	}}

	vertexInputState := vk.PipelineVertexInputStateCreateInfo{
		SType:                           vk.StructureTypePipelineVertexInputStateCreateInfo,
		VertexBindingDescriptionCount:   uint32(len(bindingDescriptions)),
		PVertexBindingDescriptions:      bindingDescriptions,
		VertexAttributeDescriptionCount: uint32(len(attributes)),
		PVertexAttributeDescriptions:    attributes,
	}
	inputAssemblyState := vk.PipelineInputAssemblyStateCreateInfo{
		SType:                  vk.StructureTypePipelineInputAssemblyStateCreateInfo,
		Topology:               vk.PrimitiveTopologyTriangleList,
		PrimitiveRestartEnable: vk.False,
	}
	viewportState := vk.PipelineViewportStateCreateInfo{
		SType:         vk.StructureTypePipelineViewportStateCreateInfo,
		ViewportCount: 1,
		PViewports:    viewport,
		ScissorCount:  1,
		PScissors:     scissor,
	}
	rasterizationState := vk.PipelineRasterizationStateCreateInfo{
		SType:                   vk.StructureTypePipelineRasterizationStateCreateInfo,
		DepthClampEnable:        vk.False,
		RasterizerDiscardEnable: vk.False,
		PolygonMode:             vk.PolygonModeFill,
		CullMode:                vk.CullModeFlags(vk.CullModeNone),
		FrontFace:               vk.FrontFaceClockwise,
		DepthBiasEnable:         vk.False,
		LineWidth:               1,
	}
	multisampleState := vk.PipelineMultisampleStateCreateInfo{
		SType:                vk.StructureTypePipelineMultisampleStateCreateInfo,
		RasterizationSamples: vk.SampleCount1Bit,
		SampleShadingEnable:  vk.False,
	}
	colorBlendAttachments := []vk.PipelineColorBlendAttachmentState{{
		ColorWriteMask: vk.ColorComponentFlags(
			vk.ColorComponentRBit |
				vk.ColorComponentGBit |
				vk.ColorComponentBBit |
				vk.ColorComponentABit,
		),
		BlendEnable: vk.False,
	}}
	colorBlendState := vk.PipelineColorBlendStateCreateInfo{
		SType:           vk.StructureTypePipelineColorBlendStateCreateInfo,
		LogicOpEnable:   vk.False,
		AttachmentCount: 1,
		PAttachments:    colorBlendAttachments,
	}

	pipelineCreateInfos := []vk.GraphicsPipelineCreateInfo{{
		SType:               vk.StructureTypeGraphicsPipelineCreateInfo,
		StageCount:          uint32(len(shaderStages)),
		PStages:             shaderStages,
		PVertexInputState:   &vertexInputState,
		PInputAssemblyState: &inputAssemblyState,
		PViewportState:      &viewportState,
		PRasterizationState: &rasterizationState,
		PMultisampleState:   &multisampleState,
		PColorBlendState:    &colorBlendState,
		Layout:              r.pipelineLayout,
		RenderPass:          r.renderPass,
		Subpass:             0,
	}}

	pipelines := make([]vk.Pipeline, 1)
	var pipelineCache vk.PipelineCache
	if err := vk.Error(vk.CreateGraphicsPipelines(r.device, pipelineCache, 1, pipelineCreateInfos, nil, pipelines)); err != nil {
		return fmt.Errorf("creating graphics pipeline: %w", err)
	}
	r.pipeline = pipelines[0]
	return nil
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
		if err := vk.Error(vk.CreateFramebuffer(r.device, &createInfo, nil, &r.swapchainFramebuffers[index])); err != nil {
			return fmt.Errorf("creating framebuffer %d: %w", index, err)
		}
	}
	return nil
}

func (r *Renderer) createCommandPool() error {
	createInfo := vk.CommandPoolCreateInfo{
		SType:            vk.StructureTypeCommandPoolCreateInfo,
		Flags:            vk.CommandPoolCreateFlags(vk.CommandPoolCreateResetCommandBufferBit),
		QueueFamilyIndex: r.graphicsQueueIndex,
	}
	if err := vk.Error(vk.CreateCommandPool(r.device, &createInfo, nil, &r.commandPool)); err != nil {
		return fmt.Errorf("creating command pool: %w", err)
	}
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
	if !isZeroValue(r.pipeline) {
		vk.DestroyPipeline(r.device, r.pipeline, nil)
	}
	if !isZeroValue(r.pipelineLayout) {
		vk.DestroyPipelineLayout(r.device, r.pipelineLayout, nil)
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
