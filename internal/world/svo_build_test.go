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
	if mask := root.childMaskAndColor & 0xFF; mask != 0x80 {
		t.Fatalf("root child mask = %#x, want %#x", mask, uint32(0x80))
	}

	branch := svo.nodes[1]
	if branch.childPointer != 2 {
		t.Fatalf("branch childPointer = %d, want 2", branch.childPointer)
	}
	if mask := branch.childMaskAndColor & 0xFF; mask != 0x80 {
		t.Fatalf("branch child mask = %#x, want %#x", mask, uint32(0x80))
	}

	leaf := svo.nodes[2]
	if leaf.childPointer != 0 {
		t.Fatalf("leaf childPointer = %d, want 0", leaf.childPointer)
	}
	if mask := leaf.childMaskAndColor & 0xFF; mask != 0x01 {
		t.Fatalf("leaf child mask = %#x, want %#x", mask, uint32(0x01))
	}
	if color := leaf.childMaskAndColor >> 8; color != 0x123456 {
		t.Fatalf("leaf color = %#x, want %#x", color, uint32(0x123456))
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
}
