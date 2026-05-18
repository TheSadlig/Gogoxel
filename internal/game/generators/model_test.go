package generators

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRSVOGeneratorLoadsSparseTree(t *testing.T) {
	generator, err := NewRSVOGeneratorFromBytes(buildTestRSVO(t, 1, []uint32{1, 2}, map[int][]byte{
		1: {0x81},
	}))
	if err != nil {
		t.Fatalf("expected RSVO payload to parse: %v", err)
	}

	data, palette := generator.Generate(2, 2, 2)
	if palette[rsvoFilledVoxel] == 0 {
		t.Fatal("expected rsvo filled voxel palette entry")
	}
	assertVoxelAt(t, data, 2, 2, 0, 0, 1, rsvoFilledVoxel)
	assertVoxelAt(t, data, 2, 2, 1, 1, 0, rsvoFilledVoxel)
	if got := voxelAt(data, 2, 2, 1, 0, 0); got != 0 {
		t.Fatalf("expected empty voxel at (1,0,0), got %d", got)
	}
}

func TestGenericGeneratorLoadsRSVOFile(t *testing.T) {
	if _, err := resolveModelPath(defaultModelPath); err != nil {
		t.Skipf("rsvo sample not available: %v", err)
	}

	generator, err := NewGeneratorFromFile(defaultModelPath)
	if err != nil {
		t.Fatalf("expected default rsvo sample to parse: %v", err)
	}

	data, palette := generator.Generate(96, 96, 48)
	if palette[1] == 0 {
		t.Fatal("expected non-empty palette for imported model")
	}
	_, _, _, _, _, maxZ, ok := occupiedBounds(data, 96, 96, 48)
	if !ok {
		t.Fatal("expected non-empty chunk data from rsvo sample")
	}
	if maxZ < 0 || maxZ >= 48 {
		t.Fatalf("expected rsvo output to fit chunk depth, got max z %d", maxZ)
	}
}

func TestGenericGeneratorRejectsUnknownFormat(t *testing.T) {
	if _, err := NewGeneratorFromBytes([]byte("NOPE")); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func buildTestRSVO(t *testing.T, topLevel uint32, nodeCounts []uint32, masks map[int][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if _, err := buffer.WriteString("RSVO"); err != nil {
		t.Fatalf("write RSVO magic: %v", err)
	}
	writeUint32(t, &buffer, 1)
	writeUint32(t, &buffer, 0)
	writeUint32(t, &buffer, 0)
	writeUint32(t, &buffer, topLevel)
	for _, count := range nodeCounts {
		writeUint32(t, &buffer, count)
	}
	for level := int(topLevel); level >= 1; level-- {
		if _, err := buffer.Write(masks[level]); err != nil {
			t.Fatalf("write RSVO masks for level %d: %v", level, err)
		}
	}
	return buffer.Bytes()
}

func writeUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("write uint32: %v", err)
	}
}

func occupiedBounds(data []uint8, width, height, depth int) (minX, maxX, minY, maxY, minZ, maxZ int, ok bool) {
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

func voxelAt(data []uint8, width, height, x, y, z int) uint8 {
	return data[z*width*height+y*width+x]
}

func assertVoxelAt(t *testing.T, data []uint8, width, height, x, y, z int, want uint8) {
	t.Helper()
	if got := voxelAt(data, width, height, x, y, z); got != want {
		t.Fatalf("expected voxel %d at (%d,%d,%d), got %d", want, x, y, z, got)
	}
}
