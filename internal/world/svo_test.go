package world

import "testing"

func TestBuildTreePreservesDescendantsForSingleVoxel(t *testing.T) {
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
