package world

import "math/bits"

type brickVoxelShape uint8

const (
	brickVoxelShapeEmpty brickVoxelShape = iota
	brickVoxelShapeUniform
	brickVoxelShapeMixed
)

func classifyBrickVoxels(voxels *[BrickVoxelCount]uint8) brickVoxelShape {
	uniformMaterialID := uint8(0)
	hasMaterial := false
	uniform := true
	for _, materialID := range voxels {
		if materialID == 0 {
			uniform = false
			continue
		}
		if !hasMaterial {
			uniformMaterialID = materialID
			hasMaterial = true
			continue
		}
		if materialID != uniformMaterialID {
			uniform = false
			break
		}
	}
	if !hasMaterial {
		return brickVoxelShapeEmpty
	}
	if uniform {
		return brickVoxelShapeUniform
	}
	return brickVoxelShapeMixed
}

func (s *SVO) ensureEditableRoot() bool {
	if s == nil {
		return false
	}
	if s.editableRoot != nil || len(s.nodes) == 0 {
		return true
	}
	root, ok := s.rebuildEditableNode(0, s.size)
	if !ok {
		return false
	}
	s.editableRoot = s.normalizeEditableNode(root, s.size)
	return true
}

func (s *SVO) ensureColorToMaterial() {
	if s == nil || s.colorToMaterial != nil {
		return
	}
	s.colorToMaterial = make(map[uint32]uint8, PaletteSize)
	for index, color := range s.palette {
		if index == 0 || color == 0 {
			continue
		}
		s.colorToMaterial[color] = uint8(index)
	}
}

func (s *SVO) rebuildEditableNode(nodeIndex uint32, nodeSize uint) (*stagingNode, bool) {
	if s == nil || int(nodeIndex) >= len(s.nodes) {
		return nil, false
	}
	node := s.nodes[nodeIndex]
	if node.isBrickLeaf() {
		brick, ok := s.brickForNode(nodeIndex)
		if !ok {
			return nil, false
		}
		editable := newStagingNode()
		voxelsCopy := *brick.Voxels
		editable.brickVoxels = &voxelsCopy
		editable.setBrickLeaf(0)
		return editable, true
	}
	if node.isSolidLeaf() {
		editable := newStagingNode()
		editable.setSolidLeaf(node.materialID())
		return editable, true
	}
	if node.childMask() == 0 || node.childPointer == 0 {
		return nil, true
	}

	editable := newStagingNode()
	editable.setBranch(node.childMask())
	childSize := nodeSize >> 1
	mask := node.childMask()
	for octant := 0; octant < 8; octant++ {
		bit := uint8(1 << uint8(octant))
		if mask&bit == 0 {
			continue
		}
		childIndex := node.childPointer + uint32(bits.OnesCount8(mask&(bit-1)))
		child, ok := s.rebuildEditableNode(childIndex, childSize)
		if !ok {
			return nil, false
		}
		editable.tempChildren[octant] = child
	}
	return editable, true
}

func (s *SVO) rebuildEditableState() {
	if s == nil {
		return
	}
	s.nodes = s.nodes[:0]
	s.bricks = s.bricks[:0]
	if s.brickNodeLookup != nil {
		clear(s.brickNodeLookup)
	}
	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.hasOccupiedBounds = false

	if s.editableRoot == nil {
		s.nodes = append(s.nodes, SvoNode{})
		s.brickNodeLookup = nil
		return
	}

	s.editableRoot = s.normalizeEditableNode(s.editableRoot, s.size)
	if s.editableRoot == nil {
		s.nodes = append(s.nodes, SvoNode{})
		s.brickNodeLookup = nil
		return
	}

	s.assignEditableBrickIndices(s.editableRoot, s.size, 0, 0, 0)
	s.extendOccupiedBoundsFromNode(s.editableRoot, s.size, 0, 0, 0)
	s.flattenTree(s.editableRoot)
	s.rebuildBrickNodeLookup()
	s.rebuildStorageWords()
}

func (s *SVO) assignEditableBrickIndices(node *stagingNode, nodeSize, originX, originY, originZ uint) {
	if node == nil {
		return
	}
	node.brickIndex = -1
	if node.brickVoxels != nil {
		brick := Brick{
			Origin: [3]uint32{uint32(originX), uint32(originY), uint32(originZ)},
			Voxels: node.brickVoxels,
		}
		node.brickIndex = len(s.bricks)
		s.bricks = append(s.bricks, brick)
		node.brickVoxels = s.bricks[node.brickIndex].Voxels
		node.setBrickLeaf(0)
		node.tempChildren = [8]*stagingNode{}
		return
	}
	if node.isSolidLeaf() || nodeSize <= 1 {
		return
	}
	childSize := nodeSize >> 1
	for octant, child := range node.tempChildren {
		if child == nil {
			continue
		}
		childOriginX := originX + uint(octant&1)*childSize
		childOriginY := originY + uint((octant>>1)&1)*childSize
		childOriginZ := originZ + uint((octant>>2)&1)*childSize
		s.assignEditableBrickIndices(child, childSize, childOriginX, childOriginY, childOriginZ)
	}
}

func (s *SVO) extendOccupiedBoundsFromNode(node *stagingNode, nodeSize, originX, originY, originZ uint) {
	if node == nil {
		return
	}
	if node.brickVoxels != nil {
		for index, materialID := range node.brickVoxels {
			if materialID == 0 {
				continue
			}
			localX := uint(index % BrickSize)
			localY := uint((index / BrickSize) % BrickSize)
			localZ := uint(index / (BrickSize * BrickSize))
			s.extendOccupiedBounds(originX+localX, originY+localY, originZ+localZ, 1)
		}
		return
	}
	if node.isSolidLeaf() {
		s.extendOccupiedBounds(originX, originY, originZ, nodeSize)
		return
	}
	if nodeSize <= 1 {
		return
	}
	childSize := nodeSize >> 1
	for octant, child := range node.tempChildren {
		if child == nil {
			continue
		}
		childOriginX := originX + uint(octant&1)*childSize
		childOriginY := originY + uint((octant>>1)&1)*childSize
		childOriginZ := originZ + uint((octant>>2)&1)*childSize
		s.extendOccupiedBoundsFromNode(child, childSize, childOriginX, childOriginY, childOriginZ)
	}
}

func (s *SVO) editVoxelNode(node *stagingNode, originX, originY, originZ, nodeSize, x, y, z uint, materialID uint8) (*stagingNode, bool) {
	if nodeSize == 0 {
		return node, false
	}
	if node == nil {
		if materialID == 0 {
			return nil, false
		}
		if nodeSize == 1 {
			leaf := newStagingNode()
			leaf.setSolidLeaf(materialID)
			return leaf, true
		}
		if nodeSize == BrickSize {
			leaf := newStagingNode()
			voxels := &[BrickVoxelCount]uint8{}
			voxels[brickVoxelIndex(int(x-originX), int(y-originY), int(z-originZ))] = materialID
			leaf.brickVoxels = voxels
			leaf.setBrickLeaf(0)
			return leaf, true
		}
		node = newStagingNode()
		node.setBranch(0)
	}

	if node.brickVoxels != nil {
		index := brickVoxelIndex(int(x-originX), int(y-originY), int(z-originZ))
		if node.brickVoxels[index] == materialID {
			return node, false
		}
		newVoxels := *node.brickVoxels
		newVoxels[index] = materialID
		node.brickVoxels = &newVoxels
		return node, true
	}

	if node.isSolidLeaf() {
		currentMaterialID := node.materialID()
		if currentMaterialID == materialID {
			return node, false
		}
		if nodeSize == 1 {
			if materialID == 0 {
				return nil, true
			}
			leaf := newStagingNode()
			leaf.setSolidLeaf(materialID)
			return leaf, true
		}
		if nodeSize == BrickSize {
			voxels := &[BrickVoxelCount]uint8{}
			for index := range voxels {
				voxels[index] = currentMaterialID
			}
			voxels[brickVoxelIndex(int(x-originX), int(y-originY), int(z-originZ))] = materialID
			node.tempChildren = [8]*stagingNode{}
			node.brickIndex = -1
			node.brickVoxels = voxels
			node.setBrickLeaf(0)
			return node, true
		}
		node = expandSolidNode(currentMaterialID)
	}

	if nodeSize == BrickSize {
		voxels := &[BrickVoxelCount]uint8{}
		s.fillEditableBrickVoxels(voxels, node, BrickSize, 0, 0, 0)
		index := brickVoxelIndex(int(x-originX), int(y-originY), int(z-originZ))
		if voxels[index] == materialID {
			return node, false
		}
		voxels[index] = materialID
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		node.brickVoxels = voxels
		node.setBrickLeaf(0)
		return node, true
	}

	childSize := nodeSize >> 1
	octant := 0
	childOriginX := originX
	childOriginY := originY
	childOriginZ := originZ
	if x >= originX+childSize {
		octant |= 1
		childOriginX += childSize
	}
	if y >= originY+childSize {
		octant |= 2
		childOriginY += childSize
	}
	if z >= originZ+childSize {
		octant |= 4
		childOriginZ += childSize
	}

	child, changed := s.editVoxelNode(node.tempChildren[octant], childOriginX, childOriginY, childOriginZ, childSize, x, y, z, materialID)
	if !changed {
		return node, false
	}
	node.tempChildren[octant] = child
	node.brickIndex = -1
	node.brickVoxels = nil
	node.setBranch(nodeActiveMask(node))
	return node, true
}

func (s *SVO) normalizeEditableNode(node *stagingNode, nodeSize uint) *stagingNode {
	if node == nil {
		return nil
	}
	if node.brickVoxels != nil {
		uniformMaterialID := uint8(0)
		hasMaterial := false
		uniform := true
		for _, materialID := range node.brickVoxels {
			if materialID == 0 {
				uniform = false
				continue
			}
			if !hasMaterial {
				uniformMaterialID = materialID
				hasMaterial = true
				continue
			}
			if materialID != uniformMaterialID {
				uniform = false
			}
		}
		if !hasMaterial {
			return nil
		}
		if uniform {
			node.tempChildren = [8]*stagingNode{}
			node.brickIndex = -1
			node.brickVoxels = nil
			node.setSolidLeaf(uniformMaterialID)
			return node
		}
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		node.setBrickLeaf(0)
		return node
	}
	if node.isSolidLeaf() {
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		return node
	}
	if nodeSize <= 1 {
		return nil
	}

	childSize := nodeSize >> 1
	activeCount := 0
	activeMask := uint8(0)
	uniformMaterialID := uint8(0)
	canCollapse := true
	for octant, child := range node.tempChildren {
		normalized := s.normalizeEditableNode(child, childSize)
		node.tempChildren[octant] = normalized
		if normalized == nil {
			canCollapse = false
			continue
		}
		activeCount++
		activeMask |= 1 << uint8(octant)
		if !normalized.isSolidLeaf() {
			canCollapse = false
			continue
		}
		if uniformMaterialID == 0 {
			uniformMaterialID = normalized.materialID()
			continue
		}
		if normalized.materialID() != uniformMaterialID {
			canCollapse = false
		}
	}
	if activeCount == 0 {
		return nil
	}
	if nodeSize == BrickSize {
		voxels := &[BrickVoxelCount]uint8{}
		s.fillEditableBrickVoxels(voxels, node, BrickSize, 0, 0, 0)
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		node.brickVoxels = voxels
		node.setBrickLeaf(0)
		return s.normalizeEditableNode(node, nodeSize)
	}
	if canCollapse && activeCount == 8 {
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		node.brickVoxels = nil
		node.setSolidLeaf(uniformMaterialID)
		return node
	}
	node.brickIndex = -1
	node.brickVoxels = nil
	node.setBranch(activeMask)
	return node
}

func (s *SVO) fillEditableBrickVoxels(voxels *[BrickVoxelCount]uint8, node *stagingNode, nodeSize, originX, originY, originZ int) {
	if node == nil {
		return
	}
	if node.brickVoxels != nil {
		copy(voxels[:], node.brickVoxels[:])
		return
	}
	if node.isSolidLeaf() {
		materialID := node.materialID()
		for z := 0; z < nodeSize; z++ {
			for y := 0; y < nodeSize; y++ {
				for x := 0; x < nodeSize; x++ {
					voxels[brickVoxelIndex(originX+x, originY+y, originZ+z)] = materialID
				}
			}
		}
		return
	}
	childSize := nodeSize / 2
	if childSize == 0 {
		return
	}
	for octant, child := range node.tempChildren {
		if child == nil {
			continue
		}
		childOriginX := originX + (octant&1)*childSize
		childOriginY := originY + ((octant>>1)&1)*childSize
		childOriginZ := originZ + ((octant>>2)&1)*childSize
		s.fillEditableBrickVoxels(voxels, child, childSize, childOriginX, childOriginY, childOriginZ)
	}
}

func expandSolidNode(materialID uint8) *stagingNode {
	node := newStagingNode()
	for octant := 0; octant < 8; octant++ {
		child := newStagingNode()
		child.setSolidLeaf(materialID)
		node.tempChildren[octant] = child
	}
	node.setBranch(0xFF)
	return node
}

func nodeActiveMask(node *stagingNode) uint8 {
	if node == nil {
		return 0
	}
	mask := uint8(0)
	for octant, child := range node.tempChildren {
		if child != nil {
			mask |= 1 << uint8(octant)
		}
	}
	return mask
}

func (s *SVO) rebuildBrickNodeLookup() {
	if s == nil || len(s.bricks) == 0 {
		s.brickNodeLookup = nil
		return
	}
	lookup := s.brickNodeLookup
	if lookup == nil || len(lookup) < len(s.bricks) {
		lookup = make(map[uint32]int, len(s.bricks))
	} else {
		clear(lookup)
	}
	for index := range s.bricks {
		lookup[s.bricks[index].NodeIndex] = index
	}
	s.brickNodeLookup = lookup
}

func (s *SVO) brickForNode(nodeIndex uint32) (Brick, bool) {
	if s == nil {
		return Brick{}, false
	}
	if s.brickNodeLookup == nil {
		s.rebuildBrickNodeLookup()
	}
	brickIndex, ok := s.brickNodeLookup[nodeIndex]
	if !ok || brickIndex < 0 || brickIndex >= len(s.bricks) {
		return Brick{}, false
	}
	return s.bricks[brickIndex], true
}
