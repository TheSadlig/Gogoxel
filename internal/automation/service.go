package automation

import (
	"context"
	"time"

	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
)

type Options struct {
	Headless     bool
	HiddenWindow bool
	TickRateHz   int
	ArtifactDir  string
}

type Readiness struct {
	EngineInitialized   bool
	RendererInitialized bool
	SceneLoaded         bool
	StreamingPlanned    bool
	StreamingSettled    bool
}

type WaitCriteria struct {
	RequireRenderer         bool
	RequireSceneLoaded      bool
	RequireStreamingSettled bool
	MaxTicks                int
}

type MetricsSnapshot struct {
	Camera                      platform.Camera
	CurrentGenerator            string
	RAMBytes                    uint64
	VRAMBytes                   uint64
	ChunkRAMBytes               uint64
	NodeCount                   int
	BrickCount                  int
	WorldSize                   uint
	ResidentBrickCount          int
	StreamingDesiredReady       bool
	StreamingPendingDesiredCount int
	StreamingResidentLimit      int
	StreamingUploadBudget       int
	PresentMode                 string
	RendererDevice              string
	AverageFPS                  float64
	AverageFrameTimeMs          float64
	P95FrameTimeMs              float64
	FrameSampleCount            int
}

type ArtifactInfo struct {
	Kind          string
	RequestedName string
	Path          string
	Format        string
}

type StepResult struct {
	Ticks   int
	Frames  int
	Metrics MetricsSnapshot
}

type Service interface {
	GetReadiness(context.Context) (Readiness, error)
	Reset(context.Context) error
	LoadGenerator(context.Context, string) error
	SetCamera(context.Context, platform.Camera) error
	SetTickRate(context.Context, int) error
	GetCamera(context.Context) (platform.Camera, error)
	PressAction(context.Context, input.Action) error
	ReleaseAction(context.Context, input.Action) error
	ClickUI(context.Context, string) error
	StepTicks(context.Context, int) (StepResult, error)
	StepFrames(context.Context, int) (StepResult, error)
	WaitUntilReady(context.Context, WaitCriteria) (Readiness, error)
	ResetMetricsWindow(context.Context) error
	GetMetrics(context.Context) (MetricsSnapshot, error)
	CaptureScreenshot(context.Context, string) (ArtifactInfo, error)
	ExportTrace(context.Context, string) (ArtifactInfo, error)
	Stop(context.Context) error
}

func normalizeOptions(options Options) Options {
	if options.TickRateHz <= 0 {
		options.TickRateHz = 60
	}
	if options.Headless {
		options.HiddenWindow = false
	}
	if options.ArtifactDir == "" {
		options.ArtifactDir = "artifacts/automation"
	}
	return options
}

func defaultWaitCriteria() WaitCriteria {
	return WaitCriteria{
		RequireSceneLoaded:      true,
		RequireStreamingSettled: true,
		MaxTicks:                600,
	}
}

func (m MetricsSnapshot) FrameWindowDuration() time.Duration {
	if m.FrameSampleCount == 0 || m.AverageFrameTimeMs <= 0 {
		return 0
	}
	return time.Duration(float64(time.Millisecond) * m.AverageFrameTimeMs * float64(m.FrameSampleCount))
}