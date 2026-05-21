package world

import (
	"math"
	"math/bits"
)

const (
	rayHitEpsilon = float32(1e-4)

	axisMaskX = 1 << iota
	axisMaskY
	axisMaskZ
)

type Ray struct {
	Origin    [3]float32
	Direction [3]float32
}

type RaycastHit struct {
	Voxel      [3]uint32
	Normal     [3]int32
	MaterialID uint8
	Distance   float32
}

type rayBoxHit struct {
	tMin      float32
	tMax      float32
	normalMask int
}

type raycastChild struct {
	nodeIndex uint32
	origin    [3]uint32
	size      uint
	hit       rayBoxHit
}

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

type brickVoxelShape uint8

const (
	brickVoxelShapeEmpty brickVoxelShape = iota
	brickVoxelShapeUniform
	brickVoxelShapeMixed
)

func (s *SVO) Raycast(ray Ray, maxDistance float32) (RaycastHit, bool) {
	if s == nil || s.size == 0 || len(s.nodes) == 0 {
		return RaycastHit{}, false
	}
	if isZeroDirection(ray.Direction) {
		return RaycastHit{}, false
	}
	root := s.nodes[0]
	if len(s.nodes) == 1 && !root.isSolidLeaf() && !root.isBrickLeaf() && root.childMask() == 0 {
		return RaycastHit{}, false
	}
	if maxDistance <= 0 {
		maxDistance = math.MaxFloat32
	}

	rootHit, ok := intersectRayBox(ray, [3]float32{}, [3]float32{float32(s.size), float32(s.size), float32(s.size)}, 0, maxDistance)
	if !ok {
		return RaycastHit{}, false
	}
	return s.raycastNode(0, [3]uint32{}, s.size, ray, rootHit)
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
		// Deep-copy voxels so the editable staging node owns its own backing
		// array and CoW edits don't alias the GPU-resident brick data.
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
		// Pointer copy only — no 512-byte voxel memcpy for unchanged bricks.
		// CoW in editVoxelNode ensures modified bricks carry a fresh pointer,
		// allowing replaceSceneBricks to detect dirtiness via pointer comparison.
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
		// Copy-on-write: allocate a fresh backing array so assignEditableBrickIndices
		// can detect this brick as modified via pointer identity, skipping the
		// 512-byte memcpy for every other unmodified brick in the scene.
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

func (s *SVO) raycastNode(nodeIndex uint32, origin [3]uint32, size uint, ray Ray, boxHit rayBoxHit) (RaycastHit, bool) {
	if int(nodeIndex) >= len(s.nodes) {
		return RaycastHit{}, false
	}
	node := s.nodes[nodeIndex]
	if node.isBrickLeaf() {
		brick, ok := s.brickForNode(nodeIndex)
		if !ok {
			return RaycastHit{}, false
		}
		return raycastBrick(origin, *brick.Voxels, ray, boxHit)
	}
	if node.isSolidLeaf() {
		return raycastSolidLeaf(origin, size, node.materialID(), ray, boxHit), true
	}
	childMask := node.childMask()
	if childMask == 0 || node.childPointer == 0 || size <= 1 {
		return RaycastHit{}, false
	}

	childSize := size >> 1
	var children [8]raycastChild
	childCount := 0
	for octant := 0; octant < 8; octant++ {
		bit := uint8(1 << uint8(octant))
		if childMask&bit == 0 {
			continue
		}
		childIndex := node.childPointer + uint32(bits.OnesCount8(childMask&(bit-1)))
		childOrigin := origin
		childOrigin[0] += uint32(octant&1) * uint32(childSize)
		childOrigin[1] += uint32((octant>>1)&1) * uint32(childSize)
		childOrigin[2] += uint32((octant>>2)&1) * uint32(childSize)
		childMin := [3]float32{float32(childOrigin[0]), float32(childOrigin[1]), float32(childOrigin[2])}
		childMax := [3]float32{childMin[0] + float32(childSize), childMin[1] + float32(childSize), childMin[2] + float32(childSize)}
		childHit, ok := intersectRayBox(ray, childMin, childMax, maxFloat32(boxHit.tMin, 0), boxHit.tMax)
		if !ok {
			continue
		}
		candidate := raycastChild{
			nodeIndex: childIndex,
			origin:    childOrigin,
			size:      childSize,
			hit:       childHit,
		}
		insertIndex := childCount
		for insertIndex > 0 && raycastChildBefore(candidate, children[insertIndex-1]) {
			children[insertIndex] = children[insertIndex-1]
			insertIndex--
		}
		children[insertIndex] = candidate
		childCount++
	}
	if childCount == 0 {
		return RaycastHit{}, false
	}
	for childIndex := 0; childIndex < childCount; childIndex++ {
		child := children[childIndex]
		if hit, ok := s.raycastNode(child.nodeIndex, child.origin, child.size, ray, child.hit); ok {
			return hit, true
		}
	}
	return RaycastHit{}, false
}

func raycastChildBefore(candidate, existing raycastChild) bool {
	if candidate.hit.tMin == existing.hit.tMin {
		return candidate.nodeIndex < existing.nodeIndex
	}
	return candidate.hit.tMin < existing.hit.tMin
}

func raycastSolidLeaf(origin [3]uint32, size uint, materialID uint8, ray Ray, boxHit rayBoxHit) RaycastHit {
	distance := maxFloat32(boxHit.tMin, 0)
	point := advancePoint(ray, distance+rayHitEpsilon)
	voxel := pointToVoxel(point, origin, size)
	return RaycastHit{
		Voxel:      voxel,
		Normal:     resolveEntryNormal(boxHit.normalMask, ray.Direction),
		MaterialID: materialID,
		Distance:   distance,
	}
}

func raycastBrick(origin [3]uint32, voxels [BrickVoxelCount]uint8, ray Ray, boxHit rayBoxHit) (RaycastHit, bool) {
	currentT := maxFloat32(boxHit.tMin, 0)
	point := advancePoint(ray, currentT+rayHitEpsilon)
	originF := [3]float32{float32(origin[0]), float32(origin[1]), float32(origin[2])}
	voxel := [3]int{
		int(math.Floor(float64(clampFloat32(point[0]-originF[0], 0, float32(BrickSize)-rayHitEpsilon)))),
		int(math.Floor(float64(clampFloat32(point[1]-originF[1], 0, float32(BrickSize)-rayHitEpsilon)))),
		int(math.Floor(float64(clampFloat32(point[2]-originF[2], 0, float32(BrickSize)-rayHitEpsilon)))),
	}
	step := [3]int{signStep(ray.Direction[0]), signStep(ray.Direction[1]), signStep(ray.Direction[2])}
	tAxis := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	tDelta := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	for axis := 0; axis < 3; axis++ {
		direction := ray.Direction[axis]
		if step[axis] == 0 {
			continue
		}
		nextBoundary := float32(origin[axis] + uint32(voxel[axis]))
		if step[axis] > 0 {
			nextBoundary++
		}
		tAxis[axis] = (nextBoundary - ray.Origin[axis]) / direction
		tDelta[axis] = float32(math.Abs(float64(1 / direction)))
	}
	currentNormal := resolveEntryNormal(boxHit.normalMask, ray.Direction)

	for stepCount := 0; stepCount < BrickVoxelCount+3; stepCount++ {
		if voxel[0] < 0 || voxel[0] >= BrickSize || voxel[1] < 0 || voxel[1] >= BrickSize || voxel[2] < 0 || voxel[2] >= BrickSize {
			return RaycastHit{}, false
		}
		materialID := voxels[brickVoxelIndex(voxel[0], voxel[1], voxel[2])]
		if materialID != 0 {
			return RaycastHit{
				Voxel:      [3]uint32{origin[0] + uint32(voxel[0]), origin[1] + uint32(voxel[1]), origin[2] + uint32(voxel[2])},
				Normal:     currentNormal,
				MaterialID: materialID,
				Distance:   currentT,
			}, true
		}

		nextT := minFloat32(tAxis[0], minFloat32(tAxis[1], tAxis[2]))
		if nextT > boxHit.tMax {
			return RaycastHit{}, false
		}
		axisMask := 0
		if nearlyEqualFloat32(tAxis[0], nextT) {
			axisMask |= axisMaskX
		}
		if nearlyEqualFloat32(tAxis[1], nextT) {
			axisMask |= axisMaskY
		}
		if nearlyEqualFloat32(tAxis[2], nextT) {
			axisMask |= axisMaskZ
		}
		if axisMask&axisMaskX != 0 {
			voxel[0] += step[0]
			tAxis[0] += tDelta[0]
		}
		if axisMask&axisMaskY != 0 {
			voxel[1] += step[1]
			tAxis[1] += tDelta[1]
		}
		if axisMask&axisMaskZ != 0 {
			voxel[2] += step[2]
			tAxis[2] += tDelta[2]
		}
		currentT = nextT
		currentNormal = normalForAxisMask(axisMask, ray.Direction)
	}

	return RaycastHit{}, false
}

func intersectRayBox(ray Ray, boxMin, boxMax [3]float32, minDistance, maxDistance float32) (rayBoxHit, bool) {
	hit := rayBoxHit{tMin: minDistance, tMax: maxDistance}
	for axis := 0; axis < 3; axis++ {
		origin := ray.Origin[axis]
		direction := ray.Direction[axis]
		if nearlyZeroFloat32(direction) {
			if origin < boxMin[axis] || origin >= boxMax[axis] {
				return rayBoxHit{}, false
			}
			continue
		}
		invDirection := 1 / direction
		nearT := (boxMin[axis] - origin) * invDirection
		farT := (boxMax[axis] - origin) * invDirection
		nearMask := axisMaskForAxis(axis)
		if nearT > farT {
			nearT, farT = farT, nearT
		}
		if nearT > hit.tMin+rayHitEpsilon {
			hit.tMin = nearT
			hit.normalMask = nearMask
		} else if nearlyEqualFloat32(nearT, hit.tMin) {
			hit.normalMask |= nearMask
		}
		if farT < hit.tMax {
			hit.tMax = farT
		}
		if hit.tMin > hit.tMax {
			return rayBoxHit{}, false
		}
	}
	return hit, hit.tMax >= maxFloat32(hit.tMin, minDistance)
}

func pointToVoxel(point [3]float32, origin [3]uint32, size uint) [3]uint32 {
	maxCoord := float32(size) - rayHitEpsilon
	if maxCoord < 0 {
		maxCoord = 0
	}
	localX := clampFloat32(point[0]-float32(origin[0]), 0, maxCoord)
	localY := clampFloat32(point[1]-float32(origin[1]), 0, maxCoord)
	localZ := clampFloat32(point[2]-float32(origin[2]), 0, maxCoord)
	return [3]uint32{
		origin[0] + uint32(math.Floor(float64(localX))),
		origin[1] + uint32(math.Floor(float64(localY))),
		origin[2] + uint32(math.Floor(float64(localZ))),
	}
}

func advancePoint(ray Ray, distance float32) [3]float32 {
	return [3]float32{
		ray.Origin[0] + ray.Direction[0]*distance,
		ray.Origin[1] + ray.Direction[1]*distance,
		ray.Origin[2] + ray.Direction[2]*distance,
	}
}

func resolveEntryNormal(mask int, direction [3]float32) [3]int32 {
	if mask != 0 {
		return normalForAxisMask(mask, direction)
	}
	return dominantAxisNormal(direction)
}

func dominantAxisNormal(direction [3]float32) [3]int32 {
	axisMask := axisMaskX | axisMaskY | axisMaskZ
	return normalForAxisMask(axisMask, direction)
}

func normalForAxisMask(mask int, direction [3]float32) [3]int32 {
	axis := axisMaskX
	best := float32(-1)
	if mask&axisMaskX != 0 {
		best = absFloat32(direction[0])
		axis = axisMaskX
	}
	if mask&axisMaskY != 0 && absFloat32(direction[1]) >= best {
		best = absFloat32(direction[1])
		axis = axisMaskY
	}
	if mask&axisMaskZ != 0 && absFloat32(direction[2]) >= best {
		axis = axisMaskZ
	}
	result := [3]int32{}
	switch axis {
	case axisMaskX:
		if direction[0] > 0 {
			result[0] = -1
		} else {
			result[0] = 1
		}
	case axisMaskY:
		if direction[1] > 0 {
			result[1] = -1
		} else {
			result[1] = 1
		}
	case axisMaskZ:
		if direction[2] > 0 {
			result[2] = -1
		} else {
			result[2] = 1
		}
	}
	return result
}

func axisMaskForAxis(axis int) int {
	switch axis {
	case 0:
		return axisMaskX
	case 1:
		return axisMaskY
	case 2:
		return axisMaskZ
	default:
		return 0
	}
}

func nearlyEqualFloat32(a, b float32) bool {
	return absFloat32(a-b) <= rayHitEpsilon
}

func nearlyZeroFloat32(value float32) bool {
	return absFloat32(value) <= rayHitEpsilon
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func clampFloat32(value, minValue, maxValue float32) float32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func signStep(value float32) int {
	if value > 0 {
		return 1
	}
	if value < 0 {
		return -1
	}
	return 0
}

func isZeroDirection(direction [3]float32) bool {
	return nearlyZeroFloat32(direction[0]) && nearlyZeroFloat32(direction[1]) && nearlyZeroFloat32(direction[2])
}
