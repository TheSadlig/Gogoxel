package generators

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"Gogoxel/internal/world"
)

const (
	generatedChunkMapManifestName = "manifest.json"
	generatedChunkMapChunkDir     = "chunks"
	generatedChunkMapChunkExt     = ".gxsvo"
	generatedChunkMapVersion      = 1
	generatedChunkMagic           = "GXCM"
)

type generatedChunkMapManifest struct {
	Version           int      `json:"version"`
	Name              string   `json:"name"`
	ChunkSize         uint     `json:"chunk_size"`
	PaletteColors     []uint32 `json:"palette_colors"`
	CameraDriven      bool     `json:"camera_driven"`
	GeneratorRevision string   `json:"generator_revision,omitempty"`
}

type GeneratedChunkMap struct {
	rootDir  string
	manifest generatedChunkMapManifest
}

func GenerateChunkMapWindow(rootDir string, generator ChunkGenerator, request BuildRequest) error {
	if generator == nil {
		return fmt.Errorf("chunk generator is required")
	}
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return fmt.Errorf("chunk map root directory is required")
	}
	request = request.Normalized()

	manifest := generatedChunkMapManifest{
		Version:           generatedChunkMapVersion,
		Name:              generator.Name(),
		ChunkSize:         generator.ChunkSize(),
		PaletteColors:     generator.ChunkPaletteColors(),
		CameraDriven:      generator.CameraDriven(),
		GeneratorRevision: chunkMapGeneratorRevision(generator),
	}
	targetRoot := rootDir
	sameManifest, err := ensureGeneratedChunkMapTarget(rootDir, manifest)
	if err != nil {
		return err
	}
	if !sameManifest {
		targetRoot = generatedChunkMapStagingRoot(rootDir)
	}
	if err := os.MkdirAll(filepath.Join(targetRoot, generatedChunkMapChunkDir), 0o755); err != nil {
		return fmt.Errorf("creating chunk map directory: %w", err)
	}

	var generationErr error
	request.ForEachChunkByDistance(generator.ChunkSize(), func(chunkX, chunkY int, _originX, _originY uint) {
		if generationErr != nil {
			return
		}
		chunkPath := chunkMapChunkPath(targetRoot, chunkX, chunkY)
		if sameManifest {
			if exists, err := generatedChunkSnapshotExists(chunkPath); err != nil {
				generationErr = fmt.Errorf("checking chunk (%d,%d): %w", chunkX, chunkY, err)
				return
			} else if exists {
				return
			}
		}
		chunk := world.NewSVO()
		if err := generator.BuildChunkSVO(chunk, chunkX, chunkY); err != nil {
			generationErr = fmt.Errorf("generating chunk (%d,%d): %w", chunkX, chunkY, err)
			return
		}
		if err := writeChunkSnapshot(chunkPath, chunk.Snapshot()); err != nil {
			generationErr = fmt.Errorf("writing chunk (%d,%d): %w", chunkX, chunkY, err)
		}
	})
	if generationErr != nil {
		if !sameManifest {
			_ = os.RemoveAll(targetRoot)
		}
		return generationErr
	}
	if !sameManifest {
		if err := promoteGeneratedChunkMapRoot(rootDir, targetRoot); err != nil {
			return err
		}
	}
	return nil
}

func OpenGeneratedChunkMap(rootDir string) (*GeneratedChunkMap, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("chunk map root directory is required")
	}
	manifest, err := readGeneratedChunkMapManifest(rootDir)
	if err != nil {
		return nil, err
	}
	return &GeneratedChunkMap{rootDir: rootDir, manifest: manifest}, nil
}

func GeneratedChunkMapDir(rootDir, name string) string {
	return filepath.Join(rootDir, generatedChunkMapSlug(name))
}

func generatedChunkSnapshotExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func chunkMapGeneratorRevision(generator ChunkGenerator) string {
	if generator == nil {
		return ""
	}
	revisioner, ok := generator.(ChunkMapRevisioner)
	if !ok {
		return ""
	}
	return strings.TrimSpace(revisioner.ChunkMapRevision())
}

func generatedChunkMapStagingRoot(rootDir string) string {
	return rootDir + ".generating"
}

func (m *GeneratedChunkMap) Name() string {
	if m == nil {
		return ""
	}
	return m.manifest.Name
}

func (m *GeneratedChunkMap) ChunkSize() uint {
	if m == nil {
		return 0
	}
	return m.manifest.ChunkSize
}

func (m *GeneratedChunkMap) CameraDriven() bool {
	if m == nil {
		return false
	}
	return m.manifest.CameraDriven
}

func (m *GeneratedChunkMap) ChunkPaletteColors() []uint32 {
	if m == nil {
		return nil
	}
	colors := make([]uint32, len(m.manifest.PaletteColors))
	copy(colors, m.manifest.PaletteColors)
	return colors
}

func (m *GeneratedChunkMap) BuildSVO(svo *world.SVO, request BuildRequest) error {
	if m == nil {
		return fmt.Errorf("chunk map is not initialized")
	}
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	return composeChunkScene(svo, request, m.ChunkSize(), m.ChunkPaletteColors(), func(chunkX, chunkY int) (*world.SVO, error) {
		chunk := world.NewSVO()
		if err := m.LoadChunkSVO(chunk, chunkX, chunkY); err != nil {
			return nil, err
		}
		return chunk, nil
	})
}

func (m *GeneratedChunkMap) LoadChunkSVO(svo *world.SVO, chunkX, chunkY int) error {
	if m == nil {
		return fmt.Errorf("chunk map is not initialized")
	}
	if svo == nil {
		return fmt.Errorf("svo is required")
	}
	snapshot, err := readChunkSnapshot(chunkMapChunkPath(m.rootDir, chunkX, chunkY))
	if err != nil {
		return fmt.Errorf("loading chunk (%d,%d): %w", chunkX, chunkY, err)
	}
	if err := svo.LoadSnapshot(snapshot); err != nil {
		return fmt.Errorf("restoring chunk (%d,%d): %w", chunkX, chunkY, err)
	}
	return nil
}

func composeChunkScene(target *world.SVO, request BuildRequest, chunkSize uint, paletteColors []uint32, load func(chunkX, chunkY int) (*world.SVO, error)) error {
	if target == nil {
		return fmt.Errorf("target svo is required")
	}
	if chunkSize == 0 {
		return fmt.Errorf("chunk size must be non-zero")
	}
	if load == nil {
		return fmt.Errorf("chunk loader callback is required")
	}
	request = request.Normalized()
	type chunkPart struct {
		originX uint
		originY uint
		scene   *world.SVO
	}
	parts := make([]chunkPart, 0, request.ChunkSpan()*request.ChunkSpan())
	var loadErr error
	request.ForEachChunk(chunkSize, func(chunkX, chunkY int, originX, originY uint) {
		if loadErr != nil {
			return
		}
		chunk, err := load(chunkX, chunkY)
		if err != nil {
			loadErr = err
			return
		}
		parts = append(parts, chunkPart{originX: originX, originY: originY, scene: chunk})
	})
	if loadErr != nil {
		return loadErr
	}
	target.BuildTreeSparseVolumesWithMaterialBricks(request.SceneSize(chunkSize), paletteColors, func(addCube func(x, y, z, cubeSize uint, color uint32), addBrick func(x, y, z uint, voxels *[world.BrickVoxelCount]uint8)) {
		for _, part := range parts {
			part.scene.EmitTranslatedMaterialVolumes(part.originX, part.originY, 0, addCube, addBrick)
		}
	})
	return nil
}

func ensureGeneratedChunkMapTarget(rootDir string, want generatedChunkMapManifest) (bool, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return false, fmt.Errorf("creating chunk map root directory: %w", err)
	}
	path := filepath.Join(rootDir, generatedChunkMapManifestName)
	if _, err := os.Stat(path); err == nil {
		have, err := readGeneratedChunkMapManifest(rootDir)
		if err != nil {
			return false, err
		}
		if have.Name == want.Name &&
			have.ChunkSize == want.ChunkSize &&
			have.CameraDriven == want.CameraDriven &&
			have.GeneratorRevision == want.GeneratorRevision &&
			equalPaletteColors(have.PaletteColors, want.PaletteColors) {
			return true, nil
		}
		stagingRoot := generatedChunkMapStagingRoot(rootDir)
		if err := os.RemoveAll(stagingRoot); err != nil {
			return false, fmt.Errorf("clearing stale staging root: %w", err)
		}
		if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
			return false, fmt.Errorf("creating chunk map staging root: %w", err)
		}
		if err := writeGeneratedChunkMapManifest(stagingRoot, want); err != nil {
			return false, err
		}
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("reading chunk map manifest: %w", err)
	}
	if err := writeGeneratedChunkMapManifest(rootDir, want); err != nil {
		return false, err
	}
	return true, nil
}

func writeGeneratedChunkMapManifest(rootDir string, manifest generatedChunkMapManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding chunk map manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(rootDir, generatedChunkMapManifestName)
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("writing chunk map manifest: %w", err)
	}
	return nil
}

func promoteGeneratedChunkMapRoot(rootDir, stagingRoot string) error {
	backupRoot := rootDir + ".backup"
	if err := os.RemoveAll(backupRoot); err != nil {
		return fmt.Errorf("clearing chunk map backup root: %w", err)
	}
	if _, err := os.Stat(rootDir); err == nil {
		if err := os.Rename(rootDir, backupRoot); err != nil {
			return fmt.Errorf("moving existing chunk map root aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading existing chunk map root: %w", err)
	}
	if err := os.Rename(stagingRoot, rootDir); err != nil {
		if _, restoreErr := os.Stat(backupRoot); restoreErr == nil {
			_ = os.Rename(backupRoot, rootDir)
		}
		return fmt.Errorf("activating regenerated chunk map root: %w", err)
	}
	if err := os.RemoveAll(backupRoot); err != nil {
		return fmt.Errorf("removing old chunk map root: %w", err)
	}
	return nil
}

func readGeneratedChunkMapManifest(rootDir string) (generatedChunkMapManifest, error) {
	path := filepath.Join(rootDir, generatedChunkMapManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return generatedChunkMapManifest{}, fmt.Errorf("reading chunk map manifest: %w", err)
	}
	var manifest generatedChunkMapManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return generatedChunkMapManifest{}, fmt.Errorf("decoding chunk map manifest: %w", err)
	}
	if manifest.Version != generatedChunkMapVersion {
		return generatedChunkMapManifest{}, fmt.Errorf("unsupported chunk map version %d", manifest.Version)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return generatedChunkMapManifest{}, fmt.Errorf("chunk map manifest name is required")
	}
	if manifest.ChunkSize == 0 {
		return generatedChunkMapManifest{}, fmt.Errorf("chunk map manifest chunk size must be non-zero")
	}
	return manifest, nil
}

func chunkMapChunkPath(rootDir string, chunkX, chunkY int) string {
	return filepath.Join(rootDir, generatedChunkMapChunkDir, fmt.Sprintf("%d_%d%s", chunkX, chunkY, generatedChunkMapChunkExt))
}

func generatedChunkMapSlug(name string) string {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		return "map"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(builder.String(), "-")
	if value == "" {
		return "map"
	}
	return value
}

func equalPaletteColors(left, right []uint32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeChunkSnapshot(path string, snapshot world.Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := encodeChunkSnapshot(snapshot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return nil
}

func readChunkSnapshot(path string) (world.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return world.Snapshot{}, err
	}
	return decodeChunkSnapshot(data)
}

func encodeChunkSnapshot(snapshot world.Snapshot) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := buffer.WriteString(generatedChunkMagic); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(generatedChunkMapVersion)); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(snapshot.Words))); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(snapshot.Bricks))); err != nil {
		return nil, err
	}
	hasBounds := uint8(0)
	if snapshot.HasOccupiedBounds {
		hasBounds = 1
	}
	if err := binary.Write(&buffer, binary.LittleEndian, hasBounds); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, snapshot.OccupiedMin); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, snapshot.OccupiedMax); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, snapshot.Words); err != nil {
		return nil, err
	}
	for _, brick := range snapshot.Bricks {
		if err := binary.Write(&buffer, binary.LittleEndian, brick.NodeIndex); err != nil {
			return nil, err
		}
		if err := binary.Write(&buffer, binary.LittleEndian, brick.Origin); err != nil {
			return nil, err
		}
		voxels := brick.Voxels
		if voxels == nil {
			voxels = new([world.BrickVoxelCount]uint8)
		}
		if _, err := buffer.Write(voxels[:]); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

func decodeChunkSnapshot(data []byte) (world.Snapshot, error) {
	reader := bytes.NewReader(data)
	magic := make([]byte, len(generatedChunkMagic))
	if _, err := io.ReadFull(reader, magic); err != nil {
		return world.Snapshot{}, err
	}
	if string(magic) != generatedChunkMagic {
		return world.Snapshot{}, fmt.Errorf("unexpected chunk magic %q", string(magic))
	}
	var version uint32
	if err := binary.Read(reader, binary.LittleEndian, &version); err != nil {
		return world.Snapshot{}, err
	}
	if version != generatedChunkMapVersion {
		return world.Snapshot{}, fmt.Errorf("unsupported chunk snapshot version %d", version)
	}
	var wordCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &wordCount); err != nil {
		return world.Snapshot{}, err
	}
	var brickCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &brickCount); err != nil {
		return world.Snapshot{}, err
	}
	var hasBounds uint8
	if err := binary.Read(reader, binary.LittleEndian, &hasBounds); err != nil {
		return world.Snapshot{}, err
	}
	snapshot := world.Snapshot{
		Words:             make([]uint32, wordCount),
		Bricks:            make([]world.Brick, brickCount),
		HasOccupiedBounds: hasBounds != 0,
	}
	if err := binary.Read(reader, binary.LittleEndian, &snapshot.OccupiedMin); err != nil {
		return world.Snapshot{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &snapshot.OccupiedMax); err != nil {
		return world.Snapshot{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, snapshot.Words); err != nil {
		return world.Snapshot{}, err
	}
	for index := range snapshot.Bricks {
		var brick world.Brick
		if err := binary.Read(reader, binary.LittleEndian, &brick.NodeIndex); err != nil {
			return world.Snapshot{}, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &brick.Origin); err != nil {
			return world.Snapshot{}, err
		}
		voxels := new([world.BrickVoxelCount]uint8)
		if _, err := io.ReadFull(reader, voxels[:]); err != nil {
			return world.Snapshot{}, err
		}
		brick.Voxels = voxels
		snapshot.Bricks[index] = brick
	}
	if reader.Len() != 0 {
		return world.Snapshot{}, fmt.Errorf("chunk snapshot contains %d trailing bytes", reader.Len())
	}
	return snapshot, nil
}
