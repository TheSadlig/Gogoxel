package generators

import (
	"math"
	"testing"

	"Gogoxel/internal/world"
)

func TestNewPerlinGeneratorUsesLargeSceneSize(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	if got, want := generator.sceneSize, perlinSceneSize; got != want {
		t.Fatalf("sceneSize = %d, want %d", got, want)
	}
}

func TestPerlinGeneratorBuildsLargeTerrain(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	generator.sceneSize = 128

	svo := world.NewSVO()
	if err := generator.BuildSVO(svo); err != nil {
		t.Fatalf("BuildSVO() error = %v", err)
	}

	minBounds, maxBounds, ok := svo.OccupiedBounds()
	if !ok {
		t.Fatal("OccupiedBounds ok = false, want true")
	}
	if got, want := maxBounds[0]-minBounds[0], uint32(generator.sceneSize); got != want {
		t.Fatalf("x span = %d, want %d", got, want)
	}
	if got, want := maxBounds[1]-minBounds[1], uint32(generator.sceneSize); got != want {
		t.Fatalf("y span = %d, want %d", got, want)
	}
	if got, wantMin := maxBounds[2]-minBounds[2], uint32(generator.sceneSize/2); got < wantMin {
		t.Fatalf("z span = %d, want at least %d", got, wantMin)
	}
	if got := svo.NodeCount(); got <= 1 {
		t.Fatalf("NodeCount() = %d, want > 1", got)
	}
}

func TestPerlinGeneratorSeedChangesTerrainShape(t *testing.T) {
	a := NewPerlinGenerator(1, 2)
	b := NewPerlinGenerator(11, 12)
	sceneScale := float64(128)
	alayers := a.layers()
	blayers := b.layers()

	columnA := sampleTerrainColumn(
		73,
		91,
		sceneScale,
		alayers.continent,
		alayers.warp,
		alayers.ridge,
		alayers.erosion,
		alayers.biome,
	)
	columnB := sampleTerrainColumn(
		73,
		91,
		sceneScale,
		blayers.continent,
		blayers.warp,
		blayers.ridge,
		blayers.erosion,
		blayers.biome,
	)

	if columnA.surface == columnB.surface &&
		math.Abs(columnA.moisture-columnB.moisture) < 1e-6 &&
		math.Abs(columnA.ruggedness-columnB.ruggedness) < 1e-6 &&
		columnA.snowLine == columnB.snowLine {
		t.Fatal("different generator seeds produced the same terrain column characteristics")
	}
}

func TestPerlinGeneratorProducesOceansAndRelief(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	generator.sceneSize = 128
	layers := generator.layers()
	sceneScale := float64(generator.sceneSize)
	totalColumns := int(generator.sceneSize * generator.sceneSize)
	seaLevel := perlinSeaLevel(sceneScale)

	waterColumns := 0
	deepWaterColumns := 0
	minSurface := 1 << 30
	maxSurface := -1
	underwaterMin := 1 << 30
	underwaterMax := -1
	underwaterHeights := map[int]struct{}{}

	for y := uint(0); y < generator.sceneSize; y++ {
		for x := uint(0); x < generator.sceneSize; x++ {
			column := sampleTerrainColumn(float64(x), float64(y), sceneScale, layers.continent, layers.warp, layers.ridge, layers.erosion, layers.biome)
			if column.surface < minSurface {
				minSurface = column.surface
			}
			if column.surface > maxSurface {
				maxSurface = column.surface
			}
			if column.surface < column.seaLevel {
				waterColumns++
				underwaterHeights[column.surface] = struct{}{}
				if column.surface < underwaterMin {
					underwaterMin = column.surface
				}
				if column.surface > underwaterMax {
					underwaterMax = column.surface
				}
			}
			if column.surface < column.seaLevel-12 {
				deepWaterColumns++
			}
		}
	}

	if waterColumns < totalColumns/5 {
		t.Fatalf("waterColumns = %d, want at least %d", waterColumns, totalColumns/5)
	}
	if deepWaterColumns < totalColumns/25 {
		t.Fatalf("deepWaterColumns = %d, want at least %d", deepWaterColumns, totalColumns/25)
	}
	if len(underwaterHeights) < 12 {
		t.Fatalf("len(underwaterHeights) = %d, want at least %d", len(underwaterHeights), 12)
	}
	if underwaterMax-underwaterMin < int(generator.sceneSize/10) {
		t.Fatalf("underwater height span = %d, want at least %d", underwaterMax-underwaterMin, int(generator.sceneSize/10))
	}
	if minSurface > seaLevel-12 {
		t.Fatalf("minSurface = %d, want <= %d", minSurface, seaLevel-12)
	}
	if maxSurface < seaLevel+int(generator.sceneSize/4) {
		t.Fatalf("maxSurface = %d, want >= %d", maxSurface, seaLevel+int(generator.sceneSize/4))
	}
}

func TestPerlinDeepUnderwaterSurfaceIsNotGrass(t *testing.T) {
	column := terrainColumn{
		surface:    8,
		seaLevel:   24,
		moisture:   0.5,
		ruggedness: 0.35,
		shoreline:  0,
		snowLine:   80,
	}

	got := perlinMaterialAtDepth(column.surface, column)
	if got == perlinGrassVoxel || got == perlinSoilVoxel {
		t.Fatalf("perlinMaterialAtDepth() = %d, want non-terrestrial underwater material", got)
	}
}

func TestPerlinDefaultScaleAvoidsFlatAbyssFloor(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	layers := generator.layers()
	sceneScale := float64(generator.sceneSize)

	waterColumns := 0
	floorColumns := 0
	step := uint(4)

	for y := uint(0); y < generator.sceneSize; y += step {
		for x := uint(0); x < generator.sceneSize; x += step {
			column := sampleTerrainColumn(float64(x), float64(y), sceneScale, layers.continent, layers.warp, layers.ridge, layers.erosion, layers.biome)
			if column.surface < column.seaLevel {
				waterColumns++
				if column.surface == 2 {
					floorColumns++
				}
			}
		}
	}

	if waterColumns == 0 {
		t.Fatal("waterColumns = 0, want > 0")
	}
	if floorColumns > waterColumns/5 {
		t.Fatalf("floorColumns = %d, want at most %d", floorColumns, waterColumns/5)
	}
}
