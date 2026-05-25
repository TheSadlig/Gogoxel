package generators

import (
	"testing"

	"Gogoxel/internal/world"
)

func TestPerlinGeneratorIsCameraDriven(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	cameraDriven, ok := any(generator).(CameraDrivenGenerator)
	if !ok || !cameraDriven.CameraDriven() {
		t.Fatal("expected perlin generator to participate in the camera-driven load path")
	}
}

func TestPerlinGeneratorDefaultsToCompactChunkSize(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	if got, want := generator.ChunkSize(), perlinChunkSize; got != want {
		t.Fatalf("chunk size = %d, want %d", got, want)
	}
	if got, want := generator.terrainScale, perlinTerrainScale; got != want {
		t.Fatalf("terrain scale = %v, want %v", got, want)
	}
}

func TestCubeGeneratorBuildRequestTilesChunkRange(t *testing.T) {
	generator := NewCubeGenerator("Cube", 16, 8, rgbaColor(0xE2, 0x55, 0x4F))
	svo := world.NewSVO()

	if err := generator.BuildSVO(svo, BuildRequest{ChunkX: 3, ChunkY: -2, ChunkRange: 1}); err != nil {
		t.Fatalf("BuildSVO returned error: %v", err)
	}

	if got, want := svo.Size(), uint(64); got != want {
		t.Fatalf("SVO size = %d, want %d", got, want)
	}

	minBounds, maxBounds, ok := svo.OccupiedBounds()
	if !ok {
		t.Fatal("expected occupied bounds")
	}
	if minBounds != [3]uint32{4, 4, 4} {
		t.Fatalf("occupied min bounds = %v, want %v", minBounds, [3]uint32{4, 4, 4})
	}
	if maxBounds != [3]uint32{44, 44, 12} {
		t.Fatalf("occupied max bounds = %v, want %v", maxBounds, [3]uint32{44, 44, 12})
	}
}

func TestPerlinGeneratorBuildRequestUsesChunkCoordinates(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	generator.sceneSize = 32
	generator.terrainScale = 32

	left := world.NewSVO()
	if err := generator.BuildSVO(left, BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 0}); err != nil {
		t.Fatalf("left BuildSVO returned error: %v", err)
	}

	right := world.NewSVO()
	if err := generator.BuildSVO(right, BuildRequest{ChunkX: 1, ChunkY: 0, ChunkRange: 0}); err != nil {
		t.Fatalf("right BuildSVO returned error: %v", err)
	}

	leftWords := left.StorageBufferWordsRef()
	rightWords := right.StorageBufferWordsRef()
	if len(leftWords) == 0 || len(rightWords) == 0 {
		t.Fatal("expected non-empty storage words")
	}
	if len(leftWords) != len(rightWords) {
		return
	}
	identical := true
	for index := range leftWords {
		if leftWords[index] != rightWords[index] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("expected different storage words for different chunk coordinates")
	}
}

func TestPerlinGeneratorCachesOverlappingChunks(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	generator.sceneSize = 32
	generator.terrainScale = 32

	first := world.NewSVO()
	if err := generator.BuildSVO(first, BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}); err != nil {
		t.Fatalf("first BuildSVO returned error: %v", err)
	}
	if got, want := len(generator.chunkCache), 9; got != want {
		t.Fatalf("chunk cache size after first build = %d, want %d", got, want)
	}
	reused := generator.chunkCache[perlinChunkCacheKey{chunkX: 0, chunkY: 0}]
	if reused == nil {
		t.Fatal("expected overlapping chunk to be cached after first build")
	}

	second := world.NewSVO()
	if err := generator.BuildSVO(second, BuildRequest{ChunkX: 1, ChunkY: 0, ChunkRange: 1}); err != nil {
		t.Fatalf("second BuildSVO returned error: %v", err)
	}
	if got, want := len(generator.chunkCache), 12; got != want {
		t.Fatalf("chunk cache size after overlapping second build = %d, want %d", got, want)
	}
	if got := generator.chunkCache[perlinChunkCacheKey{chunkX: 0, chunkY: 0}]; got != reused {
		t.Fatal("expected overlapping chunk cache entry to be reused across builds")
	}
}

func TestBuildRequestForEachChunkByDistanceStartsAtCenter(t *testing.T) {
	request := BuildRequest{ChunkX: 5, ChunkY: -2, ChunkRange: 1}
	type chunkCoord struct {
		x int
		y int
	}
	var visits []chunkCoord
	request.ForEachChunkByDistance(16, func(chunkX, chunkY int, _originX, _originY uint) {
		visits = append(visits, chunkCoord{x: chunkX, y: chunkY})
	})
	if got, want := len(visits), 9; got != want {
		t.Fatalf("visit count = %d, want %d", got, want)
	}
	if got, want := visits[0], (chunkCoord{x: 5, y: -2}); got != want {
		t.Fatalf("first chunk = %+v, want %+v", got, want)
	}
	previousDistance := -1
	for index, visit := range visits {
		dx := visit.x - request.ChunkX
		dy := visit.y - request.ChunkY
		distance := dx*dx + dy*dy
		if distance < previousDistance {
			t.Fatalf("visit %d distance = %d, previous = %d", index, distance, previousDistance)
		}
		previousDistance = distance
	}
}

func TestPerlinMaterialAtDepthUsesCliffOnSteepSnowSurface(t *testing.T) {
	column := terrainColumn{surface: 220, seaLevel: 64, snowLine: 180, ruggedness: 0.65, surfaceSlope: 1}

	if got, want := perlinMaterialAtDepth(column.surface, column), perlinCliffVoxel; got != want {
		t.Fatalf("steep snow surface material = %d, want %d", got, want)
	}
}

func TestPerlinMaterialAtDepthKeepsSnowOnFlatSnowSurface(t *testing.T) {
	column := terrainColumn{surface: 220, seaLevel: 64, snowLine: 180, ruggedness: 0.65, surfaceSlope: 0}

	if got, want := perlinMaterialAtDepth(column.surface, column), perlinSnowVoxel; got != want {
		t.Fatalf("flat snow surface material = %d, want %d", got, want)
	}
}

func TestPerlinMaterialAtDepthUsesCliffOnVerySteepGrassSurface(t *testing.T) {
	column := terrainColumn{surface: 140, seaLevel: 64, snowLine: 220, moisture: 0.5, ruggedness: 0.45, surfaceSlope: 2}

	if got, want := perlinMaterialAtDepth(column.surface, column), perlinCliffVoxel; got != want {
		t.Fatalf("steep grass surface material = %d, want %d", got, want)
	}
}
