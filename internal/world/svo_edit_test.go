package world

import (
	"math"
	"testing"
)

func TestRaycastHitsFrontFaceVoxelOfUniformCube(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 32, 0xE2554F)
	})

	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 48, 48},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false, want true")
	}
	if got, want := hit.Voxel, [3]uint32{32, 48, 48}; got != want {
		t.Fatalf("Raycast() voxel = %v, want %v", got, want)
	}
	if got, want := hit.Normal, [3]int32{-1, 0, 0}; got != want {
		t.Fatalf("Raycast() normal = %v, want %v", got, want)
	}
	if got, want := hit.MaterialID, uint8(1); got != want {
		t.Fatalf("Raycast() material = %d, want %d", got, want)
	}
}

func TestClearVoxelCarvesUniformLeafAndExposesNextVoxel(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 32, 0xE2554F)
	})

	if changed := svo.ClearVoxel(32, 48, 48); !changed {
		t.Fatal("ClearVoxel() changed = false, want true")
	}

	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 48, 48},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false, want true after carve")
	}
	if got, want := hit.Voxel, [3]uint32{33, 48, 48}; got != want {
		t.Fatalf("Raycast() voxel after carve = %v, want %v", got, want)
	}
}

func TestSetVoxelColorAddsEditableVoxelOutsideUniformCube(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 32, 0xE2554F)
	})

	if changed := svo.SetVoxelColor(31, 48, 48, 0x5A8847); !changed {
		t.Fatal("SetVoxelColor() changed = false, want true")
	}

	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 48, 48},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false, want true after add")
	}
	if got, want := hit.Voxel, [3]uint32{31, 48, 48}; got != want {
		t.Fatalf("Raycast() voxel after add = %v, want %v", got, want)
	}
	if got, want := svo.palette[hit.MaterialID], uint32(0x5A8847); got != want {
		t.Fatalf("palette[%d] = %#x, want %#x", hit.MaterialID, got, want)
	}

	minBounds, maxBounds, ok := svo.OccupiedBounds()
	if !ok {
		t.Fatal("OccupiedBounds() ok = false, want true")
	}
	if got, want := minBounds, [3]uint32{31, 32, 32}; got != want {
		t.Fatalf("OccupiedBounds() min = %v, want %v", got, want)
	}
	if got, want := maxBounds, [3]uint32{64, 64, 64}; got != want {
		t.Fatalf("OccupiedBounds() max = %v, want %v", got, want)
	}
}

func TestSetVoxelColorOnGeneratorCubeMatchesSparseGroundTruth(t *testing.T) {
	const (
		cubeColor = 0xE2554F
		editColor = 0x5A8847
	)

	edited := NewSVO()
	edited.BuildTreeSparseFunc(128, func(add func(x, y, z uint, color uint32)) {
		for z := uint(32); z < 96; z++ {
			for y := uint(32); y < 96; y++ {
				for x := uint(32); x < 96; x++ {
					add(x, y, z, cubeColor)
				}
			}
		}
	})
	if changed := edited.SetVoxelColor(31, 64, 64, editColor); !changed {
		t.Fatal("SetVoxelColor() changed = false, want true")
	}

	groundTruth := NewSVO()
	groundTruth.BuildTreeSparseFunc(128, func(add func(x, y, z uint, color uint32)) {
		for z := uint(32); z < 96; z++ {
			for y := uint(32); y < 96; y++ {
				for x := uint(32); x < 96; x++ {
					add(x, y, z, cubeColor)
				}
			}
		}
		add(31, 64, 64, editColor)
	})

	assertSVOSnapshotsEqual(t, edited.Snapshot(), groundTruth.Snapshot())
	if hit, ok := edited.Raycast(Ray{
		Origin:    [3]float32{0, 64, 64},
		Direction: [3]float32{1, 0, 0},
	}, 256); !ok {
		t.Fatal("Raycast() ok = false after add")
	} else if got, want := hit.Voxel, [3]uint32{31, 64, 64}; got != want {
		t.Fatalf("Raycast() voxel after add = %v, want %v", got, want)
	}
}

func TestApplyVoxelEditsMatchesSequentialStrokeGroundTruth(t *testing.T) {
	const (
		cubeColor = 0xE2554F
		editColor = 0x5A8847
	)

	batchEdited := NewSVO()
	batchEdited.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 64, cubeColor)
	})
	if changed := batchEdited.ApplyVoxelEdits([]VoxelEdit{
		{Position: [3]uint32{31, 64, 64}, Color: editColor},
		{Position: [3]uint32{31, 64, 73}, Color: editColor},
	}); changed != 2 {
		t.Fatalf("ApplyVoxelEdits() changed = %d, want 2", changed)
	}

	sequential := NewSVO()
	sequential.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 64, cubeColor)
	})
	if changed := sequential.SetVoxelColor(31, 64, 64, editColor); !changed {
		t.Fatal("SetVoxelColor() first changed = false, want true")
	}
	if changed := sequential.SetVoxelColor(31, 64, 73, editColor); !changed {
		t.Fatal("SetVoxelColor() second changed = false, want true")
	}

	assertSVOSnapshotsEqual(t, batchEdited.Snapshot(), sequential.Snapshot())
	if got, want := batchEdited.BrickCount(), 2; got != want {
		t.Fatalf("BrickCount() = %d, want %d", got, want)
	}
}

func TestClearVoxelInsideExistingBrickUsesIncrementalModeAndKeepsBoundsSlack(t *testing.T) {
	const color = 0xE2554F

	svo := NewSVO()
	svo.BuildTreeSparseFunc(64, func(add func(x, y, z uint, color uint32)) {
		add(10, 10, 10, color)
		add(11, 10, 10, color)
	})

	if changed := svo.ClearVoxel(11, 10, 10); !changed {
		t.Fatal("ClearVoxel() changed = false, want true")
	}
	if got, want := svo.lastEdit.mode, editApplyModeIncremental; got != want {
		t.Fatalf("lastEdit.mode = %v, want %v", got, want)
	}

	minBounds, maxBounds, ok := svo.OccupiedBounds()
	if !ok {
		t.Fatal("OccupiedBounds() ok = false, want true")
	}
	if got, want := minBounds, [3]uint32{10, 10, 10}; got != want {
		t.Fatalf("OccupiedBounds() min = %v, want %v", got, want)
	}
	if got, want := maxBounds, [3]uint32{12, 11, 11}; got != want {
		t.Fatalf("OccupiedBounds() max = %v, want %v", got, want)
	}

	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 10, 10},
		Direction: [3]float32{1, 0, 0},
	}, 128)
	if !ok {
		t.Fatal("Raycast() ok = false, want true after incremental clear")
	}
	if got, want := hit.Voxel, [3]uint32{10, 10, 10}; got != want {
		t.Fatalf("Raycast() voxel after incremental clear = %v, want %v", got, want)
	}
}

func TestSetVoxelColorOutsideExistingBrickFallsBackToFullRebuild(t *testing.T) {
	const color = 0xE2554F

	svo := NewSVO()
	svo.BuildTreeSparseFunc(64, func(add func(x, y, z uint, color uint32)) {
		add(10, 10, 10, color)
	})

	if changed := svo.SetVoxelColor(40, 10, 10, 0x5A8847); !changed {
		t.Fatal("SetVoxelColor() changed = false, want true")
	}
	if got, want := svo.lastEdit.mode, editApplyModeFullRebuild; got != want {
		t.Fatalf("lastEdit.mode = %v, want %v", got, want)
	}

	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 10, 10},
		Direction: [3]float32{1, 0, 0},
	}, 128)
	if !ok {
		t.Fatal("Raycast() ok = false, want true after full rebuild fallback")
	}
	if got, want := hit.Voxel, [3]uint32{10, 10, 10}; got != want {
		t.Fatalf("Raycast() nearest voxel = %v, want %v", got, want)
	}

	secondaryHit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{20, 10, 10},
		Direction: [3]float32{1, 0, 0},
	}, 128)
	if !ok {
		t.Fatal("Raycast() ok = false, want true for added voxel after full rebuild fallback")
	}
	if got, want := secondaryHit.Voxel, [3]uint32{40, 10, 10}; got != want {
		t.Fatalf("Raycast() added voxel = %v, want %v", got, want)
	}
	if got, want := svo.BrickCount(), 2; got != want {
		t.Fatalf("BrickCount() = %d, want %d", got, want)
	}
}

func TestClearVoxelOnUniformCubeMatchesDenseSingleVoxelGroundTruth(t *testing.T) {
	const cubeColor = 0xE2554F

	edited := NewSVO()
	edited.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 32, cubeColor)
	})
	if changed := edited.ClearVoxel(32, 48, 48); !changed {
		t.Fatal("ClearVoxel() changed = false, want true")
	}

	groundTruth := NewSVO()
	groundTruth.BuildTree(func(x, y, z int) (uint32, bool) {
		inside := x >= 32 && x < 64 && y >= 32 && y < 64 && z >= 32 && z < 64
		if !inside {
			return 0, false
		}
		if x == 32 && y == 48 && z == 48 {
			return 0, false
		}
		return cubeColor, true
	}, 128)

	assertSVOSnapshotsEqual(t, edited.Snapshot(), groundTruth.Snapshot())

	adjacentHit, ok := edited.Raycast(Ray{
		Origin:    [3]float32{0, 49, 48},
		Direction: [3]float32{1, 0, 0},
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false for adjacent face voxel")
	}
	if got, want := adjacentHit.Voxel, [3]uint32{32, 49, 48}; got != want {
		t.Fatalf("adjacent Raycast() voxel = %v, want %v", got, want)
	}
}

func TestRaycastResolvesCornerEntryFaceByDominantAxis(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(128, func(_ func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(32, 32, 32, 32, 0xE2554F)
	})

	direction := normalizeTestRay([3]float32{1, 0.25, 1})
	hit, ok := svo.Raycast(Ray{
		Origin:    [3]float32{0, 40, 0},
		Direction: direction,
	}, 256)
	if !ok {
		t.Fatal("Raycast() ok = false, want true")
	}
	if got, want := hit.Normal, [3]int32{0, 0, -1}; got != want {
		t.Fatalf("Raycast() normal = %v, want %v", got, want)
	}
}

func assertSVOSnapshotsEqual(t *testing.T, got, want Snapshot) {
	t.Helper()
	if len(got.Words) != len(want.Words) {
		t.Fatalf("len(snapshot.Words) = %d, want %d", len(got.Words), len(want.Words))
	}
	for index := range want.Words {
		if got.Words[index] != want.Words[index] {
			t.Fatalf("snapshot.Words[%d] = %#x, want %#x", index, got.Words[index], want.Words[index])
		}
	}
	if len(got.Bricks) != len(want.Bricks) {
		t.Fatalf("len(snapshot.Bricks) = %d, want %d", len(got.Bricks), len(want.Bricks))
	}
	for index := range want.Bricks {
		if got.Bricks[index].NodeIndex != want.Bricks[index].NodeIndex {
			t.Fatalf("snapshot.Bricks[%d].NodeIndex = %d, want %d", index, got.Bricks[index].NodeIndex, want.Bricks[index].NodeIndex)
		}
		if got.Bricks[index].Origin != want.Bricks[index].Origin {
			t.Fatalf("snapshot.Bricks[%d].Origin = %v, want %v", index, got.Bricks[index].Origin, want.Bricks[index].Origin)
		}
		if *got.Bricks[index].Voxels != *want.Bricks[index].Voxels {
			t.Fatalf("snapshot.Bricks[%d].Voxels differ", index)
		}
	}
	if got.OccupiedMin != want.OccupiedMin {
		t.Fatalf("snapshot.OccupiedMin = %v, want %v", got.OccupiedMin, want.OccupiedMin)
	}
	if got.OccupiedMax != want.OccupiedMax {
		t.Fatalf("snapshot.OccupiedMax = %v, want %v", got.OccupiedMax, want.OccupiedMax)
	}
	if got.HasOccupiedBounds != want.HasOccupiedBounds {
		t.Fatalf("snapshot.HasOccupiedBounds = %v, want %v", got.HasOccupiedBounds, want.HasOccupiedBounds)
	}
}

func normalizeTestRay(direction [3]float32) [3]float32 {
	length := float32(math.Sqrt(float64(direction[0]*direction[0] + direction[1]*direction[1] + direction[2]*direction[2])))
	if length == 0 {
		return [3]float32{}
	}
	return [3]float32{direction[0] / length, direction[1] / length, direction[2] / length}
}