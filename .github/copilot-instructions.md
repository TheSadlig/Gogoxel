# Copilot Instructions

- Keep deterministic simulation logic inside `internal/engine`. Platform, window, and renderer concerns belong in adapters.
- Extend automation behavior through `internal/session.AutomationSession` first, then mirror it through `internal/automation/grpcserver` and `internal/automation/client`.
- Keep `internal/automation` limited to transport adapters; concrete host/runtime/session-loop code belongs in `internal/game`, and shared session contracts belong in `internal/session`.
- Use semantic actions from `internal/control/actions.go` for input-facing automation and tests. Avoid device-specific assumptions in new control flows.
- Treat `headless` and `hidden-window` as separate validation modes. GPU screenshots and renderer-backed readiness belong to hidden-window mode only.
- Preserve the live/manual split in `internal/game.Host`: manual sessions advance only through `StepTicks` and `StepFrames`, and live sessions own the ticker-driven loop.
- Route automation-driven state changes through the owner-thread game-host request path; do not mutate session or `internal/game` state directly from transports, clients, or tests.
- Keep `--automation` and `--automation-listen` semantically distinct: dedicated automation sessions are manual by default, and live sessions expose automation over a running engine.
- Keep `WaitUntilReady` mode-aware: manual mode may satisfy readiness by stepping, while live mode must poll readiness against tick-duration deadlines.
- Reset the automation metrics window before any performance assertion or benchmark-style scenario.
- Preserve artifact formats: screenshots are PNG files from renderer readback, traces are JSONL files from the structured trace recorder.
- Keep gRPC error behavior stable. Prefer explicit validation and consistent status codes over stringly transport behavior.