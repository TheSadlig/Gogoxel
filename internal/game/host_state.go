package game

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"Gogoxel/internal/session"
)

func newSessionState(options HostOptions) (*sessionState, error) {
	if err := os.MkdirAll(options.ArtifactDir, 0o755); err != nil {
		return nil, err
	}
	g := New(Options{
		Headless:     options.Headless,
		HiddenWindow: options.HiddenWindow,
		TickRateHz:   options.TickRateHz,
	})
	if err := g.Start(); err != nil {
		return nil, err
	}
	if !options.Live && !options.Headless {
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

func (s *sessionState) step(ctx context.Context, count int) (session.StepResult, error) {
	if count < 0 {
		return session.StepResult{}, fmt.Errorf("step count must be non-negative")
	}
	for index := 0; index < count; index++ {
		select {
		case <-ctx.Done():
			return session.StepResult{}, ctx.Err()
		default:
		}
		startedAt := time.Now()
		if err := s.game.StepFrame(s.game.TickDuration()); err != nil {
			return session.StepResult{}, err
		}
		sample := time.Since(startedAt)
		s.frameWindow.Record(sample)
		s.trace.recordFrame(sample, s.metricsSnapshot())
	}
	return session.StepResult{Ticks: count, Frames: count, Metrics: s.metricsSnapshot()}, nil
}

func (s *sessionState) advanceLiveFrame() error {
	if !s.options.Headless {
		s.game.PollEvents()
		if s.game.IsIconified() {
			return nil
		}
	}
	startedAt := time.Now()
	if err := s.game.StepFrame(s.game.TickDuration()); err != nil {
		return err
	}
	s.game.RecordFrame(s.game.TickDuration())
	sample := time.Since(startedAt)
	s.frameWindow.Record(sample)
	s.trace.recordFrame(sample, s.metricsSnapshot())
	return nil
}

func (s *sessionState) waitUntilReady(ctx context.Context, criteria session.WaitCriteria) (session.Readiness, error) {
	maxTicks := criteria.MaxTicks
	if maxTicks <= 0 {
		maxTicks = defaultWaitCriteria().MaxTicks
	}
	for tick := 0; tick <= maxTicks; tick++ {
		readiness := s.readiness()
		if readinessMatches(readiness, criteria) {
			return readiness, nil
		}
		select {
		case <-ctx.Done():
			return session.Readiness{}, ctx.Err()
		default:
		}
		if _, err := s.step(ctx, 1); err != nil {
			return session.Readiness{}, err
		}
	}
	return s.readiness(), fmt.Errorf("readiness criteria were not met after %d ticks", maxTicks)
}

func (s *sessionState) readiness() session.Readiness {
	snapshot := s.game.Snapshot()
	streaming := s.game.StreamingStats()
	readiness := session.Readiness{
		EngineInitialized:   s.game != nil,
		RendererInitialized: s.game.RendererInitialized(),
		SceneLoaded:         snapshot.SceneLoaded,
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

func (s *sessionState) metricsSnapshot() session.MetricsSnapshot {
	snapshot := s.game.Snapshot()
	streaming := s.game.StreamingStats()
	frameWindow := s.frameWindow.Snapshot()
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	return session.MetricsSnapshot{
		Camera:                       snapshot.Camera,
		CurrentGenerator:             snapshot.GeneratorName,
		RAMBytes:                     memStats.Alloc,
		VRAMBytes:                    s.game.VRAMBytes(),
		ChunkRAMBytes:                s.game.ChunkRAMBytes(),
		NodeCount:                    snapshot.NodeCount,
		BrickCount:                   snapshot.BrickCount,
		WorldSize:                    snapshot.WorldSize,
		ResidentBrickCount:           s.game.ResidentBrickCount(),
		StreamingDesiredReady:        streaming.DesiredReady,
		StreamingPendingDesiredCount: streaming.PendingDesiredCount,
		StreamingResidentLimit:       streaming.ResidentLimit,
		StreamingUploadBudget:        streaming.UploadBudget,
		PresentMode:                  s.game.PresentModeName(),
		RendererDevice:               s.game.DeviceName(),
		AverageFPS:                   frameWindow.AverageFPS,
		AverageFrameTimeMs:           frameWindow.AverageFrameTime.Seconds() * 1000,
		P95FrameTimeMs:               frameWindow.P95FrameTime.Seconds() * 1000,
		FrameSampleCount:             frameWindow.SampleCount,
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
