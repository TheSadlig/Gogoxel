package world

type VoxelEdit struct {
	Position [3]uint32
	Color    uint32
}

type incrementalBrickMutation struct {
	node       *stagingNode
	brickIndex int
	origin     [3]uint32
	voxels     *[BrickVoxelCount]uint8
}

func (s *SVO) SetVoxelColor(x, y, z uint, color uint32) bool {
	edit := [1]VoxelEdit{{Position: [3]uint32{uint32(x), uint32(y), uint32(z)}, Color: color}}
	return s.ApplyVoxelEdits(edit[:]) > 0
}

func (s *SVO) ClearVoxel(x, y, z uint) bool {
	edit := [1]VoxelEdit{{Position: [3]uint32{uint32(x), uint32(y), uint32(z)}}}
	return s.ApplyVoxelEdits(edit[:]) > 0
}

func (s *SVO) ApplyVoxelEdits(edits []VoxelEdit) int {
	if s == nil || len(edits) == 0 {
		if s != nil {
			s.lastEdit = editStats{}
		}
		return 0
	}
	if !s.ensureEditableRoot() {
		s.lastEdit = editStats{}
		return 0
	}
	if changedCount, ok := s.tryApplyVoxelEditsIncremental(edits); ok {
		return changedCount
	}

	root := s.editableRoot
	changedCount := 0
	for _, edit := range edits {
		x := uint(edit.Position[0])
		y := uint(edit.Position[1])
		z := uint(edit.Position[2])
		if x >= s.size || y >= s.size || z >= s.size {
			continue
		}

		materialID := uint8(0)
		if edit.Color != 0 {
			s.ensureColorToMaterial()
			materialID = s.materialForColor(edit.Color)
		}

		var changed bool
		root, changed = s.editVoxelNode(root, 0, 0, 0, s.size, x, y, z, materialID)
		if changed {
			changedCount++
		}
	}
	if changedCount == 0 {
		s.lastEdit = editStats{}
		return 0
	}

	s.editableRoot = s.normalizeEditableNode(root, s.size)
	s.rebuildEditableState()
	s.lastEdit = editStats{mode: editApplyModeFullRebuild}
	return changedCount
}

func (s *SVO) tryApplyVoxelEditsIncremental(edits []VoxelEdit) (int, bool) {
	if s == nil || s.editableRoot == nil {
		return 0, false
	}

	mutationsByNode := make(map[*stagingNode]*incrementalBrickMutation)
	mutationOrder := make([]*incrementalBrickMutation, 0, len(edits))
	addedPositions := make([][3]uint32, 0, len(edits))
	changedCount := 0

	for _, edit := range edits {
		x := uint(edit.Position[0])
		y := uint(edit.Position[1])
		z := uint(edit.Position[2])
		if x >= s.size || y >= s.size || z >= s.size {
			continue
		}

		materialID := uint8(0)
		if edit.Color != 0 {
			s.ensureColorToMaterial()
			materialID = s.materialForColor(edit.Color)
		}

		node, origin, ok := s.findEditableBrickLeaf(s.editableRoot, s.size, 0, 0, 0, x, y, z)
		if !ok || node.brickIndex < 0 || node.brickIndex >= len(s.bricks) {
			return 0, false
		}

		mutation := mutationsByNode[node]
		if mutation == nil {
			voxelsCopy := *node.brickVoxels
			mutation = &incrementalBrickMutation{
				node:       node,
				brickIndex: node.brickIndex,
				origin:     origin,
				voxels:     &voxelsCopy,
			}
			mutationsByNode[node] = mutation
			mutationOrder = append(mutationOrder, mutation)
		}

		index := brickVoxelIndex(
			int(x-uint(mutation.origin[0])),
			int(y-uint(mutation.origin[1])),
			int(z-uint(mutation.origin[2])),
		)
		previousMaterialID := mutation.voxels[index]
		if previousMaterialID == materialID {
			continue
		}
		if previousMaterialID == 0 && materialID != 0 {
			addedPositions = append(addedPositions, edit.Position)
		}
		mutation.voxels[index] = materialID
		changedCount++
	}

	if changedCount == 0 {
		s.lastEdit = editStats{}
		return 0, true
	}

	for _, mutation := range mutationOrder {
		if classifyBrickVoxels(mutation.voxels) != brickVoxelShapeMixed {
			return 0, false
		}
	}

	for _, mutation := range mutationOrder {
		mutation.node.brickVoxels = mutation.voxels
		s.bricks[mutation.brickIndex].Voxels = mutation.voxels

		// Keep the fallback materialID in the node payload consistent with the
		// new dominant voxel material. This matters for non-resident bricks: the
		// shader uses the fallback when childPointer == 0, so a stale payload
		// would render the wrong color after eviction.
		newFallback := dominantBrickMaterial(mutation.voxels)
		mutation.node.setBrickLeaf(0, newFallback)
		nodeIdx := s.bricks[mutation.brickIndex].NodeIndex
		if int(nodeIdx) < len(s.nodes) {
			s.nodes[nodeIdx].payload = mutation.node.payload
			// childPointer in CPU-side nodes is always 0; GPU slot is
			// patched separately via patchNodePointer, so no update needed.
		}
		storageWordIdx := storageWordCount + int(nodeIdx)*2
		if storageWordIdx < len(s.storageWords) {
			s.storageWords[storageWordIdx] = mutation.node.payload
		}
	}
	for _, position := range addedPositions {
		s.extendOccupiedBounds(uint(position[0]), uint(position[1]), uint(position[2]), 1)
	}
	s.syncStorageWordsPalette()
	s.lastEdit = editStats{mode: editApplyModeIncremental, touchedBricks: len(mutationOrder)}
	return changedCount, true
}

func (s *SVO) findEditableBrickLeaf(node *stagingNode, nodeSize, originX, originY, originZ, x, y, z uint) (*stagingNode, [3]uint32, bool) {
	if node == nil {
		return nil, [3]uint32{}, false
	}
	if node.brickVoxels != nil {
		return node, [3]uint32{uint32(originX), uint32(originY), uint32(originZ)}, true
	}
	if node.isSolidLeaf() || nodeSize <= BrickSize {
		return nil, [3]uint32{}, false
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
	return s.findEditableBrickLeaf(node.tempChildren[octant], childSize, childOriginX, childOriginY, childOriginZ, x, y, z)
}
