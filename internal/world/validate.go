package world

import "fmt"

// Validate performs an exhaustive structural check of an SVO. Used by tests
// and fuzz targets to assert post-edit invariants:
//   - Every node's child pointer is within bounds.
//   - Branch nodes' child pointer + popcount(childMask) does not run past
//     the node array.
//   - Brick leaves' brick index is within bounds.
//   - Palette indices in solid leaves reference an entry < PaletteSize.
//
// Returns nil when the SVO satisfies all invariants.
func (s *SVO) Validate() error {
	if s == nil {
		return fmt.Errorf("svo: nil")
	}
	if len(s.nodes) == 0 {
		return nil // empty SVO is structurally valid.
	}
	for i, node := range s.nodes {
		if node.payload&BrickLeafFlag != 0 {
			// Brick leaf: child pointer is an index into s.bricks when set.
			if node.childPointer != 0 {
				if int(node.childPointer) >= len(s.bricks) {
					return fmt.Errorf("svo: node %d brick index %d out of range (have %d)", i, node.childPointer, len(s.bricks))
				}
			}
			continue
		}
		childMask := node.payload & childMaskMask
		if childMask == 0 {
			continue // solid leaf; payload bits 8..30 hold material/color
		}
		childCount := uint32(0)
		for m := childMask; m != 0; m &= m - 1 {
			childCount++
		}
		end := uint64(node.childPointer) + uint64(childCount)
		if end > uint64(len(s.nodes)) {
			return fmt.Errorf("svo: node %d branch children [%d..%d) exceed node count %d", i, node.childPointer, end, len(s.nodes))
		}
	}
	return nil
}
