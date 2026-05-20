package automation

import (
	"context"
	"math"
	"testing"

	"Gogoxel/internal/control"
	"Gogoxel/internal/platform"
)

func TestHeadlessHostMovesDeterministically(t *testing.T) {
	host := NewHost(Options{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
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