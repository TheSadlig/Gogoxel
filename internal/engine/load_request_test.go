package engine

import (
	"math"
	"testing"

	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
)

type cameraDrivenTestGenerator struct {
	name      string
	chunkSize uint
	requests  []generators.BuildRequest
}

func (g *cameraDrivenTestGenerator) Name() string {
	return g.name
}

func (g *cameraDrivenTestGenerator) ChunkSize() uint {
	return g.chunkSize
}

func (g *cameraDrivenTestGenerator) CameraDriven() bool {
	return true
}

func (g *cameraDrivenTestGenerator) BuildSVO(svo *world.SVO, request generators.BuildRequest) error {
	g.requests = append(g.requests, request.Normalized())
	svo.BuildTreeSparseFunc(request.SceneSize(g.chunkSize), func(add func(x, y, z uint, color uint32)) {
		add(1, 1, 1, 0xFFFFFFFF)
	})
	return nil
}

func TestLoadGeneratorAtCentersCameraOnRequestedChunkWindow(t *testing.T) {
	catalog := NewGeneratorCatalog([]generators.Generator{
		generators.NewCubeGenerator("Cube", 16, 8, 0xFF554FE2),
	})
	core := NewCore(catalog, Config{})
	request := GeneratorLoadRequest{Name: "Cube", ChunkX: 7, ChunkY: -3, ChunkRange: 2}

	if err := core.LoadGeneratorAt(request); err != nil {
		t.Fatalf("LoadGeneratorAt returned error: %v", err)
	}

	snapshot := core.Snapshot()
	if !snapshot.SceneLoaded {
		t.Fatal("expected scene to be loaded")
	}
	if got, want := snapshot.WorldSize, uint(128); got != want {
		t.Fatalf("world size = %d, want %d", got, want)
	}

	requestedSpan := float32((request.ChunkRange*2 + 1) * 16)
	wantCenterXY := float32(request.ChunkRange*16 + 8)
	minBounds, maxBounds, ok := core.CurrentSVO().OccupiedBounds()
	if !ok {
		t.Fatal("expected occupied bounds")
	}
	wantCenterZ := (float32(minBounds[2]) + float32(maxBounds[2])) * 0.5

	camera := core.Camera()
	assertFloat32Close(t, camera.Position[0], wantCenterXY-requestedSpan*1.35, 0.001)
	assertFloat32Close(t, camera.Position[1], wantCenterXY-requestedSpan*1.35, 0.001)
	assertFloat32Close(t, camera.Position[2], wantCenterZ+requestedSpan*0.75, 0.001)
	assertFloat32Close(t, camera.YawDeg, 45, 0.001)
	assertFloat32Close(t, camera.PitchDeg, -18, 0.001)
	assertFloat32Close(t, camera.FovDeg, 60, 0.001)
}

func TestLoadGeneratorFocusesInitialCameraDrivenLoadInsideChunkWindow(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 128}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}

	if len(generator.requests) != 1 {
		t.Fatalf("build request count = %d, want 1", len(generator.requests))
	}
	request := generator.requests[0]
	requestedSpan := float32(request.ExactSceneSize(generator.chunkSize))
	wantCenterXY := float32(request.ChunkRange)*float32(generator.chunkSize) + float32(generator.chunkSize)*0.5
	minBounds, maxBounds, ok := core.CurrentSVO().OccupiedBounds()
	if !ok {
		t.Fatal("expected occupied bounds")
	}
	wantCenterZ := (float32(minBounds[2]) + float32(maxBounds[2])) * 0.5

	camera := core.Camera()
	altitude := maxFloat32(float32(generator.chunkSize)*3, requestedSpan*0.22, 256)
	assertFloat32Close(t, camera.Position[0], wantCenterXY-requestedSpan*0.25, 0.001)
	assertFloat32Close(t, camera.Position[1], wantCenterXY-requestedSpan*0.25, 0.001)
	assertFloat32Close(t, camera.Position[2], wantCenterZ+altitude, 0.001)
	assertFloat32Close(t, camera.YawDeg, 45, 0.001)
	assertFloat32Close(t, camera.PitchDeg, -24, 0.001)
	assertFloat32Close(t, camera.FovDeg, 60, 0.001)
}

func TestCameraDrivenGeneratorDoesNotReloadWhileCameraIsStationary(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 128}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}
	initialVersion := core.SceneVersion()
	if err := core.StepTicks(3); err != nil {
		t.Fatalf("StepTicks returned error: %v", err)
	}

	if got := len(generator.requests); got != 1 {
		t.Fatalf("build request count = %d, want 1", got)
	}
	if got := core.SceneVersion(); got != initialVersion {
		t.Fatalf("scene version = %d, want %d", got, initialVersion)
	}
}

func TestLoadGeneratorUsesCurrentCameraForCameraDrivenGenerator(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 16}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})
	core.SetCamera(platform.Camera{Position: [3]float32{33, -17, 1}, FovDeg: 60})

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}

	if len(generator.requests) != 1 {
		t.Fatalf("build request count = %d, want 1", len(generator.requests))
	}
	if got, want := generator.requests[0], (generators.BuildRequest{ChunkX: 2, ChunkY: -2, ChunkRange: 0}); got != want {
		t.Fatalf("build request = %+v, want %+v", got, want)
	}

	camera := core.Camera()
	assertFloat32Close(t, camera.Position[0], 1, 0.001)
	assertFloat32Close(t, camera.Position[1], 15, 0.001)
	assertFloat32Close(t, camera.Position[2], 1, 0.001)
}

func TestCameraDrivenGeneratorReloadsAfterCrossingChunkBoundary(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 16}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})
	core.SetCamera(platform.Camera{Position: [3]float32{4, 4, 1}, FovDeg: 60})

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}
	initialVersion := core.SceneVersion()

	core.SetCamera(platform.Camera{Position: [3]float32{20, 4, 1}, FovDeg: 60})

	if len(generator.requests) != 2 {
		t.Fatalf("build request count = %d, want 2", len(generator.requests))
	}
	if got, want := generator.requests[1], (generators.BuildRequest{ChunkX: 1, ChunkY: 0, ChunkRange: 0}); got != want {
		t.Fatalf("second build request = %+v, want %+v", got, want)
	}
	if got, want := core.SceneVersion(), initialVersion+1; got != want {
		t.Fatalf("scene version = %d, want %d", got, want)
	}

	camera := core.Camera()
	assertFloat32Close(t, camera.Position[0], 4, 0.001)
	assertFloat32Close(t, camera.Position[1], 4, 0.001)
	assertFloat32Close(t, camera.Position[2], 1, 0.001)
}

func TestLoadGeneratorUsesExpandedAutoRangeForHighAltitudeCamera(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 128}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})
	worldCamera := platform.Camera{Position: [3]float32{0, 0, 500}, PitchDeg: 0, FovDeg: 60}
	core.SetCamera(worldCamera)

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}

	if len(generator.requests) != 1 {
		t.Fatalf("build request count = %d, want 1", len(generator.requests))
	}
	chunkRange := cameraChunkRange(worldCamera, generator.chunkSize)
	if got, want := generator.requests[0], (generators.BuildRequest{ChunkX: chunkLookahead(chunkRange), ChunkY: 0, ChunkRange: chunkRange}); got != want {
		t.Fatalf("build request = %+v, want %+v", got, want)
	}
}

func TestLoadGeneratorUsesExpandedAutoRangeForLowAltitudeCamera(t *testing.T) {
	generator := &cameraDrivenTestGenerator{name: "Camera Terrain", chunkSize: 128}
	core := NewCore(NewGeneratorCatalog([]generators.Generator{generator}), Config{})
	worldCamera := platform.Camera{Position: [3]float32{128, 128, 64}, YawDeg: 45, PitchDeg: -10, FovDeg: 60}
	core.SetCamera(worldCamera)

	if err := core.LoadGenerator("Camera Terrain"); err != nil {
		t.Fatalf("LoadGenerator returned error: %v", err)
	}

	if len(generator.requests) != 1 {
		t.Fatalf("build request count = %d, want 1", len(generator.requests))
	}
	if got := generator.requests[0].ChunkRange; got < 1 {
		t.Fatalf("chunk range = %d, want >= 1 for low-altitude explicit camera", got)
	}
}

func TestCameraBuildRequestBiasesInitialCenterForward(t *testing.T) {
	core := NewCore(nil, Config{})
	worldCamera := platform.Camera{Position: [3]float32{0, 0, 500}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}

	request := core.cameraBuildRequest(128, worldCamera, generators.BuildRequest{}, true)

	chunkRange := cameraChunkRange(worldCamera, 128)
	if got, want := request, (generators.BuildRequest{ChunkX: chunkLookahead(chunkRange), ChunkY: 0, ChunkRange: chunkRange}); got != want {
		t.Fatalf("build request = %+v, want %+v", got, want)
	}
}

func TestCameraBuildRequestKeepsCenterWithinHysteresisBand(t *testing.T) {
	core := NewCore(nil, Config{})
	current := generators.BuildRequest{ChunkX: 2, ChunkY: 0, ChunkRange: 3}
	worldCamera := platform.Camera{Position: [3]float32{128 + 12, 0, 1}, PitchDeg: 0, FovDeg: 60}

	request := core.cameraBuildRequest(128, worldCamera, current, false)

	if got, want := request, (generators.BuildRequest{ChunkX: 2, ChunkY: 0, ChunkRange: 3}); got != want {
		t.Fatalf("build request = %+v, want %+v", got, want)
	}
}

func TestCameraBuildRequestRecentersIncrementallyOutsideHysteresisBand(t *testing.T) {
	core := NewCore(nil, Config{})
	current := generators.BuildRequest{ChunkX: 2, ChunkY: 0, ChunkRange: 3}
	worldCamera := platform.Camera{Position: [3]float32{3*128 + 12, 0, 1}, PitchDeg: 0, FovDeg: 60}

	request := core.cameraBuildRequest(128, worldCamera, current, false)

	if got, want := request, (generators.BuildRequest{ChunkX: 3, ChunkY: 0, ChunkRange: 3}); got != want {
		t.Fatalf("build request = %+v, want %+v", got, want)
	}
}

func assertFloat32Close(t *testing.T, got, want, tolerance float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(tolerance) {
		t.Fatalf("value = %v, want %v +/- %v", got, want, tolerance)
	}
}
