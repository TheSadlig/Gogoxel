# Copilot Instructions

- Keep deterministic simulation logic inside `internal/engine`. Platform, window, and renderer concerns belong in adapters.
- Extend automation behavior through `internal/automation.Service` first, then mirror it through `internal/automation/grpcserver` and `internal/automation/client`.
- Use semantic actions from `internal/control/actions.go` for input-facing automation and tests. Avoid device-specific assumptions in new control flows.
- Treat `headless` and `hidden-window` as separate validation modes. GPU screenshots and renderer-backed readiness belong to hidden-window mode only.
- Reset the automation metrics window before any performance assertion or benchmark-style scenario.
- Preserve artifact formats: screenshots are PNG files from renderer readback, traces are JSONL files from the structured trace recorder.
- Keep gRPC error behavior stable. Prefer explicit validation and consistent status codes over stringly transport behavior.