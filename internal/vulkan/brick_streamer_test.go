package vulkan

import (
	"testing"

	"Gogoxel/internal/world"
)

func TestReplaceSceneBricksPreservesResidentSlotsByOrigin(t *testing.T) {
	pool := newTestBrickPool()
	firstSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() first error = %v", err)
	}
	secondSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() second error = %v", err)
	}
	// Keep a reference to the initial bricks so we can reuse the same Voxels
	// pointer for the "unchanged" brick in the replace call.  In production the
	// SVO uses copy-on-write, so unedited bricks retain the identical pointer.
	initialBricks := []world.Brick{
		testBrick(10, [3]uint32{0, 0, 0}, 1),
		testBrick(11, [3]uint32{8, 0, 0}, 2),
	}
	streamer := newBrickStreamerWithConfig(initialBricks, brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2})
	defer streamer.Close()

	streamer.mu.Lock()
	streamer.resident[0] = residentBrick{slot: firstSlot, lru: 1}
	streamer.resident[1] = residentBrick{slot: secondSlot, lru: 2}
	streamer.cameraPosition = [3]float32{4, 0, 0}
	streamer.cameraEverSet = true
	streamer.desired = []int{0, 1}
	streamer.desiredReady = true
	streamer.mu.Unlock()

	plan := streamer.replaceSceneBricks(pool, []world.Brick{
		// Unchanged: same Voxels pointer → pointer comparison skips upload.
		{NodeIndex: 20, Origin: [3]uint32{0, 0, 0}, Voxels: initialBricks[0].Voxels},
		// Changed: fresh pointer from testBrick → upload required.
		testBrick(21, [3]uint32{8, 0, 0}, 3),
		testBrick(22, [3]uint32{16, 0, 0}, 4),
	})

	if got, want := len(plan.pointerPatches), 2; got != want {
		t.Fatalf("len(pointerPatches) = %d, want %d", got, want)
	}
	if got, want := len(plan.uploads), 1; got != want {
		t.Fatalf("len(uploads) = %d, want %d", got, want)
	}
	if got, want := plan.uploads[0].slot, secondSlot; got != want {
		t.Fatalf("uploads[0].slot = %d, want %d", got, want)
	}

	streamer.mu.Lock()
	defer streamer.mu.Unlock()
	if got, want := streamer.resident[0].slot, firstSlot; got != want {
		t.Fatalf("resident slot for origin 0 = %d, want %d", got, want)
	}
	if got, want := streamer.resident[1].slot, secondSlot; got != want {
		t.Fatalf("resident slot for origin 8 = %d, want %d", got, want)
	}
}

func TestReplaceSceneBricksFreesRemovedResidentSlots(t *testing.T) {
	pool := newTestBrickPool()
	firstSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() first error = %v", err)
	}
	secondSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() second error = %v", err)
	}
	streamer := newBrickStreamerWithConfig([]world.Brick{
		testBrick(10, [3]uint32{0, 0, 0}, 1),
		testBrick(11, [3]uint32{8, 0, 0}, 2),
	}, brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2})
	defer streamer.Close()

	streamer.mu.Lock()
	streamer.resident[0] = residentBrick{slot: firstSlot, lru: 1}
	streamer.resident[1] = residentBrick{slot: secondSlot, lru: 2}
	streamer.mu.Unlock()

	_ = streamer.replaceSceneBricks(pool, []world.Brick{
		testBrick(20, [3]uint32{0, 0, 0}, 1),
	})

	reused, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if got, want := reused, secondSlot; got != want {
		t.Fatalf("reused slot = %d, want %d", got, want)
	}
}

func TestReplaceSceneBricksHandlesReusedBrickSliceBacking(t *testing.T) {
	pool := newTestBrickPool()
	firstSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() first error = %v", err)
	}
	secondSlot, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() second error = %v", err)
	}

	initialBricks := make([]world.Brick, 2, 2)
	initialBricks[0] = testBrick(10, [3]uint32{0, 0, 0}, 1)
	initialBricks[1] = testBrick(11, [3]uint32{8, 0, 0}, 2)
	streamer := newBrickStreamerWithConfig(initialBricks, brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2})
	defer streamer.Close()

	streamer.mu.Lock()
	streamer.resident[0] = residentBrick{slot: firstSlot, lru: 1}
	streamer.resident[1] = residentBrick{slot: secondSlot, lru: 2}
	streamer.mu.Unlock()

	// Simulate SVO fallback rebuild reusing the same []Brick backing array.
	reusedBacking := initialBricks[:1]
	reusedBacking[0] = world.Brick{
		NodeIndex: 20,
		Origin:    [3]uint32{8, 0, 0},
		Voxels:    initialBricks[1].Voxels,
	}

	_ = streamer.replaceSceneBricks(pool, reusedBacking)

	reused, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if got, want := reused, firstSlot; got != want {
		t.Fatalf("reused slot after aliased replace = %d, want %d", got, want)
	}
}

func TestPrimeCameraPositionMakesDesiredReadyImmediately(t *testing.T) {
	streamer := newBrickStreamerWithConfig([]world.Brick{
		testBrick(10, [3]uint32{0, 0, 0}, 1),
		testBrick(11, [3]uint32{64, 0, 0}, 2),
	}, brickStreamerConfig{ResidentLimit: 1, UploadBudget: 1})
	defer streamer.Close()

	streamer.primeCameraPosition([3]float32{4, 0, 0})

	streamer.mu.Lock()
	defer streamer.mu.Unlock()
	if !streamer.desiredReady {
		t.Fatal("desiredReady = false, want true")
	}
	if got, want := len(streamer.desired), 1; got != want {
		t.Fatalf("len(desired) = %d, want %d", got, want)
	}
	if got, want := streamer.desired[0], 0; got != want {
		t.Fatalf("desired[0] = %d, want %d", got, want)
	}
	if got, want := streamer.lastPlannedPos, ([3]float32{4, 0, 0}); got != want {
		t.Fatalf("lastPlannedPos = %v, want %v", got, want)
	}
}

func TestReplaceSceneBricksGrowsDefaultResidentWindow(t *testing.T) {
	pool := newTestBrickPool()
	streamer := newBrickStreamer([]world.Brick{
		testBrick(10, [3]uint32{0, 0, 0}, 1),
	})
	defer streamer.Close()

	streamer.primeCameraPosition([3]float32{4, 0, 0})
	initialPlan := streamer.planOps(pool)
	if got, want := len(initialPlan.uploads), 1; got != want {
		t.Fatalf("len(initial uploads) = %d, want %d", got, want)
	}
	streamer.commitPlan(pool, initialPlan)

	_ = streamer.replaceSceneBricks(pool, []world.Brick{
		testBrick(20, [3]uint32{0, 0, 0}, 1),
		testBrick(21, [3]uint32{8, 0, 0}, 2),
		testBrick(22, [3]uint32{16, 0, 0}, 3),
	})

	stats := streamer.Stats()
	if got, want := stats.DesiredCount, 3; got != want {
		t.Fatalf("DesiredCount after scene growth = %d, want %d", got, want)
	}
	if got, want := stats.ResidentLimit, 3; got != want {
		t.Fatalf("ResidentLimit after scene growth = %d, want %d", got, want)
	}

	nextPlan := streamer.planOps(pool)
	if got, want := len(nextPlan.uploads), 2; got != want {
		t.Fatalf("len(next uploads) = %d, want %d", got, want)
	}
}

func testBrick(nodeIndex uint32, origin [3]uint32, material uint8) world.Brick {
	voxels := &[world.BrickVoxelCount]uint8{}
	for index := range voxels {
		voxels[index] = material
	}
	return world.Brick{NodeIndex: nodeIndex, Origin: origin, Voxels: voxels}
}

// TestSetCameraPositionPrimesStreamerOnFirstCall verifies that the very first
// SetCameraPosition on a fresh ChunkResources primes the streamer synchronously,
// so RecordStreaming in the same frame (e.g. immediately after InitChunk) finds
// desiredReady=true without waiting for the background planner goroutine.
func TestSetCameraPositionPrimesStreamerOnFirstCall(t *testing.T) {
	bricks := []world.Brick{
		testBrick(10, [3]uint32{0, 0, 0}, 1),
		testBrick(11, [3]uint32{8, 0, 0}, 2),
	}
	chunk := &ChunkResources{
		streamer: newBrickStreamerWithConfig(bricks, brickStreamerConfig{ResidentLimit: 2, UploadBudget: 2}),
	}
	defer chunk.streamer.Close()

	// Precondition: streamer must not be ready before the first camera set.
	chunk.streamer.mu.Lock()
	if chunk.streamer.desiredReady {
		chunk.streamer.mu.Unlock()
		t.Fatal("precondition failed: desiredReady should be false on a fresh streamer")
	}
	chunk.streamer.mu.Unlock()

	// First SetCameraPosition must prime synchronously.
	chunk.SetCameraPosition([3]float32{4, 0, 0})

	chunk.streamer.mu.Lock()
	defer chunk.streamer.mu.Unlock()
	if !chunk.streamer.desiredReady {
		t.Fatal("desiredReady = false after first SetCameraPosition, want true (synchronous prime)")
	}
	if len(chunk.streamer.desired) == 0 {
		t.Fatal("desired is empty after first SetCameraPosition, want ≥1 brick")
	}
}