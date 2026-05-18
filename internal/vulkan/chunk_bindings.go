package vulkan

import (
	"fmt"

	vk "github.com/vulkan-go/vulkan"
)

type ChunkBindings struct {
	device              vk.Device
	descriptorSetLayout vk.DescriptorSetLayout
}

func (r *Renderer) NewChunkBindings() (*ChunkBindings, error) {
	bindings := &ChunkBindings{device: r.device}
	if err := bindings.init(); err != nil {
		return nil, err
	}

	return bindings, nil
}

func (b *ChunkBindings) init() error {
	bindings := []vk.DescriptorSetLayoutBinding{
		{
			Binding:         0,
			DescriptorType:  vk.DescriptorTypeStorageBuffer,
			DescriptorCount: 1,
			StageFlags:      vk.ShaderStageFlags(vk.ShaderStageComputeBit | vk.ShaderStageFragmentBit),
		},
		{
			Binding:         1,
			DescriptorType:  vk.DescriptorTypeStorageImage,
			DescriptorCount: 1,
			StageFlags:      vk.ShaderStageFlags(vk.ShaderStageComputeBit | vk.ShaderStageFragmentBit),
		},
	}

	createInfo := vk.DescriptorSetLayoutCreateInfo{
		SType:        vk.StructureTypeDescriptorSetLayoutCreateInfo,
		BindingCount: uint32(len(bindings)),
		PBindings:    bindings,
	}
	if err := withPinnedValue(&b.descriptorSetLayout, func() error {
		return vk.Error(vk.CreateDescriptorSetLayout(b.device, &createInfo, nil, &b.descriptorSetLayout))
	}); err != nil {
		return fmt.Errorf("creating chunk descriptor set layout: %w", err)
	}

	return nil
}

func (b *ChunkBindings) Layout() vk.DescriptorSetLayout {
	if b == nil {
		var zero vk.DescriptorSetLayout
		return zero
	}

	return b.descriptorSetLayout
}

func (b *ChunkBindings) Close() {
	if b == nil || isZeroValue(b.device) {
		return
	}
	if !isZeroValue(b.descriptorSetLayout) {
		vk.DestroyDescriptorSetLayout(b.device, b.descriptorSetLayout, nil)
	}

	*b = ChunkBindings{}
}