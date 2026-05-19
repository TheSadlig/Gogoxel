package game

import (
	"fmt"
	"math"
	"time"

	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan"
	"Gogoxel/internal/world"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const windowTitle = "Gogoxel Vulkan"

const moveCooldown = 120 * time.Millisecond
const modelSwitchCooldown = 300 * time.Millisecond

const (
	actionMoveForward  input.Action = "move_forward"
	actionMoveBackward input.Action = "move_backward"
	actionMoveLeft     input.Action = "move_left"
	actionMoveRight    input.Action = "move_right"
	actionMoveAway     input.Action = "move_away"
	actionMoveCloser   input.Action = "move_closer"
	actionMoveUp       input.Action = "move_up"
	actionMoveDown     input.Action = "move_down"
	actionYawDown      input.Action = "yaw_down"
	actionYawUp        input.Action = "yaw_up"
	actionTurnRight    input.Action = "turn_right"
	actionTurnLeft     input.Action = "turn_left"
	actionFaster       input.Action = "faster"
	actionNextModel    input.Action = "next_model"
)

type Game struct {
	window   *platform.Window
	renderer *vulkan.Renderer
	input    *input.Manager
	elapsed  time.Duration

	fpsFrames  int
	fpsElapsed time.Duration
	lastFPS    float64
	camera     platform.Camera
	chunk      *vulkan.ChunkResources
	bindings   *vulkan.ChunkBindings
	raytracer  *vulkan.RaytracerPipeline
	generators []generators.Generator

	svo            *world.SVO
	generatorIndex int
}

func New() *Game {
	availableGenerators := generators.DefaultGenerators()
	return &Game{
		input:      input.NewManager(defaultBindings()),
		generators: availableGenerators,
		camera: platform.Camera{
			Position: [3]float32{-140, 500, 500},
			YawDeg:   0,
			PitchDeg: 0,
			FovDeg:   60,
		},
	}
}

func (g *Game) InitChunk() error {
	if g.renderer == nil {
		return fmt.Errorf("renderer is not initialized")
	}
	if g.bindings == nil {
		return fmt.Errorf("chunk bindings are not initialized")
	}
	currentGenerator := g.currentGenerator()
	if currentGenerator == nil {
		return fmt.Errorf("no generators are available")
	}

	nextSVO := world.NewSVO()
	if err := currentGenerator.BuildSVO(nextSVO); err != nil {
		return fmt.Errorf("building generator %q: %w", currentGenerator.Name(), err)
	}

	nextChunk, err := g.renderer.CreateChunkResourcesFromSVO(g.bindings, nextSVO)
	if err != nil {
		return fmt.Errorf("creating chunk resources from svo: %w", err)
	}

	previousChunk := g.chunk
	previousSVO := g.svo
	g.chunk = nextChunk
	g.svo = nextSVO
	if previousChunk != nil {
		if err := g.renderer.WaitIdle(); err != nil {
			g.chunk = previousChunk
			g.svo = previousSVO
			nextChunk.Close()
			return fmt.Errorf("waiting for renderer before chunk swap: %w", err)
		}
		previousChunk.Close()
	}
	g.resetCameraForScene()
	g.updateWindowTitle()

	return nil
}

func (g *Game) Run() error {
	window, err := platform.NewWindow(windowTitle, vulkan.DefaultWindowWidth, vulkan.DefaultWindowHeight)
	if err != nil {
		return err
	}
	defer window.Close()

	renderer, err := vulkan.New(window)
	if err != nil {
		return err
	}
	defer renderer.Close()

	bindings, err := renderer.NewChunkBindings()
	if err != nil {
		return err
	}
	defer bindings.Close()

	raytracer, err := renderer.NewRaytracerPipeline(bindings)
	if err != nil {
		return err
	}
	defer raytracer.Close()

	g.window = window
	g.renderer = renderer
	g.bindings = bindings
	g.raytracer = raytracer
	if err := g.InitChunk(); err != nil {
		return err
	}
	defer func() {
		if g.chunk != nil {
			g.chunk.Close()
		}
	}()
	g.updateWindowTitle()
	lastFrame := time.Now()

	for !window.ShouldClose() {
		window.PollEvents()
		if window.IsIconified() {
			lastFrame = time.Now()
			continue
		}

		now := time.Now()
		delta := now.Sub(lastFrame)
		lastFrame = now

		if err := g.Update(delta); err != nil {
			return err
		}
		if err := renderer.DrawFrame(g.Render); err != nil {
			return err
		}
		g.recordFrame(delta)
	}

	return renderer.WaitIdle()
}

func (g *Game) Update(delta time.Duration) error {
	g.elapsed += delta
	g.input.Update(g.window, time.Now())
	if g.input.Triggered(actionNextModel) && len(g.generators) > 1 {
		g.generatorIndex = (g.generatorIndex + 1) % len(g.generators)
		if err := g.InitChunk(); err != nil {
			return fmt.Errorf("switching example model: %w", err)
		}
	}

	const moveUnitsPerSecond = float32(6)
	const turnDegreesPerSecond = float32(120)
	const maxPitch = float32(89)
	deltaSeconds := float32(delta.Seconds())
	moveStep := moveUnitsPerSecond * deltaSeconds
	turnStep := turnDegreesPerSecond * deltaSeconds

	forward := g.camera.Forward()
	walkForward := [3]float32{forward[0], forward[1], 0}
	walkForwardLength := float32(math.Sqrt(float64(walkForward[0]*walkForward[0] + walkForward[1]*walkForward[1])))
	if walkForwardLength > 0 {
		walkForward[0] /= walkForwardLength
		walkForward[1] /= walkForwardLength
	}
	right := g.camera.Right()
	if g.input.Down(actionFaster) {
		moveStep *= 10
		turnStep *= 2
	}

	if g.input.Down(actionMoveForward) {
		g.camera.Position[0] += walkForward[0] * moveStep
		g.camera.Position[1] += walkForward[1] * moveStep
	}
	if g.input.Down(actionMoveBackward) {
		g.camera.Position[0] -= walkForward[0] * moveStep
		g.camera.Position[1] -= walkForward[1] * moveStep
	}
	if g.input.Down(actionMoveLeft) {
		g.camera.Position[0] -= right[0] * moveStep
		g.camera.Position[1] -= right[1] * moveStep
	}
	if g.input.Down(actionMoveRight) {
		g.camera.Position[0] += right[0] * moveStep
		g.camera.Position[1] += right[1] * moveStep
	}

	if g.input.Down(actionMoveAway) {
		g.camera.Position[0] -= forward[0] * moveStep
		g.camera.Position[1] -= forward[1] * moveStep
		g.camera.Position[2] -= forward[2] * moveStep
	}
	if g.input.Down(actionMoveCloser) {
		g.camera.Position[0] += forward[0] * moveStep
		g.camera.Position[1] += forward[1] * moveStep
		g.camera.Position[2] += forward[2] * moveStep
	}

	// vertical movement (Q/E)
	if g.input.Down(actionMoveUp) {
		g.camera.Position[2] += moveStep
	}
	if g.input.Down(actionMoveDown) {
		g.camera.Position[2] -= moveStep
	}
	if g.input.Down(actionYawDown) {
		g.camera.YawDeg -= turnStep
	}
	if g.input.Down(actionYawUp) {
		g.camera.YawDeg += turnStep
	}
	if g.input.Down(actionTurnRight) {
		g.camera.PitchDeg -= turnStep
	}
	if g.input.Down(actionTurnLeft) {
		g.camera.PitchDeg += turnStep
	}

	if g.camera.PitchDeg > maxPitch {
		g.camera.PitchDeg = maxPitch
	}
	if g.camera.PitchDeg < -maxPitch {
		g.camera.PitchDeg = -maxPitch
	}

	return nil
}

func (g *Game) recordFrame(delta time.Duration) {
	g.fpsFrames++
	g.fpsElapsed += delta

	if g.fpsElapsed < time.Second {
		return
	}

	g.lastFPS = float64(g.fpsFrames) / g.fpsElapsed.Seconds()
	g.fpsFrames = 0
	g.fpsElapsed = 0
	g.updateWindowTitle()
}

func (g *Game) updateWindowTitle() {
	if g.window == nil {
		return
	}

	title := windowTitle
	if generatorName := g.currentGeneratorName(); generatorName != "" {
		title = fmt.Sprintf("%s | %s", title, generatorName)
	}
	if g.lastFPS > 0 {
		title = fmt.Sprintf("%s | %.1f FPS", title, g.lastFPS)
	}
	if g.svo != nil {
		title = fmt.Sprintf("%s | SVO %d nodes", title, g.svo.NodeCount())
	}
	if g.chunk != nil {
		title = fmt.Sprintf("%s | GPU %s", title, formatBytes(g.chunk.GPUBytes()))
	}
	g.window.SetTitle(title)
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor := float64(unit)
	suffix := "KiB"
	for _, next := range []string{"MiB", "GiB", "TiB"} {
		if float64(bytes) < divisor*unit {
			break
		}
		divisor *= unit
		suffix = next
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/divisor, suffix)
}

func (g *Game) Render(frame *vulkan.Frame) error {
	if g.raytracer == nil {
		return fmt.Errorf("raytracer pipeline is not initialized")
	}
	if g.chunk == nil {
		return fmt.Errorf("chunk resources are not initialized")
	}

	var occupiedMin [3]float32
	var occupiedMax [3]float32
	if g.svo != nil {
		minBounds, maxBounds, ok := g.svo.OccupiedBounds()
		if ok {
			occupiedMin = [3]float32{float32(minBounds[0]), float32(minBounds[1]), float32(minBounds[2])}
			occupiedMax = [3]float32{float32(maxBounds[0]), float32(maxBounds[1]), float32(maxBounds[2])}
		}
	}

	return g.raytracer.Record(frame, g.camera, g.chunk.DescriptorSet, occupiedMin, occupiedMax)
}

func (g *Game) currentGenerator() generators.Generator {
	if len(g.generators) == 0 || g.generatorIndex < 0 || g.generatorIndex >= len(g.generators) {
		return nil
	}
	return g.generators[g.generatorIndex]
}

func (g *Game) currentGeneratorName() string {
	currentGenerator := g.currentGenerator()
	if currentGenerator == nil {
		return ""
	}
	return currentGenerator.Name()
}

func (g *Game) resetCameraForScene() {
	minBounds, maxBounds, ok := [3]uint32{}, [3]uint32{}, false
	if g.svo != nil {
		minBounds, maxBounds, ok = g.svo.OccupiedBounds()
	}
	if !ok {
		g.camera = platform.Camera{
			Position: [3]float32{-140, 500, 500},
			YawDeg:   0,
			PitchDeg: 0,
			FovDeg:   60,
		}
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

	g.camera = platform.Camera{
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

func defaultBindings() map[input.Action]input.Binding {
	return map[input.Action]input.Binding{
		actionMoveForward: {
			Key:      glfw.KeyW,
			Cooldown: moveCooldown,
		},
		actionMoveBackward: {
			Key:      glfw.KeyS,
			Cooldown: moveCooldown,
		},
		actionMoveLeft: {
			Key:      glfw.KeyA,
			Cooldown: moveCooldown,
		},
		actionMoveRight: {
			Key:      glfw.KeyD,
			Cooldown: moveCooldown,
		},
		actionYawDown: {
			Key:      glfw.KeyLeft,
			Cooldown: moveCooldown,
		},
		actionYawUp: {
			Key:      glfw.KeyRight,
			Cooldown: moveCooldown,
		},
		actionTurnRight: {
			Key:      glfw.KeyDown,
			Cooldown: moveCooldown,
		},
		actionTurnLeft: {
			Key:      glfw.KeyUp,
			Cooldown: moveCooldown,
		},
		actionMoveAway: {
			Key:      glfw.KeyKPAdd,
			Cooldown: moveCooldown,
		},
		actionMoveCloser: {
			Key:      glfw.KeyKPSubtract,
			Cooldown: moveCooldown,
		},
		actionMoveUp: {
			Key:      glfw.KeyQ,
			Cooldown: moveCooldown,
		},
		actionMoveDown: {
			Key:      glfw.KeyE,
			Cooldown: moveCooldown,
		},
		actionFaster: {
			Key:      glfw.KeyLeftShift,
			Cooldown: moveCooldown,
		},
		actionNextModel: {
			Key:      glfw.KeyF1,
			Cooldown: modelSwitchCooldown,
		},
	}
}
