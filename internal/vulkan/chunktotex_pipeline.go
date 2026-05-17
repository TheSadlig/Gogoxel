package vulkan

import (
	"errors"
	"fmt"

	"Gogoxel/internal/vulkan/shaders"

	vk "github.com/vulkan-go/vulkan"
)

const (
	chunkToTexLocalSizeX uint32 = 4
	chunkToTexLocalSizeY uint32 = 4
	chunkToTexLocalSizeZ uint32 = 4
)

type ChunkToTexPipeline struct {
	device   vk.Device
	layout   vk.PipelineLayout
	pipeline vk.Pipeline
}

func (r *Renderer) NewChunkToTexPipeline(chunkBindings *ChunkBindings) (*ChunkToTexPipeline, error) {
	if chunkBindings == nil || isZeroValue(chunkBindings.Layout()) {
		return nil, errors.New("chunk bindings are required for the chunktotex pipeline")
	}

	pipeline := &ChunkToTexPipeline{device: r.device}
	if err := pipeline.init(chunkBindings.Layout()); err != nil {
		return nil, err
	}

	return pipeline, nil
}

func (p *ChunkToTexPipeline) init(descriptorSetLayout vk.DescriptorSetLayout) error {
	strings := &cStringArena{}
	defer strings.Free()

	setLayouts := []vk.DescriptorSetLayout{descriptorSetLayout}
	if err := withPinnedValue(&p.layout, func() error {
		return vk.Error(vk.CreatePipelineLayout(p.device, &vk.PipelineLayoutCreateInfo{
		SType:          vk.StructureTypePipelineLayoutCreateInfo,
		SetLayoutCount: uint32(len(setLayouts)),
		PSetLayouts:    setLayouts,
		}, nil, &p.layout))
	}); err != nil {
		p.Close()
		return fmt.Errorf("creating chunktotex pipeline layout: %w", err)
	}

	shaderModule, err := CreateShaderModule(p.device, shaders.ChunkToTex)
	if err != nil {
		p.Close()
		return fmt.Errorf("creating chunktotex shader module: %w", err)
	}
	defer vk.DestroyShaderModule(p.device, shaderModule, nil)

	createInfos := []vk.ComputePipelineCreateInfo{{
		SType: vk.StructureTypeComputePipelineCreateInfo,
		Stage: vk.PipelineShaderStageCreateInfo{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageComputeBit,
			Module: shaderModule,
			PName:  strings.String("main"),
		},
		Layout: p.layout,
	}}

	pipelines := make([]vk.Pipeline, 1)
	var pipelineCache vk.PipelineCache
	if err := withPinnedSlice(pipelines, func() error {
		return vk.Error(vk.CreateComputePipelines(p.device, pipelineCache, 1, createInfos, nil, pipelines))
	}); err != nil {
		p.Close()
		return fmt.Errorf("creating chunktotex compute pipeline: %w", err)
	}

	p.pipeline = pipelines[0]
	return nil
}

func (p *ChunkToTexPipeline) Dispatch(commandBuffer vk.CommandBuffer, chunk *ChunkResources) error {
	if isZeroValue(p.pipeline) || isZeroValue(p.layout) {
		return errors.New("chunktotex pipeline is not initialized")
	}
	if chunk == nil {
		return errors.New("chunk resources are required")
	}
	if isZeroValue(commandBuffer) {
		return errors.New("command buffer is required for chunktotex dispatch")
	}
	if isZeroValue(chunk.DescriptorSet) {
		return errors.New("chunktotex dispatch requires a descriptor set")
	}
	if chunk.Width == 0 || chunk.Height == 0 || chunk.Depth == 0 {
		return errors.New("chunktotex dispatch dimensions must be non-zero")
	}

	if chunk.imageLayout != vk.ImageLayoutGeneral {
		barrier := vk.ImageMemoryBarrier{
			SType:         vk.StructureTypeImageMemoryBarrier,
			OldLayout:     chunk.imageLayout,
			NewLayout:     vk.ImageLayoutGeneral,
			SrcAccessMask: 0,
			DstAccessMask: vk.AccessFlags(vk.AccessShaderWriteBit),
			Image:         chunk.image,
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
				BaseMipLevel:   0,
				LevelCount:     1,
				BaseArrayLayer: 0,
				LayerCount:     1,
			},
		}
		vk.CmdPipelineBarrier(
			commandBuffer,
			vk.PipelineStageFlags(vk.PipelineStageTopOfPipeBit),
			vk.PipelineStageFlags(vk.PipelineStageComputeShaderBit),
			0,
			0,
			nil,
			0,
			nil,
			1,
			[]vk.ImageMemoryBarrier{barrier},
		)
		chunk.imageLayout = vk.ImageLayoutGeneral
	}

	descriptorSets := []vk.DescriptorSet{chunk.DescriptorSet}
	vk.CmdBindPipeline(commandBuffer, vk.PipelineBindPointCompute, p.pipeline)
	vk.CmdBindDescriptorSets(commandBuffer, vk.PipelineBindPointCompute, p.layout, 0, uint32(len(descriptorSets)), descriptorSets, 0, nil)
	vk.CmdDispatch(
		commandBuffer,
		dispatchGroupCount(chunk.Width, chunkToTexLocalSizeX),
		dispatchGroupCount(chunk.Height, chunkToTexLocalSizeY),
		dispatchGroupCount(chunk.Depth, chunkToTexLocalSizeZ),
	)

	return nil
}

func (p *ChunkToTexPipeline) DispatchOnce(renderer *Renderer, chunk *ChunkResources) error {
	if renderer == nil {
		return errors.New("renderer is required")
	}

	return renderer.SubmitOneTimeCommands(func(commandBuffer vk.CommandBuffer) error {
		return p.Dispatch(commandBuffer, chunk)
	})
}

func (p *ChunkToTexPipeline) Close() {
	if p == nil || isZeroValue(p.device) {
		return
	}

	if !isZeroValue(p.pipeline) {
		vk.DestroyPipeline(p.device, p.pipeline, nil)
	}
	if !isZeroValue(p.layout) {
		vk.DestroyPipelineLayout(p.device, p.layout, nil)
	}

	*p = ChunkToTexPipeline{}
}

func dispatchGroupCount(size, localSize uint32) uint32 {
	return (size + localSize - 1) / localSize
}
