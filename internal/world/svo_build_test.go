package world

import "testing"

func TestBuildTreePreservesDescendantsForSingleVoxelDenseTracked(t *testing.T) {
	svo := NewSVO()
	svo.BuildTree(func(x, y, z int) (uint32, bool) {
		return 0x123456, x == 3 && y == 3 && z == 3
	}, 4)

	if got, want := len(svo.nodes), 3; got != want {
		t.Fatalf("len(nodes) = %d, want %d", got, want)
	}
	if got, want := svo.size, uint(4); got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}

	root := svo.nodes[0]
	if root.childPointer != 1 {
		t.Fatalf("root childPointer = %d, want 1", root.childPointer)
	}
	if mask := root.childMask(); mask != 0x80 {
		t.Fatalf("root child mask = %#x, want %#x", mask, uint8(0x80))
	}

	branch := svo.nodes[1]
	if branch.childPointer != 2 {
		t.Fatalf("branch childPointer = %d, want 2", branch.childPointer)
	}
	if mask := branch.childMask(); mask != 0x80 {
		t.Fatalf("branch child mask = %#x, want %#x", mask, uint8(0x80))
	}

	leaf := svo.nodes[2]
	if !leaf.isSolidLeaf() {
		t.Fatalf("leaf = %+v, want solid material leaf", leaf)
	}
	if materialID := leaf.materialID(); materialID != 1 {
		t.Fatalf("leaf material ID = %d, want %d", materialID, uint8(1))
	}
	if color := svo.palette[1]; color != 0x123456 {
		t.Fatalf("palette[1] = %#x, want %#x", color, uint32(0x123456))
	}
}

func TestBuildTreeSparseFuncMatchesDenseSingleVoxel(t *testing.T) {
	dense := NewSVO()
	dense.BuildTree(func(x, y, z int) (uint32, bool) {
		return 0x123456, x == 3 && y == 3 && z == 3
	}, 4)

	sparse := NewSVO()
	sparse.BuildTreeSparseFunc(4, func(add func(x, y, z uint, color uint32)) {
		add(3, 3, 3, 0x123456)
	})

	denseWords := dense.StorageBufferWords()
	sparseWords := sparse.StorageBufferWords()
	if len(denseWords) != len(sparseWords) {
		t.Fatalf("len(StorageBufferWords) = %d, want %d", len(sparseWords), len(denseWords))
	}
	for index := range denseWords {
		if denseWords[index] != sparseWords[index] {
			t.Fatalf("StorageBufferWords()[%d] = %#x, want %#x", index, sparseWords[index], denseWords[index])
		}
	}
}

func TestBuildTreeSparseFuncTracksOccupiedBounds(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseFunc(16, func(add func(x, y, z uint, color uint32)) {
		add(2, 4, 6, 0x112233)
		add(9, 7, 3, 0x445566)
	})

	gotMin, gotMax, gotOK := svo.OccupiedBounds()
	if !gotOK {
		t.Fatal("OccupiedBounds ok = false, want true")
	}
	if gotMin != [3]uint32{2, 4, 3} {
		t.Fatalf("OccupiedBounds min = %v, want %v", gotMin, [3]uint32{2, 4, 3})
	}
	if gotMax != [3]uint32{10, 8, 7} {
		t.Fatalf("OccupiedBounds max = %v, want %v", gotMax, [3]uint32{10, 8, 7})
	}
}

func TestBuildTreeCreatesBrickLeafAtSizeEight(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseFunc(16, func(add func(x, y, z uint, color uint32)) {
		add(1, 1, 1, 0xAA5500)
		add(2, 3, 4, 0x00AA55)
	})

	if got, want := svo.NodeCount(), 2; got != want {
		t.Fatalf("NodeCount() = %d, want %d", got, want)
	}
	if got, want := svo.BrickCount(), 1; got != want {
		t.Fatalf("BrickCount() = %d, want %d", got, want)
	}

	root := svo.nodes[0]
	if mask := root.childMask(); mask != 0x01 {
		t.Fatalf("root child mask = %#x, want %#x", mask, uint8(0x01))
	}
	if root.childPointer != 1 {
		t.Fatalf("root childPointer = %d, want 1", root.childPointer)
	}

	brickLeaf := svo.nodes[1]
	if !brickLeaf.isBrickLeaf() {
		t.Fatalf("brick leaf = %+v, want brick leaf flag", brickLeaf)
	}
	if brickLeaf.childPointer != 0 {
		t.Fatalf("brick leaf childPointer = %d, want 0", brickLeaf.childPointer)
	}

	brick := svo.Bricks()[0]
	if brick.NodeIndex != 1 {
		t.Fatalf("brick NodeIndex = %d, want 1", brick.NodeIndex)
	}
	if brick.Origin != [3]uint32{0, 0, 0} {
		t.Fatalf("brick Origin = %v, want %v", brick.Origin, [3]uint32{0, 0, 0})
	}
	if got, want := brick.Voxels[brickVoxelIndex(1, 1, 1)], uint8(1); got != want {
		t.Fatalf("brick voxel (1,1,1) = %d, want %d", got, want)
	}
	if got, want := brick.Voxels[brickVoxelIndex(2, 3, 4)], uint8(2); got != want {
		t.Fatalf("brick voxel (2,3,4) = %d, want %d", got, want)
	}
	if got, want := svo.palette[1], uint32(0xAA5500); got != want {
		t.Fatalf("palette[1] = %#x, want %#x", got, want)
	}
	if got, want := svo.palette[2], uint32(0x00AA55); got != want {
		t.Fatalf("palette[2] = %#x, want %#x", got, want)
	}
}

func TestBuildTreeCollapsesUniformBrickToSolidLeaf(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseFunc(8, func(add func(x, y, z uint, color uint32)) {
		for z := uint(0); z < BrickSize; z++ {
			for y := uint(0); y < BrickSize; y++ {
				for x := uint(0); x < BrickSize; x++ {
					add(x, y, z, 0x884422)
				}
			}
		}
	})

	if got := svo.BrickCount(); got != 0 {
		t.Fatalf("BrickCount() = %d, want 0", got)
	}
	if got := svo.NodeCount(); got != 1 {
		t.Fatalf("NodeCount() = %d, want 1", got)
	}
	if !svo.nodes[0].isSolidLeaf() {
		t.Fatalf("root = %+v, want solid leaf", svo.nodes[0])
	}
	if materialID := svo.nodes[0].materialID(); materialID != 1 {
		t.Fatalf("root material ID = %d, want %d", materialID, uint8(1))
	}
}

func TestLoadStorageBufferWordsCopiesState(t *testing.T) {
	source := NewSVO()
	source.BuildTreeSparseFunc(8, func(add func(x, y, z uint, color uint32)) {
		add(1, 2, 3, 0x654321)
	})

	minBounds, maxBounds, ok := source.OccupiedBounds()
	clone := NewSVO()
	if err := clone.LoadStorageBufferWords(source.StorageBufferWords(), minBounds, maxBounds, ok); err != nil {
		t.Fatalf("LoadStorageBufferWords() error = %v", err)
	}

	sourceWords := source.StorageBufferWords()
	cloneWords := clone.StorageBufferWords()
	if len(sourceWords) != len(cloneWords) {
		t.Fatalf("len(StorageBufferWords) = %d, want %d", len(cloneWords), len(sourceWords))
	}
	for index := range sourceWords {
		if sourceWords[index] != cloneWords[index] {
			t.Fatalf("StorageBufferWords()[%d] = %#x, want %#x", index, cloneWords[index], sourceWords[index])
		}
	}

	gotMin, gotMax, gotOK := clone.OccupiedBounds()
	if gotOK != ok {
		t.Fatalf("OccupiedBounds ok = %t, want %t", gotOK, ok)
	}
	if gotMin != minBounds {
		t.Fatalf("OccupiedBounds min = %v, want %v", gotMin, minBounds)
	}
	if gotMax != maxBounds {
		t.Fatalf("OccupiedBounds max = %v, want %v", gotMax, maxBounds)
	}
	if got := clone.BrickCount(); got != 0 {
		t.Fatalf("BrickCount() = %d, want 0", got)
	}
}

func TestSnapshotCopiesBrickState(t *testing.T) {
	source := NewSVO()
	source.BuildTreeSparseFunc(16, func(add func(x, y, z uint, color uint32)) {
		add(1, 1, 1, 0x123456)
		add(7, 7, 7, 0x654321)
	})

	clone := NewSVO()
	if err := clone.LoadSnapshot(source.Snapshot()); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	sourceBricks := source.Bricks()
	cloneBricks := clone.Bricks()
	if len(sourceBricks) != len(cloneBricks) {
		t.Fatalf("len(Bricks()) = %d, want %d", len(cloneBricks), len(sourceBricks))
	}
	if len(sourceBricks) != 1 {
		t.Fatalf("len(Bricks()) = %d, want 1", len(sourceBricks))
	}
	if sourceBricks[0] != cloneBricks[0] {
		t.Fatalf("Bricks()[0] = %+v, want %+v", cloneBricks[0], sourceBricks[0])
	}

	sourceWords := source.StorageBufferWords()
	cloneWords := clone.StorageBufferWords()
	for index := range sourceWords {
		if sourceWords[index] != cloneWords[index] {
			t.Fatalf("StorageBufferWords()[%d] = %#x, want %#x", index, cloneWords[index], sourceWords[index])
		}
	}
}
