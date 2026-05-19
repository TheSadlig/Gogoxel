package world

type SVO struct {
	nodes []SvoNode
	size  uint

	occupiedMin      [3]uint
	occupiedMax      [3]uint
	hasOccupiedBounds bool
}

type SvoNode struct {
	childMaskAndColor uint32 // Bits 0-7: Child mask | Bits 8-31: Packed RGB color
	childPointer      uint32 // Index of the first child in the global array
}

func (n *SvoNode) packColorAndMask(mask uint8, rgbColor uint32) {
	n.childMaskAndColor = ((rgbColor & 0xFFFFFF) << 8) | uint32(mask)
}

type stagingNode struct {
	SvoNode
	tempChildren [8]*stagingNode
}

func NewSVO() *SVO {
	return &SVO{nodes: make([]SvoNode, 0)}
}

func (s *SVO) BuildTree(voxelGrid func(x, y, z int) (uint32, bool), size uint) {
	s.size = octreeSize(size)
	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.hasOccupiedBounds = false
	leafLayer := make(map[uint64]*stagingNode)

	// Step 1: Naively generate the baseline leaf layer (1x1x1 voxels)
	for z := 0; z < int(size); z++ {
		for y := 0; y < int(size); y++ {
			for x := 0; x < int(size); x++ {
				if color, active := voxelGrid(x, y, z); active {
					if !s.hasOccupiedBounds {
						s.occupiedMin = [3]uint{uint(x), uint(y), uint(z)}
						s.occupiedMax = [3]uint{uint(x + 1), uint(y + 1), uint(z + 1)}
						s.hasOccupiedBounds = true
					} else {
						if uint(x) < s.occupiedMin[0] {
							s.occupiedMin[0] = uint(x)
						}
						if uint(y) < s.occupiedMin[1] {
							s.occupiedMin[1] = uint(y)
						}
						if uint(z) < s.occupiedMin[2] {
							s.occupiedMin[2] = uint(z)
						}
						if uint(x+1) > s.occupiedMax[0] {
							s.occupiedMax[0] = uint(x + 1)
						}
						if uint(y+1) > s.occupiedMax[1] {
							s.occupiedMax[1] = uint(y + 1)
						}
						if uint(z+1) > s.occupiedMax[2] {
							s.occupiedMax[2] = uint(z + 1)
						}
					}

					leaf := &stagingNode{}
					leaf.packColorAndMask(1, color) // Individual solid leaf marker

					key := uint64(x) | (uint64(y) << 20) | (uint64(z) << 40)
					leafLayer[key] = leaf
				}
			}
		}
	}

	// Step 2: Assemble intermediate branches from the bottom up AND optimize on the fly
	currentLayer := leafLayer
	currentSize := 1

	for currentSize < int(s.size) {
		parentLayer := make(map[uint64]*stagingNode)
		halfParentSize := currentSize
		currentSize *= 2

		// 2a. Populate parents naively for this layer scale
		for key, childNode := range currentLayer {
			cx := int(key & 0xFFFFF)
			cy := int((key >> 20) & 0xFFFFF)
			cz := int((key >> 40) & 0xFFFFF)

			px := (cx / currentSize) * currentSize
			py := (cy / currentSize) * currentSize
			pz := (cz / currentSize) * currentSize

			parentKey := uint64(px) | (uint64(py) << 20) | (uint64(pz) << 40)

			parent, exists := parentLayer[parentKey]
			if !exists {
				parent = &stagingNode{}
				parentLayer[parentKey] = parent
			}

			ox := (cx - px) / halfParentSize
			oy := (cy - py) / halfParentSize
			oz := (cz - pz) / halfParentSize
			octantIdx := ox | (oy << 1) | (oz << 2)

			parent.tempChildren[octantIdx] = childNode
		}

		// 2b. OPTIMIZATION ON THE FLY: Evaluate and collapse parents right now
		for _, parent := range parentLayer {
			activeCount := 0
			var activeMask uint8 = 0
			var firstColor uint32 = 0
			canCollapse := true

			for o := 0; o < 8; o++ {
				child := parent.tempChildren[o]
				if child != nil {
					activeMask |= (1 << uint8(o))
					activeCount++

					// Extract the mask and color configuration of this child
					childMask := uint8(child.childMaskAndColor & 0xFF)
					childColor := child.childMaskAndColor >> 8

					if firstColor == 0 {
						firstColor = childColor
					}

					// To collapse: child must be a solid leaf (mask==1) and colors must match
					if childMask != 1 || childColor != firstColor {
						canCollapse = false
					}
				} else {
					// Missing child (air) means this node is not uniform
					canCollapse = false
				}
			}

			if canCollapse && activeCount == 8 {
				// COLLAPSE: Turn this branch into a single giant leaf node
				parent.tempChildren = [8]*stagingNode{} // Sever child tracking links
				parent.childPointer = 0
				parent.packColorAndMask(1, firstColor) // Flag as solid leaf (1) with color
			} else {
				// KEEP BRANCH: Standard layout mapping setup
				parent.packColorAndMask(activeMask, 0)
			}
		}

		currentLayer = parentLayer
	}

	var root *stagingNode
	for _, node := range currentLayer {
		root = node
		break
	}

	if root == nil {
		s.nodes = make([]SvoNode, 1)
		return
	}

	// Step 3: Flatten the final tree
	s.nodes = make([]SvoNode, 0)
	s.flattenTree(root)
}

func octreeSize(size uint) uint {
	if size <= 1 {
		return 1
	}

	rootSize := uint(1)
	for rootSize < size {
		rootSize <<= 1
	}

	return rootSize
}

func (s *SVO) flattenTree(root *stagingNode) uint32 {
	nodeIdx := uint32(len(s.nodes))
	s.nodes = append(s.nodes, SvoNode{})
	s.flattenTreeInto(root, nodeIdx)
	return nodeIdx
}

func (s *SVO) flattenTreeInto(root *stagingNode, nodeIdx uint32) {
	s.nodes[nodeIdx] = root.SvoNode

	activeChildren := make([]*stagingNode, 0, 8)
	for o := 0; o < 8; o++ {
		if root.tempChildren[o] != nil {
			activeChildren = append(activeChildren, root.tempChildren[o])
		}
	}

	if len(activeChildren) == 0 {
		return
	}

	baseChildPointer := uint32(len(s.nodes))
	s.nodes[nodeIdx].childPointer = baseChildPointer

	for range activeChildren {
		s.nodes = append(s.nodes, SvoNode{})
	}

	for childOffset, child := range activeChildren {
		s.flattenTreeInto(child, baseChildPointer+uint32(childOffset))
	}
}

func (s *SVO) StorageBufferWords() []uint32 {
	if s == nil {
		return nil
	}

	words := make([]uint32, 2+len(s.nodes)*2)
	words[0] = uint32(s.size)
	// words[1] left as padding
	for index, node := range s.nodes {
		wordIndex := 2 + index*2
		words[wordIndex] = node.childMaskAndColor
		words[wordIndex+1] = node.childPointer
	}
	return words
}

func (s *SVO) NodeCount() int {
	if s == nil {
		return 0
	}
	return len(s.nodes)
}

func (s *SVO) OccupiedBounds() (min, max [3]uint32, ok bool) {
	if s == nil || !s.hasOccupiedBounds {
		return [3]uint32{}, [3]uint32{}, false
	}

	return [3]uint32{uint32(s.occupiedMin[0]), uint32(s.occupiedMin[1]), uint32(s.occupiedMin[2])},
		[3]uint32{uint32(s.occupiedMax[0]), uint32(s.occupiedMax[1]), uint32(s.occupiedMax[2])},
		true
}
