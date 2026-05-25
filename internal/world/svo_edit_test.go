package world

import "testing"

func TestBuildTreeSparseVolumesKeepsEditableTreeLazy(t *testing.T) {
	svo := NewSVO()
	svo.BuildTreeSparseVolumes(16, func(addVoxel func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32)) {
		addCube(0, 0, 0, 8, 0xFF0000FF)
	})

	if svo.editableRoot != nil {
		t.Fatal("expected generated scene to drop the editable staging tree until an edit is requested")
	}
}

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
	if got := svo.LastEditTouchedBrickIndices(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected touched brick indices [0], got %v", got)
	}
}

func TestBuildTreeSparseVolumesWithBricksKeepsMixedBrickEditsIncremental(t *testing.T) {
	svo := NewSVO()
	var colors [BrickVoxelCount]uint32
	colors[0] = 0xFF0000FF
	colors[1+BrickSize+BrickSize*BrickSize] = 0xFF0000FF
	svo.BuildTreeSparseVolumesWithBricks(16, func(_addVoxel func(x, y, z uint, color uint32), _addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, colors *[BrickVoxelCount]uint32)) {
		addBrick(0, 0, 0, &colors)
	})

	if svo.editableRoot != nil {
		t.Fatal("expected generated brick leaves to keep the editable tree lazy")
	}
	if !svo.SetVoxelColor(0, 0, 0, 0xFF00FF00) {
		t.Fatal("expected direct-brick edit to change the scene")
	}
	if svo.LastEditUsedFullRebuild() {
		t.Fatal("expected direct-brick edit to stay incremental")
	}
}
