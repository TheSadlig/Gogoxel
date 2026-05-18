package generators

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestVOXGeneratorLoadsVoxelPayload(t *testing.T) {
	generator, err := NewVOXGeneratorFromBytes(buildTestVOX(t, [3]uint32{2, 3, 2}, []testVOXEntry{
		{x: 0, y: 0, z: 0, color: 9},
		{x: 1, y: 2, z: 1, color: 17},
	}))
	if err != nil {
		t.Fatalf("expected VOX payload to parse: %v", err)
	}

	data := generator.Generate(2, 3, 2)
	assertVoxel(t, data, 2, 3, 0, 0, 0, 9)
	assertVoxel(t, data, 2, 3, 1, 2, 1, 17)
	if got := voxelAt(data, 2, 3, 1, 1, 0); got != 0 {
		t.Fatalf("expected empty voxel at (1,1,0), got %d", got)
	}
}

func TestVOXGeneratorScalesTallModelToChunkDepth(t *testing.T) {
	generator, err := NewVOXGeneratorFromBytes(buildTestVOX(t, [3]uint32{1, 1, 4}, []testVOXEntry{
		{x: 0, y: 0, z: 0, color: 10},
		{x: 0, y: 0, z: 3, color: 20},
	}))
	if err != nil {
		t.Fatalf("expected VOX payload to parse: %v", err)
	}

	data := generator.Generate(4, 4, 2)
	_, _, _, _, minZ, maxZ, ok := occupiedBounds(data, 4, 4, 2)
	if !ok {
		t.Fatal("expected generated voxels")
	}
	if minZ != 0 || maxZ != 1 {
		t.Fatalf("expected grounded scaled model to occupy z range 0..1, got %d..%d", minZ, maxZ)
	}
	assertVoxel(t, data, 4, 4, 1, 1, 0, 10)
	assertVoxel(t, data, 4, 4, 1, 1, 1, 20)
}

func TestVOXGeneratorLoadsDragonSample(t *testing.T) {
	if _, err := os.Stat(defaultVOXModelPath); err != nil {
		t.Skipf("dragon sample not available: %v", err)
	}

	generator, err := NewVOXGeneratorFromFile(defaultVOXModelPath)
	if err != nil {
		t.Fatalf("expected dragon VOX file to parse: %v", err)
	}

	data := generator.Generate(140, 70, 50)
	_, _, _, _, minZ, maxZ, ok := occupiedBounds(data, 140, 70, 50)
	if !ok {
		t.Fatal("expected generated dragon voxels")
	}
	if minZ != 0 {
		t.Fatalf("expected dragon to sit on the ground plane, got min z %d", minZ)
	}
	if maxZ != 49 {
		t.Fatalf("expected dragon to scale to chunk depth 50, got max z %d", maxZ)
	}
	if maxZForVoxel(data, 140, 70, 50, 0) != -1 {
		t.Fatal("empty voxels should not be written as palette index 0")
	}
}

type testVOXEntry struct {
	x     byte
	y     byte
	z     byte
	color byte
}

func buildTestVOX(t *testing.T, size [3]uint32, voxels []testVOXEntry) []byte {
	t.Helper()

	var sizeChunk bytes.Buffer
	writeUint32(t, &sizeChunk, size[0])
	writeUint32(t, &sizeChunk, size[1])
	writeUint32(t, &sizeChunk, size[2])

	var xyziChunk bytes.Buffer
	writeUint32(t, &xyziChunk, uint32(len(voxels)))
	for _, voxel := range voxels {
		if err := xyziChunk.WriteByte(voxel.x); err != nil {
			t.Fatalf("write x byte: %v", err)
		}
		if err := xyziChunk.WriteByte(voxel.y); err != nil {
			t.Fatalf("write y byte: %v", err)
		}
		if err := xyziChunk.WriteByte(voxel.z); err != nil {
			t.Fatalf("write z byte: %v", err)
		}
		if err := xyziChunk.WriteByte(voxel.color); err != nil {
			t.Fatalf("write color byte: %v", err)
		}
	}

	var mainChildren bytes.Buffer
	writeChunk(t, &mainChildren, "SIZE", sizeChunk.Bytes(), nil)
	writeChunk(t, &mainChildren, "XYZI", xyziChunk.Bytes(), nil)

	var file bytes.Buffer
	if _, err := file.WriteString("VOX "); err != nil {
		t.Fatalf("write header: %v", err)
	}
	writeUint32(t, &file, 150)
	writeChunk(t, &file, "MAIN", nil, mainChildren.Bytes())

	return file.Bytes()
}

func writeChunk(t *testing.T, buffer *bytes.Buffer, id string, content, children []byte) {
	t.Helper()
	if _, err := buffer.WriteString(id); err != nil {
		t.Fatalf("write chunk id %q: %v", id, err)
	}
	writeUint32(t, buffer, uint32(len(content)))
	writeUint32(t, buffer, uint32(len(children)))
	if _, err := buffer.Write(content); err != nil {
		t.Fatalf("write chunk content %q: %v", id, err)
	}
	if _, err := buffer.Write(children); err != nil {
		t.Fatalf("write chunk children %q: %v", id, err)
	}
}

func writeUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("write uint32: %v", err)
	}
}
