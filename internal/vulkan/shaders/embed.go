package shaders

import _ "embed"

var (
	//go:embed raytracer.frag.spv
	RaytracerSPV []byte

	//go:embed raytracer.vert.spv
	RaytracerVertexSPV []byte

	//go:embed chunktotex.comp.spv
	ChunkToTex []byte
)
