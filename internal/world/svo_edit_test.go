package world

import "testing"

func TestLastEditUsedFullRebuildForStructuralEdit(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(16, func(addVoxel func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(0, 0, 0, 8, 0xFF0000FF)
	})

	if !svo.SetVoxelColor(0, 0, 0, 0xFF00FF00) {
		t.Fatal("expected structural edit to change the scene")
	}
	if !svo.LastEditUsedFullRebuild() {
		t.Fatal("expected structural edit to require a full rebuild")
	}
}

func TestLastEditUsedFullRebuildFalseForIncrementalBrickEdit(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(16, func(addVoxel func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addVoxel(0, 0, 0, 0xFF0000FF)
		addVoxel(1, 1, 1, 0xFF0000FF)
	})

	if !svo.SetVoxelColor(0, 0, 0, 0xFF00FF00) {
		t.Fatal("expected mixed-brick edit to change the scene")
	}
	if svo.LastEditUsedFullRebuild() {
		t.Fatal("expected mixed-brick edit to stay incremental")
	}
}
