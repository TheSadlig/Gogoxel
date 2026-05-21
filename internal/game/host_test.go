package game

import (
	"context"
	"math"
	"testing"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/session"
)

func TestHeadlessLiveHostAdvancesRunningSession(t *testing.T) {
	host := NewHost(HostOptions{Headless: true, Live: true, TickRateHz: 120, ArtifactDir: t.TempDir()})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(runCtx)
	}()

	callCtx, cancelCall := context.WithTimeout(context.Background(), time.Second)
	defer cancelCall()
	if err := host.LoadGenerator(callCtx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := host.SetCamera(callCtx, platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := host.PressAction(callCtx, control.ActionMoveForward); err != nil {
		t.Fatalf("PressAction() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		camera, err := host.GetCamera(callCtx)
		if err != nil {
			t.Fatalf("GetCamera() error = %v", err)
		}
		if camera.Position[0] >= 0.5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("camera X = %f, want at least 0.5 while live session is running", camera.Position[0])
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := host.StepTicks(callCtx, 1); err == nil || err.Error() != "manual stepping requires manual automation mode" {
		t.Fatalf("StepTicks() error = %v, want manual-step rejection", err)
	}

	if err := host.Stop(callCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestHeadlessManualHostStillUsesDeterministicSteps(t *testing.T) {
	host := NewHost(HostOptions{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(runCtx)
	}()

	callCtx := context.Background()
	if err := host.LoadGenerator(callCtx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := host.SetCamera(callCtx, platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := host.PressAction(callCtx, control.ActionMoveForward); err != nil {
		t.Fatalf("PressAction() error = %v", err)
	}
	if _, err := host.StepTicks(callCtx, 20); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	camera, err := host.GetCamera(callCtx)
	if err != nil {
		t.Fatalf("GetCamera() error = %v", err)
	}
	if diff := math.Abs(float64(camera.Position[0] - 2)); diff > 0.0001 {
		t.Fatalf("camera X = %f, want 2.0", camera.Position[0])
	}

	if err := host.Stop(callCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestHeadlessLiveHostSetTickRateUpdatesLiveTickDuration(t *testing.T) {
	host := NewHost(HostOptions{Headless: true, Live: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(runCtx)
	}()

	callCtx, cancelCall := context.WithTimeout(context.Background(), time.Second)
	defer cancelCall()
	if err := host.SetTickRate(callCtx, 120); err != nil {
		t.Fatalf("SetTickRate() error = %v", err)
	}

	tickDuration, err := host.currentTickDuration(callCtx)
	if err != nil {
		t.Fatalf("currentTickDuration() error = %v", err)
	}
	if tickDuration != time.Second/120 {
		t.Fatalf("tick duration = %v, want %v", tickDuration, time.Second/120)
	}

	if err := host.Stop(callCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestAdvanceLiveFrameUsesWallIntervalForFPSMetrics(t *testing.T) {
	const liveDelta = 100 * time.Millisecond // simulate ~10 FPS live cadence
	state := &sessionState{
		options: HostOptions{Headless: true, Live: true, TickRateHz: 60, AutomationExposed: true},
		game:        New(Options{Headless: true, TickRateHz: 60}),
		frameWindow: &frameWindow{},
		trace:       newTraceRecorder(nil),
	}
	state.game.fpsFrames = 59
	state.game.fpsElapsed = 950 * time.Millisecond

	if err := state.advanceLiveFrame(liveDelta); err != nil {
		t.Fatalf("advanceLiveFrame() error = %v", err)
	}
	if state.game.lastFPS == 0 {
		t.Fatal("lastFPS = 0, want delta to push the FPS window past one second")
	}
	if state.game.lastFPS >= 59 {
		t.Fatalf("lastFPS = %.2f, want it to reflect the slower live interval", state.game.lastFPS)
	}

	snapshot := state.frameWindow.Snapshot()
	if snapshot.SampleCount != 1 {
		t.Fatalf("SampleCount = %d, want 1", snapshot.SampleCount)
	}
	if snapshot.AverageFPS >= 20 {
		t.Fatalf("AverageFPS = %.2f, want a live cadence below 20 FPS for a 100ms interval", snapshot.AverageFPS)
	}
}

func TestAdvanceLiveFrameSkipsAutomationMetricsWhenNotExposed(t *testing.T) {
	state := &sessionState{
		options:     HostOptions{Headless: true, Live: true, TickRateHz: 60, AutomationExposed: false},
		game:        New(Options{Headless: true, TickRateHz: 60}),
		frameWindow: &frameWindow{},
		trace:       newTraceRecorder(nil),
	}
	initialTraceEvents := len(state.trace.events)

	if err := state.advanceLiveFrame(16 * time.Millisecond); err != nil {
		t.Fatalf("advanceLiveFrame() error = %v", err)
	}
	if got := state.frameWindow.Snapshot().SampleCount; got != 0 {
		t.Fatalf("frame sample count = %d, want 0 when automation is not exposed", got)
	}
	if got := len(state.trace.events); got != initialTraceEvents {
		t.Fatalf("trace event count = %d, want %d when automation is not exposed", got, initialTraceEvents)
	}
}

func TestHeadlessManualHostEditsCenteredCursor(t *testing.T) {
	host := NewHost(HostOptions{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(runCtx)
	}()

	callCtx := context.Background()
	if err := host.LoadGenerator(callCtx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := host.SetCamera(callCtx, platform.Camera{
		Position: [3]float32{0, 64, 64},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := host.SetSelectedMaterial(callCtx, "grass"); err != nil {
		t.Fatalf("SetSelectedMaterial() error = %v", err)
	}

	placeResult, err := host.EditAtCursor(callCtx, session.EditModePlace, session.CursorPosition{NormalizedX: 0.5, NormalizedY: 0.5})
	if err != nil {
		t.Fatalf("EditAtCursor(place) error = %v", err)
	}
	if !placeResult.Changed {
		t.Fatal("EditAtCursor(place) changed = false, want true")
	}
	if got, want := placeResult.TargetVoxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("EditAtCursor(place) target = %v, want %v", got, want)
	}

	removeResult, err := host.EditAtCursor(callCtx, session.EditModeRemove, session.CursorPosition{NormalizedX: 0.5, NormalizedY: 0.5})
	if err != nil {
		t.Fatalf("EditAtCursor(remove) error = %v", err)
	}
	if !removeResult.Changed {
		t.Fatal("EditAtCursor(remove) changed = false, want true")
	}
	if got, want := removeResult.TargetVoxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("EditAtCursor(remove) target = %v, want %v", got, want)
	}

	if err := host.Stop(callCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}