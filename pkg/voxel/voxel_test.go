package voxel

import "testing"

func TestCoordAdd(t *testing.T) {
	a := Coord{1, 2, 3}
	b := Coord{4, 5, 6}
	got := a.Add(b)
	if got != (Coord{5, 7, 9}) {
		t.Fatalf("Add wrong: %+v", got)
	}
}

func TestBrickConstants(t *testing.T) {
	if BrickVoxelCount != 512 {
		t.Fatalf("BrickVoxelCount: got %d want 512", BrickVoxelCount)
	}
}
