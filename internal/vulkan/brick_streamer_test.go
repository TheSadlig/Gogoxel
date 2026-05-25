package vulkan

import (
	"testing"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
)

func TestCurrentUploadBudgetLockedRaisesWithMotionAndPending(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{0, 0, 0},
		[3]uint32{16, 0, 0},
		[3]uint32{24, 0, 0},
	), brickStreamerConfig{ResidentLimit: 3, UploadBudget: 64})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.desiredReady = true
	streamer.desired = []int{0, 1, 2}
	streamer.resident = map[int]residentBrick{}
	streamer.cameraMotion = [3]float32{10, 0, 0}

	got := streamer.currentUploadBudgetLocked()
	if got <= streamer.uploadBudget {
		t.Fatalf("expected dynamic budget above base upload budget, got %d (base=%d)", got, streamer.uploadBudget)
	}
	if got > streamer.maxUploadBudget {
		t.Fatalf("expected dynamic budget to stay within cap %d, got %d", streamer.maxUploadBudget, got)
	}
}

func TestCurrentUploadBudgetLockedAllowsSmallScenePrefill(t *testing.T) {
	bricks := make([]world.Brick, scenePrefillBudgetLimit)
	for index := range bricks {
		bricks[index] = world.Brick{Origin: [3]uint32{uint32(index * 8), 0, 0}}
	}

	streamer := newBrickStreamerWithConfig(bricks, brickStreamerConfig{ResidentLimit: 200, UploadBudget: 192})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.desiredReady = true
	streamer.desired = make([]int, len(bricks))
	for index := range streamer.desired {
		streamer.desired[index] = index
	}
	streamer.resident = map[int]residentBrick{}
	streamer.scenePrefill = true

	if got, want := streamer.currentUploadBudgetLocked(), len(bricks); got != want {
		t.Fatalf("scene prefill upload budget = %d, want %d", got, want)
	}
}

func TestCurrentUploadBudgetLockedBoundsLargeScenePrefill(t *testing.T) {
	bricks := make([]world.Brick, 500)
	for index := range bricks {
		bricks[index] = world.Brick{Origin: [3]uint32{uint32(index * 8), 0, 0}}
	}

	streamer := newBrickStreamerWithConfig(bricks, brickStreamerConfig{ResidentLimit: 500, UploadBudget: 192})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.desiredReady = true
	streamer.desired = make([]int, len(bricks))
	for index := range streamer.desired {
		streamer.desired[index] = index
	}
	streamer.resident = map[int]residentBrick{}
	streamer.scenePrefill = true

	if got, want := streamer.currentUploadBudgetLocked(), streamer.uploadBudget; got != want {
		t.Fatalf("large scene prefill upload budget = %d, want %d", got, want)
	}
	if streamer.scenePrefill {
		t.Fatal("expected large scene prefill to fall back to steady-state streaming")
	}
}

func TestNewBrickStreamerDefaultResidentLimitStaysBelowPoolCapacity(t *testing.T) {
	bricks := make([]world.Brick, defaultResidentBrickLimit+1024)
	for index := range bricks {
		bricks[index] = world.Brick{Origin: [3]uint32{uint32(index * 8), 0, 0}}
	}

	streamer := newBrickStreamer(bricks)
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	if streamer.residentCap != defaultResidentBrickCap {
		t.Fatalf("resident cap = %d, want %d", streamer.residentCap, defaultResidentBrickCap)
	}
	if streamer.residentTargetLimit != defaultResidentBrickLimit {
		t.Fatalf("resident target limit = %d, want %d", streamer.residentTargetLimit, defaultResidentBrickLimit)
	}
	if streamer.residentLimit != defaultResidentBrickLimit {
		t.Fatalf("resident limit = %d, want %d", streamer.residentLimit, defaultResidentBrickLimit)
	}
}

func TestComputeDesiredPrefersViewConeOverBehindCamera(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{16, 0, 0},
		[3]uint32{0, 16, 0},
		[3]uint32{^uint32(0) - 11, 0, 0},
	), brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	desired := streamer.computeDesired(platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}, [3]float32{})
	if len(desired) != 2 {
		t.Fatalf("desired count = %d, want 2", len(desired))
	}
	if desired[0] != 0 {
		t.Fatalf("expected front-facing brick first, got logical index %d", desired[0])
	}
	for _, logicalIndex := range desired {
		if logicalIndex == 2 {
			t.Fatalf("expected behind-camera brick to lose to front-hemisphere candidates, desired=%v", desired)
		}
	}
}

func TestComputeDesiredKeepsNearFieldBrickResident(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{0, 0, 0},
		[3]uint32{80, 0, 0},
	), brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	desired := streamer.computeDesired(platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 180, PitchDeg: 0, FovDeg: 60}, [3]float32{})
	if len(desired) != 1 {
		t.Fatalf("desired count = %d, want 1", len(desired))
	}
	if desired[0] != 0 {
		t.Fatalf("expected near-field brick to stay resident regardless of heading, got logical index %d", desired[0])
	}
}

func TestComputeDesiredPrefetchesMotionCorridor(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{0, 16, 0},
		[3]uint32{48, 0, 0},
		[3]uint32{0, 64, 0},
	), brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	desired := streamer.computeDesired(
		platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 90, PitchDeg: 0, FovDeg: 60},
		[3]float32{12, 0, 0},
	)
	if len(desired) != 2 {
		t.Fatalf("desired count = %d, want 2", len(desired))
	}
	if desired[0] != 0 {
		t.Fatalf("expected current near field to remain first, got logical index %d", desired[0])
	}
	if desired[1] != 1 {
		t.Fatalf("expected motion corridor to prefetch the upcoming brick next, got desired=%v", desired)
	}
}

func TestComputeDesiredUsesFullResidentLimitWhenStationary(t *testing.T) {
	bricks := make([]world.Brick, 700)
	for index := range bricks {
		bricks[index] = world.Brick{Origin: [3]uint32{uint32(index * 8), 0, 0}}
	}

	streamer := newBrickStreamerWithConfig(bricks, brickStreamerConfig{ResidentLimit: 4096, UploadBudget: 192})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	desired := streamer.computeDesired(platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}, [3]float32{})
	if got, want := len(desired), len(bricks); got != want {
		t.Fatalf("stationary desired count = %d, want %d", got, want)
	}
}

func TestComputeDesiredUsesFullResidentLimitWhenMoving(t *testing.T) {
	bricks := make([]world.Brick, 1500)
	for index := range bricks {
		bricks[index] = world.Brick{Origin: [3]uint32{uint32(index * 8), 0, 0}}
	}

	streamer := newBrickStreamerWithConfig(bricks, brickStreamerConfig{ResidentLimit: 4096, UploadBudget: 192})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	desired := streamer.computeDesired(platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}, [3]float32{halfBrickWorldUnits, 0, 0})
	if got, want := len(desired), len(bricks); got != want {
		t.Fatalf("moving desired count = %d, want %d", got, want)
	}
}

func TestPlanOpsAvoidsEagerEvictionsUnderCapacity(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{0, 0, 0},
		[3]uint32{16, 0, 0},
	), brickStreamerConfig{ResidentLimit: 4, UploadBudget: 2})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.desiredReady = true
	streamer.desired = []int{0}
	streamer.resident = map[int]residentBrick{
		0: {slot: 1, lru: 2},
		1: {slot: 2, lru: 1},
	}

	pool, err := newBrickPoolState(16)
	if err != nil {
		t.Fatalf("newBrickPoolState returned error: %v", err)
	}

	plan := streamer.planOps(pool)
	if len(plan.evictions) != 0 {
		t.Fatalf("expected no proactive evictions while under capacity, got %d", len(plan.evictions))
	}
}

func TestPlanOpsRepairsStaleDesiredIndices(t *testing.T) {
	streamer := newBrickStreamerWithConfig(testStreamBricks(
		[3]uint32{0, 0, 0},
		[3]uint32{16, 0, 0},
	), brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.camera = platform.Camera{Position: [3]float32{4, 4, 4}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}
	streamer.cameraEverSet = true
	streamer.desiredReady = true
	streamer.desired = []int{5}

	pool, err := newBrickPoolState(16)
	if err != nil {
		t.Fatalf("newBrickPoolState returned error: %v", err)
	}

	plan := streamer.planOps(pool)
	if len(plan.uploads) == 0 {
		t.Fatal("expected stale desired indices to be recomputed into a valid upload plan")
	}
	for _, upload := range plan.uploads {
		if upload.logicalIndex < 0 || upload.logicalIndex >= len(streamer.bricks) {
			t.Fatalf("expected repaired upload index within range, got %d for %d bricks", upload.logicalIndex, len(streamer.bricks))
		}
	}
}

func TestReplaceSceneBricksSkipsUploadForEqualVoxelContent(t *testing.T) {
	oldVoxels := &[world.BrickVoxelCount]uint8{}
	oldVoxels[0] = 1
	oldVoxels[1] = 2
	newVoxels := &[world.BrickVoxelCount]uint8{}
	copy(newVoxels[:], oldVoxels[:])

	streamer := newBrickStreamerWithConfig([]world.Brick{{
		NodeIndex: 7,
		Origin:    [3]uint32{16, 24, 0},
		Voxels:    oldVoxels,
	}}, brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.resident = map[int]residentBrick{0: {slot: 9, lru: 1}}
	plan := streamer.replaceSceneBricks(nil, [3]int32{}, []world.Brick{{
		NodeIndex: 11,
		Origin:    [3]uint32{16, 24, 0},
		Voxels:    newVoxels,
	}})

	if len(plan.uploads) != 0 {
		t.Fatalf("expected no upload for equal voxel content, got %d uploads", len(plan.uploads))
	}
	if len(plan.pointerPatches) != 1 {
		t.Fatalf("expected one pointer patch, got %d", len(plan.pointerPatches))
	}
	if got := streamer.resident[0].slot; got != 9 {
		t.Fatalf("expected resident slot to be preserved, got %d", got)
	}
}

func TestReplaceSceneBricksReusesResidentSlotAcrossSceneOriginShift(t *testing.T) {
	oldVoxels := &[world.BrickVoxelCount]uint8{}
	oldVoxels[0] = 1
	oldVoxels[1] = 2
	newVoxels := &[world.BrickVoxelCount]uint8{}
	copy(newVoxels[:], oldVoxels[:])

	streamer := newBrickStreamerWithConfig([]world.Brick{{
		NodeIndex: 7,
		Origin:    [3]uint32{16, 24, 0},
		Voxels:    oldVoxels,
	}}, brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.setSceneOrigin([3]int32{0, 0, 0})
	streamer.resident = map[int]residentBrick{0: {slot: 9, lru: 1}}
	plan := streamer.replaceSceneBricks(nil, [3]int32{16, 0, 0}, []world.Brick{{
		NodeIndex: 11,
		Origin:    [3]uint32{0, 24, 0},
		Voxels:    newVoxels,
	}})

	if len(plan.uploads) != 0 {
		t.Fatalf("expected no upload across scene origin shift for equal voxel content, got %d uploads", len(plan.uploads))
	}
	if len(plan.pointerPatches) != 1 {
		t.Fatalf("expected one pointer patch, got %d", len(plan.pointerPatches))
	}
	if got := streamer.resident[0].slot; got != 9 {
		t.Fatalf("expected resident slot to be preserved across scene shift, got %d", got)
	}
}

func TestUpdateSceneBricksUploadsOnlyChangedResidentBricks(t *testing.T) {
	oldVoxels := &[world.BrickVoxelCount]uint8{}
	oldVoxels[0] = 1
	newVoxels := &[world.BrickVoxelCount]uint8{}
	newVoxels[0] = 2

	streamer := newBrickStreamerWithConfig([]world.Brick{{
		NodeIndex: 7,
		Origin:    [3]uint32{16, 24, 0},
		Voxels:    oldVoxels,
	}}, brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	if streamer == nil {
		t.Fatal("expected streamer")
	}
	defer streamer.Close()

	streamer.resident = map[int]residentBrick{0: {slot: 9, lru: 1}}
	uploads := streamer.updateSceneBricks([]world.Brick{{
		NodeIndex: 7,
		Origin:    [3]uint32{16, 24, 0},
		Voxels:    newVoxels,
	}}, []int{0})

	if len(uploads) != 1 {
		t.Fatalf("expected one upload, got %d", len(uploads))
	}
	if uploads[0] != (sceneResidentUpload{logicalIndex: 0, slot: 9}) {
		t.Fatalf("unexpected upload %+v", uploads[0])
	}
	if streamer.sourceBricks[0].Voxels != newVoxels {
		t.Fatal("expected source bricks to update to new voxel pointer")
	}
	if streamer.bricks[0].voxels != newVoxels {
		t.Fatal("expected streaming brick voxels to update to new voxel pointer")
	}
}

func testStreamBricks(origins ...[3]uint32) []world.Brick {
	bricks := make([]world.Brick, len(origins))
	for index, origin := range origins {
		bricks[index] = world.Brick{Origin: origin}
	}
	return bricks
}
