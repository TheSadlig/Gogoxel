package generators

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"Gogoxel/internal/world"
)

type generatedChunkMapTestGenerator struct {
	name       string
	chunkSize  uint
	palette    []uint32
	revision   string
	failAfter  int
	buildOrder [][2]int
}

func (g *generatedChunkMapTestGenerator) Name() string {
	return g.name
}

func (g *generatedChunkMapTestGenerator) ChunkSize() uint {
	return g.chunkSize
}

func (g *generatedChunkMapTestGenerator) CameraDriven() bool {
	return true
}

func (g *generatedChunkMapTestGenerator) ChunkPaletteColors() []uint32 {
	colors := make([]uint32, len(g.palette))
	copy(colors, g.palette)
	return colors
}

func (g *generatedChunkMapTestGenerator) ChunkMapRevision() string {
	return g.revision
}

func (g *generatedChunkMapTestGenerator) BuildChunkSVO(svo *world.SVO, chunkX, chunkY int) error {
	if svo == nil {
		return nil
	}
	g.buildOrder = append(g.buildOrder, [2]int{chunkX, chunkY})
	if g.failAfter > 0 && len(g.buildOrder) >= g.failAfter {
		return os.ErrInvalid
	}
	voxels := new([world.BrickVoxelCount]uint8)
	voxels[0] = 1
	voxels[1] = 2
	voxels[world.BrickSize] = 2
	baseX := uint((chunkX & 1) + 1)
	baseY := uint((chunkY & 1) + 2)
	baseZ := uint((chunkX*chunkX + chunkY*chunkY) % 4)
	svo.BuildTreeSparseVolumesWithMaterialBricks(g.chunkSize, g.palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[world.BrickVoxelCount]uint8)) {
		addCube(baseX, baseY, baseZ, 4, g.palette[0])
		addBrick(8, 8, 0, voxels)
	})
	return nil
}

func (g *generatedChunkMapTestGenerator) BuildSVO(svo *world.SVO, request BuildRequest) error {
	if svo == nil {
		return nil
	}
	request = request.Normalized()
	svo.BuildTreeSparseVolumesWithMaterialBricks(request.SceneSize(g.chunkSize), g.palette, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[world.BrickVoxelCount]uint8)) {
		request.ForEachChunk(g.chunkSize, func(chunkX, chunkY int, originX, originY uint) {
			voxels := new([world.BrickVoxelCount]uint8)
			voxels[0] = 1
			voxels[1] = 2
			voxels[world.BrickSize] = 2
			baseX := uint((chunkX & 1) + 1)
			baseY := uint((chunkY & 1) + 2)
			baseZ := uint((chunkX*chunkX + chunkY*chunkY) % 4)
			addCube(originX+baseX, originY+baseY, baseZ, 4, g.palette[0])
			addBrick(originX+8, originY+8, 0, voxels)
		})
	})
	return nil
}

func TestGeneratedChunkMapRoundTripsChunkSnapshots(t *testing.T) {
	generator := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
	}
	mapDir := GeneratedChunkMapDir(t.TempDir(), generator.Name())
	request := BuildRequest{ChunkX: 1, ChunkY: -1, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, generator, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow returned error: %v", err)
	}
	loader, err := OpenGeneratedChunkMap(mapDir)
	if err != nil {
		t.Fatalf("OpenGeneratedChunkMap returned error: %v", err)
	}

	want := world.NewSVO()
	if err := generator.BuildChunkSVO(want, 1, -1); err != nil {
		t.Fatalf("BuildChunkSVO returned error: %v", err)
	}
	got := world.NewSVO()
	if err := loader.LoadChunkSVO(got, 1, -1); err != nil {
		t.Fatalf("LoadChunkSVO returned error: %v", err)
	}

	assertSnapshotsEqual(t, got.Snapshot(), want.Snapshot())
}

func TestGeneratedChunkMapBuildsRequestedSceneFromDisk(t *testing.T) {
	generator := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
	}
	mapDir := GeneratedChunkMapDir(t.TempDir(), generator.Name())
	request := BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, generator, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow returned error: %v", err)
	}
	loader, err := OpenGeneratedChunkMap(mapDir)
	if err != nil {
		t.Fatalf("OpenGeneratedChunkMap returned error: %v", err)
	}

	want := world.NewSVO()
	if err := generator.BuildSVO(want, request); err != nil {
		t.Fatalf("BuildSVO returned error: %v", err)
	}
	got := world.NewSVO()
	if err := loader.BuildSVO(got, request); err != nil {
		t.Fatalf("BuildSVO returned error: %v", err)
	}

	assertSnapshotsEqual(t, got.Snapshot(), want.Snapshot())
}

func TestGeneratedChunkMapBuildsRequestedSceneFromDiskWithDuplicatePaletteColors(t *testing.T) {
	generator := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette: []uint32{
			rgbaColor(0x60, 0x67, 0x6F),
			rgbaColor(0x60, 0x67, 0x6F),
			rgbaColor(0x4B, 0x7F, 0xB4),
		},
	}
	mapDir := GeneratedChunkMapDir(t.TempDir(), generator.Name())
	request := BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, generator, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow returned error: %v", err)
	}
	loader, err := OpenGeneratedChunkMap(mapDir)
	if err != nil {
		t.Fatalf("OpenGeneratedChunkMap returned error: %v", err)
	}

	want := world.NewSVO()
	if err := generator.BuildSVO(want, request); err != nil {
		t.Fatalf("BuildSVO returned error: %v", err)
	}
	got := world.NewSVO()
	if err := loader.BuildSVO(got, request); err != nil {
		t.Fatalf("BuildSVO returned error: %v", err)
	}

	assertSnapshotsEqual(t, got.Snapshot(), want.Snapshot())
}

func TestGenerateChunkMapWindowBuildsChunksNearestCenterFirst(t *testing.T) {
	generator := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
	}
	mapDir := GeneratedChunkMapDir(t.TempDir(), generator.Name())
	request := BuildRequest{ChunkX: 2, ChunkY: -3, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, generator, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow returned error: %v", err)
	}
	if got, want := len(generator.buildOrder), 9; got != want {
		t.Fatalf("build order count = %d, want %d", got, want)
	}
	if got, want := generator.buildOrder[0], ([2]int{request.ChunkX, request.ChunkY}); got != want {
		t.Fatalf("first generated chunk = %v, want %v", got, want)
	}
	previousDistance := -1
	for index, coord := range generator.buildOrder {
		dx := coord[0] - request.ChunkX
		dy := coord[1] - request.ChunkY
		distance := dx*dx + dy*dy
		if distance < previousDistance {
			t.Fatalf("generation distance decreased at step %d: got %d after %d", index, distance, previousDistance)
		}
		previousDistance = distance
	}
}

func TestGenerateChunkMapWindowSkipsAlreadyGeneratedChunks(t *testing.T) {
	generator := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
		revision:  "rev-a",
	}
	mapDir := GeneratedChunkMapDir(t.TempDir(), generator.Name())
	centerOnly := BuildRequest{ChunkX: 2, ChunkY: -3, ChunkRange: 0}
	fullWindow := BuildRequest{ChunkX: 2, ChunkY: -3, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, generator, centerOnly); err != nil {
		t.Fatalf("GenerateChunkMapWindow(centerOnly) returned error: %v", err)
	}
	generator.buildOrder = nil
	if err := GenerateChunkMapWindow(mapDir, generator, fullWindow); err != nil {
		t.Fatalf("GenerateChunkMapWindow(fullWindow) returned error: %v", err)
	}

	if got, want := len(generator.buildOrder), 8; got != want {
		t.Fatalf("rebuilt chunk count = %d, want %d", got, want)
	}
	for _, coord := range generator.buildOrder {
		if coord == [2]int{fullWindow.ChunkX, fullWindow.ChunkY} {
			t.Fatalf("expected pre-generated center chunk %v to be skipped", coord)
		}
	}
}

func TestGenerateChunkMapWindowRegeneratesChunksWhenRevisionChanges(t *testing.T) {
	mapDir := GeneratedChunkMapDir(t.TempDir(), "Disk Terrain")
	initial := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
		revision:  "rev-a",
	}
	updated := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
		revision:  "rev-b",
	}
	request := BuildRequest{ChunkX: 2, ChunkY: -3, ChunkRange: 1}

	if err := GenerateChunkMapWindow(mapDir, initial, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow(initial) returned error: %v", err)
	}
	if err := GenerateChunkMapWindow(mapDir, updated, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow(updated) returned error: %v", err)
	}
	if got, want := len(updated.buildOrder), 9; got != want {
		t.Fatalf("regenerated chunk count = %d, want %d", got, want)
	}

	manifest, err := readGeneratedChunkMapManifest(mapDir)
	if err != nil {
		t.Fatalf("readGeneratedChunkMapManifest returned error: %v", err)
	}
	if got, want := manifest.GeneratorRevision, "rev-b"; got != want {
		t.Fatalf("generator revision = %q, want %q", got, want)
	}
}

func TestGenerateChunkMapWindowKeepsExistingMapWhenRevisionRegenerationFails(t *testing.T) {
	mapDir := GeneratedChunkMapDir(t.TempDir(), "Disk Terrain")
	initial := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
		revision:  "rev-a",
	}
	failing := &generatedChunkMapTestGenerator{
		name:      "Disk Terrain",
		chunkSize: 16,
		palette:   []uint32{rgbaColor(0x55, 0xAA, 0x44), rgbaColor(0xD7, 0xC5, 0x92)},
		revision:  "rev-b",
		failAfter: 1,
	}
	request := BuildRequest{ChunkX: 2, ChunkY: -3, ChunkRange: 0}

	if err := GenerateChunkMapWindow(mapDir, initial, request); err != nil {
		t.Fatalf("GenerateChunkMapWindow(initial) returned error: %v", err)
	}
	if err := GenerateChunkMapWindow(mapDir, failing, request); err == nil {
		t.Fatal("expected revision regeneration failure")
	}

	manifest, err := readGeneratedChunkMapManifest(mapDir)
	if err != nil {
		t.Fatalf("readGeneratedChunkMapManifest returned error: %v", err)
	}
	if got, want := manifest.GeneratorRevision, "rev-a"; got != want {
		t.Fatalf("generator revision after failed regeneration = %q, want %q", got, want)
	}

	loader, err := OpenGeneratedChunkMap(mapDir)
	if err != nil {
		t.Fatalf("OpenGeneratedChunkMap returned error: %v", err)
	}
	got := world.NewSVO()
	if err := loader.LoadChunkSVO(got, request.ChunkX, request.ChunkY); err != nil {
		t.Fatalf("LoadChunkSVO returned error after failed regeneration: %v", err)
	}
}

func TestCheckedInPerlinChunkMapMatchesGeneratorSampleChunks(t *testing.T) {
	if os.Getenv("GOGOXEL_VERIFY_CHECKED_IN_PERLIN") == "" {
		t.Skip("set GOGOXEL_VERIFY_CHECKED_IN_PERLIN=1 to verify the local terrain/perlin-terrain cache against the current generator")
	}
	mapDir := filepath.Clean(filepath.Join("..", "..", "..", "terrain", "perlin-terrain"))
	if _, err := os.Stat(filepath.Join(mapDir, generatedChunkMapManifestName)); err != nil {
		t.Skipf("checked-in perlin chunk map not available: %v", err)
	}

	loader, err := OpenGeneratedChunkMap(mapDir)
	if err != nil {
		t.Fatalf("OpenGeneratedChunkMap returned error: %v", err)
	}

	generator := NewPerlinGenerator(1, 2)
	coords := []struct {
		name   string
		chunkX int
		chunkY int
	}{
		{name: "origin", chunkX: 0, chunkY: 0},
		{name: "positive", chunkX: 6, chunkY: 1},
		{name: "negative", chunkX: -7, chunkY: -2},
	}

	for _, coord := range coords {
		t.Run(coord.name, func(t *testing.T) {
			want := world.NewSVO()
			if err := generator.BuildChunkSVO(want, coord.chunkX, coord.chunkY); err != nil {
				t.Fatalf("BuildChunkSVO(%d,%d) returned error: %v", coord.chunkX, coord.chunkY, err)
			}

			got := world.NewSVO()
			if err := loader.LoadChunkSVO(got, coord.chunkX, coord.chunkY); err != nil {
				t.Fatalf("LoadChunkSVO(%d,%d) returned error: %v", coord.chunkX, coord.chunkY, err)
			}

			assertSnapshotsEqual(t, got.Snapshot(), want.Snapshot())
		})
	}
}

func assertSnapshotsEqual(t *testing.T, got, want world.Snapshot) {
	t.Helper()
	if got.HasOccupiedBounds != want.HasOccupiedBounds || got.OccupiedMin != want.OccupiedMin || got.OccupiedMax != want.OccupiedMax {
		t.Fatalf("occupied bounds = (%v, %v, %t), want (%v, %v, %t)", got.OccupiedMin, got.OccupiedMax, got.HasOccupiedBounds, want.OccupiedMin, want.OccupiedMax, want.HasOccupiedBounds)
	}
	if !reflect.DeepEqual(got.Words, want.Words) {
		t.Fatalf("snapshot words differ: got %d words, want %d words", len(got.Words), len(want.Words))
	}
	if len(got.Bricks) != len(want.Bricks) {
		t.Fatalf("brick count = %d, want %d", len(got.Bricks), len(want.Bricks))
	}
	for index := range got.Bricks {
		if got.Bricks[index].NodeIndex != want.Bricks[index].NodeIndex || got.Bricks[index].Origin != want.Bricks[index].Origin {
			t.Fatalf("brick %d metadata = (%d, %v), want (%d, %v)", index, got.Bricks[index].NodeIndex, got.Bricks[index].Origin, want.Bricks[index].NodeIndex, want.Bricks[index].Origin)
		}
		if got.Bricks[index].Voxels == nil || want.Bricks[index].Voxels == nil {
			t.Fatalf("brick %d voxels unexpectedly nil", index)
		}
		if *got.Bricks[index].Voxels != *want.Bricks[index].Voxels {
			t.Fatalf("brick %d voxels differ", index)
		}
	}
}
