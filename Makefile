SHADER_DIR := internal/vulkan/shaders
VERT_SHADER := $(SHADER_DIR)/raytracer.vert
FRAG_SHADER := $(SHADER_DIR)/raytracer.frag
VERT_SPV := $(VERT_SHADER).spv
FRAG_SPV := $(FRAG_SHADER).spv
PKG_CONFIG_LIBS := $(shell pkg-config --libs glfw3 vulkan)
PKG_CONFIG_CFLAGS := $(shell pkg-config --cflags glfw3 vulkan)
RUN_ENV := GODEBUG=cgocheck=0

.PHONY: shaders build run clean

shaders: $(VERT_SPV) $(FRAG_SPV)

$(VERT_SPV): $(VERT_SHADER)
	glslangValidator -V -S vert -o $@ $<

$(FRAG_SPV): $(FRAG_SHADER)
	glslangValidator -V -S frag -o $@ $<

build: shaders
	CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go build ./cmd/gogoxel

run: shaders
	$(RUN_ENV) CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go run ./cmd/gogoxel

clean:
	rm -f $(VERT_SPV) $(FRAG_SPV)