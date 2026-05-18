package vulkan

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan/shaders"

	vk "github.com/vulkan-go/vulkan"
)

type RaytracerPipeline struct {
	device   vk.Device
	layout   vk.PipelineLayout
	pipeline vk.Pipeline
}

func (r *Renderer) NewRaytracerPipeline(chunkBindings *ChunkBindings) (*RaytracerPipeline, error) {
	if chunkBindings == nil || isZeroValue(chunkBindings.Layout()) {
		return nil, errors.New("chunk bindings are required for the raytracer pipeline")
	}

	pipeline := &RaytracerPipeline{device: r.device}
	if err := pipeline.init(r.renderPass, r.swapchainExtent, chunkBindings.Layout()); err != nil {
		return nil, err
	}

	return pipeline, nil
}

func (p *RaytracerPipeline) init(renderPass vk.RenderPass, swapchainExtent vk.Extent2D, chunkDescriptorLayout vk.DescriptorSetLayout) error {
	device := p.device
	strings := &cStringArena{}
	defer strings.Free()

	pushConstantRange := vk.PushConstantRange{
		StageFlags: vk.ShaderStageFlags(vk.ShaderStageVertexBit | vk.ShaderStageFragmentBit),
		Offset:     0,
		Size:       uint32(unsafe.Sizeof(CameraPushConstant{})),
	}

	if err := withPinnedValue(&p.layout, func() error {
		return vk.Error(vk.CreatePipelineLayout(device, &vk.PipelineLayoutCreateInfo{
			SType:                  vk.StructureTypePipelineLayoutCreateInfo,
			PushConstantRangeCount: 1,
			PPushConstantRanges:    []vk.PushConstantRange{pushConstantRange},
			SetLayoutCount:         1,
			PSetLayouts:            []vk.DescriptorSetLayout{chunkDescriptorLayout},
		}, nil, &p.layout))
	}); err != nil {
		return fmt.Errorf("creating graphics pipeline layout: %w", err)
	}

	vertexShader, err := CreateShaderModule(device, shaders.RaytracerVertexSPV)
	if err != nil {
		p.Close()
		return fmt.Errorf("creating vertex shader module: %w", err)
	}
	defer vk.DestroyShaderModule(device, vertexShader, nil)

	fragmentShader, err := CreateShaderModule(device, shaders.RaytracerSPV)
	if err != nil {
		p.Close()
		return fmt.Errorf("creating fragment shader module: %w", err)
	}
	defer vk.DestroyShaderModule(device, fragmentShader, nil)

	shaderStages := []vk.PipelineShaderStageCreateInfo{
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageVertexBit,
			Module: vertexShader,
			PName:  strings.String("main"),
		},
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageFragmentBit,
			Module: fragmentShader,
			PName:  strings.String("main"),
		},
	}

	attributes := []vk.VertexInputAttributeDescription{{
		Location: 0,
		Binding:  0,
		Format:   vk.FormatR32g32Sfloat,
		Offset:   0,
	}}
	bindingDescriptions := []vk.VertexInputBindingDescription{{
		Binding:   0,
		Stride:    12,
		InputRate: vk.VertexInputRateVertex,
	}}
	viewport := []vk.Viewport{{
		X:        0,
		Y:        0,
		Width:    float32(swapchainExtent.Width),
		Height:   float32(swapchainExtent.Height),
		MinDepth: 0,
		MaxDepth: 1,
	}}
	scissor := []vk.Rect2D{{
		Offset: vk.Offset2D{X: 0, Y: 0},
		Extent: swapchainExtent,
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
		Layout:              p.layout,
		RenderPass:          renderPass,
		Subpass:             0,
	}}

	pipelines := make([]vk.Pipeline, 1)
	var pipelineCache vk.PipelineCache
	if err := withPinnedSlice(pipelines, func() error {
		return vk.Error(vk.CreateGraphicsPipelines(device, pipelineCache, 1, pipelineCreateInfos, nil, pipelines))
	}); err != nil {
		p.Close()
		return fmt.Errorf("creating graphics pipeline: %w", err)
	}

	p.pipeline = pipelines[0]
	return nil
}

func (p *RaytracerPipeline) Bind(frame *Frame, camera platform.Camera, descriptorSet vk.DescriptorSet) error {
	if p == nil || isZeroValue(p.pipeline) || isZeroValue(p.layout) {
		return errors.New("raytracer pipeline is not initialized")
	}
	if frame == nil {
		return errors.New("frame is required")
	}
	if isZeroValue(descriptorSet) {
		return errors.New("descriptor set is required")
	}

	forward := camera.Forward()
	right := camera.Right()
	up := camera.Up()
	aspect := float32(frame.Extent.Width) / float32(frame.Extent.Height)
	pushConstants := CameraPushConstant{
		CameraPos: [4]float32{camera.Position[0], camera.Position[1], camera.Position[2], 0},
		Forward:   [4]float32{forward[0], forward[1], forward[2], 0},
		Right:     [4]float32{right[0], right[1], right[2], 0},
		Up:        [4]float32{up[0], up[1], up[2], 0},
		Aspect:    aspect,
		FovScale:  float32(math.Tan(float64(camera.FovDeg) * 0.5 * math.Pi / 180.0)),
	}

	frame.BeginRenderPass()
	vk.CmdBindPipeline(frame.CommandBuffer, vk.PipelineBindPointGraphics, p.pipeline)
	descriptorSets := []vk.DescriptorSet{descriptorSet}
	vk.CmdBindDescriptorSets(
		frame.CommandBuffer,
		vk.PipelineBindPointGraphics,
		p.layout,
		0,
		uint32(len(descriptorSets)),
		descriptorSets,
		0,
		nil,
	)
	vk.CmdPushConstants(
		frame.CommandBuffer,
		p.layout,
		vk.ShaderStageFlags(vk.ShaderStageVertexBit|vk.ShaderStageFragmentBit),
		0,
		uint32(unsafe.Sizeof(pushConstants)),
		unsafe.Pointer(&pushConstants),
	)

	return nil
}

func (p *RaytracerPipeline) DrawFullscreen(frame *Frame) error {
	if frame == nil {
		return errors.New("frame is required")
	}

	frame.Draw(3, 1, 0, 0)
	return nil
}

func (p *RaytracerPipeline) Record(frame *Frame, camera platform.Camera, descriptorSet vk.DescriptorSet) error {
	if err := p.Bind(frame, camera, descriptorSet); err != nil {
		return err
	}

	return p.DrawFullscreen(frame)
}

func (p *RaytracerPipeline) Close() {
	if p == nil || isZeroValue(p.device) {
		return
	}

	if !isZeroValue(p.pipeline) {
		vk.DestroyPipeline(p.device, p.pipeline, nil)
	}
	if !isZeroValue(p.layout) {
		vk.DestroyPipelineLayout(p.device, p.layout, nil)
	}

	*p = RaytracerPipeline{}
}
