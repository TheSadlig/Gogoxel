package game

import (
	"fmt"
	"math"
	"time"

	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan"
	"Gogoxel/internal/world"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const windowTitle = "Gogoxel Vulkan"

const moveCooldown = 120 * time.Millisecond

// Temporary test scene dimensions.
const (
	chunkWidth   = uint32(1000)
	chunkHeight  = uint32(1000)
	chunkDepth   = uint32(1000)
	testCubeSize = uint32(64)
)

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

	svo *world.SVO
}

func New() *Game {
	return &Game{
		input: input.NewManager(defaultBindings()),
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

	if g.chunk != nil {
		g.chunk.Close()
		g.chunk = nil
	}

	// Some temporary SVO for testing
	g.svo = world.NewSVO()
	sceneSize := chunkWidth
	if chunkHeight > sceneSize {
		sceneSize = chunkHeight
	}
	if chunkDepth > sceneSize {
		sceneSize = chunkDepth
	}
	centerX := int(chunkWidth / 2)
	centerY := int(chunkHeight / 2)
	centerZ := int(chunkDepth / 2)
	halfCube := int(testCubeSize / 2)
	g.svo.BuildTree(func(x, y, z int) (uint32, bool) {
		inCubeX := x >= centerX-halfCube && x < centerX+halfCube
		inCubeY := y >= centerY-halfCube && y < centerY+halfCube
		inCubeZ := z >= centerZ-halfCube && z < centerZ+halfCube
		return 0xFF0000, inCubeX && inCubeY && inCubeZ
	}, uint(sceneSize))

	chunk, err := g.renderer.CreateChunkResourcesFromSVO(g.bindings, g.svo)
	if err != nil {
		return fmt.Errorf("creating chunk resources from svo: %w", err)
	}
	g.chunk = chunk

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
	defer g.chunk.Close()
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
	if g.lastFPS > 0 {
		title = fmt.Sprintf("%s | %.1f FPS", windowTitle, g.lastFPS)
	}
	if g.chunk != nil {
		title = fmt.Sprintf("%s | RAM %s | VRAM %s", title, formatBytes(g.chunk.RAMBytes()), formatBytes(g.chunk.VRAMBytes()))
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

	return g.raytracer.Record(frame, g.camera, g.chunk.DescriptorSet)
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
	}
}
