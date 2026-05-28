package game

import (
	"context"
	"errors"
	"fmt"
	"time"

	"Gogoxel/internal/engine"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/session"
)

func (h *Host) GetReadiness(ctx context.Context) (session.Readiness, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.readiness(), nil
	})
	if err != nil {
		return session.Readiness{}, err
	}
	return value.(session.Readiness), nil
}

func (h *Host) Reset(ctx context.Context) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("reset", nil)
		state.frameWindow.Reset()
		return nil, state.game.Reset()
	})
	return err
}

func (h *Host) LoadGenerator(ctx context.Context, request session.GeneratorLoadRequest) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("load_generator", map[string]any{
			"name":        request.Name,
			"chunk_x":     request.ChunkX,
			"chunk_y":     request.ChunkY,
			"chunk_range": request.ChunkRange,
		})
		if request.ChunkRange == 0 {
			return nil, state.game.LoadGenerator(request.Name)
		}
		return nil, state.game.LoadGeneratorAt(engine.GeneratorLoadRequest{
			Name:       request.Name,
			ChunkX:     request.ChunkX,
			ChunkY:     request.ChunkY,
			ChunkRange: request.ChunkRange,
		})
	})
	return err
}

func (h *Host) SetCamera(ctx context.Context, camera platform.Camera) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("set_camera", map[string]any{
			"position":  []float32{camera.Position[0], camera.Position[1], camera.Position[2]},
			"yaw_deg":   camera.YawDeg,
			"pitch_deg": camera.PitchDeg,
			"fov_deg":   camera.FovDeg,
		})
		state.game.SetCamera(camera)
		state.trace.recordCamera(camera)
		return nil, nil
	})
	return err
}

func (h *Host) GetCamera(ctx context.Context) (platform.Camera, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.game.Camera(), nil
	})
	if err != nil {
		return platform.Camera{}, err
	}
	return value.(platform.Camera), nil
}

func (h *Host) SetTickRate(ctx context.Context, tickRateHz int) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		if tickRateHz <= 0 {
			return nil, engine.ErrInvalidTickRate
		}
		state.trace.recordCommand("set_tick_rate", map[string]any{"tick_rate_hz": tickRateHz})
		state.game.SetTickRateHz(tickRateHz)
		return nil, nil
	})
	return err
}

func (h *Host) PressAction(ctx context.Context, action input.Action) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("press_action", map[string]any{"action": action})
		state.game.PressAction(action)
		return nil, nil
	})
	return err
}

func (h *Host) ReleaseAction(ctx context.Context, action input.Action) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("release_action", map[string]any{"action": action})
		state.game.ReleaseAction(action)
		return nil, nil
	})
	return err
}

func (h *Host) SetSelectedMaterial(ctx context.Context, name string) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("set_selected_material", map[string]any{"material_name": name})
		return nil, state.game.SetSelectedEditMaterial(name)
	})
	return err
}

// SetOnEdit registers a per-edit callback through the owner-thread
// request path. The callback runs on the game-host owner thread; it
// must not block. Pass nil to clear.
func (h *Host) SetOnEdit(ctx context.Context, fn func(engine.EditResult)) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.game.SetOnEdit(fn)
		return nil, nil
	})
	return err
}

func (h *Host) EditAtCursor(ctx context.Context, mode session.EditMode, cursor session.CursorPosition) (session.CursorEditResult, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("edit_at_cursor", map[string]any{
			"mode":         mode,
			"normalized_x": cursor.NormalizedX,
			"normalized_y": cursor.NormalizedY,
		})
		editMode, err := editModeToEngine(mode)
		if err != nil {
			return session.CursorEditResult{}, err
		}
		result, err := state.game.EditAtCursor(editMode, cursor.NormalizedX, cursor.NormalizedY)
		if err != nil {
			return session.CursorEditResult{}, err
		}
		return session.CursorEditResult{
			Changed:      result.Changed,
			HitVoxel:     result.Hit.Voxel,
			TargetVoxel:  result.TargetVoxel,
			MaterialName: result.MaterialName,
		}, nil
	})
	if err != nil {
		return session.CursorEditResult{}, err
	}
	return value.(session.CursorEditResult), nil
}

func (h *Host) ClickUI(ctx context.Context, logicalID string) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("click_ui", map[string]any{"logical_id": logicalID})
		return nil, engine.ErrUIAutomationUnsupported
	})
	return err
}

func (h *Host) StepTicks(ctx context.Context, ticks int) (session.StepResult, error) {
	if h.options.Live {
		return session.StepResult{}, engine.ErrManualStepUnavailable
	}
	value, err := h.invoke(ctx, func(callCtx context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("step_ticks", map[string]any{"ticks": ticks})
		return state.step(callCtx, ticks)
	})
	if err != nil {
		return session.StepResult{}, err
	}
	return value.(session.StepResult), nil
}

func (h *Host) StepFrames(ctx context.Context, frames int) (session.StepResult, error) {
	if h.options.Live {
		return session.StepResult{}, engine.ErrManualStepUnavailable
	}
	value, err := h.invoke(ctx, func(callCtx context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("step_frames", map[string]any{"frames": frames})
		return state.step(callCtx, frames)
	})
	if err != nil {
		return session.StepResult{}, err
	}
	return value.(session.StepResult), nil
}

func (h *Host) WaitUntilReady(ctx context.Context, criteria session.WaitCriteria) (session.Readiness, error) {
	if criteria == (session.WaitCriteria{}) {
		criteria = defaultWaitCriteria()
	}
	if h.options.Live {
		return h.waitUntilReadyLive(ctx, criteria)
	}
	value, err := h.invoke(ctx, func(callCtx context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("wait_until_ready", map[string]any{
			"require_renderer":          criteria.RequireRenderer,
			"require_scene_loaded":      criteria.RequireSceneLoaded,
			"require_streaming_settled": criteria.RequireStreamingSettled,
			"max_ticks":                 criteria.MaxTicks,
		})
		return state.waitUntilReady(callCtx, criteria)
	})
	if err != nil {
		return session.Readiness{}, err
	}
	return value.(session.Readiness), nil
}

func (h *Host) waitUntilReadyLive(ctx context.Context, criteria session.WaitCriteria) (session.Readiness, error) {
	maxTicks := criteria.MaxTicks
	if maxTicks <= 0 {
		maxTicks = defaultWaitCriteria().MaxTicks
	}
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("wait_until_ready", map[string]any{
			"require_renderer":          criteria.RequireRenderer,
			"require_scene_loaded":      criteria.RequireSceneLoaded,
			"require_streaming_settled": criteria.RequireStreamingSettled,
			"max_ticks":                 maxTicks,
		})
		return nil, nil
	})
	if err != nil {
		return session.Readiness{}, err
	}
	tickDuration, err := h.currentTickDuration(ctx)
	if err != nil {
		return session.Readiness{}, err
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, time.Duration(maxTicks)*tickDuration)
	defer cancel()
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	lastReadiness := session.Readiness{}
	for {
		readiness, err := h.GetReadiness(deadlineCtx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lastReadiness, fmt.Errorf("%w after %d ticks", engine.ErrReadinessNotMet, maxTicks)
			}
			return session.Readiness{}, err
		}
		lastReadiness = readiness
		if readinessMatches(readiness, criteria) {
			return readiness, nil
		}
		select {
		case <-deadlineCtx.Done():
			if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
				return lastReadiness, fmt.Errorf("%w after %d ticks", engine.ErrReadinessNotMet, maxTicks)
			}
			return session.Readiness{}, deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}

func editModeToEngine(mode session.EditMode) (engine.EditMode, error) {
	switch mode {
	case session.EditModePlace:
		return engine.EditModePlace, nil
	case session.EditModeRemove:
		return engine.EditModeRemove, nil
	default:
		return engine.EditModeUnknown, fmt.Errorf("unsupported edit mode %q", mode)
	}
}

func (h *Host) ResetMetricsWindow(ctx context.Context) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("reset_metrics_window", nil)
		state.frameWindow.Reset()
		state.heapAllocBaselineBytes = currentHeapTotalAllocBytes()
		state.game.ResetTransferQueueUploadCounter()
		return nil, nil
	})
	return err
}

func (h *Host) GetMetrics(ctx context.Context) (session.MetricsSnapshot, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.metricsSnapshot(), nil
	})
	if err != nil {
		return session.MetricsSnapshot{}, err
	}
	return value.(session.MetricsSnapshot), nil
}

func (h *Host) CaptureScreenshot(ctx context.Context, name string) (session.ArtifactInfo, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("capture_screenshot", map[string]any{"name": name})
		path, err := state.screenshotPath(name)
		if err != nil {
			return session.ArtifactInfo{}, err
		}
		if err := state.game.CaptureScreenshot(path); err != nil {
			return session.ArtifactInfo{}, err
		}
		artifact := session.ArtifactInfo{Kind: "screenshot", RequestedName: name, Path: path, Format: "png"}
		state.trace.recordArtifact(artifact.Kind, artifact.RequestedName, artifact.Path)
		return artifact, nil
	})
	if err != nil {
		return session.ArtifactInfo{}, err
	}
	return value.(session.ArtifactInfo), nil
}

func (h *Host) ExportTrace(ctx context.Context, name string) (session.ArtifactInfo, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("export_trace", map[string]any{"name": name})
		path, err := state.tracePath(name)
		if err != nil {
			return session.ArtifactInfo{}, err
		}
		if err := state.trace.Export(path); err != nil {
			return session.ArtifactInfo{}, err
		}
		artifact := session.ArtifactInfo{Kind: "trace", RequestedName: name, Path: path, Format: "jsonl"}
		state.trace.recordArtifact(artifact.Kind, artifact.RequestedName, artifact.Path)
		return artifact, nil
	})
	if err != nil {
		return session.ArtifactInfo{}, err
	}
	return value.(session.ArtifactInfo), nil
}

func (h *Host) Stop(ctx context.Context) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("stop", nil)
		state.stopRequested = true
		state.game.RequestClose()
		return nil, nil
	})
	return err
}
