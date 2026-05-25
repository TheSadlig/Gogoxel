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

func TestBrickLeafPayloadStoresDominantFallbackMaterial(t *testing.T) {
	svo := mixedBrickFallbackSVO(t)
	brick := svo.BricksRef()[0]
	node := svo.nodes[brick.NodeIndex]

	if !node.isBrickLeaf() {
		t.Fatalf("node %d is not a brick leaf", brick.NodeIndex)
	}
	if got, want := node.materialID(), uint8(2); got != want {
		t.Fatalf("brick fallback material = %d, want %d", got, want)
	}
	if got, want := node.payload, BrickLeafFlag|uint32(2)<<8; got != want {
		t.Fatalf("brick payload = %#x, want %#x", got, want)
	}
}

func TestLoadSnapshotRepairsMissingBrickFallbackMaterial(t *testing.T) {
	source := mixedBrickFallbackSVO(t)
	snapshot := source.Snapshot()
	brick := snapshot.Bricks[0]
	nodeWordIndex := storageWordCount + int(brick.NodeIndex)*2
	snapshot.Words[nodeWordIndex] = BrickLeafFlag

	loaded := NewSVO()
	if err := loaded.LoadSnapshot(snapshot); err != nil {
		t.Fatalf("LoadSnapshot returned error: %v", err)
	}
	loadedBrick := loaded.BricksRef()[0]
	loadedNode := loaded.nodes[loadedBrick.NodeIndex]
	if got, want := loadedNode.materialID(), uint8(2); got != want {
		t.Fatalf("repaired fallback material = %d, want %d", got, want)
	}
	if got, want := loaded.StorageBufferWordsRef()[nodeWordIndex], BrickLeafFlag|uint32(2)<<8; got != want {
		t.Fatalf("repaired storage payload = %#x, want %#x", got, want)
	}
}

func mixedBrickFallbackSVO(t *testing.T) *SVO {
	t.Helper()
	palette := []uint32{0xFF00FF00, 0xFF888888}
	voxels := &[BrickVoxelCount]uint8{}
	for index := 0; index < 300; index++ {
		voxels[index] = 2
	}
	for index := 300; index < 360; index++ {
		voxels[index] = 1
	}

	svo := NewSVO()
	svo.BuildTreeSparseVolumesWithMaterialBricks(8, palette, func(_ func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		addBrick(0, 0, 0, voxels)
	})
	if got := svo.BrickCount(); got != 1 {
		t.Fatalf("brick count = %d, want 1", got)
	}
	return svo
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
