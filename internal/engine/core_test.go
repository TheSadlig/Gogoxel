package engine

import (
	"math"
	"testing"

	"Gogoxel/internal/control"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/platform"
)

func TestForwardMovementIsDeterministic(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xAA5500),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	core.SetCamera(platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	})
	core.PressAction(control.ActionMoveForward)
	if err := core.StepTicks(20); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	got := core.Camera()
	if diff := math.Abs(float64(got.Position[0] - 2)); diff > 0.0001 {
		t.Fatalf("camera X = %f, want 2.0", got.Position[0])
	}
	if diff := math.Abs(float64(got.Position[1])); diff > 0.0001 {
		t.Fatalf("camera Y = %f, want 0.0", got.Position[1])
	}
	if diff := math.Abs(float64(got.Position[2])); diff > 0.0001 {
		t.Fatalf("camera Z = %f, want 0.0", got.Position[2])
	}
}

func TestLoadGeneratorByStableName(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 64, 32, 0xAA5500),
		generators.NewCubeGenerator("Second", 32, 16, 0x00AA55),
	}), Config{TickRateHz: 60})

	if err := core.LoadGenerator("Second"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}

	snapshot := core.Snapshot()
	if !snapshot.SceneLoaded {
		t.Fatal("expected scene to be loaded")
	}
	if snapshot.GeneratorName != "Second" {
		t.Fatalf("GeneratorName = %q, want %q", snapshot.GeneratorName, "Second")
	}
	if snapshot.NodeCount == 0 {
		t.Fatal("expected generated scene to contain nodes")
	}
}

func TestResetClearsLoadedSceneAndBumpsVersion(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 64, 32, 0xAA5500),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	versionBeforeReset := core.SceneVersion()

	core.Reset()

	snapshot := core.Snapshot()
	if snapshot.SceneLoaded {
		t.Fatal("expected reset core to clear the active scene")
	}
	if snapshot.SceneVersion <= versionBeforeReset {
		t.Fatalf("SceneVersion = %d, want > %d", snapshot.SceneVersion, versionBeforeReset)
	}
}