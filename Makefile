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
PROTO_SRC := api/proto/gogoxel/automation/v1/automation.proto
GODOG_TAGS ?= ~@gpu
BDD_GPU ?=
BDD_ARTIFACT_DIR ?= $(CURDIR)/.artifacts/godog
RUN_ARGS ?=

.PHONY: headers shaders proto build run clean test test-unit test-godog test-godog-artifacts

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

proto:
	PATH="$(shell go env GOPATH)/bin:$$PATH" protoc -I api/proto --go_out=. --go_opt=module=Gogoxel --go-grpc_out=. --go-grpc_opt=module=Gogoxel $(PROTO_SRC)

build: headers shaders
	CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go build ./cmd/gogoxel

run: headers shaders
	$(RUN_ENV) CGO_CFLAGS="$(PKG_CONFIG_CFLAGS)" CGO_LDFLAGS="$(PKG_CONFIG_LIBS)" go run ./cmd/gogoxel $(RUN_ARGS)

test: test-unit test-godog

test-unit:
	go test ./...

test-godog:
	GOGOXEL_BDD_GPU='$(BDD_GPU)' GODOG_TAGS='$(GODOG_TAGS)' go test -count=1 -tags=godog ./test/bdd

test-godog-artifacts: shaders
	rm -rf '$(BDD_ARTIFACT_DIR)/hidden-window'
	mkdir -p '$(BDD_ARTIFACT_DIR)/hidden-window'
	GOGOXEL_BDD_GPU='1' GOGOXEL_BDD_ARTIFACT_DIR='$(BDD_ARTIFACT_DIR)/hidden-window' GODOG_TAGS='@gpu&&~@perf' go test -v -tags=godog ./test/bdd
	@printf 'Artifacts written to %s\n' '$(BDD_ARTIFACT_DIR)/hidden-window'
	@find '$(BDD_ARTIFACT_DIR)/hidden-window' -mindepth 2 -type f \( -name '*.png' -o -name '*.jsonl' \) -print | sort

clean:
	rm -f $(SHADER_SPV)