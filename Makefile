SHADER_DIR := internal/vulkan/shaders
SHADER_SRC := $(wildcard $(SHADER_DIR)/*.vert $(SHADER_DIR)/*.frag $(SHADER_DIR)/*.comp)
SHADER_SPV := $(SHADER_SRC:%=%.spv)
VULKAN_HEADERS_VERSION := v1.3.290
VULKAN_HEADERS_URL := https://github.com/KhronosGroup/Vulkan-Headers/archive/refs/tags/$(VULKAN_HEADERS_VERSION).tar.gz
VULKAN_HEADERS_DIR := third_party/vulkan-headers
VULKAN_HEADER := $(VULKAN_HEADERS_DIR)/vulkan/vulkan.h
PKG_CONFIG_LIBS := $(shell pkg-config --libs glfw3 vulkan)
PKG_CONFIG_CFLAGS := $(shell pkg-config --cflags glfw3 vulkan)
RUN_ENV := GODEBUG=cgocheck=0

.PHONY: headers shaders build run clean

headers: $(VULKAN_HEADER)

$(VULKAN_HEADER):
	@set -eu; \
	tmp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp_dir"' EXIT; \
	mkdir -p $(VULKAN_HEADERS_DIR); \
	archive="$$tmp_dir/vulkan-headers.tar.gz"; \
	if command -v curl >/dev/null 2>&1; then \
		curl -L "$(VULKAN_HEADERS_URL)" -o "$$archive"; \
	elif command -v wget >/dev/null 2>&1; then \
		wget -O "$$archive" "$(VULKAN_HEADERS_URL)"; \
	else \
		echo "need curl or wget to download Vulkan headers" >&2; \
		exit 1; \
	fi; \
	tar -xzf "$$archive" -C "$$tmp_dir"; \
	chmod -R u+w $(VULKAN_HEADERS_DIR) 2>/dev/null || true; \
	rm -rf $(VULKAN_HEADERS_DIR); \
	mkdir -p $(VULKAN_HEADERS_DIR); \
	cp -R "$$tmp_dir"/Vulkan-Headers-$(patsubst v%,%,$(VULKAN_HEADERS_VERSION))/include/* $(VULKAN_HEADERS_DIR)/

shaders: $(SHADER_SPV)

$(SHADER_DIR)/%.vert.spv: $(SHADER_DIR)/%.vert
	glslangValidator -V -S vert -o $@ $<

$(SHADER_DIR)/%.frag.spv: $(SHADER_DIR)/%.frag
	glslangValidator -V -S frag -o $@ $<

$(SHADER_DIR)/%.comp.spv: $(SHADER_DIR)/%.comp
	glslangValidator -V -S comp -o $@ $<

build: headers shaders
	CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go build ./cmd/gogoxel

run: headers shaders
	$(RUN_ENV) CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go run ./cmd/gogoxel

clean:
	rm -f $(SHADER_SPV)