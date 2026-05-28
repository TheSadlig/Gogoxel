# Architecture overview

> Status: skeleton — issue #9 will expand this with sequence diagrams.

Gogoxel is structured around the rule that **deterministic simulation
logic lives in `internal/engine` and `internal/world`**, and **platform
/ window / renderer concerns live in adapters under
`internal/platform`, `internal/vulkan`, and `internal/game`**.

## Layers

1. `internal/world` — voxel data model, SVO, palettes, file formats.
2. `internal/engine` — simulation core, semantic actions, sentinel
   errors, frustum culling.
3. `internal/vulkan` — Vulkan renderer + brick streamer; this is the
   only cgo-touching layer.
4. `internal/game` — session loop, host/owner-thread request path.
5. `internal/automation` — gRPC transport for headless control.
6. `pkg/` — stable public surface (see issue #5).

## Cross-cutting

- `internal/profiler` — Chrome-trace + Tracy backends.
- `internal/log`     — slog facade.
- `internal/engine/frustum` — reusable culling primitive.
