package generators

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"sort"

	"Gogoxel/internal/world"
)

const defaultModelPath = "third_party/voxel-model/svo/buddha_16k.rsvo"

// Keep the bundled buddha example at prune level 4 instead of the much coarser
// prune level 5 while still bounding the import to a manageable explicit SVO.
const maxRSVOPackedNodes = 3_000_000

var rsvoPalette = [255]uint32{
	0,
	rgbaColor(0xD7, 0xD1, 0xC3),
}

type voxGenerator struct {
	name  string
	model voxModel
	cache *cachedSVO
}

type rsvoGenerator struct {
	name  string
	model rsvoModel
	cache *cachedSVO
}

type voxModel struct {
	sizeX   int
	sizeY   int
	sizeZ   int
	palette [255]uint32
	voxels  []voxEntry
}

type rsvoModel struct {
	topLevel   int
	nodeCounts []uint32
	levels     []rsvoLevel
	bounds     solidBounds
	palette    [255]uint32
}

type rsvoLevel struct {
	masks []byte
	rank  []uint32
}

type voxEntry struct {
	x     int
	y     int
	z     int
	color uint8
}

type rawVOXEntry struct {
	x          int
	y          int
	z          int
	colorIndex uint8
}

type rawVOXModel struct {
	sizeX  int
	sizeY  int
	sizeZ  int
	voxels []rawVOXEntry
}

type rsvoNode struct {
	level     int
	nodeIndex int
	minX      int
	minY      int
	minZ      int
	size      int
}

type rsvoChild struct {
	bitIndex  int
	nodeIndex int
}

type rsvoPackedChild struct {
	mirroredBit int
	node        rsvoNode
}

func NewGeneratorFromFile(path string) (Generator, error) {
	data, err := readModelFile(path)
	if err != nil {
		return nil, err
	}
	return newGeneratorFromData(modelDisplayName(path), data)
}

func NewGeneratorFromBytes(data []byte) (Generator, error) {
	return newGeneratorFromData("Model", data)
}

func newGeneratorFromData(name string, data []byte) (Generator, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("model data too short")
	}

	switch string(data[:4]) {
	case "VOX ":
		return newVOXGenerator(name, data)
	case "RSVO":
		return newRSVOGenerator(name, data)
	default:
		return nil, fmt.Errorf("unsupported model format %q", string(data[:4]))
	}
}

func NewVOXGeneratorFromFile(path string) (*voxGenerator, error) {
	data, err := readModelFile(path)
	if err != nil {
		return nil, err
	}
	return newVOXGenerator(modelDisplayName(path), data)
}

func NewVOXGeneratorFromBytes(data []byte) (*voxGenerator, error) {
	return newVOXGenerator("VOX Model", data)
}

func newVOXGenerator(name string, data []byte) (*voxGenerator, error) {
	model, err := parseVOX(data)
	if err != nil {
		return nil, err
	}
	return &voxGenerator{name: name, model: model}, nil
}

func NewRSVOGeneratorFromFile(path string) (*rsvoGenerator, error) {
	data, err := readModelFile(path)
	if err != nil {
		return nil, err
	}
	return newRSVOGenerator(modelDisplayName(path), data)
}

func NewRSVOGeneratorFromBytes(data []byte) (*rsvoGenerator, error) {
	return newRSVOGenerator("RSVO Model", data)
}

func newRSVOGenerator(name string, data []byte) (*rsvoGenerator, error) {
	model, err := parseRSVO(data)
	if err != nil {
		return nil, err
	}
	return &rsvoGenerator{name: name, model: model}, nil
}

func (g *voxGenerator) Name() string {
	return g.name
}

func (g *voxGenerator) BuildSVO(svo *world.SVO) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	if g.cache != nil {
		g.cache.apply(svo)
		return nil
	}

	maxDimension := maxInt(g.model.sizeX, maxInt(g.model.sizeY, g.model.sizeZ))
	sceneSize := sceneSizeForDimension(maxDimension)
	offsetX := (int(sceneSize) - g.model.sizeX) / 2
	offsetY := (int(sceneSize) - g.model.sizeY) / 2

	svo.BuildTreeSparseFunc(sceneSize, func(add func(world.VoxelPoint)) {
		for _, voxel := range g.model.voxels {
			add(world.VoxelPoint{
				X:     uint(offsetX + voxel.x),
				Y:     uint(offsetY + voxel.y),
				Z:     uint(voxel.z),
				Color: g.model.palette[voxel.color],
			})
		}
	})

	cache := captureCache(svo)
	g.cache = &cache
	return nil
}

func (g *rsvoGenerator) Name() string {
	return g.name
}

func (g *rsvoGenerator) BuildSVO(svo *world.SVO) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	if g.cache != nil {
		g.cache.apply(svo)
		return nil
	}

	sceneSize := uint(1) << uint(g.model.topLevel)
	pruneLevel := g.model.pruneLevelForNodeBudget(maxRSVOPackedNodes)
	minBounds, maxBounds, ok := g.model.mirroredBoundsForPruneLevel(pruneLevel)
	svo.LoadPackedNodes(sceneSize, g.model.toPackedNodes(pruneLevel), minBounds, maxBounds, ok)

	cache := captureCache(svo)
	g.cache = &cache
	return nil
}

func parseVOX(data []byte) (voxModel, error) {
	parser := voxParser{data: data}

	magic, err := parser.readID()
	if err != nil {
		return voxModel{}, err
	}
	if magic != "VOX " {
		return voxModel{}, fmt.Errorf("invalid VOX header %q", magic)
	}
	if _, err := parser.readUint32(); err != nil {
		return voxModel{}, err
	}

	chunkID, contentSize, childrenSize, err := parser.readChunkHeader()
	if err != nil {
		return voxModel{}, err
	}
	if chunkID != "MAIN" {
		return voxModel{}, fmt.Errorf("expected MAIN chunk, got %q", chunkID)
	}

	contentEnd := parser.pos + contentSize
	childrenEnd := contentEnd + childrenSize
	if childrenEnd > len(data) {
		return voxModel{}, fmt.Errorf("MAIN chunk exceeds file size")
	}
	parser.pos = contentEnd

	currentSize := rawVOXModel{}
	haveSize := false
	bestModel := rawVOXModel{}
	var sourcePalette [256]uint32

	for parser.pos < childrenEnd {
		chunkID, contentSize, childrenSize, err = parser.readChunkHeader()
		if err != nil {
			return voxModel{}, err
		}

		contentStart := parser.pos
		contentEnd = contentStart + contentSize
		childrenChunkEnd := contentEnd + childrenSize
		if childrenChunkEnd > childrenEnd {
			return voxModel{}, fmt.Errorf("chunk %q exceeds MAIN bounds", chunkID)
		}

		switch chunkID {
		case "PACK":
			if _, err := parser.readUint32(); err != nil {
				return voxModel{}, err
			}
		case "SIZE":
			sizeX, err := parser.readUint32()
			if err != nil {
				return voxModel{}, err
			}
			sizeY, err := parser.readUint32()
			if err != nil {
				return voxModel{}, err
			}
			sizeZ, err := parser.readUint32()
			if err != nil {
				return voxModel{}, err
			}
			currentSize = rawVOXModel{sizeX: int(sizeX), sizeY: int(sizeY), sizeZ: int(sizeZ)}
			haveSize = true
		case "XYZI":
			if !haveSize {
				return voxModel{}, fmt.Errorf("XYZI chunk encountered before SIZE")
			}
			voxelCount, err := parser.readUint32()
			if err != nil {
				return voxModel{}, err
			}
			model := rawVOXModel{sizeX: currentSize.sizeX, sizeY: currentSize.sizeY, sizeZ: currentSize.sizeZ, voxels: make([]rawVOXEntry, 0, voxelCount)}
			for voxelIndex := uint32(0); voxelIndex < voxelCount; voxelIndex++ {
				xValue, err := parser.readByte()
				if err != nil {
					return voxModel{}, err
				}
				yValue, err := parser.readByte()
				if err != nil {
					return voxModel{}, err
				}
				zValue, err := parser.readByte()
				if err != nil {
					return voxModel{}, err
				}
				colorIndex, err := parser.readByte()
				if err != nil {
					return voxModel{}, err
				}
				if colorIndex == 0 {
					continue
				}
				model.voxels = append(model.voxels, rawVOXEntry{
					x:          int(xValue),
					y:          int(yValue),
					z:          int(zValue),
					colorIndex: colorIndex,
				})
			}
			if len(model.voxels) > len(bestModel.voxels) {
				bestModel = model
			}
			haveSize = false
		case "RGBA":
			for paletteIndex := 1; paletteIndex < len(sourcePalette) && parser.pos+4 <= contentEnd; paletteIndex++ {
				color, err := parser.readUint32()
				if err != nil {
					return voxModel{}, err
				}
				sourcePalette[paletteIndex] = ensureOpaqueColor(color)
			}
		}

		parser.pos = childrenChunkEnd
	}

	if len(bestModel.voxels) == 0 {
		return voxModel{}, fmt.Errorf("no voxel model found in VOX data")
	}

	return compressVOXModel(bestModel, sourcePalette), nil
}

func parseRSVO(data []byte) (rsvoModel, error) {
	parser := voxParser{data: data}

	magic, err := parser.readID()
	if err != nil {
		return rsvoModel{}, err
	}
	if magic != "RSVO" {
		return rsvoModel{}, fmt.Errorf("invalid RSVO header %q", magic)
	}
	version, err := parser.readUint32()
	if err != nil {
		return rsvoModel{}, err
	}
	if version != 1 {
		return rsvoModel{}, fmt.Errorf("unsupported RSVO version %d", version)
	}
	if _, err := parser.readUint32(); err != nil {
		return rsvoModel{}, err
	}
	if _, err := parser.readUint32(); err != nil {
		return rsvoModel{}, err
	}
	topLevelValue, err := parser.readUint32()
	if err != nil {
		return rsvoModel{}, err
	}
	topLevel := int(topLevelValue)
	if topLevel < 0 || topLevel > 30 {
		return rsvoModel{}, fmt.Errorf("invalid RSVO top level %d", topLevel)
	}

	nodeCounts := make([]uint32, topLevel+1)
	for level := topLevel; level >= 0; level-- {
		count, err := parser.readUint32()
		if err != nil {
			return rsvoModel{}, err
		}
		nodeCounts[level] = count
	}
	if nodeCounts[topLevel] != 1 {
		return rsvoModel{}, fmt.Errorf("invalid RSVO root node count %d", nodeCounts[topLevel])
	}

	levels := make([]rsvoLevel, topLevel+1)
	for level := topLevel; level >= 1; level-- {
		count := int(nodeCounts[level])
		if parser.pos+count > len(data) {
			return rsvoModel{}, fmt.Errorf("RSVO level %d mask data exceeds file size", level)
		}
		masks := data[parser.pos : parser.pos+count]
		parser.pos += count
		rank := buildRSVORank(masks)
		if rank[len(rank)-1] != nodeCounts[level-1] {
			return rsvoModel{}, fmt.Errorf("RSVO level %d child count mismatch: got %d want %d", level, rank[len(rank)-1], nodeCounts[level-1])
		}
		levels[level] = rsvoLevel{masks: masks, rank: rank}
	}
	if parser.pos != len(data) {
		remaining := bytes.TrimSpace(data[parser.pos:])
		if len(remaining) > 0 {
			return rsvoModel{}, fmt.Errorf("unexpected trailing RSVO data")
		}
	}

	model := rsvoModel{topLevel: topLevel, nodeCounts: nodeCounts, levels: levels, palette: rsvoPalette}
	bounds, err := model.computeBounds()
	if err != nil {
		return rsvoModel{}, err
	}
	model.bounds = bounds
	return model, nil
}

type voxParser struct {
	data []byte
	pos  int
}

func (p *voxParser) readID() (string, error) {
	if p.pos+4 > len(p.data) {
		return "", fmt.Errorf("unexpected EOF reading chunk id")
	}
	value := string(p.data[p.pos : p.pos+4])
	p.pos += 4
	return value, nil
}

func (p *voxParser) readUint32() (uint32, error) {
	if p.pos+4 > len(p.data) {
		return 0, fmt.Errorf("unexpected EOF reading uint32")
	}
	value := binary.LittleEndian.Uint32(p.data[p.pos : p.pos+4])
	p.pos += 4
	return value, nil
}

func (p *voxParser) readByte() (byte, error) {
	if p.pos >= len(p.data) {
		return 0, fmt.Errorf("unexpected EOF reading byte")
	}
	value := p.data[p.pos]
	p.pos++
	return value, nil
}

func (p *voxParser) readChunkHeader() (string, int, int, error) {
	id, err := p.readID()
	if err != nil {
		return "", 0, 0, err
	}
	contentSize, err := p.readUint32()
	if err != nil {
		return "", 0, 0, err
	}
	childrenSize, err := p.readUint32()
	if err != nil {
		return "", 0, 0, err
	}
	return id, int(contentSize), int(childrenSize), nil
}

func (m rsvoModel) computeBounds() (solidBounds, error) {
	minX, err := m.findExtreme(0, false)
	if err != nil {
		return solidBounds{}, err
	}
	maxX, err := m.findExtreme(0, true)
	if err != nil {
		return solidBounds{}, err
	}
	minY, err := m.findExtreme(1, false)
	if err != nil {
		return solidBounds{}, err
	}
	maxY, err := m.findExtreme(1, true)
	if err != nil {
		return solidBounds{}, err
	}
	minZ, err := m.findExtreme(2, false)
	if err != nil {
		return solidBounds{}, err
	}
	maxZ, err := m.findExtreme(2, true)
	if err != nil {
		return solidBounds{}, err
	}
	return solidBounds{minX: minX, maxX: maxX, minY: minY, maxY: maxY, minZ: minZ, maxZ: maxZ}, nil
}

func (m rsvoModel) findExtreme(axis int, wantMax bool) (int, error) {
	rootSize := 1 << uint(m.topLevel)
	return m.findExtremeFromNode(rsvoNode{level: m.topLevel, nodeIndex: 0, minX: 0, minY: 0, minZ: 0, size: rootSize}, axis, wantMax)
}

func (m rsvoModel) findExtremeFromNode(node rsvoNode, axis int, wantMax bool) (int, error) {
	if node.level == 0 {
		switch axis {
		case 0:
			return node.minX, nil
		case 1:
			return node.minY, nil
		default:
			return node.minZ, nil
		}
	}

	mask := m.levels[node.level].masks[node.nodeIndex]
	if mask == 0 {
		return 0, fmt.Errorf("empty RSVO internal node at level %d index %d", node.level, node.nodeIndex)
	}
	childBase := m.childBase(node.level, node.nodeIndex)
	childSize := node.size / 2
	children := m.presentChildren(mask, childBase)
	preferredBit := 0
	if wantMax {
		preferredBit = 1
	}

	for pass := 0; pass < 2; pass++ {
		wanted := preferredBit
		if pass == 1 {
			wanted = 1 - preferredBit
		}
		for _, child := range children {
			if rsvoChildAxisBit(child.bitIndex, axis) != wanted {
				continue
			}
			next := rsvoNode{
				level:     node.level - 1,
				nodeIndex: child.nodeIndex,
				minX:      node.minX + (child.bitIndex&1)*childSize,
				minY:      node.minY + ((child.bitIndex>>1)&1)*childSize,
				minZ:      node.minZ + ((child.bitIndex>>2)&1)*childSize,
				size:      childSize,
			}
			return m.findExtremeFromNode(next, axis, wantMax)
		}
	}

	return 0, fmt.Errorf("no occupied RSVO child found at level %d index %d", node.level, node.nodeIndex)
}

func (m rsvoModel) mirroredBounds() (min, max [3]uint32, ok bool) {
	if !m.bounds.valid() {
		return [3]uint32{}, [3]uint32{}, false
	}

	rootSize := 1 << uint(m.topLevel)
	return [3]uint32{uint32(m.bounds.minX), uint32(m.bounds.minY), uint32(rootSize - (m.bounds.maxZ + 1))},
		[3]uint32{uint32(m.bounds.maxX + 1), uint32(m.bounds.maxY + 1), uint32(rootSize - m.bounds.minZ)},
		true
}

func (m rsvoModel) mirroredBoundsForPruneLevel(pruneLevel int) (min, max [3]uint32, ok bool) {
	min, max, ok = m.mirroredBounds()
	if !ok {
		return [3]uint32{}, [3]uint32{}, false
	}

	if pruneLevel <= 0 {
		return min, max, true
	}

	rootSize := uint32(1) << uint(m.topLevel)
	cellSize := uint32(1) << uint(pruneLevel)
	for axis := range min {
		min[axis] = alignDownUint32(min[axis], cellSize)
		max[axis] = alignUpUint32(max[axis], cellSize)
		if max[axis] > rootSize {
			max[axis] = rootSize
		}
	}

	return min, max, true
}

func (m rsvoModel) pruneLevelForNodeBudget(maxNodes int) int {
	if maxNodes <= 1 {
		return m.topLevel
	}

	totalNodes := 0
	for level := m.topLevel; level >= 0; level-- {
		nextTotal := totalNodes + int(m.nodeCounts[level])
		if nextTotal > maxNodes {
			if level == m.topLevel {
				return m.topLevel
			}
			return level + 1
		}
		totalNodes = nextTotal
	}

	return 0
}

func (m rsvoModel) toPackedNodes(pruneLevel int) []world.PackedNode {
	totalNodes := 0
	for level, count := range m.nodeCounts {
		if level < pruneLevel {
			continue
		}
		totalNodes += int(count)
	}
	if totalNodes == 0 {
		return nil
	}

	nodes := make([]world.PackedNode, 0, totalNodes)
	rootSize := 1 << uint(m.topLevel)
	m.appendPackedNode(&nodes, rsvoNode{level: m.topLevel, nodeIndex: 0, minX: 0, minY: 0, minZ: 0, size: rootSize}, pruneLevel)
	return nodes
}

func (m rsvoModel) appendPackedNode(nodes *[]world.PackedNode, node rsvoNode, pruneLevel int) uint32 {
	nodeIndex := uint32(len(*nodes))
	*nodes = append(*nodes, world.PackedNode{})
	m.fillPackedNode(nodes, nodeIndex, node, pruneLevel)
	return nodeIndex
}

func (m rsvoModel) fillPackedNode(nodes *[]world.PackedNode, nodeIndex uint32, node rsvoNode, pruneLevel int) {
	if node.level <= pruneLevel {
		(*nodes)[nodeIndex] = packLeafNode(m.palette[1])
		return
	}

	mask := m.levels[node.level].masks[node.nodeIndex]
	if mask == 0 {
		(*nodes)[nodeIndex] = world.PackedNode{}
		return
	}

	childBase := m.childBase(node.level, node.nodeIndex)
	childSize := node.size / 2
	childOffset := 0
	children := make([]rsvoPackedChild, 0, bits.OnesCount8(mask))
	for bitIndex := 0; bitIndex < 8; bitIndex++ {
		if mask&(1<<uint(bitIndex)) == 0 {
			continue
		}
		children = append(children, rsvoPackedChild{
			mirroredBit: mirrorRSVOBitZ(bitIndex),
			node: rsvoNode{
				level:     node.level - 1,
				nodeIndex: childBase + childOffset,
				minX:      node.minX + (bitIndex&1)*childSize,
				minY:      node.minY + ((bitIndex>>1)&1)*childSize,
				minZ:      node.minZ + ((bitIndex>>2)&1)*childSize,
				size:      childSize,
			},
		})
		childOffset++
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].mirroredBit < children[j].mirroredBit
	})

	packed := world.PackedNode{}
	for _, child := range children {
		packed.ChildMaskAndColor |= 1 << uint(child.mirroredBit)
	}
	if len(children) == 0 {
		(*nodes)[nodeIndex] = packed
		return
	}

	baseChildPointer := uint32(len(*nodes))
	packed.ChildPointer = baseChildPointer
	(*nodes)[nodeIndex] = packed
	for range children {
		*nodes = append(*nodes, world.PackedNode{})
	}
	for childIndex, child := range children {
		m.fillPackedNode(nodes, baseChildPointer+uint32(childIndex), child.node, pruneLevel)
	}
}

func (m rsvoModel) childBase(level, nodeIndex int) int {
	if level <= 0 {
		return 0
	}
	info := m.levels[level]
	block := nodeIndex / rsvoRankBlockSize
	total := info.rank[block]
	start := block * rsvoRankBlockSize
	for index := start; index < nodeIndex; index++ {
		total += uint32(bits.OnesCount8(info.masks[index]))
	}
	return int(total)
}

func (m rsvoModel) presentChildren(mask byte, childBase int) []rsvoChild {
	children := make([]rsvoChild, 0, bits.OnesCount8(mask))
	childOffset := 0
	for bitIndex := 0; bitIndex < 8; bitIndex++ {
		if mask&(1<<uint(bitIndex)) == 0 {
			continue
		}
		children = append(children, rsvoChild{bitIndex: bitIndex, nodeIndex: childBase + childOffset})
		childOffset++
	}
	return children
}

func rsvoChildAxisBit(bitIndex, axis int) int {
	return (bitIndex >> uint(axis)) & 1
}

func mirrorRSVOBitZ(bitIndex int) int {
	return bitIndex ^ 0x4
}

func packLeafNode(color uint32) world.PackedNode {
	return world.PackedNode{ChildMaskAndColor: ((color & 0xFFFFFF) << 8) | 1}
}

func alignDownUint32(value, alignment uint32) uint32 {
	if alignment <= 1 {
		return value
	}
	return value &^ (alignment - 1)
}

func alignUpUint32(value, alignment uint32) uint32 {
	if alignment <= 1 {
		return value
	}
	mask := alignment - 1
	return (value + mask) &^ mask
}

const rsvoRankBlockSize = 1024

func buildRSVORank(masks []byte) []uint32 {
	blockCount := (len(masks) + rsvoRankBlockSize - 1) / rsvoRankBlockSize
	rank := make([]uint32, blockCount+1)
	var total uint32
	for block := 0; block < blockCount; block++ {
		rank[block] = total
		start := block * rsvoRankBlockSize
		end := minInt(len(masks), start+rsvoRankBlockSize)
		for _, mask := range masks[start:end] {
			total += uint32(bits.OnesCount8(mask))
		}
	}
	rank[blockCount] = total
	return rank
}

func compressVOXModel(rawModel rawVOXModel, sourcePalette [256]uint32) voxModel {
	model := voxModel{
		sizeX:  rawModel.sizeX,
		sizeY:  rawModel.sizeY,
		sizeZ:  rawModel.sizeZ,
		voxels: make([]voxEntry, 0, len(rawModel.voxels)),
	}

	colorToPaletteIndex := map[uint32]uint8{}
	nextPaletteIndex := uint8(1)
	for _, voxel := range rawModel.voxels {
		color := resolveVOXColor(voxel.colorIndex, sourcePalette)
		paletteIndex, ok := colorToPaletteIndex[color]
		if !ok {
			if nextPaletteIndex < uint8(len(model.palette)) {
				paletteIndex = nextPaletteIndex
				model.palette[paletteIndex] = color
				nextPaletteIndex++
			} else {
				paletteIndex = nearestPaletteIndex(model.palette, color, nextPaletteIndex)
			}
			colorToPaletteIndex[color] = paletteIndex
		}

		model.voxels = append(model.voxels, voxEntry{
			x:     voxel.x,
			y:     voxel.y,
			z:     voxel.z,
			color: paletteIndex,
		})
	}

	return model
}

func resolveVOXColor(colorIndex uint8, sourcePalette [256]uint32) uint32 {
	if colorIndex == 0 {
		return 0
	}
	color := ensureOpaqueColor(sourcePalette[colorIndex])
	if color != 0 {
		return color
	}
	return grayscaleColor(colorIndex)
}

func ensureOpaqueColor(color uint32) uint32 {
	if color == 0 {
		return 0
	}
	if color>>24 == 0 {
		return color | 0xFF000000
	}
	return color
}

func nearestPaletteIndex(palette [255]uint32, color uint32, paletteCount uint8) uint8 {
	bestIndex := uint8(1)
	bestDistance := paletteColorDistance(palette[1], color)
	for paletteIndex := uint8(2); paletteIndex < paletteCount; paletteIndex++ {
		distance := paletteColorDistance(palette[paletteIndex], color)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = paletteIndex
		}
	}
	return bestIndex
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

func readModelFile(path string) ([]byte, error) {
	resolvedPath, err := resolveModelPath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read model file %q: %w", resolvedPath, err)
	}

	return data, nil
}

func resolveModelPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve model path %q: %w", path, err)
	}

	currentDir := workingDir
	for {
		candidate := filepath.Join(currentDir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}
		currentDir = parentDir
	}

	return "", fmt.Errorf("model file %q not found from %q", path, workingDir)
}
