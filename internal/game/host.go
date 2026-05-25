package game

import (
	"context"
	"errors"

	"Gogoxel/internal/session"
)

var errStopped = errors.New("automation host is stopped")

type HostOptions struct {
	Headless     bool
	HiddenWindow bool
	Live         bool
	AutomationExposed bool
	TickRateHz   int
	ArtifactDir  string
	ChunkRoot    string
}

type Host struct {
	options  HostOptions
	ready    chan struct{}
	done     chan struct{}
	requests chan hostRequest
	initErr  error
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
	options       HostOptions
	game          *Game
	frameWindow   *frameWindow
	trace         *traceRecorder
	stopRequested bool
}

var _ session.AutomationSession = (*Host)(nil)

func NewHost(options HostOptions) *Host {
	return &Host{
		options:  normalizeHostOptions(options),
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		requests: make(chan hostRequest),
	}
}

func normalizeHostOptions(options HostOptions) HostOptions {
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

func defaultWaitCriteria() session.WaitCriteria {
	return session.WaitCriteria{
		RequireSceneLoaded:      true,
		RequireStreamingSettled: true,
		MaxTicks:                600,
	}
}

func readinessMatches(readiness session.Readiness, criteria session.WaitCriteria) bool {
	if criteria.RequireRenderer && !readiness.RendererInitialized {
		return false
	}
	if criteria.RequireSceneLoaded && !readiness.SceneLoaded {
		return false
	}
	if criteria.RequireStreamingSettled && !readiness.StreamingSettled {
		return false
	}
	return readiness.EngineInitialized
}
