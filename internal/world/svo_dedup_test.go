package world

import "testing"

// TestDedupReducesUniformBranches verifies SVDAG dedup collapses identical
// subtrees on a manually constructed SVO that contains known duplicates.
func TestDedupReducesUniformBranches(t *testing.T) {
	// Hand-build a 2-level SVO whose 8 children are identical "branch with
	// one solid-stone child" subtrees. Naive layout = 1 root + 8 branches +
	// 8 leaves = 17 nodes; dedup should fold to 1 root + 1 shared branch
	// child-block (8 entries) + 1 shared solid leaf block = 10 nodes.
	s := NewSVO()
	s.size = 8
	// Root: branch with all 8 children present.
	root := SvoNode{payload: uint32(0xFF), childPointer: 1}
	s.nodes = append(s.nodes[:0], root)
	// 8 branch children, each pointing at its own private leaf.
	branchBase := uint32(len(s.nodes))
	for i := 0; i < 8; i++ {
		// each branch has a single child (mask=1)
		s.nodes = append(s.nodes, SvoNode{payload: 1, childPointer: 0}) // childPointer fixed up next
	}
	// 8 leaf solid-stone nodes, one per branch.
	leafBase := uint32(len(s.nodes))
	for i := 0; i < 8; i++ {
		var leaf SvoNode
		leaf.setSolidLeaf(1)
		s.nodes = append(s.nodes, leaf)
	}
	for i := 0; i < 8; i++ {
		s.nodes[branchBase+uint32(i)].childPointer = leafBase + uint32(i)
	}

	before := s.NodeCount()
	after := s.DedupNodes()

	if after >= before {
		t.Fatalf("expected dedup to reduce node count, before=%d after=%d", before, after)
	}
	// Idempotent: running again must not change anything.
	again := s.DedupNodes()
	if again != after {
		t.Fatalf("dedup not idempotent: first=%d second=%d", after, again)
	}
}

// TestDedupPreservesBrickIndices verifies that bricks remain reachable and
// that brick.NodeIndex points at the same payload kind after dedup.
func TestDedupPreservesBrickIndices(t *testing.T) {
	palette := []uint32{0xFFAABBCC, 0xFF112233}
	s := NewSVO()
	s.BuildTreeSparseVolumesWithMaterialBricks(32, palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[BrickVoxelCount]uint8)) {
		// Two distinct bricks — must remain distinct after dedup.
		v1 := &[BrickVoxelCount]uint8{}
		v1[0] = 1
		v1[7] = 1
		addBrick(0, 0, 0, v1)

		v2 := &[BrickVoxelCount]uint8{}
		v2[0] = 2
		v2[BrickSize*BrickSize] = 2
		addBrick(8, 0, 0, v2)

		// And a uniform region to encourage non-brick dedup elsewhere.
		for x := uint(16); x < 32; x += 8 {
			for y := uint(0); y < 32; y += 8 {
				for z := uint(0); z < 32; z += 8 {
					addCube(x, y, z, 8, palette[0])
				}
			}
		}
	})

	if got := s.BrickCount(); got != 2 {
		t.Fatalf("setup: want 2 bricks, got %d", got)
	}

	s.DedupNodes()
	s.rebuildBrickNodeLookup()
	s.rebuildStorageWords()

	if got := s.BrickCount(); got != 2 {
		t.Fatalf("brick count changed after dedup: want 2, got %d", got)
	}
	// Each brick should point at a distinct brick-leaf node.
	seen := map[uint32]bool{}
	for _, brick := range s.bricks {
		if int(brick.NodeIndex) >= s.NodeCount() {
			t.Fatalf("brick NodeIndex %d out of range (nodeCount=%d)", brick.NodeIndex, s.NodeCount())
		}
		node := s.nodes[brick.NodeIndex]
		if !node.isBrickLeaf() {
			t.Fatalf("brick NodeIndex %d does not point at a brick leaf (payload=%#x)", brick.NodeIndex, node.payload)
		}
		if seen[brick.NodeIndex] {
			t.Fatalf("two bricks share NodeIndex %d", brick.NodeIndex)
		}
		seen[brick.NodeIndex] = true
	}
}
