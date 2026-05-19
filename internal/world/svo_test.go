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
	if got, want := leaf.materialID(), uint8(1); got != want {
		t.Fatalf("leaf material ID = %d, want %d", got, want)
	}
	if got, want := svo.palette[1], uint32(0x123456); got != want {
		t.Fatalf("palette[1] = %#x, want %#x", got, want)
	}
}
