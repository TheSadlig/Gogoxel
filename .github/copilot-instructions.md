# Copilot Instructions

- Keep deterministic simulation logic inside `internal/engine`. Platform, window, and renderer concerns belong in adapters.
- Keep cursor-ray construction and voxel-edit decisions in `internal/engine` and `internal/world`; adapters may only provide normalized cursor coordinates, viewport size, and device input state.
- Extend automation behavior through `internal/session.AutomationSession` first, then mirror it through `internal/automation/grpcserver` and `internal/automation/client`.
- Keep `internal/automation` limited to transport adapters; concrete host/runtime/session-loop code belongs in `internal/game`, and shared session contracts belong in `internal/session`.
- Use semantic actions from `internal/control/actions.go` for input-facing automation and tests. Avoid device-specific assumptions in new control flows.
- Keep voxel edit actions edge-triggered. Held manual placement is a continuous stroke sampled from motion against the stroke-start scene snapshot, not against newly placed voxels.
- Treat normalized cursor coordinates as top-left renderer UV space. `EditAtCursor` is a direct normalized-ray path; validate manual click regressions through the game/input path, not only transport-level RPCs.
- Treat `headless` and `hidden-window` as distinct modes. GPU screenshots and renderer-backed readiness belong only to hidden-window mode.
- Preserve the live/manual split in `internal/game.Host`: manual sessions advance only through `StepTicks` and `StepFrames`, and live sessions own the frame loop. In live renderer mode, run as fast as present-mode pacing allows and report FPS from measured wall-clock frame intervals.
- Route automation-driven state changes through the owner-thread game-host request path; do not mutate session or `internal/game` state directly from transports, clients, or tests.
- Keep `--automation` and `--automation-listen` distinct: dedicated automation sessions are manual by default, and live sessions expose automation over a running engine. Keep automation metrics and tracing scoped to automation-exposed sessions.
- For local voxel edits on a loaded scene, prefer `ChunkResources.QueueSceneUpdate` plus frame-recorded SSBO uploads so existing brick residency is preserved when the chunk buffer budget still fits. Batch multi-point strokes through `internal/world` and reuse cached packed storage words instead of repacking on every enqueue.
- Keep `Brick.Voxels` as a `*[BrickVoxelCount]uint8` with copy-on-write. Unchanged bricks should preserve pointer identity for dirty detection, and `internal/vulkan/brick_streamer` must own a copied snapshot of brick metadata when `internal/world` reuses the `[]Brick` backing slice.
- Prefer an incremental brick-leaf fast path for edits that stay inside existing mixed brick leaves; reserve normalize/flatten rebuilds for structural edits. Reuse the `storageWords` backing slice across rebuilds.
- After recording a scene SSBO copy, insert a transfer barrier before child-pointer fills. If an edit introduces the first bricks in a scene, prime the replacement `brickStreamer` from the last camera position in the same frame, and keep configured resident capacity separate from the effective per-scene limit.
- Keep `WaitUntilReady` mode-aware. Reset the automation metrics window before performance assertions. Preserve artifacts as PNG screenshots and JSONL traces.
- Keep gRPC error behavior stable. Prefer explicit validation and consistent status codes over stringly transport behavior.