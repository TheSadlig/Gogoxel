package generators

import "testing"

func TestPerlinSurfaceAdjacencyProfile(t *testing.T) {
	generator := NewPerlinGenerator(1, 2)
	layers := generator.layers()
	center := generator.terrainScale * 0.5
	start := center - 64
	stride := 130
	columns := make([]terrainColumn, stride*stride)
	for y := 0; y < stride; y++ {
		for x := 0; x < stride; x++ {
			columns[y*stride+x] = sampleTerrainColumn(
				start+float64(x-1),
				start+float64(y-1),
				generator.terrainScale,
				layers.continent,
				layers.warp,
				layers.ridge,
				layers.erosion,
				layers.biome,
			)
		}
	}
	smoothed := make([]terrainColumn, len(columns))
	copy(smoothed, columns)
	applyPerlinSurfaceSmoothing(columns, smoothed, stride)

	maxDelta := 0
	totalDelta := 0
	pairCount := 0

	for y := 1; y < stride-1; y++ {
		for x := 1; x < stride-1; x++ {
			column := columns[y*stride+x]
			right := columns[y*stride+x+1]
			down := columns[(y+1)*stride+x]

			dx := absInt(column.surface - right.surface)
			dy := absInt(column.surface - down.surface)
			if dx > maxDelta {
				maxDelta = dx
			}
			if dy > maxDelta {
				maxDelta = dy
			}
			totalDelta += dx + dy
			pairCount += 2
		}
	}

	if pairCount == 0 {
		t.Fatal("expected non-zero adjacency samples")
	}

	averageDelta := float64(totalDelta) / float64(pairCount)
	if averageDelta > 1.60 {
		t.Fatalf("average adjacent delta = %.3f, want <= 1.60", averageDelta)
	}
	if maxDelta > 7 {
		t.Fatalf("max adjacent delta = %d, want <= 7", maxDelta)
	}
}