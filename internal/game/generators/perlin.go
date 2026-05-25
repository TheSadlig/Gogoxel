package generators

import (
	"container/list"
	"fmt"
	"math"
	rand "math/rand/v2"

	"Gogoxel/internal/world"
)

const (
	perlinChunkSize        uint    = 128
	perlinTerrainScale     float64 = 1024
	perlinMaxCaveDepth     int     = 48
	perlinChunkCacheLimit          = 64
	perlinChunkMapRevision         = "2026-05-24-terrain-v2"
)

type perlinGenerator struct {
	name          string
	sceneSize     uint
	terrainScale  float64
	seed1         uint64
	seed2         uint64
	perm          [512]int
	chunkCache    map[perlinChunkCacheKey]*list.Element
	chunkCacheLRU *list.List
}

type perlinChunkCacheKey struct {
	chunkX int
	chunkY int
}

type perlinCachedCube struct {
	localX   uint16
	localY   uint16
	localZ   uint16
	cubeSize uint16
	color    uint32
}

type perlinCachedBrick struct {
	localX uint16
	localY uint16
	localZ uint16
	voxels *[world.BrickVoxelCount]uint8
}

type perlinChunkData struct {
	cubes  []perlinCachedCube
	bricks []perlinCachedBrick
}

type perlinChunkCacheEntry struct {
	key  perlinChunkCacheKey
	data perlinChunkData
}

const (
	perlinDeepVoxel  uint8 = 1
	perlinRockVoxel  uint8 = 2
	perlinSoilVoxel  uint8 = 3
	perlinGrassVoxel uint8 = 4
	perlinSandVoxel  uint8 = 5
	perlinSnowVoxel  uint8 = 6
	perlinCliffVoxel uint8 = 7
	perlinWaterVoxel uint8 = 8
)

var perlinPalette = [255]uint32{
	0,
	rgbaColor(0x60, 0x67, 0x6F),
	rgbaColor(0x60, 0x67, 0x6F),
	rgbaColor(0x74, 0x59, 0x40),
	rgbaColor(0x5A, 0x88, 0x47),
	rgbaColor(0xC9, 0xB5, 0x82),
	rgbaColor(0xEE, 0xF2, 0xF5),
	rgbaColor(0x7B, 0x83, 0x89),
	rgbaColor(0x4B, 0x7F, 0xB4),
}

var perlinPaletteColors = []uint32{
	perlinPalette[perlinDeepVoxel],
	perlinPalette[perlinRockVoxel],
	perlinPalette[perlinSoilVoxel],
	perlinPalette[perlinGrassVoxel],
	perlinPalette[perlinSandVoxel],
	perlinPalette[perlinSnowVoxel],
	perlinPalette[perlinCliffVoxel],
	perlinPalette[perlinWaterVoxel],
}

type terrainColumn struct {
	surface      int
	seaLevel     int
	moisture     float64
	ruggedness   float64
	shoreline    float64
	snowLine     int
	surfaceSlope int
}

type perlinNoiseLayers struct {
	continent *perlinGenerator
	warp      *perlinGenerator
	ridge     *perlinGenerator
	erosion   *perlinGenerator
	biome     *perlinGenerator
	cave      *perlinGenerator
}

func NewPerlinGenerator(seed1, seed2 uint64) *perlinGenerator {
	return newPerlinGenerator(seed1, seed2, true)
}

func newPerlinGenerator(seed1, seed2 uint64, enableCache bool) *perlinGenerator {
	values := make([]int, 256)
	for index := range values {
		values[index] = index
	}

	rng := rand.New(rand.NewPCG(seed1, seed2))
	for index := len(values) - 1; index > 0; index-- {
		swapIndex := rng.IntN(index + 1)
		values[index], values[swapIndex] = values[swapIndex], values[index]
	}

	generator := &perlinGenerator{name: "Perlin Terrain", sceneSize: perlinChunkSize, terrainScale: perlinTerrainScale, seed1: seed1, seed2: seed2}
	if enableCache {
		generator.chunkCache = make(map[perlinChunkCacheKey]*list.Element, perlinChunkCacheLimit)
		generator.chunkCacheLRU = list.New()
	}
	for index := range generator.perm {
		generator.perm[index] = values[index&255]
	}
	return generator
}

func (p *perlinGenerator) variant(seed1Delta, seed2Delta uint64) *perlinGenerator {
	return newPerlinGenerator(p.seed1+seed1Delta, p.seed2+seed2Delta, false)
}

func (p *perlinGenerator) layers() perlinNoiseLayers {
	return perlinNoiseLayers{
		continent: p,
		warp:      p.variant(0x9E3779B97F4A7C15, 0xBF58476D1CE4E5B9),
		ridge:     p.variant(0x94D049BB133111EB, 0xD2B74407B1CE6E93),
		erosion:   p.variant(0xDB4F0B9175AE2165, 0xBBE0563303A4615F),
		biome:     p.variant(0xA0F2EC75A1FE1575, 0x89E182857D9ED689),
		cave:      p.variant(0xC2B2AE3D27D4EB4F, 0x165667B19E3779F9),
	}
}

func (p *perlinGenerator) Name() string {
	return p.name
}

func (p *perlinGenerator) CameraDriven() bool {
	return true
}

func (p *perlinGenerator) ChunkPaletteColors() []uint32 {
	colors := make([]uint32, len(perlinPaletteColors))
	copy(colors, perlinPaletteColors)
	return colors
}

func (p *perlinGenerator) ChunkSize() uint {
	return p.sceneSize
}

func (p *perlinGenerator) ChunkMapRevision() string {
	return fmt.Sprintf("%s:%d:%d:%d:%g", perlinChunkMapRevision, p.seed1, p.seed2, p.sceneSize, p.terrainScale)
}

func (p *perlinGenerator) BuildChunkSVO(svo *world.SVO, chunkX, chunkY int) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	request := BuildRequest{ChunkX: chunkX, ChunkY: chunkY, ChunkRange: 0}
	return p.buildSVO(svo, request, sceneSizeForDimension(max(int(p.sceneSize), int(p.terrainScale))))
}

func (p *perlinGenerator) BuildSVO(svo *world.SVO, request BuildRequest) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	return p.buildSVO(svo, request, request.SceneSize(p.ChunkSize()))
}

func (p *perlinGenerator) buildSVO(svo *world.SVO, request BuildRequest, sceneSize uint) error {
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	request = request.Normalized()

	layers := p.layers()
	sceneScale := p.terrainScale
	chunkSize := p.ChunkSize()

	svo.BuildTreeSparseVolumesWithMaterialBricks(sceneSize, perlinPaletteColors, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[world.BrickVoxelCount]uint8)) {
		request.ForEachChunkByDistance(chunkSize, func(chunkX, chunkY int, originX, originY uint) {
			chunk := p.chunkData(chunkX, chunkY, chunkSize, sceneScale, layers)
			for _, cube := range chunk.cubes {
				addCube(originX+uint(cube.localX), originY+uint(cube.localY), uint(cube.localZ), uint(cube.cubeSize), cube.color)
			}
			for _, brick := range chunk.bricks {
				addBrick(originX+uint(brick.localX), originY+uint(brick.localY), uint(brick.localZ), brick.voxels)
			}
		})
	})
	return nil
}

func (p *perlinGenerator) chunkData(chunkX, chunkY int, chunkSize uint, sceneScale float64, layers perlinNoiseLayers) perlinChunkData {
	key := perlinChunkCacheKey{chunkX: chunkX, chunkY: chunkY}
	if p.chunkCache != nil {
		if element, ok := p.chunkCache[key]; ok {
			p.chunkCacheLRU.MoveToBack(element)
			return element.Value.(*perlinChunkCacheEntry).data
		}
	}

	data := p.buildChunkData(chunkX, chunkY, chunkSize, sceneScale, layers)
	if p.chunkCache == nil || p.chunkCacheLRU == nil {
		return data
	}

	entry := &perlinChunkCacheEntry{key: key, data: data}
	element := p.chunkCacheLRU.PushBack(entry)
	p.chunkCache[key] = element
	if p.chunkCacheLRU.Len() > perlinChunkCacheLimit {
		oldest := p.chunkCacheLRU.Front()
		if oldest != nil {
			p.chunkCacheLRU.Remove(oldest)
			delete(p.chunkCache, oldest.Value.(*perlinChunkCacheEntry).key)
		}
	}
	return data
}

func (p *perlinGenerator) buildChunkData(chunkX, chunkY int, chunkSize uint, sceneScale float64, layers perlinNoiseLayers) perlinChunkData {
	brickSize := uint(world.BrickSize)
	columnGridWidth := world.BrickSize + 2
	worldChunkX := int64(chunkX) * int64(chunkSize)
	worldChunkY := int64(chunkY) * int64(chunkSize)
	data := perlinChunkData{}
	var blockColumns [(world.BrickSize + 2) * (world.BrickSize + 2)]terrainColumn

	for localChunkY := uint(0); localChunkY < chunkSize; localChunkY += brickSize {
		for localChunkX := uint(0); localChunkX < chunkSize; localChunkX += brickSize {
			worldBlockX := worldChunkX + int64(localChunkX)
			worldBlockY := worldChunkY + int64(localChunkY)
			for localY := -1; localY <= world.BrickSize; localY++ {
				for localX := -1; localX <= world.BrickSize; localX++ {
					column := sampleTerrainColumn(
						float64(worldBlockX+int64(localX)),
						float64(worldBlockY+int64(localY)),
						sceneScale,
						layers.continent,
						layers.warp,
						layers.ridge,
						layers.erosion,
						layers.biome,
					)
					blockColumns[(localY+1)*columnGridWidth+(localX+1)] = column
				}
			}
			smoothedColumns := blockColumns
			applyPerlinSurfaceSmoothing(blockColumns[:], smoothedColumns[:], columnGridWidth)
			maxColumnTop := 0
			for localY := 0; localY < world.BrickSize; localY++ {
				for localX := 0; localX < world.BrickSize; localX++ {
					column := blockColumns[(localY+1)*columnGridWidth+(localX+1)]
					columnTop := column.surface
					if column.seaLevel > columnTop {
						columnTop = column.seaLevel
					}
					if columnTop > maxColumnTop {
						maxColumnTop = columnTop
					}
				}
			}
			applyPerlinSurfaceSlope(blockColumns[:], columnGridWidth)

			for z0 := uint(0); z0 <= uint(maxColumnTop); z0 += brickSize {
				if perlinBlockIsUniformWater(blockColumns[:], columnGridWidth, int(z0)) {
					data.cubes = append(data.cubes, perlinCachedCube{localX: uint16(localChunkX), localY: uint16(localChunkY), localZ: uint16(z0), cubeSize: uint16(brickSize), color: perlinPalette[perlinWaterVoxel]})
					continue
				}
				if perlinBlockIsUniformDeepSolid(blockColumns[:], columnGridWidth, int(z0)) {
					data.cubes = append(data.cubes, perlinCachedCube{localX: uint16(localChunkX), localY: uint16(localChunkY), localZ: uint16(z0), cubeSize: uint16(brickSize), color: perlinPalette[perlinDeepVoxel]})
					continue
				}

				voxels := buildPerlinMixedBlockMaterials(z0, worldBlockX, worldBlockY, sceneScale, blockColumns[:], columnGridWidth, layers)
				if voxels == nil {
					continue
				}
				data.bricks = append(data.bricks, perlinCachedBrick{localX: uint16(localChunkX), localY: uint16(localChunkY), localZ: uint16(z0), voxels: voxels})
			}
		}
	}

	return data
}

func sampleTerrainColumn(x, y, sceneScale float64, continentNoise, warpNoise, ridgeNoise, erosionNoise, biomeNoise *perlinGenerator) terrainColumn {
	halfSpan := sceneScale * 0.5
	nx := (x - halfSpan) / sceneScale
	ny := (y - halfSpan) / sceneScale

	warpX := fbm2(warpNoise, nx*0.72+17.1, ny*0.72-11.4, 4, 2.0, 0.5) - 0.5
	warpY := fbm2(warpNoise, nx*0.72-23.7, ny*0.72+9.2, 4, 2.0, 0.5) - 0.5
	wx := nx + warpX*0.28
	wy := ny + warpY*0.28

	continentRaw := fbm2(continentNoise, wx*0.95, wy*0.95, 5, 2.0, 0.56)
	shelf := smoothstep(0.38, 0.55, continentRaw)
	continent := smoothstep(0.44, 0.61, continentRaw)
	foothills := fbm2(continentNoise, wx*1.7+3.1, wy*1.7-7.7, 5, 2.05, 0.54)
	erosion := fbm2(erosionNoise, wx*5.2+12.4, wy*5.2-15.2, 4, 2.1, 0.52)
	alpineDetail := fbm2(erosionNoise, wx*7.0-18.3, wy*7.0+14.1, 4, 2.2, 0.48)
	moisture := fbm2(biomeNoise, wx*1.3-4.7, wy*1.3+9.1, 4, 2.0, 0.57)

	ridgeRaw := fbm2(ridgeNoise, wx*2.6, wy*2.6, 5, 2.0, 0.58)
	ridge := 1.0 - math.Abs(ridgeRaw*2.0-1.0)
	ridge = math.Pow(clamp01(ridge), 1.4)

	riverField := fbm2(warpNoise, wx*2.6+21.8, wy*2.6-14.6, 4, 2.0, 0.5)
	riverDistance := math.Abs(riverField*2.0 - 1.0)
	riverCarve := 1.0 - smoothstep(0.02, 0.18, riverDistance)
	riverCarve *= shelf * (0.35 + moisture*0.65)
	trenchField := fbm2(ridgeNoise, wx*1.7-8.4, wy*1.7+12.1, 4, 2.0, 0.55)

	basePlateau := sceneScale * 0.05
	rollingHills := continent * math.Pow(foothills, 1.15) * sceneScale * 0.09
	mountainMass := math.Pow(ridge, 1.7) * math.Pow(continent, 1.25) * sceneScale * (0.10 + (1.0-erosion)*0.13)
	microRelief := continent * alpineDetail * sceneScale * 0.018 * (0.35 + ridge*0.45)
	basinCut := continent * (1.0 - erosion) * (0.50 + (1.0-moisture)*0.50) * sceneScale * 0.03

	seaLevel := perlinSeaLevel(sceneScale)
	oceanRelief := (foothills-0.5)*sceneScale*0.08 + (alpineDetail-0.5)*sceneScale*0.04
	trenchCarve := smoothstep(0.63, 0.82, trenchField) * math.Pow(1.0-shelf, 1.8) * sceneScale * 0.11
	oceanFloor := float64(seaLevel) - sceneScale*0.04 - math.Pow(1.0-shelf, 1.35)*sceneScale*0.10 + oceanRelief*(1.0-continent*0.4) - trenchCarve
	landHeight := float64(seaLevel) + basePlateau + shelf*sceneScale*0.09 + rollingHills + mountainMass + microRelief - basinCut - riverCarve*sceneScale*0.09
	surface := int(lerp(oceanFloor, landHeight, shelf))

	minSurface := 2
	maxSurface := int(sceneScale * 0.88)
	if surface < minSurface {
		surface = minSurface
	}
	if surface > maxSurface {
		surface = maxSurface
	}

	shoreline := 1.0 - smoothstep(0.0, sceneScale*0.03, math.Abs(float64(surface-seaLevel)))
	snowLine := int(sceneScale*0.56 - moisture*sceneScale*0.03 - (1.0-continent)*sceneScale*0.04)
	if snowLine < seaLevel+24 {
		snowLine = seaLevel + 24
	}

	ruggedness := clamp01(math.Pow(ridge, 1.25)*0.7 + (1.0-erosion)*0.3)
	return terrainColumn{
		surface:    surface,
		seaLevel:   seaLevel,
		moisture:   moisture,
		ruggedness: ruggedness,
		shoreline:  shoreline,
		snowLine:   snowLine,
	}
}

func perlinSeaLevel(sceneScale float64) int {
	return int(sceneScale * 0.20)
}

func applyPerlinSurfaceSlope(columns []terrainColumn, stride int) {
	if stride < 3 {
		return
	}
	for y := 1; y < stride-1; y++ {
		for x := 1; x < stride-1; x++ {
			index := y*stride + x
			surface := columns[index].surface
			slope := absInt(surface - columns[index-1].surface)
			slope = maxInt(slope, absInt(surface-columns[index+1].surface))
			slope = maxInt(slope, absInt(surface-columns[index-stride].surface))
			slope = maxInt(slope, absInt(surface-columns[index+stride].surface))
			columns[index].surfaceSlope = slope
		}
	}
}

func applyPerlinSurfaceSmoothing(columns, source []terrainColumn, stride int) {
	if stride < 3 || len(columns) != len(source) {
		return
	}
	for y := 1; y < stride-1; y++ {
		for x := 1; x < stride-1; x++ {
			index := y*stride + x
			smoothedSurface := source[index].surface*4 + source[index-1].surface + source[index+1].surface + source[index-stride].surface + source[index+stride].surface
			columns[index].surface = (smoothedSurface + 4) / 8
		}
	}
}

func perlinBlockIsUniformWater(columns []terrainColumn, stride, blockBaseZ int) bool {
	blockTop := blockBaseZ + world.BrickSize - 1
	for localY := 1; localY <= world.BrickSize; localY++ {
		for localX := 1; localX <= world.BrickSize; localX++ {
			column := columns[localY*stride+localX]
			if blockBaseZ <= column.surface || blockTop > column.seaLevel {
				return false
			}
		}
	}
	return true
}

func perlinBlockIsUniformDeepSolid(columns []terrainColumn, stride, blockBaseZ int) bool {
	blockTop := blockBaseZ + world.BrickSize - 1
	for localY := 1; localY <= world.BrickSize; localY++ {
		for localX := 1; localX <= world.BrickSize; localX++ {
			column := columns[localY*stride+localX]
			if blockTop > column.surface {
				return false
			}
			depthFromSurface := column.surface - blockTop
			if depthFromSurface <= perlinMaxCaveDepth {
				return false
			}
		}
	}
	return true
}

func buildPerlinMixedBlockMaterials(baseZ uint, worldBaseX, worldBaseY int64, sceneScale float64, columns []terrainColumn, stride int, layers perlinNoiseLayers) *[world.BrickVoxelCount]uint8 {
	voxels := &[world.BrickVoxelCount]uint8{}
	hasMaterial := false
	for localY := 0; localY < world.BrickSize; localY++ {
		for localX := 0; localX < world.BrickSize; localX++ {
			worldX := worldBaseX + int64(localX)
			worldY := worldBaseY + int64(localY)
			column := columns[(localY+1)*stride+(localX+1)]

			for localZ := 0; localZ < world.BrickSize; localZ++ {
				worldZ := int(baseZ) + localZ
				materialID := uint8(0)
				if worldZ > column.surface {
					if worldZ <= column.seaLevel {
						materialID = perlinWaterVoxel
					}
				} else if shouldCarveCave(float64(worldX), float64(worldY), float64(worldZ), sceneScale, column, layers.warp, layers.cave) {
					if worldZ <= column.seaLevel {
						materialID = perlinWaterVoxel
					}
				} else {
					materialID = perlinMaterialAtDepth(worldZ, column)
				}
				if materialID != 0 {
					hasMaterial = true
				}
				voxels[localX+localY*world.BrickSize+localZ*world.BrickSize*world.BrickSize] = materialID
			}
		}
	}
	if !hasMaterial {
		return nil
	}
	return voxels
}

func shouldCarveCave(x, y, z, sceneScale float64, column terrainColumn, warpNoise, caveNoise *perlinGenerator) bool {
	depthFromSurface := column.surface - int(z)
	if depthFromSurface < 8 || depthFromSurface > perlinMaxCaveDepth || int(z) < column.seaLevel/3 {
		return false
	}
	if column.ruggedness < 0.35 && depthFromSurface < 20 {
		return false
	}

	halfSpan := sceneScale * 0.5
	nx := (x - halfSpan) / sceneScale
	ny := (y - halfSpan) / sceneScale
	nz := z / sceneScale

	warp := fbm2(warpNoise, nx*3.0-7.1, ny*3.0+13.4, 3, 2.0, 0.5) - 0.5
	caveField := fbm3(caveNoise, nx*5.0+warp*1.5, ny*5.0-warp*1.5, nz*7.0, 4, 2.0, 0.56)
	chamberField := fbm3(caveNoise, nx*9.0-11.7, ny*9.0+4.3, nz*10.0, 3, 2.0, 0.5)
	threshold := 0.82 - column.ruggedness*0.10
	return caveField > threshold && chamberField > 0.58
}

func perlinMaterialAtDepth(z int, column terrainColumn) uint8 {
	depthFromSurface := column.surface - z
	steepColumn := column.surfaceSlope >= 2 || (column.surfaceSlope >= 1 && column.ruggedness >= 0.62)
	if z <= column.seaLevel {
		if z <= column.seaLevel-12 {
			switch {
			case depthFromSurface == 0 && column.ruggedness > 0.55:
				return perlinCliffVoxel
			case depthFromSurface == 0:
				return perlinDeepVoxel
			case depthFromSurface < 4:
				return perlinRockVoxel
			}
		}
		if depthFromSurface == 0 || depthFromSurface < 4 {
			return perlinSandVoxel
		}
	}

	if steepColumn && depthFromSurface < 16 {
		if depthFromSurface < 10 {
			return perlinCliffVoxel
		}
		return perlinRockVoxel
	}

	if z >= column.snowLine {
		if depthFromSurface == 0 {
			if steepColumn {
				return perlinCliffVoxel
			}
			return perlinSnowVoxel
		}
		if depthFromSurface < 4 {
			return perlinRockVoxel
		}
	}

	if column.shoreline > 0.45 && z >= column.seaLevel-6 && depthFromSurface < 5 && column.ruggedness < 0.58 {
		return perlinSandVoxel
	}

	switch {
	case depthFromSurface == 0:
		if steepColumn {
			return perlinCliffVoxel
		}
		if column.ruggedness > 0.68 {
			return perlinCliffVoxel
		}
		if column.moisture < 0.28 && z > column.seaLevel+10 {
			return perlinRockVoxel
		}
		return perlinGrassVoxel
	case depthFromSurface < 4:
		if column.shoreline > 0.30 && z >= column.seaLevel-4 {
			return perlinSandVoxel
		}
		return perlinSoilVoxel
	case depthFromSurface < 14:
		if column.ruggedness > 0.58 {
			return perlinCliffVoxel
		}
		return perlinRockVoxel
	default:
		return perlinDeepVoxel
	}
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func fbm2(noise *perlinGenerator, x, y float64, octaves int, lacunarity, gain float64) float64 {
	frequency := 1.0
	amplitude := 1.0
	total := 0.0
	totalAmplitude := 0.0
	for octave := 0; octave < octaves; octave++ {
		total += noise.noise2(x*frequency, y*frequency) * amplitude
		totalAmplitude += amplitude
		frequency *= lacunarity
		amplitude *= gain
	}
	if totalAmplitude == 0 {
		return 0
	}
	return total / totalAmplitude
}

func fbm3(noise *perlinGenerator, x, y, z float64, octaves int, lacunarity, gain float64) float64 {
	frequency := 1.0
	amplitude := 1.0
	total := 0.0
	totalAmplitude := 0.0
	for octave := 0; octave < octaves; octave++ {
		total += noise.noise3(x*frequency, y*frequency, z*frequency) * amplitude
		totalAmplitude += amplitude
		frequency *= lacunarity
		amplitude *= gain
	}
	if totalAmplitude == 0 {
		return 0
	}
	return total / totalAmplitude
}

func (p *perlinGenerator) noise2(x, y float64) float64 {
	xFloor := int(math.Floor(x))
	yFloor := int(math.Floor(y))
	xi := xFloor & 255
	yi := yFloor & 255
	xf := x - float64(xFloor)
	yf := y - float64(yFloor)
	u := fade(xf)
	v := fade(yf)
	aa := p.perm[p.perm[xi]+yi]
	ab := p.perm[p.perm[xi]+yi+1]
	ba := p.perm[p.perm[xi+1]+yi]
	bb := p.perm[p.perm[xi+1]+yi+1]
	x1 := lerp(grad2(aa, xf, yf), grad2(ba, xf-1, yf), u)
	x2 := lerp(grad2(ab, xf, yf-1), grad2(bb, xf-1, yf-1), u)
	return (lerp(x1, x2, v) + 1) * 0.5
}

func (p *perlinGenerator) noise3(x, y, z float64) float64 {
	xFloor := int(math.Floor(x))
	yFloor := int(math.Floor(y))
	zFloor := int(math.Floor(z))
	xi := xFloor & 255
	yi := yFloor & 255
	zi := zFloor & 255
	xf := x - float64(xFloor)
	yf := y - float64(yFloor)
	zf := z - float64(zFloor)
	u := fade(xf)
	v := fade(yf)
	w := fade(zf)
	aaa := p.perm[p.perm[p.perm[xi]+yi]+zi]
	aba := p.perm[p.perm[p.perm[xi]+yi+1]+zi]
	aab := p.perm[p.perm[p.perm[xi]+yi]+zi+1]
	abb := p.perm[p.perm[p.perm[xi]+yi+1]+zi+1]
	baa := p.perm[p.perm[p.perm[xi+1]+yi]+zi]
	bba := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi]
	bab := p.perm[p.perm[p.perm[xi+1]+yi]+zi+1]
	bbb := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi+1]
	x1 := lerp(grad3(aaa, xf, yf, zf), grad3(baa, xf-1, yf, zf), u)
	x2 := lerp(grad3(aba, xf, yf-1, zf), grad3(bba, xf-1, yf-1, zf), u)
	y1 := lerp(x1, x2, v)
	x3 := lerp(grad3(aab, xf, yf, zf-1), grad3(bab, xf-1, yf, zf-1), u)
	x4 := lerp(grad3(abb, xf, yf-1, zf-1), grad3(bbb, xf-1, yf-1, zf-1), u)
	y2 := lerp(x3, x4, v)
	return (lerp(y1, y2, w) + 1) * 0.5
}

func grad2(hash int, x, y float64) float64 {
	switch hash & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

func grad3(hash int, x, y, z float64) float64 {
	switch hash & 15 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x + z
	case 5:
		return -x + z
	case 6:
		return x - z
	case 7:
		return -x - z
	case 8:
		return y + z
	case 9:
		return -y + z
	case 10:
		return y - z
	case 11:
		return -y - z
	case 12:
		return x + y
	case 13:
		return -x + y
	case 14:
		return y + z
	default:
		return -y - z
	}
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func smoothstep(edge0, edge1, value float64) float64 {
	if edge0 == edge1 {
		if value < edge0 {
			return 0
		}
		return 1
	}
	t := clamp01((value - edge0) / (edge1 - edge0))
	return t * t * (3 - 2*t)
}

func fade(value float64) float64              { return value * value * value * (value*(value*6-15) + 10) }
func lerp(start, end, amount float64) float64 { return start + amount*(end-start) }
