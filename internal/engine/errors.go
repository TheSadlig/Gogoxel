package engine

import "errors"

// Sentinel errors exported from the engine package. Callers (notably the
// gRPC automation transport) match on these with errors.Is to map to
// transport-level codes, replacing brittle strings.Contains checks.
var (
	ErrUnknownGenerator        = errors.New("unknown generator")
	ErrNoSceneLoaded           = errors.New("no scene is loaded")
	ErrRendererNotInitialized  = errors.New("renderer is not initialized")
	ErrUIAutomationUnsupported = errors.New("ui automation is not implemented")
	ErrScreenshotUnsupported   = errors.New("screenshot capture is not implemented")
	ErrReadinessNotMet         = errors.New("readiness criteria were not met")
	ErrInvalidStepCount        = errors.New("step count must be non-negative")
	ErrManualStepUnavailable   = errors.New("manual stepping requires manual automation mode")
	ErrInvalidTickRate         = errors.New("tick rate must be positive")
)
