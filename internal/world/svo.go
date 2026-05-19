package world

import "fmt"

const (
	PaletteSize     = 255
	BrickSize       = 8
	BrickVoxelCount = BrickSize * BrickSize * BrickSize

	BrickLeafFlag uint32 = 1 << 31

	childMaskMask    uint32 = 0xFF
	storageWordCount        = 2
)

type Brick struct {
	NodeIndex uint32
	Origin    [3]uint32
	Voxels    [BrickVoxelCount]uint8
}

type Snapshot struct {
	Words             []uint32
	Bricks            []Brick
	OccupiedMin       [3]uint32
	OccupiedMax       [3]uint32
	HasOccupiedBounds bool
}

type SVO struct {
	nodes []SvoNode
	size  uint

	palette         [PaletteSize]uint32
	bricks          []Brick
	colorToMaterial map[uint32]uint8

	occupiedMin       [3]uint
	occupiedMax       [3]uint
	hasOccupiedBounds bool
}

type SvoNode struct {
	childMaskAndColor uint32
	childPointer      uint32
}

func (n *SvoNode) setBranch(mask uint8) {
	n.childMaskAndColor = uint32(mask)
	n.childPointer = 0
}

func (n *SvoNode) setSolidLeaf(materialID uint8) {
	n.childMaskAndColor = uint32(materialID)<<8 | 1
	n.childPointer = 0
}

func (n *SvoNode) setBrickLeaf(slot uint32) {
	n.childMaskAndColor = BrickLeafFlag
	n.childPointer = slot
}

func (n SvoNode) childMask() uint8 {
	return uint8(n.childMaskAndColor & childMaskMask)
}

func (n SvoNode) materialID() uint8 {
	return uint8((n.childMaskAndColor >> 8) & 0x7FFFFF)
}

func (n SvoNode) isBrickLeaf() bool {
	return n.childMaskAndColor&BrickLeafFlag != 0
}

func (n SvoNode) isSolidLeaf() bool {
	return !n.isBrickLeaf() && n.childPointer == 0 && n.childMask() == 1 && n.materialID() != 0
}

type stagingNode struct {
	SvoNode
	tempChildren [8]*stagingNode
	brickIndex   int
}

func newStagingNode() *stagingNode {
	return &stagingNode{brickIndex: -1}
}

func NewSVO() *SVO {
	return &SVO{nodes: make([]SvoNode, 0)}
}

func (s *SVO) BuildTree(voxelGrid func(x, y, z int) (uint32, bool), size uint) {
	leafLayer := s.beginBuild(size)

	for z := 0; z < int(size); z++ {
		for y := 0; y < int(size); y++ {
			for x := 0; x < int(size); x++ {
				if color, active := voxelGrid(x, y, z); active {
					s.addLeaf(leafLayer, uint(x), uint(y), uint(z), color)
				}
			}
		}
	}

	s.finishBuild(leafLayer)
}

func (s *SVO) BuildTreeSparseFunc(size uint, emit func(add func(x, y, z uint, color uint32))) {
	leafLayer := s.beginBuild(size)
	if emit != nil {
		emit(func(x, y, z uint, color uint32) {
			s.addLeaf(leafLayer, x, y, z, color)
		})
	}
	s.finishBuild(leafLayer)
}

func (s *SVO) LoadStorageBufferWords(words []uint32, occupiedMin, occupiedMax [3]uint32, hasOccupiedBounds bool) error {
	if len(words) < storageWordCount {
		return fmt.Errorf("storage buffer words must include at least the size header")
	}

	nodeCount, paletteOffset, err := parseNodeCount(words)
	if err != nil {
		return err
	}

	s.size = octreeSize(uint(words[0]))
	if nodeCount == 0 {
		s.nodes = make([]SvoNode, 1)
	} else {
		s.nodes = make([]SvoNode, nodeCount)
		for index := range s.nodes {
			wordIndex := storageWordCount + index*2
			s.nodes[index] = SvoNode{
				childMaskAndColor: words[wordIndex],
				childPointer:      words[wordIndex+1],
			}
		}
	}

	clear(s.palette[:])
	if len(words) >= paletteOffset+PaletteSize {
		copy(s.palette[:], words[paletteOffset:paletteOffset+PaletteSize])
	}

	s.bricks = nil
	s.colorToMaterial = nil
	s.hasOccupiedBounds = hasOccupiedBounds
	if hasOccupiedBounds {
		s.occupiedMin = [3]uint{uint(occupiedMin[0]), uint(occupiedMin[1]), uint(occupiedMin[2])}
		s.occupiedMax = [3]uint{uint(occupiedMax[0]), uint(occupiedMax[1]), uint(occupiedMax[2])}
		return nil
	}

	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	return nil
}

func parseNodeCount(words []uint32) (int, int, error) {
	if len(words) < storageWordCount {
		return 0, 0, fmt.Errorf("storage buffer words must include at least the size header")
	}

	declaredNodeCount := int(words[1])
	if declaredNodeCount > 0 {
		paletteOffset := storageWordCount + declaredNodeCount*2
		if len(words) < paletteOffset {
			return 0, 0, fmt.Errorf("storage buffer words ended before %d declared nodes", declaredNodeCount)
		}
		return declaredNodeCount, paletteOffset, nil
	}

	if len(words) >= storageWordCount+PaletteSize && (len(words)-storageWordCount-PaletteSize)%2 == 0 {
		nodeCount := (len(words) - storageWordCount - PaletteSize) / 2
		return nodeCount, storageWordCount + nodeCount*2, nil
	}

	if (len(words)-storageWordCount)%2 != 0 {
		return 0, 0, fmt.Errorf("storage buffer words payload must contain an even number of node words")
	}

	nodeCount := (len(words) - storageWordCount) / 2
	return nodeCount, len(words), nil
}

func (s *SVO) LoadSnapshot(snapshot Snapshot) error {
	if err := s.LoadStorageBufferWords(snapshot.Words, snapshot.OccupiedMin, snapshot.OccupiedMax, snapshot.HasOccupiedBounds); err != nil {
		return err
	}

	s.bricks = make([]Brick, len(snapshot.Bricks))
	copy(s.bricks, snapshot.Bricks)
	return nil
}

func (s *SVO) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}

	bricks := make([]Brick, len(s.bricks))
	copy(bricks, s.bricks)
	minBounds, maxBounds, ok := s.OccupiedBounds()
	return Snapshot{
		Words:             s.StorageBufferWords(),
		Bricks:            bricks,
		OccupiedMin:       minBounds,
		OccupiedMax:       maxBounds,
		HasOccupiedBounds: ok,
	}
}

func (s *SVO) beginBuild(size uint) map[uint64]*stagingNode {
	s.size = octreeSize(size)
	s.nodes = s.nodes[:0]
	clear(s.palette[:])
	s.bricks = nil
	s.colorToMaterial = make(map[uint32]uint8)
	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.hasOccupiedBounds = false
	return make(map[uint64]*stagingNode)
}

func (s *SVO) finishBuild(leafLayer map[uint64]*stagingNode) {
	s.deriveOccupiedBounds(leafLayer)
	s.buildFromLeafLayer(leafLayer)
	s.colorToMaterial = nil
}

func (s *SVO) addLeaf(leafLayer map[uint64]*stagingNode, x, y, z uint, color uint32) {
	if x >= s.size || y >= s.size || z >= s.size {
		return
	}

	materialID := s.materialForColor(color)
	leaf := newStagingNode()
	leaf.setSolidLeaf(materialID)
	leafLayer[voxelKey(x, y, z)] = leaf
}

func (s *SVO) materialForColor(color uint32) uint8 {
	if color == 0 {
		return 0
	}
	if materialID, ok := s.colorToMaterial[color]; ok {
		return materialID
	}

	for index := 1; index < len(s.palette); index++ {
		if s.palette[index] != 0 {
			continue
		}
		materialID := uint8(index)
		s.palette[index] = color
		s.colorToMaterial[color] = materialID
		return materialID
	}

	bestMaterialID := uint8(1)
	bestDistance := paletteColorDistance(s.palette[bestMaterialID], color)
	for index := uint8(2); index < uint8(len(s.palette)); index++ {
		distance := paletteColorDistance(s.palette[index], color)
		if distance < bestDistance {
			bestDistance = distance
			bestMaterialID = index
		}
	}
	s.colorToMaterial[color] = bestMaterialID
	return bestMaterialID
}

func paletteColorDistance(a, b uint32) int {
	redA, greenA, blueA := packedColorRGB(a)
	redB, greenB, blueB := packedColorRGB(b)
	redDelta := redA - redB
	greenDelta := greenA - greenB
	blueDelta := blueA - blueB
	return redDelta*redDelta + greenDelta*greenDelta + blueDelta*blueDelta
}

func packedColorRGB(color uint32) (int, int, int) {
	return int(color & 0xFF), int((color >> 8) & 0xFF), int((color >> 16) & 0xFF)
}

func (s *SVO) deriveOccupiedBounds(leafLayer map[uint64]*stagingNode) {
	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.hasOccupiedBounds = false

	for key := range leafLayer {
		x, y, z := voxelKeyXYZ(key)
		if !s.hasOccupiedBounds {
			s.occupiedMin = [3]uint{x, y, z}
			s.occupiedMax = [3]uint{x + 1, y + 1, z + 1}
			s.hasOccupiedBounds = true
			continue
		}
		if x < s.occupiedMin[0] {
			s.occupiedMin[0] = x
		}
		if y < s.occupiedMin[1] {
			s.occupiedMin[1] = y
		}
		if z < s.occupiedMin[2] {
			s.occupiedMin[2] = z
		}
		if x+1 > s.occupiedMax[0] {
			s.occupiedMax[0] = x + 1
		}
		if y+1 > s.occupiedMax[1] {
			s.occupiedMax[1] = y + 1
		}
		if z+1 > s.occupiedMax[2] {
			s.occupiedMax[2] = z + 1
		}
	}
}

func (s *SVO) buildFromLeafLayer(leafLayer map[uint64]*stagingNode) {
	currentLayer := leafLayer
	currentSize := 1

	for currentSize < int(s.size) {
		parentLayer := make(map[uint64]*stagingNode)
		halfParentSize := currentSize
		currentSize *= 2

		for key, childNode := range currentLayer {
			cx, cy, cz := voxelKeyXYZInt(key)
			px := (cx / currentSize) * currentSize
			py := (cy / currentSize) * currentSize
			pz := (cz / currentSize) * currentSize

			parentKey := voxelKey(uint(px), uint(py), uint(pz))

			parent, exists := parentLayer[parentKey]
			if !exists {
				parent = newStagingNode()
				parentLayer[parentKey] = parent
			}

			ox := (cx - px) / halfParentSize
			oy := (cy - py) / halfParentSize
			oz := (cz - pz) / halfParentSize
			octantIdx := ox | (oy << 1) | (oz << 2)

			parent.tempChildren[octantIdx] = childNode
		}

		for key, parent := range parentLayer {
			if currentSize == BrickSize {
				s.finalizeBrickParent(parent, key)
				continue
			}
			s.finalizeParent(parent)
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

	s.nodes = make([]SvoNode, 0)
	s.flattenTree(root)
}

func (s *SVO) finalizeParent(parent *stagingNode) {
	activeMask, activeCount, uniformMaterialID, canCollapse := summarizeParent(parent)
	if canCollapse && activeCount == 8 {
		parent.tempChildren = [8]*stagingNode{}
		parent.setSolidLeaf(uniformMaterialID)
		return
	}

	parent.setBranch(activeMask)
}

func summarizeParent(parent *stagingNode) (uint8, int, uint8, bool) {
	activeCount := 0
	activeMask := uint8(0)
	uniformMaterialID := uint8(0)
	canCollapse := true

	for octant := 0; octant < 8; octant++ {
		child := parent.tempChildren[octant]
		if child == nil {
			canCollapse = false
			continue
		}

		activeMask |= 1 << uint8(octant)
		activeCount++
		if !child.isSolidLeaf() {
			canCollapse = false
			continue
		}

		childMaterialID := child.materialID()
		if uniformMaterialID == 0 {
			uniformMaterialID = childMaterialID
			continue
		}
		if childMaterialID != uniformMaterialID {
			canCollapse = false
		}
	}

	return activeMask, activeCount, uniformMaterialID, canCollapse
}

func (s *SVO) finalizeBrickParent(parent *stagingNode, key uint64) {
	_, activeCount, uniformMaterialID, canCollapse := summarizeParent(parent)
	if canCollapse && activeCount == 8 {
		parent.tempChildren = [8]*stagingNode{}
		parent.setSolidLeaf(uniformMaterialID)
		return
	}

	originX, originY, originZ := voxelKeyXYZ(key)
	brick := Brick{Origin: [3]uint32{uint32(originX), uint32(originY), uint32(originZ)}}
	s.fillBrickVoxels(&brick.Voxels, parent, BrickSize, 0, 0, 0)
	parent.tempChildren = [8]*stagingNode{}
	parent.setBrickLeaf(0)
	parent.brickIndex = len(s.bricks)
	s.bricks = append(s.bricks, brick)
}

func (s *SVO) fillBrickVoxels(voxels *[BrickVoxelCount]uint8, node *stagingNode, nodeSize, originX, originY, originZ int) {
	if node == nil {
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
		s.fillBrickVoxels(voxels, child, childSize, childOriginX, childOriginY, childOriginZ)
	}
}

func brickVoxelIndex(x, y, z int) int {
	return x + y*BrickSize + z*BrickSize*BrickSize
}

func voxelKey(x, y, z uint) uint64 {
	return uint64(x) | (uint64(y) << 20) | (uint64(z) << 40)
}

func voxelKeyXYZ(key uint64) (uint, uint, uint) {
	return uint(key & 0xFFFFF), uint((key >> 20) & 0xFFFFF), uint((key >> 40) & 0xFFFFF)
}

func voxelKeyXYZInt(key uint64) (int, int, int) {
	x, y, z := voxelKeyXYZ(key)
	return int(x), int(y), int(z)
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
	if root.brickIndex >= 0 {
		s.bricks[root.brickIndex].NodeIndex = nodeIdx
	}

	activeChildren := make([]*stagingNode, 0, 8)
	for octant := 0; octant < 8; octant++ {
		if root.tempChildren[octant] != nil {
			activeChildren = append(activeChildren, root.tempChildren[octant])
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

	words := make([]uint32, storageWordCount+len(s.nodes)*2+PaletteSize)
	words[0] = uint32(s.size)
	words[1] = uint32(len(s.nodes))
	for index, node := range s.nodes {
		wordIndex := storageWordCount + index*2
		words[wordIndex] = node.childMaskAndColor
		words[wordIndex+1] = node.childPointer
	}
	copy(words[storageWordCount+len(s.nodes)*2:], s.palette[:])
	return words
}

func (s *SVO) NodeCount() int {
	if s == nil {
		return 0
	}
	return len(s.nodes)
}

func (s *SVO) Size() uint {
	if s == nil {
		return 0
	}
	return s.size
}

func (s *SVO) Palette() [PaletteSize]uint32 {
	if s == nil {
		return [PaletteSize]uint32{}
	}
	return s.palette
}

func (s *SVO) Bricks() []Brick {
	if s == nil {
		return nil
	}
	bricks := make([]Brick, len(s.bricks))
	copy(bricks, s.bricks)
	return bricks
}

func (s *SVO) BrickCount() int {
	if s == nil {
		return 0
	}
	return len(s.bricks)
}

func (s *SVO) OccupiedBounds() (min, max [3]uint32, ok bool) {
	if s == nil || !s.hasOccupiedBounds {
		return [3]uint32{}, [3]uint32{}, false
	}

	return [3]uint32{uint32(s.occupiedMin[0]), uint32(s.occupiedMin[1]), uint32(s.occupiedMin[2])},
		[3]uint32{uint32(s.occupiedMax[0]), uint32(s.occupiedMax[1]), uint32(s.occupiedMax[2])},
		true
}
