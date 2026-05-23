package engine

import (
	"fmt"
	"math"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
)

const (
	defaultTickRateHz    = 60
	moveUnitsPerSecond   = float32(6)
	turnDegreesPerSecond = float32(120)
	maxPitchDegrees      = float32(89)
	maxAutoChunkRange    = 10
)

type Config struct {
	TickRateHz int
	StartTime  time.Time
}

type GeneratorLoadRequest struct {
	Name       string
	ChunkX     int
	ChunkY     int
	ChunkRange int
}

type Snapshot struct {
	Camera        platform.Camera
	GeneratorName string
	SceneLoaded   bool
	NodeCount     int
	BrickCount    int
	WorldSize     uint
	SceneVersion  uint64
}

type Core struct {
	clock                 Clock
	input                 *input.Manager
	heldActions           input.Snapshot
	catalog               *GeneratorCatalog
	chunkScene            *chunkSceneManager
	generator             generators.Generator
	camera                platform.Camera
	sceneWorldOrigin      [3]float32
	svo                   *world.SVO
	cursor                CursorSample
	generatorName         string
	generatorBuildRequest generators.BuildRequest
	generatorChunkSize    uint
	generatorAutoRange    bool
	sceneVersion          uint64
	elapsed               time.Duration
	tickRateHz            int
	tickDuration          time.Duration
	selectedEditMaterial  int
	placeStroke           continuousEditState
}

func NewCore(catalog *GeneratorCatalog, cfg Config) *Core {
	if cfg.TickRateHz <= 0 {
		cfg.TickRateHz = defaultTickRateHz
	}
	bindings := make(map[input.Action]input.Binding, len(control.DefaultPolicies()))
	for action, policy := range control.DefaultPolicies() {
		bindings[action] = input.Binding{Cooldown: policy.Cooldown}
	}
	return &Core{
		clock:        NewManualClock(cfg.StartTime),
		input:        input.NewManager(bindings),
		heldActions:  make(input.Snapshot),
		catalog:      catalog,
		camera:       defaultCamera(),
		cursor:       CursorSample{NormalizedX: 0.5, NormalizedY: 0.5},
		tickRateHz:   cfg.TickRateHz,
		tickDuration: time.Second / time.Duration(cfg.TickRateHz),
	}
}

func (c *Core) LoadDefaultGenerator() error {
	if c == nil || c.catalog == nil {
		return nil
	}
	defaultName := c.catalog.DefaultName()
	if defaultName == "" {
		return nil
	}
	return c.LoadGenerator(defaultName)
}

func (c *Core) Reset() {
	if c == nil {
		return
	}
	c.closeChunkSceneManager()
	clear(c.heldActions)
	c.input.UpdateSnapshot(nil, c.clock.Now())
	c.camera = defaultCamera()
	c.sceneWorldOrigin = [3]float32{}
	c.svo = nil
	c.generator = nil
	c.generatorName = ""
	c.generatorBuildRequest = generators.BuildRequest{}
	c.generatorChunkSize = 0
	c.generatorAutoRange = false
	c.elapsed = 0
	c.sceneVersion++
}

func (c *Core) LoadGenerator(name string) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	item, err := c.lookupGenerator(name)
	if err != nil {
		return err
	}
	if streamGenerator, ok := asChunkStreamGenerator(item); ok {
		worldCamera := c.worldCamera()
		buildRequest := c.cameraBuildRequest(item.ChunkSize(), worldCamera, generators.BuildRequest{}, true)
		return c.loadChunkStreamGenerator(streamGenerator, name, buildRequest, true, worldCamera, true)
	}
	if !isCameraDrivenGenerator(item) {
		return c.loadGenerator(item, name, generators.BuildRequest{}, false, platform.Camera{}, false)
	}
	worldCamera := c.worldCamera()
	buildRequest := c.cameraBuildRequest(item.ChunkSize(), worldCamera, generators.BuildRequest{}, true)
	return c.loadGenerator(item, name, buildRequest, true, worldCamera, true)
}

func (c *Core) LoadGeneratorAt(request GeneratorLoadRequest) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	item, err := c.lookupGenerator(request.Name)
	if err != nil {
		return err
	}
	buildRequest := generators.BuildRequest{
		ChunkX:     request.ChunkX,
		ChunkY:     request.ChunkY,
		ChunkRange: request.ChunkRange,
	}.Normalized()
	if streamGenerator, ok := asChunkStreamGenerator(item); ok {
		return c.loadChunkStreamGenerator(streamGenerator, request.Name, buildRequest, false, platform.Camera{}, false)
	}
	return c.loadGenerator(item, request.Name, buildRequest, false, platform.Camera{}, false)
}

func (c *Core) lookupGenerator(name string) (generators.Generator, error) {
	if c == nil {
		return nil, fmt.Errorf("engine core is not initialized")
	}
	if c.catalog == nil {
		return nil, fmt.Errorf("generator catalog is not initialized")
	}
	item, ok := c.catalog.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown generator %q", name)
	}
	return item, nil
}

func (c *Core) loadGenerator(item generators.Generator, name string, buildRequest generators.BuildRequest, preserveWorldCamera bool, worldCamera platform.Camera, autoRange bool) error {
	c.closeChunkSceneManager()
	buildRequest = buildRequest.Normalized()
	svo, chunkSize, err := c.catalog.Build(name, buildRequest)
	if err != nil {
		return err
	}
	c.svo = svo
	c.generator = item
	c.generatorName = name
	c.generatorBuildRequest = buildRequest
	c.generatorChunkSize = chunkSize
	c.generatorAutoRange = autoRange
	worldOriginX, worldOriginY := buildRequest.WorldOrigin(chunkSize)
	c.sceneWorldOrigin = [3]float32{worldOriginX, worldOriginY, 0}
	if preserveWorldCamera {
		c.camera = c.localCamera(worldCamera)
		c.clampCamera()
	} else {
		c.resetCameraForScene(buildRequest, chunkSize)
	}
	c.sceneVersion++
	return nil
}

func (c *Core) loadChunkStreamGenerator(item generators.ChunkStreamGenerator, name string, buildRequest generators.BuildRequest, preserveWorldCamera bool, worldCamera platform.Camera, autoRange bool) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	c.closeChunkSceneManager()

	manager := newChunkSceneManager(item)
	buildRequest = buildRequest.Normalized()
	svo, err := manager.LoadInitial(buildRequest)
	if err != nil {
		manager.Close()
		return err
	}

	c.chunkScene = manager
	c.svo = svo
	c.generator = item
	c.generatorName = name
	c.generatorBuildRequest = buildRequest
	c.generatorChunkSize = item.ChunkSize()
	c.generatorAutoRange = autoRange
	worldOriginX, worldOriginY := buildRequest.WorldOrigin(c.generatorChunkSize)
	c.sceneWorldOrigin = [3]float32{worldOriginX, worldOriginY, 0}
	if preserveWorldCamera {
		c.camera = c.localCamera(worldCamera)
		c.clampCamera()
	} else {
		c.resetCameraForScene(buildRequest, c.generatorChunkSize)
	}
	c.sceneVersion++
	return nil
}

func (c *Core) closeChunkSceneManager() {
	if c == nil || c.chunkScene == nil {
		return
	}
	c.chunkScene.Close()
	c.chunkScene = nil
}

func (c *Core) TickRateHz() int {
	if c == nil {
		return defaultTickRateHz
	}
	return c.tickRateHz
}

func (c *Core) TickDuration() time.Duration {
	if c == nil {
		return time.Second / defaultTickRateHz
	}
	return c.tickDuration
}

func (c *Core) SetTickRateHz(rate int) {
	if c == nil || rate <= 0 {
		return
	}
	c.tickRateHz = rate
	c.tickDuration = time.Second / time.Duration(rate)
}

func (c *Core) SetHeldActions(snapshot input.Snapshot) {
	if c == nil {
		return
	}
	next := make(input.Snapshot, len(snapshot))
	for action, down := range snapshot {
		if !down {
			continue
		}
		next[action] = true
	}
	c.heldActions = next
}

func (c *Core) PressAction(action input.Action) {
	if c == nil {
		return
	}
	if c.heldActions == nil {
		c.heldActions = make(input.Snapshot)
	}
	c.heldActions[action] = true
}

func (c *Core) ReleaseAction(action input.Action) {
	if c == nil || c.heldActions == nil {
		return
	}
	delete(c.heldActions, action)
}

func (c *Core) Camera() platform.Camera {
	if c == nil {
		return platform.Camera{}
	}
	return c.camera
}

func (c *Core) SceneWorldOrigin() [3]int32 {
	if c == nil {
		return [3]int32{}
	}
	return [3]int32{int32(c.sceneWorldOrigin[0]), int32(c.sceneWorldOrigin[1]), int32(c.sceneWorldOrigin[2])}
}

func (c *Core) SetCamera(camera platform.Camera) {
	if c == nil {
		return
	}
	c.camera = camera
	c.clampCamera()
	_ = c.syncCameraDrivenGenerator()
}

func (c *Core) CurrentGeneratorName() string {
	if c == nil {
		return ""
	}
	return c.generatorName
}

func (c *Core) CurrentSVO() *world.SVO {
	if c == nil {
		return nil
	}
	return c.svo
}

func (c *Core) SceneVersion() uint64 {
	if c == nil {
		return 0
	}
	return c.sceneVersion
}

func (c *Core) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{
		Camera:        c.camera,
		GeneratorName: c.generatorName,
		SceneLoaded:   c.svo != nil,
		SceneVersion:  c.sceneVersion,
	}
	if c.svo != nil {
		snapshot.NodeCount = c.svo.NodeCount()
		snapshot.BrickCount = c.svo.BrickCount()
		snapshot.WorldSize = c.svo.Size()
	}
	return snapshot
}

func (c *Core) Step(delta time.Duration) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	if delta < 0 {
		return fmt.Errorf("delta must be non-negative")
	}
	if delta > 0 {
		c.elapsed += delta
		c.clock.Advance(delta)
	}
	c.input.UpdateSnapshot(c.heldActions, c.clock.Now())
	if err := c.handleGeneratorCycle(); err != nil {
		return err
	}
	c.handleMaterialCycle()
	c.handleCursorEdits()
	if err := c.advanceCamera(delta); err != nil {
		return err
	}
	return nil
}

func (c *Core) StepTicks(count int) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	if count < 0 {
		return fmt.Errorf("tick count must be non-negative")
	}
	for index := 0; index < count; index++ {
		if err := c.Step(c.tickDuration); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) handleGeneratorCycle() error {
	if c == nil || c.catalog == nil || !c.input.Triggered(control.ActionNextModel) {
		return nil
	}
	next := c.catalog.NextName(c.generatorName)
	if next == "" || next == c.generatorName {
		return nil
	}
	return c.LoadGenerator(next)
}

func (c *Core) handleMaterialCycle() {
	if c == nil {
		return
	}
	if c.input.Triggered(control.ActionNextMaterial) {
		c.cycleSelectedEditMaterial(1)
	}
	if c.input.Triggered(control.ActionPreviousMaterial) {
		c.cycleSelectedEditMaterial(-1)
	}
}

func (c *Core) handleCursorEdits() {
	if c == nil || c.svo == nil {
		return
	}
	if c.input.Triggered(control.ActionPlaceCube) {
		strokeSurface := cloneSVO(c.svo)
		_, _ = c.applyCursorEditSamples(EditModePlace, []cursorEditSample{{camera: c.camera, cursor: c.cursor}}, strokeSurface, true)
		c.placeStroke = continuousEditState{
			active:  true,
			last:    cursorEditSample{camera: c.camera, cursor: c.cursor},
			surface: strokeSurface,
		}
	} else if c.input.Down(control.ActionPlaceCube) {
		c.continuePlaceStroke()
	} else {
		c.placeStroke = continuousEditState{}
	}
	if c.input.Triggered(control.ActionRemoveCube) {
		_, _ = c.EditAtCursor(EditModeRemove, c.cursor)
	}
}

func (c *Core) advanceCamera(delta time.Duration) error {
	if c == nil || delta <= 0 {
		return nil
	}
	deltaSeconds := float32(delta.Seconds())
	moveStep := moveUnitsPerSecond * deltaSeconds
	turnStep := turnDegreesPerSecond * deltaSeconds

	forward := c.camera.Forward()
	walkForward := [3]float32{forward[0], forward[1], 0}
	walkForwardLength := float32(math.Sqrt(float64(walkForward[0]*walkForward[0] + walkForward[1]*walkForward[1])))
	if walkForwardLength > 0 {
		walkForward[0] /= walkForwardLength
		walkForward[1] /= walkForwardLength
	}
	right := c.camera.Right()
	if c.input.Down(control.ActionFaster) {
		moveStep *= 100
		turnStep *= 2
	}

	if c.input.Down(control.ActionMoveForward) {
		c.camera.Position[0] += walkForward[0] * moveStep
		c.camera.Position[1] += walkForward[1] * moveStep
	}
	if c.input.Down(control.ActionMoveBackward) {
		c.camera.Position[0] -= walkForward[0] * moveStep
		c.camera.Position[1] -= walkForward[1] * moveStep
	}
	if c.input.Down(control.ActionMoveLeft) {
		c.camera.Position[0] -= right[0] * moveStep
		c.camera.Position[1] -= right[1] * moveStep
	}
	if c.input.Down(control.ActionMoveRight) {
		c.camera.Position[0] += right[0] * moveStep
		c.camera.Position[1] += right[1] * moveStep
	}
	if c.input.Down(control.ActionMoveAway) {
		c.camera.Position[0] -= forward[0] * moveStep
		c.camera.Position[1] -= forward[1] * moveStep
		c.camera.Position[2] -= forward[2] * moveStep
	}
	if c.input.Down(control.ActionMoveCloser) {
		c.camera.Position[0] += forward[0] * moveStep
		c.camera.Position[1] += forward[1] * moveStep
		c.camera.Position[2] += forward[2] * moveStep
	}
	if c.input.Down(control.ActionMoveUp) {
		c.camera.Position[2] += moveStep
	}
	if c.input.Down(control.ActionMoveDown) {
		c.camera.Position[2] -= moveStep
	}
	if c.input.Down(control.ActionYawDown) {
		c.camera.YawDeg -= turnStep
	}
	if c.input.Down(control.ActionYawUp) {
		c.camera.YawDeg += turnStep
	}
	if c.input.Down(control.ActionTurnRight) {
		c.camera.PitchDeg -= turnStep
	}
	if c.input.Down(control.ActionTurnLeft) {
		c.camera.PitchDeg += turnStep
	}
	c.clampCamera()
	return c.syncCameraDrivenGenerator()
}

func (c *Core) syncCameraDrivenGenerator() error {
	if c == nil || c.generator == nil || !isCameraDrivenGenerator(c.generator) || c.generatorChunkSize == 0 {
		return nil
	}
	worldCamera := c.worldCamera()
	if c.chunkScene != nil {
		nextRequest := c.cameraBuildRequest(c.generatorChunkSize, worldCamera, c.generatorBuildRequest, c.generatorAutoRange)
		c.chunkScene.SetRequest(nextRequest)
		return c.applyChunkSceneUpdate(worldCamera)
	}
	nextRequest := c.cameraBuildRequest(c.generatorChunkSize, worldCamera, c.generatorBuildRequest, c.generatorAutoRange)
	if nextRequest == c.generatorBuildRequest {
		return nil
	}
	return c.loadGenerator(c.generator, c.generatorName, nextRequest, true, worldCamera, c.generatorAutoRange)
}

func (c *Core) applyChunkSceneUpdate(worldCamera platform.Camera) error {
	if c == nil || c.chunkScene == nil {
		return nil
	}
	scene, request, err, ok := c.chunkScene.TakeUpdate()
	if !ok {
		return nil
	}
	if err != nil {
		return err
	}
	if scene == nil {
		return nil
	}

	c.svo = scene
	c.generatorBuildRequest = request
	worldOriginX, worldOriginY := request.WorldOrigin(c.generatorChunkSize)
	c.sceneWorldOrigin = [3]float32{worldOriginX, worldOriginY, 0}
	c.camera = c.localCamera(worldCamera)
	c.clampCamera()
	c.sceneVersion++
	return nil
}

func (c *Core) cameraBuildRequest(chunkSize uint, worldCamera platform.Camera, current generators.BuildRequest, autoRange bool) generators.BuildRequest {
	request := current.Normalized()
	cameraChunkX := chunkIndexForPosition(worldCamera.Position[0], chunkSize)
	cameraChunkY := chunkIndexForPosition(worldCamera.Position[1], chunkSize)
	if autoRange {
		request.ChunkRange = cameraChunkRange(worldCamera, chunkSize)
	}
	desiredChunkX, desiredChunkY := forwardBiasedChunkCenter(cameraChunkX, cameraChunkY, worldCamera, request.ChunkRange)
	if current.Normalized() == (generators.BuildRequest{}) || current.ChunkRange != request.ChunkRange {
		request.ChunkX = desiredChunkX
		request.ChunkY = desiredChunkY
		return request
	}
	request.ChunkX = stabilizedCameraChunkCenter(request.ChunkX, desiredChunkX, request.ChunkRange)
	request.ChunkY = stabilizedCameraChunkCenter(request.ChunkY, desiredChunkY, request.ChunkRange)
	return request
}

func forwardBiasedChunkCenter(cameraChunkX, cameraChunkY int, camera platform.Camera, chunkRange int) (int, int) {
	lookahead := chunkLookahead(chunkRange)
	if lookahead <= 0 {
		return cameraChunkX, cameraChunkY
	}
	forward := camera.Forward()
	horizontalX := float64(forward[0])
	horizontalY := float64(forward[1])
	horizontalLength := math.Hypot(horizontalX, horizontalY)
	if horizontalLength == 0 {
		return cameraChunkX, cameraChunkY
	}
	offsetX := int(math.Round(horizontalX / horizontalLength * float64(lookahead)))
	offsetY := int(math.Round(horizontalY / horizontalLength * float64(lookahead)))
	return cameraChunkX + offsetX, cameraChunkY + offsetY
}

func chunkLookahead(chunkRange int) int {
	if chunkRange <= 0 {
		return 0
	}
	if chunkRange == 1 {
		return 1
	}
	return chunkRange - 1
}

func stabilizedCameraChunkCenter(currentCenter, cameraChunk, chunkRange int) int {
	hysteresis := max(0, chunkRange-1)
	if cameraChunk < currentCenter-hysteresis {
		return cameraChunk + hysteresis
	}
	if cameraChunk > currentCenter+hysteresis {
		return cameraChunk - hysteresis
	}
	return currentCenter
}

func (c *Core) worldCamera() platform.Camera {
	if c == nil {
		return platform.Camera{}
	}
	worldCamera := c.camera
	worldCamera.Position[0] += c.sceneWorldOrigin[0]
	worldCamera.Position[1] += c.sceneWorldOrigin[1]
	worldCamera.Position[2] += c.sceneWorldOrigin[2]
	return worldCamera
}

func (c *Core) localCamera(worldCamera platform.Camera) platform.Camera {
	if c == nil {
		return platform.Camera{}
	}
	localCamera := worldCamera
	localCamera.Position[0] -= c.sceneWorldOrigin[0]
	localCamera.Position[1] -= c.sceneWorldOrigin[1]
	localCamera.Position[2] -= c.sceneWorldOrigin[2]
	return localCamera
}

func isCameraDrivenGenerator(item generators.Generator) bool {
	if item == nil {
		return false
	}
	cameraDriven, ok := item.(generators.CameraDrivenGenerator)
	return ok && cameraDriven.CameraDriven()
}

func asChunkStreamGenerator(item generators.Generator) (generators.ChunkStreamGenerator, bool) {
	if item == nil {
		return nil, false
	}
	streamGenerator, ok := item.(generators.ChunkStreamGenerator)
	return streamGenerator, ok
}

func chunkIndexForPosition(position float32, chunkSize uint) int {
	if chunkSize == 0 {
		return 0
	}
	return int(math.Floor(float64(position) / float64(chunkSize)))
}

func cameraChunkRange(camera platform.Camera, chunkSize uint) int {
	if chunkSize == 0 {
		return 0
	}
	halfFovRad := float64(camera.FovDeg) * math.Pi / 360
	if halfFovRad <= 0 {
		halfFovRad = math.Pi / 6
	}
	altitude := float32(math.Abs(float64(camera.Position[2])))
	lateralReach := altitude * float32(math.Tan(halfFovRad))
	forwardReach := float32(0)
	bottomPitchRad := float64(camera.PitchDeg)*math.Pi/180 - halfFovRad
	if bottomPitchRad < -0.017453292519943295 {
		tanPitch := math.Tan(-bottomPitchRad)
		if tanPitch > 0 {
			forwardReach = altitude / float32(tanPitch)
		}
	}
	visibleReach := maxFloat32(lateralReach, forwardReach)
	if visibleReach <= float32(chunkSize) {
		return 0
	}
	rangeChunks := int(math.Ceil(float64(visibleReach) / float64(chunkSize)))
	if rangeChunks > maxAutoChunkRange {
		return maxAutoChunkRange
	}
	return rangeChunks
}

func (c *Core) clampCamera() {
	if c.camera.PitchDeg > maxPitchDegrees {
		c.camera.PitchDeg = maxPitchDegrees
	}
	if c.camera.PitchDeg < -maxPitchDegrees {
		c.camera.PitchDeg = -maxPitchDegrees
	}
}

func defaultCamera() platform.Camera {
	return platform.Camera{
		Position: [3]float32{-140, 500, 500},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}
}

func (c *Core) resetCameraForScene(request generators.BuildRequest, chunkSize uint) {
	if c == nil {
		return
	}
	minBounds, maxBounds, ok := [3]uint32{}, [3]uint32{}, false
	if c.svo != nil {
		minBounds, maxBounds, ok = c.svo.OccupiedBounds()
	}
	if !ok {
		c.camera = defaultCamera()
		return
	}

	centerX := (float32(minBounds[0]) + float32(maxBounds[0])) * 0.5
	centerY := (float32(minBounds[1]) + float32(maxBounds[1])) * 0.5
	centerZ := (float32(minBounds[2]) + float32(maxBounds[2])) * 0.5
	if request != (generators.BuildRequest{}) && chunkSize > 0 {
		centerX, centerY = request.LocalChunkCenter(chunkSize)
	}
	requestedSpan := float32(0)
	if request != (generators.BuildRequest{}) && chunkSize > 0 {
		requestedSpan = float32(request.ExactSceneSize(chunkSize))
	}
	span := maxFloat32(
		float32(maxBounds[0]-minBounds[0]),
		float32(maxBounds[1]-minBounds[1]),
		float32(maxBounds[2]-minBounds[2]),
		requestedSpan,
	)
	if span < 16 {
		span = 16
	}

	c.camera = platform.Camera{
		Position: [3]float32{centerX - span*1.35, centerY - span*1.35, centerZ + span*0.75},
		YawDeg:   45,
		PitchDeg: -18,
		FovDeg:   60,
	}
}

func maxFloat32(values ...float32) float32 {
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
