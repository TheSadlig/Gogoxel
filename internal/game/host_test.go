package game

import (
	"context"
	"math"
	"testing"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/platform"
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