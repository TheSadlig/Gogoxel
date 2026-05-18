package generators

import "testing"

func TestHouseGeneratorUsesScaleAsFullHeight(t *testing.T) {
	generator := NewHouseGenerator(18)
	data := generator.Generate(48, 48, 24)

	_, _, _, _, minZ, maxZ, ok := occupiedBounds(data, 48, 48, 24)
	if !ok {
		t.Fatal("expected generated house voxels")
	}
	if minZ != 0 {
		t.Fatalf("expected house to start at ground level, got min z %d", minZ)
	}
	if maxZ+1 != 18 {
		t.Fatalf("expected full house height 18, got %d", maxZ+1)
	}
	if voxelAt(data, 48, 48, 24, 24, maxZ) != houseRoofVoxel {
		t.Fatalf("expected roof voxel at the ridge, got %d", voxelAt(data, 48, 48, 24, 24, maxZ))
	}
}

func TestHouseGeneratorBuildsRoofAboveWalls(t *testing.T) {
	generator := NewHouseGenerator(21)
	data := generator.Generate(64, 64, 30)

	maxWallZ := maxZForVoxel(data, 64, 64, 30, houseWallVoxel)
	maxRoofZ := maxZForVoxel(data, 64, 64, 30, houseRoofVoxel)
	if maxWallZ == -1 {
		t.Fatal("expected wall voxels")
	}
	if maxRoofZ == -1 {
		t.Fatal("expected roof voxels")
	}
	if maxRoofZ <= maxWallZ {
		t.Fatalf("expected roof above walls, got roof max z %d and wall max z %d", maxRoofZ, maxWallZ)
	}
	if maxRoofZ+1 != 21 {
		t.Fatalf("expected roof ridge to reach full height 21, got %d", maxRoofZ+1)
	}
}

func TestHouseGeneratorClampsScaleToChunkDepth(t *testing.T) {
	generator := NewHouseGenerator(40)
	data := generator.Generate(48, 48, 16)

	_, _, _, _, _, maxZ, ok := occupiedBounds(data, 48, 48, 16)
	if !ok {
		t.Fatal("expected generated house voxels")
	}
	if maxZ != 15 {
		t.Fatalf("expected house height to clamp to chunk depth, got max z %d", maxZ)
	}
}

func TestHouseGeneratorCreatesCompoundCSGLayout(t *testing.T) {
	generator := NewHouseGenerator(24)
	width, height, depth := 80, 80, 36
	layout := generator.layout(width, height, depth)
	if !layout.valid {
		t.Fatal("expected valid complex house layout")
	}

	data := generator.Generate(uint32(width), uint32(height), uint32(depth))
	minX, _, minY, maxY, _, _, ok := occupiedBounds(data, width, height, depth)
	if !ok {
		t.Fatal("expected generated house voxels")
	}
	if minX > layout.wingMass.minX {
		t.Fatalf("expected occupied footprint to include side wing, got min x %d want <= %d", minX, layout.wingMass.minX)
	}
	if minY > layout.rearMass.minY {
		t.Fatalf("expected occupied footprint to include rear section, got min y %d want <= %d", minY, layout.rearMass.minY)
	}
	if maxY < layout.porchMass.maxY {
		t.Fatalf("expected occupied footprint to include porch, got max y %d want >= %d", maxY, layout.porchMass.maxY)
	}

	assertVoxel(t, data, width, height, layout.wingMass.minX, midpoint(layout.wingMass.minY, layout.wingMass.maxY), 1, houseWallVoxel)
	assertVoxel(t, data, width, height, layout.rearMass.minX+1, layout.rearMass.minY, 1, houseWallVoxel)
	assertVoxel(t, data, width, height, layout.partitions[0].minX, layout.partitions[0].minY, layout.partitions[0].minZ, houseWallVoxel)
	assertVoxel(t, data, width, height, layout.chimneys[0].minX, layout.chimneys[0].minY, layout.chimneys[0].minZ, houseWallVoxel)

	if got := voxelAt(data, width, height, layout.mainMass.minX+2, layout.mainMass.minY+2, 2); got != 0 {
		t.Fatalf("expected carved interior void, got voxel %d", got)
	}
}

func occupiedBounds(data []uint32, width, height, depth int) (minX, maxX, minY, maxY, minZ, maxZ int, ok bool) {
	minX, minY, minZ = width, height, depth
	maxX, maxY, maxZ = -1, -1, -1

	for z := 0; z < depth; z++ {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if voxelAt(data, width, height, x, y, z) == 0 {
					continue
				}

				ok = true
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
				if z < minZ {
					minZ = z
				}
				if z > maxZ {
					maxZ = z
				}
			}
		}
	}

	return minX, maxX, minY, maxY, minZ, maxZ, ok
}

func maxZForVoxel(data []uint32, width, height, depth int, voxel uint32) int {
	for z := depth - 1; z >= 0; z-- {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if voxelAt(data, width, height, x, y, z) == voxel {
					return z
				}
			}
		}
	}

	return -1
}

func voxelAt(data []uint32, width, height, x, y, z int) uint32 {
	return data[z*width*height+y*width+x]
}

func assertVoxel(t *testing.T, data []uint32, width, height, x, y, z int, want uint32) {
	t.Helper()
	if got := voxelAt(data, width, height, x, y, z); got != want {
		t.Fatalf("expected voxel %d at (%d,%d,%d), got %d", want, x, y, z, got)
	}
}
