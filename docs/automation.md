# Automation Architecture

The automation stack is split into a deterministic engine core, a thin runtime adapter, and transport adapters layered on top of a shared internal service.

## Layers

- `internal/engine` owns deterministic simulation state, generator loading, fixed-tick stepping, and camera movement.
- `internal/game` owns the window and Vulkan renderer when a renderer is required, and forwards semantic input snapshots into the core.
- `internal/automation` owns the transport-neutral automation service, owner-thread host, metrics window, and structured trace export.
- `internal/automation/grpcserver` exposes the same automation service over gRPC.
- `internal/automation/client` provides a typed gRPC client for tests and tooling.

## Session Modes

- Headless mode skips GLFW and Vulkan entirely. Use it for deterministic simulation tests and CI-safe control flows.
- Hidden-window mode creates the renderer but keeps the window invisible. Use it for renderer readiness, screenshot capture, and renderer-backed metrics.

Screenshots are produced from Vulkan swapchain image readback and written as PNG artifacts. Trace export writes JSONL events containing session metadata, commands, camera updates, frame samples, and artifact records.

## Running The Server

Start the binary in automation mode:

```bash
go run ./cmd/gogoxel --automation --headless --listen 127.0.0.1:50051
```

For renderer-backed automation on Linux, use hidden-window mode instead of headless:

```bash
go run ./cmd/gogoxel --automation --hidden-window --listen 127.0.0.1:50051
```

The gRPC `Stop` RPC requests shutdown through the automation host and closes the game loop cleanly.

## BDD Coverage

The Godog suite lives under `test/bdd` and drives the real binary over gRPC.

- `test/features/movement.feature` covers deterministic headless movement and trace export.
- `test/features/artifacts.feature` covers renderer-backed screenshot capture and is tagged `@gpu`.
- `test/features/performance.feature` covers renderer metrics and is tagged `@gpu @perf`.

By default, the GPU-tagged scenarios are excluded. Enable them explicitly when a Vulkan-capable display environment is available.

## Make Targets

- `make proto` regenerates protobuf and gRPC stubs.
- `make test` runs the Go unit and package tests.
- `make test-godog` runs the default headless Godog slice.
- `make test-gpu-artifacts` runs the hidden-window screenshot scenario.
- `make test-perf` runs the hidden-window performance scenario.

GPU-tagged BDD targets set `GOGOXEL_BDD_GPU=1` automatically because the suite intentionally skips those scenarios unless that environment flag is present.