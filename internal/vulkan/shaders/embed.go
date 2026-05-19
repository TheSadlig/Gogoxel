package shaders

import (
	_ "embed"
)

type Shader = []byte

var (
	//go:embed raytracer.frag.spv
	RaytracerSPV Shader

	//go:embed raytracer.vert.spv
	RaytracerVertexSPV Shader
)
