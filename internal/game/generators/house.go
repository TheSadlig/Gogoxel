package generators

import (
	"fmt"
	"math"

	"Gogoxel/internal/world"
)

const defaultHouseScale uint32 = 32

const (
	houseFoundationVoxel uint8 = 1
	houseWallVoxel       uint8 = 2
	houseRoofVoxel       uint8 = 3
)

var housePalette = [255]uint32{
	0,
	rgbaColor(0x65, 0x43, 0x21),
	rgbaColor(0x8B, 0x45, 0x13),
	rgbaColor(0xA5, 0x2A, 0x2A),
}

type houseGenerator struct {
	name  string
	scale uint32
	cache *cachedSVO
}

func NewHouseGenerator(scale uint32) *houseGenerator {
	if scale == 0 {
		scale = defaultHouseScale
	}
	return &houseGenerator{name: "House", scale: scale}
}

type solidBounds struct {
	minX int
	maxX int
	minY int
	maxY int
	minZ int
	maxZ int
}

type solidFunc func(x, y, z int) bool

type roofAxis int

const (
	roofAxisX roofAxis = iota
	roofAxisY
)

type roofSpec struct {
	bounds solidBounds
	axis   roofAxis
}

type houseLayout struct {
	valid            bool
	fullHeight       int
	shellBounds      solidBounds
	mainMass         solidBounds
	wingMass         solidBounds
	rearMass         solidBounds
	porchMass        solidBounds
	massPieces       []solidBounds
	foundationPieces []solidBounds
	voids            []solidBounds
	openings         []solidBounds
	partitions       []solidBounds
	roofs            []roofSpec
	chimneys         []solidBounds
}

func (g *houseGenerator) Name() string {
	return g.name
}

func (g *houseGenerator) BuildSVO(svo *world.SVO) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	if g.cache != nil {
		g.cache.apply(svo)
		return nil
	}

	sceneSize := sceneSizeForDimension(maxInt(int(g.scale*2), 64))
	layout := g.layout(int(sceneSize), int(sceneSize), int(sceneSize))
	if !layout.valid {
		svo.BuildTreeSparse(nil, sceneSize)
		cache := captureCache(svo)
		g.cache = &cache
		return nil
	}

	massSolids := make([]solidFunc, 0, len(layout.massPieces))
	for _, piece := range layout.massPieces {
		massSolids = append(massSolids, box(piece))
	}

	cutouts := make([]solidFunc, 0, len(layout.voids)+len(layout.openings))
	for _, void := range layout.voids {
		cutouts = append(cutouts, box(void))
	}
	for _, opening := range layout.openings {
		cutouts = append(cutouts, box(opening))
	}

	shell := subtract(union(massSolids...), cutouts...)
	svo.BuildTreeSparseFunc(sceneSize, func(add func(world.VoxelPoint)) {
		emitSolid(add, layout.shellBounds, shell, housePalette[houseWallVoxel])
		for _, piece := range layout.foundationPieces {
			emitSolid(add, piece, box(piece), housePalette[houseFoundationVoxel])
		}
		for _, partition := range layout.partitions {
			emitSolid(add, partition, box(partition), housePalette[houseWallVoxel])
		}
		for _, roof := range layout.roofs {
			emitSolid(add, roof.bounds, gableRoof(roof.bounds, roof.axis), housePalette[houseRoofVoxel])
		}
		for _, chimney := range layout.chimneys {
			emitSolid(add, chimney, box(chimney), housePalette[houseWallVoxel])
		}
	})

	cache := captureCache(svo)
	g.cache = &cache
	return nil
}

func (g *houseGenerator) layout(width, height, depth int) houseLayout {
	if width < 18 || height < 18 || depth < 8 {
		return houseLayout{}
	}

	fullHeight := clampInt(int(g.scale), 8, depth)
	mainRoofHeight := maxInt(3, fullHeight/4)
	if mainRoofHeight >= fullHeight {
		mainRoofHeight = fullHeight - 1
	}
	mainWallHeight := fullHeight - mainRoofHeight
	if mainWallHeight < 5 {
		mainWallHeight = 5
		mainRoofHeight = fullHeight - mainWallHeight
	}

	mainWidth := minInt(width-8, maxInt(11, fullHeight+fullHeight/3))
	mainDepth := minInt(height-10, maxInt(10, fullHeight+fullHeight/4))
	if mainWidth < 11 || mainDepth < 10 {
		return houseLayout{}
	}

	mainMinX := (width - mainWidth) / 2
	mainMaxX := mainMinX + mainWidth - 1
	mainMinY := (height - mainDepth) / 2
	mainMaxY := mainMinY + mainDepth - 1
	mainWallTop := mainWallHeight - 1
	centerX := midpoint(mainMinX, mainMaxX)

	mainMass := solidBounds{
		minX: mainMinX,
		maxX: mainMaxX,
		minY: mainMinY,
		maxY: mainMaxY,
		minZ: 1,
		maxZ: mainWallTop,
	}

	wingWidth := maxInt(7, mainWidth/2)
	wingDepth := maxInt(8, mainDepth/2+1)
	wingMinX := maxInt(1, mainMinX-wingWidth+2)
	wingMaxX := minInt(width-2, wingMinX+wingWidth-1)
	wingMinY := mainMinY + maxInt(1, mainDepth/5)
	wingMaxY := minInt(height-2, wingMinY+wingDepth-1)
	wingWallTop := maxInt(4, mainWallTop-maxInt(1, mainRoofHeight/2))
	wingMass := solidBounds{
		minX: wingMinX,
		maxX: wingMaxX,
		minY: wingMinY,
		maxY: wingMaxY,
		minZ: 1,
		maxZ: wingWallTop,
	}

	rearWidth := maxInt(6, mainWidth/3)
	rearDepth := maxInt(6, mainDepth/3)
	rearMaxX := mainMaxX - 1
	rearMinX := maxInt(mainMinX+2, rearMaxX-rearWidth+1)
	rearMinY := maxInt(1, mainMinY-rearDepth+2)
	rearMaxY := minInt(mainMaxY-2, rearMinY+rearDepth-1)
	rearWallTop := maxInt(4, mainWallTop-1)
	rearMass := solidBounds{
		minX: rearMinX,
		maxX: rearMaxX,
		minY: rearMinY,
		maxY: rearMaxY,
		minZ: 1,
		maxZ: rearWallTop,
	}

	porchWidth := maxInt(5, mainWidth/3)
	if porchWidth%2 == 0 {
		porchWidth++
	}
	porchDepth := maxInt(4, mainDepth/4)
	porchMinX := centerX - porchWidth/2
	porchMaxX := porchMinX + porchWidth - 1
	porchMinY := mainMaxY - 1
	porchMaxY := minInt(height-2, porchMinY+porchDepth-1)
	porchWallTop := maxInt(3, mainWallTop/2)
	porchMass := solidBounds{
		minX: porchMinX,
		maxX: porchMaxX,
		minY: porchMinY,
		maxY: porchMaxY,
		minZ: 1,
		maxZ: porchWallTop,
	}

	massPieces := []solidBounds{mainMass}
	for _, piece := range []solidBounds{wingMass, rearMass, porchMass} {
		if piece.valid() {
			massPieces = append(massPieces, piece)
		}
	}

	foundationPieces := make([]solidBounds, 0, len(massPieces))
	for _, piece := range massPieces {
		foundationPieces = append(foundationPieces, solidBounds{
			minX: piece.minX,
			maxX: piece.maxX,
			minY: piece.minY,
			maxY: piece.maxY,
			minZ: 0,
			maxZ: 0,
		})
	}

	voids := make([]solidBounds, 0, len(massPieces))
	for _, piece := range massPieces {
		inner := insetBounds(piece, 1, 1, 1, 1)
		if inner.valid() {
			voids = append(voids, inner)
		}
	}

	windowWidth := minInt(2, maxInt(1, mainWidth/9))
	windowHeight := minInt(3, maxInt(2, mainWallHeight/4))
	windowSill := maxInt(2, mainWallTop/2-windowHeight/2)
	doorHeight := minInt(mainWallTop-1, maxInt(3, mainWallHeight/3))
	if doorHeight < 2 {
		doorHeight = 2
	}

	openings := make([]solidBounds, 0, 32)
	openings = append(openings,
		solidBounds{
			minX: centerX - 1,
			maxX: centerX + 1,
			minY: porchMaxY,
			maxY: porchMaxY,
			minZ: 1,
			maxZ: minInt(porchWallTop-1, doorHeight),
		},
		solidBounds{
			minX: centerX - 1,
			maxX: centerX + 1,
			minY: porchMinY,
			maxY: porchMinY,
			minZ: 1,
			maxZ: doorHeight,
		},
		solidBounds{
			minX: midpoint(rearMinX, rearMaxX),
			maxX: midpoint(rearMinX, rearMaxX),
			minY: rearMinY,
			maxY: rearMinY,
			minZ: 1,
			maxZ: minInt(rearWallTop-1, maxInt(2, doorHeight-1)),
		},
	)

	openings = appendFacadeWindows(openings, mainMass, facadeFront, 2, windowWidth, windowHeight, windowSill, 2)
	openings = appendFacadeWindows(openings, mainMass, facadeBack, 3, windowWidth, windowHeight, windowSill, 2)
	openings = appendFacadeWindows(openings, mainMass, facadeLeft, 2, windowWidth, windowHeight, windowSill, 2)
	openings = appendFacadeWindows(openings, mainMass, facadeRight, 2, windowWidth, windowHeight, windowSill, 2)
	openings = appendFacadeWindows(openings, wingMass, facadeFront, 2, 1, windowHeight, windowSill, 1)
	openings = appendFacadeWindows(openings, wingMass, facadeLeft, 2, 1, windowHeight, windowSill, 1)
	openings = appendFacadeWindows(openings, rearMass, facadeBack, 2, 1, windowHeight, windowSill, 1)
	openings = appendFacadeWindows(openings, porchMass, facadeLeft, 1, 1, 2, 2, 1)
	openings = appendFacadeWindows(openings, porchMass, facadeRight, 1, 1, 2, 2, 1)

	corridorWidth := 3
	corridorMinX := centerX - corridorWidth/2
	corridorMaxX := corridorMinX + corridorWidth - 1
	crossY := midpoint(mainMinY+2, mainMaxY-2)
	partitions := []solidBounds{
		{
			minX: corridorMinX - 1,
			maxX: corridorMinX - 1,
			minY: mainMinY + 2,
			maxY: mainMaxY - 2,
			minZ: 1,
			maxZ: mainWallTop - 1,
		},
		{
			minX: corridorMaxX + 1,
			maxX: corridorMaxX + 1,
			minY: mainMinY + 2,
			maxY: mainMaxY - 2,
			minZ: 1,
			maxZ: mainWallTop - 1,
		},
		{
			minX: mainMinX + 1,
			maxX: corridorMinX - 2,
			minY: crossY,
			maxY: crossY,
			minZ: 1,
			maxZ: mainWallTop - 1,
		},
		{
			minX: corridorMaxX + 2,
			maxX: mainMaxX - 1,
			minY: crossY,
			maxY: crossY,
			minZ: 1,
			maxZ: mainWallTop - 1,
		},
		{
			minX: midpoint(wingMinX+1, wingMaxX-1),
			maxX: midpoint(wingMinX+1, wingMaxX-1),
			minY: wingMinY + 1,
			maxY: wingMaxY - 1,
			minZ: 1,
			maxZ: wingWallTop - 1,
		},
		{
			minX: rearMinX + 1,
			maxX: rearMaxX - 1,
			minY: midpoint(rearMinY+1, rearMaxY-1),
			maxY: midpoint(rearMinY+1, rearMaxY-1),
			minZ: 1,
			maxZ: rearWallTop - 1,
		},
	}

	filteredPartitions := make([]solidBounds, 0, len(partitions))
	for _, partition := range partitions {
		if partition.valid() {
			filteredPartitions = append(filteredPartitions, partition)
		}
	}

	mainRoof := roofSpec{
		bounds: solidBounds{
			minX: maxInt(0, mainMinX-1),
			maxX: minInt(width-1, mainMaxX+1),
			minY: maxInt(0, mainMinY-1),
			maxY: minInt(height-1, mainMaxY+1),
			minZ: mainWallTop + 1,
			maxZ: fullHeight - 1,
		},
		axis: roofAxisY,
	}
	wingRoof := roofSpec{
		bounds: solidBounds{
			minX: maxInt(0, wingMinX-1),
			maxX: minInt(width-1, wingMaxX+1),
			minY: maxInt(0, wingMinY-1),
			maxY: minInt(height-1, wingMaxY+1),
			minZ: wingWallTop + 1,
			maxZ: minInt(fullHeight-2, wingWallTop+maxInt(2, mainRoofHeight-1)),
		},
		axis: roofAxisX,
	}
	rearRoof := roofSpec{
		bounds: solidBounds{
			minX: maxInt(0, rearMinX-1),
			maxX: minInt(width-1, rearMaxX+1),
			minY: maxInt(0, rearMinY-1),
			maxY: minInt(height-1, rearMaxY+1),
			minZ: rearWallTop + 1,
			maxZ: minInt(fullHeight-2, rearWallTop+maxInt(2, mainRoofHeight-2)),
		},
		axis: roofAxisY,
	}
	porchRoof := roofSpec{
		bounds: solidBounds{
			minX: maxInt(0, porchMinX),
			maxX: minInt(width-1, porchMaxX),
			minY: maxInt(0, porchMinY),
			maxY: minInt(height-1, porchMaxY),
			minZ: porchWallTop + 1,
			maxZ: minInt(fullHeight-3, porchWallTop+maxInt(2, mainRoofHeight-3)),
		},
		axis: roofAxisY,
	}
	roofs := []roofSpec{mainRoof}
	for _, roof := range []roofSpec{wingRoof, rearRoof, porchRoof} {
		if roof.bounds.valid() {
			roofs = append(roofs, roof)
		}
	}

	chimneys := []solidBounds{
		{
			minX: centerX - 5,
			maxX: centerX - 4,
			minY: mainMinY + 2,
			maxY: mainMinY + 3,
			minZ: mainWallTop + 1,
			maxZ: fullHeight - 2,
		},
		{
			minX: mainMaxX - 3,
			maxX: mainMaxX - 2,
			minY: mainMinY + 3,
			maxY: mainMinY + 4,
			minZ: mainWallTop + 1,
			maxZ: fullHeight - 3,
		},
	}
	filteredChimneys := make([]solidBounds, 0, len(chimneys))
	for _, chimney := range chimneys {
		if chimney.valid() {
			filteredChimneys = append(filteredChimneys, chimney)
		}
	}

	return houseLayout{
		valid:            true,
		fullHeight:       fullHeight,
		shellBounds:      combinedBounds(massPieces...),
		mainMass:         mainMass,
		wingMass:         wingMass,
		rearMass:         rearMass,
		porchMass:        porchMass,
		massPieces:       massPieces,
		foundationPieces: foundationPieces,
		voids:            voids,
		openings:         filterValidBounds(openings),
		partitions:       filteredPartitions,
		roofs:            roofs,
		chimneys:         filteredChimneys,
	}
}

func emitSolid(add func(world.VoxelPoint), bounds solidBounds, solid solidFunc, color uint32) {
	if !bounds.valid() {
		return
	}

	for z := bounds.minZ; z <= bounds.maxZ; z++ {
		for y := bounds.minY; y <= bounds.maxY; y++ {
			for x := bounds.minX; x <= bounds.maxX; x++ {
				if solid(x, y, z) {
					add(world.VoxelPoint{X: uint(x), Y: uint(y), Z: uint(z), Color: color})
				}
			}
		}
	}
}

func box(bounds solidBounds) solidFunc {
	if !bounds.valid() {
		return func(x, y, z int) bool { return false }
	}

	return func(x, y, z int) bool {
		return x >= bounds.minX && x <= bounds.maxX &&
			y >= bounds.minY && y <= bounds.maxY &&
			z >= bounds.minZ && z <= bounds.maxZ
	}
}

func union(solids ...solidFunc) solidFunc {
	return func(x, y, z int) bool {
		for _, solid := range solids {
			if solid(x, y, z) {
				return true
			}
		}
		return false
	}
}

func subtract(base solidFunc, cutters ...solidFunc) solidFunc {
	return func(x, y, z int) bool {
		if !base(x, y, z) {
			return false
		}
		for _, cutter := range cutters {
			if cutter(x, y, z) {
				return false
			}
		}
		return true
	}
}

func gableRoof(bounds solidBounds, axis roofAxis) solidFunc {
	if !bounds.valid() {
		return func(x, y, z int) bool { return false }
	}

	roofHeight := bounds.maxZ - bounds.minZ + 1
	center := 0.5 * float64(axisMin(bounds, axis)+axisMax(bounds, axis))
	halfSpan := math.Max(1, 0.5*float64(axisMax(bounds, axis)-axisMin(bounds, axis)))

	return func(x, y, z int) bool {
		if x < bounds.minX || x > bounds.maxX || y < bounds.minY || y > bounds.maxY || z < bounds.minZ || z > bounds.maxZ {
			return false
		}

		position := float64(y)
		if axis == roofAxisX {
			position = float64(x)
		}
		normalized := 1.0 - math.Abs(position-center)/halfSpan
		if normalized < 0 {
			normalized = 0
		}

		columnHeight := 1
		if roofHeight > 1 {
			columnHeight += int(math.Round(normalized * float64(roofHeight-1)))
		}

		return z <= bounds.minZ+columnHeight-1
	}
}

type facadeFace int

const (
	facadeFront facadeFace = iota
	facadeBack
	facadeLeft
	facadeRight
)

func appendFacadeWindows(openings []solidBounds, bounds solidBounds, face facadeFace, count, windowWidth, windowHeight, sillZ, margin int) []solidBounds {
	if !bounds.valid() || count <= 0 || windowWidth <= 0 || windowHeight <= 0 {
		return openings
	}

	minZ := sillZ
	maxZ := minInt(bounds.maxZ-1, sillZ+windowHeight-1)
	if maxZ < minZ {
		return openings
	}

	slotStart := 0
	slotEnd := 0
	constantMin := 0
	constantMax := 0
	verticalFace := face == facadeFront || face == facadeBack
	if verticalFace {
		slotStart = bounds.minX + margin
		slotEnd = bounds.maxX - margin
		constantMin = bounds.maxY
		constantMax = bounds.maxY
		if face == facadeBack {
			constantMin = bounds.minY
			constantMax = bounds.minY
		}
	} else {
		slotStart = bounds.minY + margin
		slotEnd = bounds.maxY - margin
		constantMin = bounds.maxX
		constantMax = bounds.maxX
		if face == facadeLeft {
			constantMin = bounds.minX
			constantMax = bounds.minX
		}
	}

	usable := slotEnd - slotStart + 1
	required := count * windowWidth
	if usable < required {
		return openings
	}

	spacing := 0
	if count > 0 {
		spacing = (usable - required) / (count + 1)
	}
	position := slotStart + spacing
	for windowIndex := 0; windowIndex < count; windowIndex++ {
		if verticalFace {
			openings = append(openings, solidBounds{
				minX: position,
				maxX: position + windowWidth - 1,
				minY: constantMin,
				maxY: constantMax,
				minZ: minZ,
				maxZ: maxZ,
			})
		} else {
			openings = append(openings, solidBounds{
				minX: constantMin,
				maxX: constantMax,
				minY: position,
				maxY: position + windowWidth - 1,
				minZ: minZ,
				maxZ: maxZ,
			})
		}
		position += windowWidth + spacing
	}

	return openings
}

func (b solidBounds) valid() bool {
	return b.minX <= b.maxX && b.minY <= b.maxY && b.minZ <= b.maxZ
}

func insetBounds(bounds solidBounds, xInset, yInset, minZInset, maxZInset int) solidBounds {
	return solidBounds{
		minX: bounds.minX + xInset,
		maxX: bounds.maxX - xInset,
		minY: bounds.minY + yInset,
		maxY: bounds.maxY - yInset,
		minZ: bounds.minZ + minZInset,
		maxZ: bounds.maxZ - maxZInset,
	}
}

func filterValidBounds(bounds []solidBounds) []solidBounds {
	filtered := make([]solidBounds, 0, len(bounds))
	for _, bound := range bounds {
		if bound.valid() {
			filtered = append(filtered, bound)
		}
	}
	return filtered
}

func combinedBounds(bounds ...solidBounds) solidBounds {
	combined := solidBounds{}
	initialized := false
	for _, bound := range bounds {
		if !bound.valid() {
			continue
		}
		if !initialized {
			combined = bound
			initialized = true
			continue
		}
		combined.minX = minInt(combined.minX, bound.minX)
		combined.maxX = maxInt(combined.maxX, bound.maxX)
		combined.minY = minInt(combined.minY, bound.minY)
		combined.maxY = maxInt(combined.maxY, bound.maxY)
		combined.minZ = minInt(combined.minZ, bound.minZ)
		combined.maxZ = maxInt(combined.maxZ, bound.maxZ)
	}
	return combined
}

func axisMin(bounds solidBounds, axis roofAxis) int {
	if axis == roofAxisX {
		return bounds.minX
	}
	return bounds.minY
}

func axisMax(bounds solidBounds, axis roofAxis) int {
	if axis == roofAxisX {
		return bounds.maxX
	}
	return bounds.maxY
}

func midpoint(minValue, maxValue int) int {
	return (minValue + maxValue) / 2
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value, minValue, maxValue int) int {
	return minInt(maxInt(value, minValue), maxValue)
}
