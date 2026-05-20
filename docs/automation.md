# Automation Architecture

The automation stack is split into a deterministic engine core, a thin runtime adapter, and transport adapters layered on top of a shared internal automation session contract.

## Layers

- `internal/engine` owns deterministic simulation state, generator loading, fixed-tick stepping, and camera movement.
- `internal/game` owns the session object, the concrete owner-thread host for both manual and live sessions, window and Vulkan renderer adapters when a renderer is required, the metrics window, structured trace export, and forwards semantic input snapshots into the core.
- `internal/session` owns the transport-neutral `AutomationSession` contract shared by the game host, BDD harness, and transport adapters.
- `internal/automation` owns only the transport adapters layered on top of the shared game runtime.
- `internal/automation/grpcserver` exposes the same automation service over gRPC.
- `internal/automation/client` provides a typed gRPC client for tests and tooling.

## Session Modes

- Headless mode skips GLFW and Vulkan entirely. Use it for deterministic simulation tests and CI-safe control flows.
- Hidden-window mode creates the renderer but keeps the window invisible. Use it for renderer readiness, screenshot capture, and renderer-backed metrics.

## Run Modes

- Manual mode advances only when automation requests `StepTicks` or `StepFrames`. Use it for deterministic tick-count assertions and the existing BDD suite.
- Live mode owns a ticker-driven loop, can accept local window input and remote gRPC commands against the same session, and rejects manual step RPCs.

`headless` and `hidden-window` describe renderer availability. `manual` and `live` describe how the session advances.

Screenshots are produced from Vulkan swapchain image readback and written as PNG artifacts. Trace export writes JSONL events containing session metadata, commands, camera updates, frame samples, and artifact records.

## Running Sessions

Start a dedicated manual automation session in headless mode:

```bash
go run ./cmd/gogoxel --automation --headless --listen 127.0.0.1:50051
```

For renderer-backed manual automation on Linux, use hidden-window mode instead of headless:

```bash
go run ./cmd/gogoxel --automation --hidden-window --listen 127.0.0.1:50051
```

Expose a running visible session over gRPC:

```bash
go run ./cmd/gogoxel --automation-listen 127.0.0.1:50051
```

Expose a running hidden-window session over gRPC:

```bash
go run ./cmd/gogoxel --hidden-window --automation-listen 127.0.0.1:50051
```

The gRPC `Stop` RPC requests shutdown through the game host and closes the game loop cleanly.
`StepTicks` and `StepFrames` are valid only in manual mode. `WaitUntilReady` steps the session in manual mode and polls readiness against tick-duration deadlines in live mode.

## BDD Coverage

The Godog suite lives under `test/bdd` and drives the real binary over gRPC.

- `test/features/movement.feature` covers deterministic headless movement and trace export.
- `test/features/live.feature` covers the live headless `--automation-listen` path and uses eventual-state assertions instead of manual stepping.
- `test/features/artifacts.feature` covers renderer-backed screenshot capture with one technical scenario per generator and is tagged `@gpu @trace`.
- `test/features/performance.feature` covers renderer metrics and is tagged `@gpu @perf`.

By default, the GPU-tagged scenarios are excluded. Enable them explicitly when a Vulkan-capable display environment is available.
The Godog harness now covers both manual sessions and an initial live headless session. Live scenarios should use dedicated startup steps and avoid manual step assertions.

## Make Targets

- `make proto` regenerates protobuf and gRPC stubs.
- `make test` runs the Go unit and package tests.
- `make test-godog` runs the default headless Godog slice.
- `make test-godog BDD_GPU=1 GODOG_TAGS='@gpu&&~@perf'` runs the hidden-window screenshot scenarios.
- `make test-godog BDD_GPU=1 GODOG_TAGS='@perf'` runs the hidden-window performance scenario.
- `make test-godog-artifacts` runs the hidden-window screenshot feature end to end, preserves the per-scenario PNGs and trace JSONL files under `.artifacts/godog/hidden-window/<scenario>/`, and prints the file paths.

GPU-tagged BDD runs require `BDD_GPU=1` because the suite intentionally skips those scenarios unless renderer-backed validation is requested explicitly.

Set `BDD_ARTIFACT_DIR=/absolute/or/relative/path` to redirect preserved Godog artifacts to a different location.

Scenarios tagged `@trace` automatically export a JSONL trace artifact named from the scenario title during teardown, and each scenario writes into its own artifact subdirectory, so traces remain controlled from the feature file without requiring an explicit step.