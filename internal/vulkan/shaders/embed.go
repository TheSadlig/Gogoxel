package shaders

import _ "embed"

var (
	//go:embed raytracer.frag.spv
	RaytracerSPV []byte

	//go:embed raytracer.vert.spv
	RaytracerVertexSPV []byte

	//go:embed shader.vert.spv
	VertexSPV []byte
	//go:embed shader.frag.spv
	FragmentSPV []byte
)
