package generators

import (
	"math/bits"
	"testing"
)

func TestRSVOPackedNodesStayInBounds(t *testing.T) {
	generator, err := NewRSVOGeneratorFromFile(defaultModelPath)
	if err != nil {
		t.Fatalf("NewRSVOGeneratorFromFile() error = %v", err)
	}

	pruneLevel := generator.model.pruneLevelForNodeBudget(maxRSVOPackedNodes)
	nodes := generator.model.toPackedNodes(pruneLevel)
	if len(nodes) == 0 {
		t.Fatal("toPackedNodes(pruneLevel) returned no nodes")
	}
	if len(nodes) > maxRSVOPackedNodes {
		t.Fatalf("len(nodes) = %d, want <= %d", len(nodes), maxRSVOPackedNodes)
	}

	for index, node := range nodes {
		childMask := uint8(node.ChildMaskAndColor & 0xFF)
		if node.ChildPointer == 0 {
			if childMask != 0x01 {
				t.Fatalf("node %d: leaf child mask = %#02x, want %#02x", index, childMask, uint8(0x01))
			}
			continue
		}

		childCount := bits.OnesCount8(childMask)
		if childCount == 0 {
			t.Fatalf("node %d: branch childPointer = %d with empty child mask", index, node.ChildPointer)
		}

		if node.ChildPointer == 0 {
			t.Fatalf("node %d: branch childPointer = 0 with child mask %#02x", index, childMask)
		}
		childStart := int(node.ChildPointer)
		childEnd := childStart + childCount
		if childStart < 0 || childStart >= len(nodes) {
			t.Fatalf("node %d: childPointer = %d out of bounds for %d nodes", index, node.ChildPointer, len(nodes))
		}
		if childEnd > len(nodes) {
			t.Fatalf("node %d: child range [%d,%d) out of bounds for %d nodes", index, childStart, childEnd, len(nodes))
		}
	}
}
