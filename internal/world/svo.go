package world

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

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
	// Voxels is a pointer so that assignEditableBrickIndices can copy bricks
	// without a 512-byte memcpy for every unchanged brick in the scene.
	// Copy-on-write in editVoxelNode ensures pointer identity detects changes.
	Voxels *[BrickVoxelCount]uint8
}

type Snapshot struct {
	Words             []uint32
	Bricks            []Brick
	OccupiedMin       [3]uint32
	OccupiedMax       [3]uint32
	HasOccupiedBounds bool
}

type editApplyMode uint8

const (
	editApplyModeNone editApplyMode = iota
	editApplyModeIncremental
	editApplyModeFullRebuild
)

type editStats struct {
	mode          editApplyMode
	touchedBricks int
}

type SVO struct {
	nodes []SvoNode
	size  uint

	palette         [PaletteSize]uint32
	bricks          []Brick
	storageWords    []uint32
	colorToMaterial map[uint32]uint8
	brickNodeLookup map[uint32]int
	editableRoot    *stagingNode

	occupiedMin       [3]uint
	occupiedMax       [3]uint
	hasOccupiedBounds bool
	lastEdit          editStats
}

// SvoNode is the on-GPU SVO node. The first word (Payload) packs different
// meanings depending on node kind:
//   - Branch:     low 8 bits = child mask
//   - SolidLeaf:  bits 8..30 = materialID; low 8 bits = 1 (sentinel)
//   - BrickLeaf:  bit 31 set (BrickLeafFlag); ChildPointer holds the brick slot
//
// The shader-side SVONode struct mirrors this layout (see raytracer.frag).
type SvoNode struct {
	payload      uint32
	childPointer uint32
}

func (n *SvoNode) setBranch(mask uint8) {
	n.payload = uint32(mask)
	n.childPointer = 0
}

func (n *SvoNode) setSolidLeaf(materialID uint8) {
	n.payload = uint32(materialID)<<8 | 1
	n.childPointer = 0
}

func (n *SvoNode) setBrickLeaf(slot uint32) {
	n.payload = BrickLeafFlag
	n.childPointer = slot
}

func (n SvoNode) childMask() uint8 {
	return uint8(n.payload & childMaskMask)
}

func (n SvoNode) materialID() uint8 {
	return uint8((n.payload >> 8) & 0x7FFFFF)
}

func (n SvoNode) isBrickLeaf() bool {
	return n.payload&BrickLeafFlag != 0
}

func (n SvoNode) isSolidLeaf() bool {
	return !n.isBrickLeaf() && n.childPointer == 0 && n.childMask() == 1 && n.materialID() != 0
}

type stagingNode struct {
	SvoNode
	tempChildren [8]*stagingNode
	brickIndex   int
	brickVoxels  *[BrickVoxelCount]uint8
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

func (s *SVO) BuildTreeSparseVolumes(size uint, emit func(addVoxel func(x, y, z uint, color uint32), addCube func(x, y, z, cubeSize uint, color uint32))) {
	s.beginBuild(size)

	var root *stagingNode
	if emit != nil {
		emit(
			func(x, y, z uint, color uint32) {
				s.addVolumeVoxel(&root, x, y, z, color)
			},
			func(x, y, z, cubeSize uint, color uint32) {
				s.addVolumeCube(&root, x, y, z, cubeSize, color)
			},
		)
	}

	if root == nil {
		s.nodes = make([]SvoNode, 1)
		s.rebuildStorageWords()
		s.colorToMaterial = nil
		s.brickNodeLookup = nil
		s.editableRoot = nil
		return
	}

	var brickJobs []*stagingNode
	s.compactSparseVolume(root, s.size, 0, 0, 0, &brickJobs)
	if len(brickJobs) > 0 {
		s.fillBrickJobsParallel(brickJobs)
	}
	s.nodes = s.nodes[:0]
	s.editableRoot = root
	s.flattenTree(root)
	s.colorToMaterial = nil
	s.rebuildBrickNodeLookup()
	s.rebuildStorageWords()
}

func (s *SVO) LoadStorageBufferWords(words []uint32, occupiedMin, occupiedMax [3]uint32, hasOccupiedBounds bool) error {
	if len(words) < storageWordCount {
		return fmt.Errorf("storage buffer words must include at least the size header")
	}
	s.lastEdit = editStats{}

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
				payload:      words[wordIndex],
				childPointer: words[wordIndex+1],
			}
		}
	}

	clear(s.palette[:])
	if len(words) >= paletteOffset+PaletteSize {
		copy(s.palette[:], words[paletteOffset:paletteOffset+PaletteSize])
	}

	s.bricks = nil
	s.colorToMaterial = nil
	s.brickNodeLookup = nil
	s.editableRoot = nil
	s.hasOccupiedBounds = hasOccupiedBounds
	if hasOccupiedBounds {
		s.occupiedMin = [3]uint{uint(occupiedMin[0]), uint(occupiedMin[1]), uint(occupiedMin[2])}
		s.occupiedMax = [3]uint{uint(occupiedMax[0]), uint(occupiedMax[1]), uint(occupiedMax[2])}
		s.rebuildStorageWords()
		return nil
	}

	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.rebuildStorageWords()
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
	s.rebuildBrickNodeLookup()
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

func (s *SVO) LastEditUsedFullRebuild() bool {
	return s != nil && s.lastEdit.mode == editApplyModeFullRebuild
}

func (s *SVO) beginBuild(size uint) map[uint64]*stagingNode {
	s.size = octreeSize(size)
	s.nodes = s.nodes[:0]
	clear(s.palette[:])
	s.bricks = nil
	s.storageWords = nil
	s.colorToMaterial = make(map[uint32]uint8)
	s.brickNodeLookup = nil
	s.editableRoot = nil
	s.occupiedMin = [3]uint{}
	s.occupiedMax = [3]uint{}
	s.hasOccupiedBounds = false
	s.lastEdit = editStats{}
	return make(map[uint64]*stagingNode)
}

func (s *SVO) finishBuild(leafLayer map[uint64]*stagingNode) {
	s.deriveOccupiedBounds(leafLayer)
	s.buildFromLeafLayer(leafLayer)
	s.colorToMaterial = nil
	s.rebuildStorageWords()
}

func (s *SVO) rebuildStorageWords() {
	if s == nil || len(s.nodes) == 0 {
		s.storageWords = nil
		return
	}

	required := storageWordCount + len(s.nodes)*2 + PaletteSize
	if cap(s.storageWords) < required {
		s.storageWords = make([]uint32, required)
	} else {
		s.storageWords = s.storageWords[:required]
	}
	s.storageWords[0] = uint32(s.size)
	s.storageWords[1] = uint32(len(s.nodes))
	for index, node := range s.nodes {
		wordIndex := storageWordCount + index*2
		s.storageWords[wordIndex] = node.payload
		s.storageWords[wordIndex+1] = node.childPointer
	}
	copy(s.storageWords[storageWordCount+len(s.nodes)*2:], s.palette[:])
}

func (s *SVO) syncStorageWordsPalette() {
	if s == nil || len(s.nodes) == 0 {
		return
	}
	required := storageWordCount + len(s.nodes)*2 + PaletteSize
	if len(s.storageWords) < required {
		s.rebuildStorageWords()
		return
	}
	copy(s.storageWords[storageWordCount+len(s.nodes)*2:], s.palette[:])
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

func (s *SVO) extendOccupiedBounds(x, y, z, span uint) {
	if span == 0 {
		return
	}

	maxX := x + span
	maxY := y + span
	maxZ := z + span
	if !s.hasOccupiedBounds {
		s.occupiedMin = [3]uint{x, y, z}
		s.occupiedMax = [3]uint{maxX, maxY, maxZ}
		s.hasOccupiedBounds = true
		return
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
	if maxX > s.occupiedMax[0] {
		s.occupiedMax[0] = maxX
	}
	if maxY > s.occupiedMax[1] {
		s.occupiedMax[1] = maxY
	}
	if maxZ > s.occupiedMax[2] {
		s.occupiedMax[2] = maxZ
	}
}

func (s *SVO) addVolumeVoxel(root **stagingNode, x, y, z uint, color uint32) {
	s.addVolumeCube(root, x, y, z, 1, color)
}

func (s *SVO) addVolumeCube(root **stagingNode, x, y, z, cubeSize uint, color uint32) {
	if cubeSize == 0 || color == 0 {
		return
	}
	if cubeSize > s.size || x+cubeSize > s.size || y+cubeSize > s.size || z+cubeSize > s.size {
		return
	}
	if cubeSize&(cubeSize-1) != 0 {
		return
	}
	if x%cubeSize != 0 || y%cubeSize != 0 || z%cubeSize != 0 {
		return
	}

	materialID := s.materialForColor(color)
	if *root == nil {
		*root = newStagingNode()
	}
	s.insertVolumeCube(*root, 0, 0, 0, s.size, x, y, z, cubeSize, materialID)
	s.extendOccupiedBounds(x, y, z, cubeSize)
}

func (s *SVO) insertVolumeCube(node *stagingNode, originX, originY, originZ, nodeSize, cubeX, cubeY, cubeZ, cubeSize uint, materialID uint8) {
	if node == nil || cubeSize == 0 || cubeSize > nodeSize {
		return
	}
	if cubeSize == nodeSize {
		node.tempChildren = [8]*stagingNode{}
		node.brickIndex = -1
		node.setSolidLeaf(materialID)
		return
	}

	halfSize := nodeSize >> 1
	if halfSize == 0 {
		return
	}

	octant := 0
	childOriginX := originX
	childOriginY := originY
	childOriginZ := originZ
	if cubeX >= originX+halfSize {
		octant |= 1
		childOriginX += halfSize
	}
	if cubeY >= originY+halfSize {
		octant |= 2
		childOriginY += halfSize
	}
	if cubeZ >= originZ+halfSize {
		octant |= 4
		childOriginZ += halfSize
	}

	child := node.tempChildren[octant]
	if child == nil {
		child = newStagingNode()
		node.tempChildren[octant] = child
	}
	s.insertVolumeCube(child, childOriginX, childOriginY, childOriginZ, halfSize, cubeX, cubeY, cubeZ, cubeSize, materialID)
}

func (s *SVO) compactSparseVolume(node *stagingNode, nodeSize, originX, originY, originZ uint, brickJobs *[]*stagingNode) {
	if node == nil || node.isSolidLeaf() {
		return
	}

	childSize := nodeSize >> 1
	if childSize == 0 {
		return
	}

	for octant, child := range node.tempChildren {
		if child == nil {
			continue
		}
		childOriginX := originX
		childOriginY := originY
		childOriginZ := originZ
		if (octant & 1) != 0 {
			childOriginX += childSize
		}
		if (octant & 2) != 0 {
			childOriginY += childSize
		}
		if (octant & 4) != 0 {
			childOriginZ += childSize
		}
		s.compactSparseVolume(child, childSize, childOriginX, childOriginY, childOriginZ, brickJobs)
	}

	if nodeSize == BrickSize {
		_, activeCount, uniformMaterialID, canCollapse := summarizeParent(node)
		if canCollapse && activeCount == 8 {
			node.tempChildren = [8]*stagingNode{}
			node.brickIndex = -1
			node.setSolidLeaf(uniformMaterialID)
			return
		}

		brick := Brick{Origin: [3]uint32{uint32(originX), uint32(originY), uint32(originZ)}}
		node.brickIndex = len(s.bricks)
		s.bricks = append(s.bricks, brick)
		*brickJobs = append(*brickJobs, node)
		return
	}

	s.finalizeParent(node)
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

		// First pass: classify each parent. At the brick layer we either
		// collapse to a solid leaf (serial) or reserve a brick slot and queue
		// the voxel-fill for parallel execution. Other layers run serially.
		var brickJobs []*stagingNode
		for key, parent := range parentLayer {
			if currentSize == BrickSize {
				_, activeCount, uniformMaterialID, canCollapse := summarizeParent(parent)
				if canCollapse && activeCount == 8 {
					parent.tempChildren = [8]*stagingNode{}
					parent.setSolidLeaf(uniformMaterialID)
					continue
				}
				ox, oy, oz := voxelKeyXYZ(key)
				brickIdx := len(s.bricks)
				s.bricks = append(s.bricks, Brick{Origin: [3]uint32{uint32(ox), uint32(oy), uint32(oz)}})
				parent.brickIndex = brickIdx
				brickJobs = append(brickJobs, parent)
				continue
			}
			s.finalizeParent(parent)
		}

		// Second pass (brick layer only): fill voxels in parallel. Each job
		// writes to a distinct s.bricks[i].Voxels and a distinct *stagingNode,
		// so there is no contention. fillBrickVoxels only reads the staging
		// subtree.
		if len(brickJobs) > 0 {
			s.fillBrickJobsParallel(brickJobs)
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
		s.brickNodeLookup = nil
		s.editableRoot = nil
		return
	}

	s.nodes = make([]SvoNode, 0)
	s.editableRoot = root
	s.flattenTree(root)
	s.rebuildBrickNodeLookup()
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
	voxels := &[BrickVoxelCount]uint8{}
	brick := Brick{Origin: [3]uint32{uint32(originX), uint32(originY), uint32(originZ)}, Voxels: voxels}
	s.fillBrickVoxels(voxels, parent, BrickSize, 0, 0, 0)
	parent.tempChildren = [8]*stagingNode{}
	parent.brickVoxels = voxels
	parent.setBrickLeaf(0)
	parent.brickIndex = len(s.bricks)
	s.bricks = append(s.bricks, brick)
}

// fillBrickJobsParallel fans the per-brick voxel fill out across NumCPU
// workers. Each job mutates only its own brick slot and staging node, so no
// locking is required around the SVO state.
func (s *SVO) fillBrickJobsParallel(jobs []*stagingNode) {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}
	// Pre-allocate voxel backing arrays for all jobs; parallel workers then
	// fill them without touching each other's slice entries.
	for _, parent := range jobs {
		s.bricks[parent.brickIndex].Voxels = &[BrickVoxelCount]uint8{}
	}

	// Skip the goroutine overhead for trivially small batches.
	if workers == 1 {
		for _, parent := range jobs {
			voxels := s.bricks[parent.brickIndex].Voxels
			s.fillBrickVoxels(voxels, parent, BrickSize, 0, 0, 0)
			parent.tempChildren = [8]*stagingNode{}
			parent.brickVoxels = voxels
			parent.setBrickLeaf(0)
		}
		return
	}

	var next int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(atomic.AddInt64(&next, 1)) - 1
				if i >= len(jobs) {
					return
				}
				parent := jobs[i]
				voxels := s.bricks[parent.brickIndex].Voxels
				s.fillBrickVoxels(voxels, parent, BrickSize, 0, 0, 0)
				parent.tempChildren = [8]*stagingNode{}
				parent.brickVoxels = voxels
				parent.setBrickLeaf(0)
			}
		}()
	}
	wg.Wait()
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

	// Hot path: avoid per-node []*stagingNode allocation by reusing a fixed
	// 8-slot stack array. Each SVO node has at most 8 octant children.
	var activeChildren [8]*stagingNode
	activeCount := 0
	for octant := 0; octant < 8; octant++ {
		if root.tempChildren[octant] != nil {
			activeChildren[activeCount] = root.tempChildren[octant]
			activeCount++
		}
	}

	if activeCount == 0 {
		return
	}

	baseChildPointer := uint32(len(s.nodes))
	s.nodes[nodeIdx].childPointer = baseChildPointer

	for i := 0; i < activeCount; i++ {
		s.nodes = append(s.nodes, SvoNode{})
	}

	for childOffset := 0; childOffset < activeCount; childOffset++ {
		s.flattenTreeInto(activeChildren[childOffset], baseChildPointer+uint32(childOffset))
	}
}

func (s *SVO) StorageBufferWords() []uint32 {
	words := s.StorageBufferWordsRef()
	if len(words) == 0 {
		return nil
	}

	clone := make([]uint32, len(words))
	copy(clone, words)
	return clone
}

// StorageBufferWordsRef returns the cached storage-buffer payload backing the
// current SVO snapshot. Callers must treat the returned slice as read-only and
// must not retain it across rebuilds, reloads, or voxel edits that can refresh
// the underlying storage words.
func (s *SVO) StorageBufferWordsRef() []uint32 {
	if s == nil || len(s.nodes) == 0 {
		return nil
	}
	if len(s.storageWords) == 0 {
		s.rebuildStorageWords()
	}
	return s.storageWords
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

// Bricks returns a defensive deep copy of the per-brick voxel data. Prefer
// BricksRef when the caller can guarantee it will not mutate the returned
// slice (e.g. read-only GPU upload pipelines), which avoids a 512B memcpy
// per brick.
func (s *SVO) Bricks() []Brick {
	if s == nil {
		return nil
	}
	bricks := make([]Brick, len(s.bricks))
	copy(bricks, s.bricks)
	return bricks
}

// BricksRef returns the SVO's backing brick slice without copying. The
// caller must treat the result (and every voxel array it references) as
// read-only for the lifetime of the SVO.
func (s *SVO) BricksRef() []Brick {
	if s == nil {
		return nil
	}
	return s.bricks
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
