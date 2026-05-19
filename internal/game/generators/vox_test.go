package generators

import (
	"math/bits"
	"testing"

	"Gogoxel/internal/world"
)

func TestRSVOStorageWordsStayInBounds(t *testing.T) {
	generator, err := NewRSVOGeneratorFromFile(defaultModelPath)
	if err != nil {
		t.Fatalf("NewRSVOGeneratorFromFile() error = %v", err)
	}

	pruneLevel := generator.model.pruneLevelForNodeBudget(maxRSVONodeBudget)
	words := generator.model.toStorageBufferWords(pruneLevel)
	if len(words) < 2+world.PaletteSize+2 {
		t.Fatal("toStorageBufferWords(pruneLevel) returned too few words")
	}
	nodeCount := int(words[1])
	if nodeCount > maxRSVONodeBudget {
		t.Fatalf("nodeCount = %d, want <= %d", nodeCount, maxRSVONodeBudget)
	}
	paletteOffset := 2 + nodeCount*2
	if len(words) != paletteOffset+world.PaletteSize {
		t.Fatalf("len(words) = %d, want %d", len(words), paletteOffset+world.PaletteSize)
	}
	if got, want := words[paletteOffset+1], generator.model.palette[1]; got != want {
		t.Fatalf("palette word = %#x, want %#x", got, want)
	}

	for index := 0; index < nodeCount; index++ {
		wordIndex := 2 + index*2
		childMask := uint8(words[wordIndex] & 0xFF)
		childPointer := words[wordIndex+1]
		if childPointer == 0 {
			if childMask != 0x01 {
				t.Fatalf("node %d: leaf child mask = %#02x, want %#02x", index, childMask, uint8(0x01))
			}
			continue
		}

		childCount := bits.OnesCount8(childMask)
		if childCount == 0 {
			t.Fatalf("node %d: branch childPointer = %d with empty child mask", index, childPointer)
		}

		childStart := int(childPointer)
		childEnd := childStart + childCount
		if childStart < 0 || childStart >= nodeCount {
			t.Fatalf("node %d: childPointer = %d out of bounds for %d nodes", index, childPointer, nodeCount)
		}
		if childEnd > nodeCount {
			t.Fatalf("node %d: child range [%d,%d) out of bounds for %d nodes", index, childStart, childEnd, nodeCount)
		}
	}
}
