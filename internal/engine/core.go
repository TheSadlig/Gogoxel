package engine

import (
	"fmt"
	"math"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
)

const (
	defaultTickRateHz      = 60
	moveUnitsPerSecond     = float32(6)
	turnDegreesPerSecond   = float32(120)
	maxPitchDegrees        = float32(89)
)

type Config struct {
	TickRateHz int
	StartTime  time.Time
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
	clock         Clock
	input         *input.Manager
	heldActions   input.Snapshot
	catalog       *GeneratorCatalog
	camera        platform.Camera
	svo           *world.SVO
	cursor        CursorSample
	generatorName string
	sceneVersion  uint64
	elapsed       time.Duration
	tickRateHz    int
	tickDuration  time.Duration
	selectedEditMaterial int
	placeStroke   continuousEditState
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
	clear(c.heldActions)
	c.input.UpdateSnapshot(nil, c.clock.Now())
	c.camera = defaultCamera()
	c.svo = nil
	c.generatorName = ""
	c.elapsed = 0
	c.sceneVersion++
}

func (c *Core) LoadGenerator(name string) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	if c.catalog == nil {
		return fmt.Errorf("generator catalog is not initialized")
	}
	svo, err := c.catalog.Build(name)
	if err != nil {
		return err
	}
	c.svo = svo
	c.generatorName = name
	c.resetCameraForScene()
	c.sceneVersion++
	return nil
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

func (c *Core) SetCamera(camera platform.Camera) {
	if c == nil {
		return
	}
	c.camera = camera
	c.clampCamera()
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
	c.advanceCamera(delta)
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
		_, _ = c.EditAtCursor(EditModePlace, c.cursor)
		c.placeStroke = continuousEditState{
			active:  true,
			last:    cursorEditSample{camera: c.camera, cursor: c.cursor},
			surface: cloneSVO(c.svo),
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

func (c *Core) advanceCamera(delta time.Duration) {
	if c == nil || delta <= 0 {
		return
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

func (c *Core) resetCameraForScene() {
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
	span := maxFloat32(
		float32(maxBounds[0]-minBounds[0]),
		float32(maxBounds[1]-minBounds[1]),
		float32(maxBounds[2]-minBounds[2]),
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