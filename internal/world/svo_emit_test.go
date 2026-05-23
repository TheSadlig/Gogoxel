package world

import "testing"

func TestEmitTranslatedMaterialVolumesRoundTripsScene(t *testing.T) {
	palette := []uint32{0xFF00FF00, 0xFF888888}
	source := NewSVO()
	source.BuildTreeSparseVolumesWithMaterialBricks(32, palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		addCube(0, 0, 0, 8, palette[0])
		addCube(8, 0, 0, 8, palette[1])

		voxels := &[BrickVoxelCount]uint8{}
		voxels[0] = 1
		voxels[1] = 2
		voxels[BrickSize+1] = 1
		addBrick(0, 8, 0, voxels)
	})

	target := NewSVO()
	target.BuildTreeSparseVolumesWithMaterialBricks(32, palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		source.EmitTranslatedMaterialVolumes(0, 0, 0, addCube, addBrick)
	})

	assertSVOEquivalent(t, target, source)
}

func TestEmitTranslatedMaterialVolumesAppliesOffset(t *testing.T) {
	palette := []uint32{0xFF00FF00, 0xFF888888}
	source := NewSVO()
	source.BuildTreeSparseVolumesWithMaterialBricks(16, palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		addCube(0, 0, 0, 8, palette[0])
		voxels := &[BrickVoxelCount]uint8{}
		voxels[0] = 2
		addBrick(8, 0, 0, voxels)
	})

	target := NewSVO()
	target.BuildTreeSparseVolumesWithMaterialBricks(32, palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		source.EmitTranslatedMaterialVolumes(8, 8, 0, addCube, addBrick)
	})

	minBounds, maxBounds, ok := target.OccupiedBounds()
	if !ok {
		t.Fatal("expected occupied bounds after translated emit")
	}
	if got, want := minBounds, ([3]uint32{8, 8, 0}); got != want {
		t.Fatalf("occupied min = %v, want %v", got, want)
	}
	if got, want := maxBounds, ([3]uint32{24, 16, 8}); got != want {
		t.Fatalf("occupied max = %v, want %v", got, want)
	}
}

func assertSVOEquivalent(t *testing.T, got, want *SVO) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("expected non-nil SVOs: got=%v want=%v", got, want)
	}
	if got.NodeCount() != want.NodeCount() {
		t.Fatalf("node count = %d, want %d", got.NodeCount(), want.NodeCount())
	}
	if got.BrickCount() != want.BrickCount() {
		t.Fatalf("brick count = %d, want %d", got.BrickCount(), want.BrickCount())
	}
	gotMin, gotMax, gotOK := got.OccupiedBounds()
	wantMin, wantMax, wantOK := want.OccupiedBounds()
	if gotOK != wantOK || gotMin != wantMin || gotMax != wantMax {
		t.Fatalf("occupied bounds = (%v, %v, %t), want (%v, %v, %t)", gotMin, gotMax, gotOK, wantMin, wantMax, wantOK)
	}
	gotWords := got.StorageBufferWordsRef()
	wantWords := want.StorageBufferWordsRef()
	if len(gotWords) != len(wantWords) {
		t.Fatalf("storage word count = %d, want %d", len(gotWords), len(wantWords))
	}
	for index := range gotWords {
		if gotWords[index] != wantWords[index] {
			t.Fatalf("storage word %d = %d, want %d", index, gotWords[index], wantWords[index])
		}
	}
}