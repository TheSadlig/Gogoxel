package automation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"Gogoxel/internal/game"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
)

var errStopped = errors.New("automation host is stopped")

type Host struct {
	options Options
	ready   chan struct{}
	done    chan struct{}
	requests chan hostRequest
	initErr error
}

type hostRequest struct {
	ctx    context.Context
	run    func(context.Context, *sessionState) (any, error)
	result chan hostResult
}

type hostResult struct {
	value any
	err   error
}

type sessionState struct {
	options       Options
	game          *game.Game
	frameWindow   *frameWindow
	trace         *traceRecorder
	stopRequested bool
}

func NewHost(options Options) *Host {
	return &Host{
		options:  normalizeOptions(options),
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		requests: make(chan hostRequest),
	}
}

func (h *Host) Run(ctx context.Context) error {
	defer close(h.done)
	state, err := newSessionState(h.options)
	if err != nil {
		h.initErr = err
		close(h.ready)
		return err
	}
	defer state.close()
	close(h.ready)

	for {
		if state.stopRequested || state.game.ShouldClose() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request := <-h.requests:
			value, err := request.run(request.ctx, state)
			request.result <- hostResult{value: value, err: err}
		}
	}
}

func newSessionState(options Options) (*sessionState, error) {
	if err := os.MkdirAll(options.ArtifactDir, 0o755); err != nil {
		return nil, err
	}
	g := game.New()
	g.SetTickRateHz(options.TickRateHz)
	if !options.Headless {
		if err := g.InitWindowed(options.HiddenWindow); err != nil {
			return nil, err
		}
		if err := g.Reset(); err != nil {
			return nil, err
		}
	}
	return &sessionState{
		options:     options,
		game:        g,
		frameWindow: &frameWindow{},
		trace: newTraceRecorder(map[string]any{
			"headless":      options.Headless,
			"hidden_window": options.HiddenWindow,
			"tick_rate_hz":  options.TickRateHz,
			"go_version":    runtime.Version(),
		}),
	}, nil
}

func (s *sessionState) close() error {
	if s == nil || s.game == nil {
		return nil
	}
	return s.game.Close()
}

func (h *Host) invoke(ctx context.Context, fn func(context.Context, *sessionState) (any, error)) (any, error) {
	select {
	case <-h.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if h.initErr != nil {
		return nil, h.initErr
	}
	result := make(chan hostResult, 1)
	request := hostRequest{ctx: ctx, run: fn, result: result}
	select {
	case h.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.done:
		return nil, errStopped
	}
	select {
	case response := <-result:
		return response.value, response.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.done:
		select {
		case response := <-result:
			return response.value, response.err
		default:
			return nil, errStopped
		}
	}
}

func (h *Host) GetReadiness(ctx context.Context) (Readiness, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.readiness(), nil
	})
	if err != nil {
		return Readiness{}, err
	}
	return value.(Readiness), nil
}

func (h *Host) Reset(ctx context.Context) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("reset", nil)
		state.frameWindow.Reset()
		return nil, state.game.Reset()
	})
	return err
}

func (h *Host) LoadGenerator(ctx context.Context, name string) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("load_generator", map[string]any{"name": name})
		return nil, state.game.LoadGenerator(name)
	})
	return err
}

func (h *Host) SetCamera(ctx context.Context, camera platform.Camera) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("set_camera", map[string]any{
			"position": []float32{camera.Position[0], camera.Position[1], camera.Position[2]},
			"yaw_deg":  camera.YawDeg,
			"pitch_deg": camera.PitchDeg,
			"fov_deg":  camera.FovDeg,
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
			return nil, fmt.Errorf("tick rate must be positive")
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

func (h *Host) ClickUI(ctx context.Context, logicalID string) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("click_ui", map[string]any{"logical_id": logicalID})
		return nil, fmt.Errorf("ui automation is not implemented")
	})
	return err
}

func (h *Host) StepTicks(ctx context.Context, ticks int) (StepResult, error) {
	value, err := h.invoke(ctx, func(callCtx context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("step_ticks", map[string]any{"ticks": ticks})
		return state.step(callCtx, ticks)
	})
	if err != nil {
		return StepResult{}, err
	}
	return value.(StepResult), nil
}

func (h *Host) StepFrames(ctx context.Context, frames int) (StepResult, error) {
	value, err := h.invoke(ctx, func(callCtx context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("step_frames", map[string]any{"frames": frames})
		return state.step(callCtx, frames)
	})
	if err != nil {
		return StepResult{}, err
	}
	return value.(StepResult), nil
}

func (h *Host) WaitUntilReady(ctx context.Context, criteria WaitCriteria) (Readiness, error) {
	if criteria == (WaitCriteria{}) {
		criteria = defaultWaitCriteria()
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
		return Readiness{}, err
	}
	return value.(Readiness), nil
}

func (h *Host) ResetMetricsWindow(ctx context.Context) error {
	_, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("reset_metrics_window", nil)
		state.frameWindow.Reset()
		return nil, nil
	})
	return err
}

func (h *Host) GetMetrics(ctx context.Context) (MetricsSnapshot, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		return state.metricsSnapshot(), nil
	})
	if err != nil {
		return MetricsSnapshot{}, err
	}
	return value.(MetricsSnapshot), nil
}

func (h *Host) CaptureScreenshot(ctx context.Context, name string) (ArtifactInfo, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("capture_screenshot", map[string]any{"name": name})
		path, err := state.screenshotPath(name)
		if err != nil {
			return ArtifactInfo{}, err
		}
		if err := state.game.CaptureScreenshot(path); err != nil {
			return ArtifactInfo{}, err
		}
		artifact := ArtifactInfo{Kind: "screenshot", RequestedName: name, Path: path, Format: "png"}
		state.trace.recordArtifact(artifact.Kind, artifact.RequestedName, artifact.Path)
		return artifact, nil
	})
	if err != nil {
		return ArtifactInfo{}, err
	}
	return value.(ArtifactInfo), nil
}

func (h *Host) ExportTrace(ctx context.Context, name string) (ArtifactInfo, error) {
	value, err := h.invoke(ctx, func(_ context.Context, state *sessionState) (any, error) {
		state.trace.recordCommand("export_trace", map[string]any{"name": name})
		path, err := state.tracePath(name)
		if err != nil {
			return ArtifactInfo{}, err
		}
		if err := state.trace.Export(path); err != nil {
			return ArtifactInfo{}, err
		}
		artifact := ArtifactInfo{Kind: "trace", RequestedName: name, Path: path, Format: "jsonl"}
		state.trace.recordArtifact(artifact.Kind, artifact.RequestedName, artifact.Path)
		return artifact, nil
	})
	if err != nil {
		return ArtifactInfo{}, err
	}
	return value.(ArtifactInfo), nil
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

func (s *sessionState) step(ctx context.Context, count int) (StepResult, error) {
	if count < 0 {
		return StepResult{}, fmt.Errorf("step count must be non-negative")
	}
	for index := 0; index < count; index++ {
		select {
		case <-ctx.Done():
			return StepResult{}, ctx.Err()
		default:
		}
		startedAt := time.Now()
		if err := s.game.StepFrame(s.game.TickDuration()); err != nil {
			return StepResult{}, err
		}
		sample := time.Since(startedAt)
		s.frameWindow.Record(sample)
		s.trace.recordFrame(sample, s.metricsSnapshot())
	}
	return StepResult{Ticks: count, Frames: count, Metrics: s.metricsSnapshot()}, nil
}

func (s *sessionState) waitUntilReady(ctx context.Context, criteria WaitCriteria) (Readiness, error) {
	maxTicks := criteria.MaxTicks
	if maxTicks <= 0 {
		maxTicks = defaultWaitCriteria().MaxTicks
	}
	for tick := 0; tick <= maxTicks; tick++ {
		readiness := s.readiness()
		if readiness.matches(criteria) {
			return readiness, nil
		}
		select {
		case <-ctx.Done():
			return Readiness{}, ctx.Err()
		default:
		}
		if _, err := s.step(ctx, 1); err != nil {
			return Readiness{}, err
		}
	}
	return s.readiness(), fmt.Errorf("readiness criteria were not met after %d ticks", maxTicks)
}

func (s *sessionState) readiness() Readiness {
	metrics := s.game.Snapshot()
	streaming := s.game.StreamingStats()
	readiness := Readiness{
		EngineInitialized:   s.game != nil,
		RendererInitialized: s.game.RendererInitialized(),
		SceneLoaded:         metrics.SceneLoaded,
	}
	if !readiness.RendererInitialized {
		readiness.StreamingPlanned = readiness.SceneLoaded
		readiness.StreamingSettled = readiness.SceneLoaded
		return readiness
	}
	if !readiness.SceneLoaded {
		return readiness
	}
	readiness.StreamingPlanned = streaming.DesiredReady
	readiness.StreamingSettled = streaming.DesiredReady && streaming.PendingDesiredCount == 0
	return readiness
}

func (r Readiness) matches(criteria WaitCriteria) bool {
	if criteria.RequireRenderer && !r.RendererInitialized {
		return false
	}
	if criteria.RequireSceneLoaded && !r.SceneLoaded {
		return false
	}
	if criteria.RequireStreamingSettled && !r.StreamingSettled {
		return false
	}
	return r.EngineInitialized
}

func (s *sessionState) metricsSnapshot() MetricsSnapshot {
	snapshot := s.game.Snapshot()
	streaming := s.game.StreamingStats()
	frameWindow := s.frameWindow.Snapshot()
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	return MetricsSnapshot{
		Camera:                      snapshot.Camera,
		CurrentGenerator:            snapshot.GeneratorName,
		RAMBytes:                    memStats.Alloc,
		VRAMBytes:                   s.game.VRAMBytes(),
		ChunkRAMBytes:               s.game.ChunkRAMBytes(),
		NodeCount:                   snapshot.NodeCount,
		BrickCount:                  snapshot.BrickCount,
		WorldSize:                   snapshot.WorldSize,
		ResidentBrickCount:          s.game.ResidentBrickCount(),
		StreamingDesiredReady:       streaming.DesiredReady,
		StreamingPendingDesiredCount: streaming.PendingDesiredCount,
		StreamingResidentLimit:      streaming.ResidentLimit,
		StreamingUploadBudget:       streaming.UploadBudget,
		PresentMode:                 s.game.PresentModeName(),
		RendererDevice:              s.game.DeviceName(),
		AverageFPS:                  frameWindow.AverageFPS,
		AverageFrameTimeMs:          frameWindow.AverageFrameTime.Seconds() * 1000,
		P95FrameTimeMs:              frameWindow.P95FrameTime.Seconds() * 1000,
		FrameSampleCount:            frameWindow.SampleCount,
	}
}

func (s *sessionState) tracePath(requestedName string) (string, error) {
	if strings.TrimSpace(requestedName) == "" {
		requestedName = "automation.trace.jsonl"
	}
	path := filepath.Join(s.options.ArtifactDir, filepath.Base(requestedName))
	if !strings.HasSuffix(path, ".jsonl") {
		path += ".jsonl"
	}
	return path, nil
}

func (s *sessionState) screenshotPath(requestedName string) (string, error) {
	if strings.TrimSpace(requestedName) == "" {
		requestedName = "automation.png"
	}
	path := filepath.Join(s.options.ArtifactDir, filepath.Base(requestedName))
	if !strings.HasSuffix(strings.ToLower(path), ".png") {
		path += ".png"
	}
	return path, nil
}