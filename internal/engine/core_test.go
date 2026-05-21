package engine

import (
	"math"
	"testing"

	"Gogoxel/internal/control"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
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

func TestCenteredCursorEditPlacesAndRemovesVoxel(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xE2554F),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	core.SetCamera(platform.Camera{
		Position: [3]float32{0, 64, 64},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	})
	if err := core.SetSelectedEditMaterial("grass"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}

	versionBeforePlace := core.SceneVersion()
	placeResult, err := core.EditAtCursor(EditModePlace, CursorSample{
		NormalizedX:  0.5,
		NormalizedY:  0.5,
		ViewportWidth: 1280,
		ViewportHeight: 720,
	})
	if err != nil {
		t.Fatalf("EditAtCursor(place) error = %v", err)
	}
	if !placeResult.Changed {
		t.Fatal("EditAtCursor(place) changed = false, want true")
	}
	if got, want := placeResult.TargetVoxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("EditAtCursor(place) target = %v, want %v", got, want)
	}
	if core.SceneVersion() <= versionBeforePlace {
		t.Fatalf("SceneVersion() = %d, want > %d after place", core.SceneVersion(), versionBeforePlace)
	}

	hit, ok := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{0, 64, 64},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false after place")
	}
	if got, want := hit.Voxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("Raycast() voxel after place = %v, want %v", got, want)
	}

	versionBeforeRemove := core.SceneVersion()
	removeResult, err := core.EditAtCursor(EditModeRemove, CursorSample{
		NormalizedX:  0.5,
		NormalizedY:  0.5,
		ViewportWidth: 1280,
		ViewportHeight: 720,
	})
	if err != nil {
		t.Fatalf("EditAtCursor(remove) error = %v", err)
	}
	if !removeResult.Changed {
		t.Fatal("EditAtCursor(remove) changed = false, want true")
	}
	if got, want := removeResult.TargetVoxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("EditAtCursor(remove) target = %v, want %v", got, want)
	}
	if core.SceneVersion() <= versionBeforeRemove {
		t.Fatalf("SceneVersion() = %d, want > %d after remove", core.SceneVersion(), versionBeforeRemove)
	}

	hit, ok = core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{0, 64, 64},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false after remove")
	}
	if got, want := hit.Voxel, [3]uint32{32, 64, 64}; got != want {
		t.Fatalf("Raycast() voxel after remove = %v, want %v", got, want)
	}
}

func TestTriggeredPlaceCubeActionUsesCursorSample(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xE2554F),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	core.SetCamera(platform.Camera{
		Position: [3]float32{0, 64, 64},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	})
	if err := core.SetSelectedEditMaterial("grass"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}
	core.SetCursorSample(CursorSample{NormalizedX: 0.5, NormalizedY: 0.5, ViewportWidth: 1280, ViewportHeight: 720})
	core.PressAction(control.ActionPlaceCube)
	if err := core.StepTicks(1); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	hit, ok := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{0, 64, 64},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false after action-driven place")
	}
	if got, want := hit.Voxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("Raycast() voxel after action-driven place = %v, want %v", got, want)
	}
}

func TestHeldPlaceCubeActionDoesNotRepeatEdits(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xE2554F),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	core.SetCamera(platform.Camera{
		Position: [3]float32{0, 64, 64},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	})
	if err := core.SetSelectedEditMaterial("grass"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}
	core.SetCursorSample(CursorSample{NormalizedX: 0.5, NormalizedY: 0.5, ViewportWidth: 1280, ViewportHeight: 720})
	core.PressAction(control.ActionPlaceCube)
	if err := core.StepTicks(10); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	hit, ok := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{0, 64, 64},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false after held action-driven place")
	}
	if got, want := hit.Voxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("Raycast() voxel after held action-driven place = %v, want %v (single edit only)", got, want)
	}
}

func TestHeldPlaceCubeActionPaintsStrokeWhileCameraMoves(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xE2554F),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	core.SetCamera(platform.Camera{
		Position: [3]float32{0, 64, 64},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	})
	if err := core.SetSelectedEditMaterial("grass"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}
	core.SetCursorSample(CursorSample{NormalizedX: 0.5, NormalizedY: 0.5, ViewportWidth: 1280, ViewportHeight: 720})
	core.PressAction(control.ActionPlaceCube)
	core.PressAction(control.ActionMoveUp)
	if err := core.StepTicks(100); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	if got := core.CurrentSVO().BrickCount(); got < 2 {
		t.Fatalf("BrickCount() = %d, want >= 2 after held stroke", got)
	}
	hit, ok := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{0, 64, 73},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false after held stroke")
	}
	if got, want := hit.Voxel, [3]uint32{31, 64, 73}; got != want {
		t.Fatalf("Raycast() voxel after held stroke = %v, want %v", got, want)
	}
}

func TestEditAtCursorHitsProjectedTopFacePoint(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 128, 64, 0xE2554F),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := core.SetSelectedEditMaterial("grass"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}

	camera := lookAtCamera([3]float32{0, 64, 128}, [3]float32{64, 64, 64}, 60)
	core.SetCamera(camera)

	projectedPoint := [3]float32{80.5, 52.5, 96.0}
	cursor := projectWorldPointToCursor(t, camera, projectedPoint, 1920, 1080)

	result, err := core.EditAtCursor(EditModePlace, cursor)
	if err != nil {
		t.Fatalf("EditAtCursor() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("EditAtCursor() changed = false, want true")
	}
	if got, want := result.Hit.Voxel, [3]uint32{80, 52, 95}; got != want {
		t.Fatalf("EditAtCursor() hit voxel = %v, want %v", got, want)
	}
	if got, want := result.TargetVoxel, [3]uint32{80, 52, 96}; got != want {
		t.Fatalf("EditAtCursor() target voxel = %v, want %v", got, want)
	}
	if got, want := result.Hit.Normal, [3]int32{0, 0, 1}; got != want {
		t.Fatalf("EditAtCursor() hit normal = %v, want %v", got, want)
	}
	if _, ok := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{80.5, 52.5, 127.5},
		Direction: [3]float32{0, 0, -1},
	}, 256); !ok {
		t.Fatal("Raycast() ok = false after projected top-face placement")
	}
}

func TestEditAtCursorPlacesOnProjectedPerlinTopFace(t *testing.T) {
	core := NewCore(NewGeneratorCatalog([]generators.Generator{
		generators.NewPerlinGenerator(1, 2),
	}), Config{TickRateHz: 60})
	if err := core.LoadGenerator("Perlin Terrain"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := core.SetSelectedEditMaterial("sand"); err != nil {
		t.Fatalf("SetSelectedEditMaterial() error = %v", err)
	}

	hit, ok := highestPerlinTopFace(core.CurrentSVO())
	if !ok {
		t.Fatal("highestPerlinTopFace() ok = false, want true")
	}
	point := [3]float32{float32(hit.Voxel[0]) + 0.5, float32(hit.Voxel[1]) + 0.5, float32(hit.Voxel[2]) + 1.0}
	camera := lookAtCamera(
		[3]float32{point[0] - 96, point[1], point[2] + 96},
		point,
		60,
	)
	core.SetCamera(camera)

	cursor := projectWorldPointToCursor(t, camera, point, 1920, 1080)
	result, err := core.EditAtCursor(EditModePlace, cursor)
	if err != nil {
		t.Fatalf("EditAtCursor() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("EditAtCursor() changed = false, want true")
	}
	if got, want := result.Hit.Voxel, hit.Voxel; got != want {
		t.Fatalf("EditAtCursor() hit voxel = %v, want %v", got, want)
	}
	if got, want := result.Hit.Normal, [3]int32{0, 0, 1}; got != want {
		t.Fatalf("EditAtCursor() hit normal = %v, want %v", got, want)
	}
	if got, want := result.TargetVoxel, [3]uint32{hit.Voxel[0], hit.Voxel[1], hit.Voxel[2] + 1}; got != want {
		t.Fatalf("EditAtCursor() target voxel = %v, want %v", got, want)
	}
	placedHit, placedOk := core.CurrentSVO().Raycast(world.Ray{
		Origin:    [3]float32{point[0], point[1], point[2] + 32},
		Direction: [3]float32{0, 0, -1},
	}, 512)
	if !placedOk {
		t.Fatal("Raycast() ok = false after terrain top-face placement")
	}
	if got, want := placedHit.Voxel, result.TargetVoxel; got != want {
		t.Fatalf("Raycast() voxel after place = %v, want %v", got, want)
	}
}

func highestPerlinTopFace(svo *world.SVO) (world.RaycastHit, bool) {
	if svo == nil {
		return world.RaycastHit{}, false
	}
	size := svo.Size()
	if size < 4 {
		return world.RaycastHit{}, false
	}
	start := size / 4
	end := (size * 3) / 4
	if end <= start {
		start = 0
		end = size
	}

	step := uint(16)
	var best world.RaycastHit
	haveBest := false
	for y := start; y < end; y += step {
		for x := start; x < end; x += step {
			hit, ok := svo.Raycast(world.Ray{
				Origin:    [3]float32{float32(x) + 0.5, float32(y) + 0.5, float32(size) + 1},
				Direction: [3]float32{0, 0, -1},
			}, float32(size)*2)
			if !ok {
				continue
			}
			if hit.Normal != [3]int32{0, 0, 1} {
				continue
			}
			if hit.Voxel[2]+1 >= uint32(size) {
				continue
			}
			if !haveBest || hit.Voxel[2] > best.Voxel[2] {
				best = hit
				haveBest = true
			}
		}
	}
	return best, haveBest
}

func lookAtCamera(position, target [3]float32, fovDeg float32) platform.Camera {
	dx := target[0] - position[0]
	dy := target[1] - position[1]
	dz := target[2] - position[2]
	horizontal := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	return platform.Camera{
		Position: position,
		YawDeg:   float32(math.Atan2(float64(dy), float64(dx)) * 180 / math.Pi),
		PitchDeg: float32(math.Atan2(float64(dz), float64(horizontal)) * 180 / math.Pi),
		FovDeg:   fovDeg,
	}
}

func projectWorldPointToCursor(t *testing.T, camera platform.Camera, point [3]float32, viewportWidth, viewportHeight int) CursorSample {
	t.Helper()
	forward := camera.Forward()
	right := camera.Right()
	up := camera.Up()
	delta := [3]float32{point[0] - camera.Position[0], point[1] - camera.Position[1], point[2] - camera.Position[2]}
	forwardDepth := dot3(delta, forward)
	if forwardDepth <= 0 {
		t.Fatalf("projected point depth = %f, want > 0", forwardDepth)
	}
	fovScale := float32(math.Tan(float64(camera.FovDeg) * 0.5 * math.Pi / 180.0))
	aspect := float32(viewportWidth) / float32(viewportHeight)
	screenX := dot3(delta, right) / (forwardDepth * fovScale * aspect)
	screenY := dot3(delta, up) / (forwardDepth * fovScale)
	return CursorSample{
		NormalizedX:   (screenX + 1) * 0.5,
		NormalizedY:   (screenY + 1) * 0.5,
		ViewportWidth:  viewportWidth,
		ViewportHeight: viewportHeight,
	}
}

func dot3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}