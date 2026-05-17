SHADER_DIR := internal/vulkan/shaders
SHADER_SRC := $(wildcard $(SHADER_DIR)/*.vert $(SHADER_DIR)/*.frag $(SHADER_DIR)/*.comp)
SHADER_SPV := $(SHADER_SRC:%=%.spv)
PKG_CONFIG_LIBS := $(shell pkg-config --libs glfw3 vulkan)
PKG_CONFIG_CFLAGS := $(shell pkg-config --cflags glfw3 vulkan)
RUN_ENV := GODEBUG=cgocheck=0

.PHONY: shaders build run clean

shaders: $(SHADER_SPV)

$(SHADER_DIR)/%.vert.spv: $(SHADER_DIR)/%.vert
	glslangValidator -V -S vert -o $@ $<

$(SHADER_DIR)/%.frag.spv: $(SHADER_DIR)/%.frag
	glslangValidator -V -S frag -o $@ $<

$(SHADER_DIR)/%.comp.spv: $(SHADER_DIR)/%.comp
	glslangValidator -V -S comp -o $@ $<

build: shaders
	CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go build ./cmd/gogoxel

run: shaders
	$(RUN_ENV) CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go run ./cmd/gogoxel

clean:
	rm -f $(SHADER_SPV)