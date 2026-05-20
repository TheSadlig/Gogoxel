package game

import (
	"fmt"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/engine"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const windowTitle = "Gogoxel Vulkan"

type Game struct {
	window   *platform.Window
	renderer *vulkan.Renderer
	core     *engine.Core
	externalHeldActions input.Snapshot
	requestedClose bool

	fpsFrames  int
	fpsElapsed time.Duration
	lastFPS    float64
	chunk      *vulkan.ChunkResources
	bindings   *vulkan.ChunkBindings
	raytracer  *vulkan.RaytracerPipeline
	loadedSceneVersion uint64
}

func New() *Game {
	return &Game{
		core:                engine.NewCore(engine.DefaultGeneratorCatalog(), engine.Config{}),
		externalHeldActions: make(input.Snapshot),
	}
}

func (g *Game) InitWindowed(hiddenWindow bool) error {
	if g.window != nil || g.renderer != nil {
		return nil
	}
	window, err := platform.NewWindowWithOptions(windowTitle, vulkan.DefaultWindowWidth, vulkan.DefaultWindowHeight, platform.WindowOptions{Visible: !hiddenWindow})
	if err != nil {
		return err
	}

	renderer, err := vulkan.New(window)
	if err != nil {
		window.Close()
		return err
	}

	bindings, err := renderer.NewChunkBindings()
	if err != nil {
		renderer.Close()
		window.Close()
		return err
	}

	raytracer, err := renderer.NewRaytracerPipeline(bindings)
	if err != nil {
		bindings.Close()
		renderer.Close()
		window.Close()
		return err
	}

	g.window = window
	g.renderer = renderer
	g.bindings = bindings
	g.raytracer = raytracer
	if g.core.CurrentSVO() == nil {
		if err := g.core.LoadDefaultGenerator(); err != nil {
			g.Close()
			return err
		}
	}
	if g.core.CurrentSVO() != nil {
		if err := g.InitChunk(); err != nil {
			g.Close()
			return err
		}
	}
	g.updateWindowTitle()
	return nil
}

func (g *Game) Close() error {
	if g == nil {
		return nil
	}
	if g.renderer != nil {
		if err := g.renderer.WaitIdle(); err != nil {
			return err
		}
	}
	if g.chunk != nil {
		g.chunk.Close()
		g.chunk = nil
	}
	if g.raytracer != nil {
		g.raytracer.Close()
		g.raytracer = nil
	}
	if g.bindings != nil {
		g.bindings.Close()
		g.bindings = nil
	}
	if g.renderer != nil {
		g.renderer.Close()
		g.renderer = nil
	}
	if g.window != nil {
		g.window.Close()
		g.window = nil
	}
	return nil
}

func (g *Game) InitChunk() error {
	if g.renderer == nil {
		return fmt.Errorf("renderer is not initialized")
	}
	if g.bindings == nil {
		return fmt.Errorf("chunk bindings are not initialized")
	}
	currentSVO := g.core.CurrentSVO()
	if currentSVO == nil {
		return fmt.Errorf("no scene is loaded")
	}

	nextChunk, err := g.renderer.CreateChunkResourcesFromSVO(g.bindings, currentSVO)
	if err != nil {
		return fmt.Errorf("creating chunk resources from svo: %w", err)
	}

	previousChunk := g.chunk
	g.chunk = nextChunk
	if previousChunk != nil {
		if err := g.renderer.WaitIdle(); err != nil {
			g.chunk = previousChunk
			nextChunk.Close()
			return fmt.Errorf("waiting for renderer before chunk swap: %w", err)
		}
		previousChunk.Close()
	}
	g.loadedSceneVersion = g.core.SceneVersion()
	g.chunk.SetCameraPosition(g.core.Camera().Position)
	g.updateWindowTitle()

	return nil
}

func (g *Game) Run() error {
	if err := g.InitWindowed(false); err != nil {
		return err
	}
	defer g.Close()
	g.updateWindowTitle()
	lastFrame := time.Now()

	for !g.ShouldClose() {
		g.window.PollEvents()
		if g.window.IsIconified() {
			lastFrame = time.Now()
			continue
		}

		now := time.Now()
		delta := now.Sub(lastFrame)
		lastFrame = now

		if err := g.StepFrame(delta); err != nil {
			return err
		}
		g.recordFrame(delta)
	}

	return nil
}

func (g *Game) StepFrame(delta time.Duration) error {
	if err := g.Update(delta); err != nil {
		return err
	}
	if g.renderer != nil {
		if err := g.renderer.DrawFrame(g.Render); err != nil {
			return err
		}
	}
	return nil
}

func (g *Game) ShouldClose() bool {
	if g == nil {
		return true
	}
	if g.requestedClose {
		return true
	}
	return g.window != nil && g.window.ShouldClose()
}

func (g *Game) RequestClose() {
	if g == nil {
		return
	}
	g.requestedClose = true
	if g.window != nil {
		g.window.RequestClose()
	}
}

func (g *Game) SetTickRateHz(rate int) {
	g.core.SetTickRateHz(rate)
}

func (g *Game) TickDuration() time.Duration {
	return g.core.TickDuration()
}

func (g *Game) Reset() error {
	g.core.Reset()
	clear(g.externalHeldActions)
	g.requestedClose = false
	if g.chunk != nil {
		if g.renderer != nil {
			if err := g.renderer.WaitIdle(); err != nil {
				return err
			}
		}
		g.chunk.Close()
		g.chunk = nil
	}
	g.loadedSceneVersion = g.core.SceneVersion()
	g.updateWindowTitle()
	return nil
}

func (g *Game) LoadGenerator(name string) error {
	if err := g.core.LoadGenerator(name); err != nil {
		return err
	}
	if g.renderer != nil {
		return g.InitChunk()
	}
	g.loadedSceneVersion = g.core.SceneVersion()
	return nil
}

func (g *Game) PressAction(action input.Action) {
	if g.externalHeldActions == nil {
		g.externalHeldActions = make(input.Snapshot)
	}
	g.externalHeldActions[action] = true
}

func (g *Game) ReleaseAction(action input.Action) {
	delete(g.externalHeldActions, action)
}

func (g *Game) Camera() platform.Camera {
	return g.core.Camera()
}

func (g *Game) SetCamera(camera platform.Camera) {
	g.core.SetCamera(camera)
}

func (g *Game) Snapshot() engine.Snapshot {
	return g.core.Snapshot()
}

func (g *Game) RendererInitialized() bool {
	return g.renderer != nil
}

func (g *Game) DeviceName() string {
	if g.renderer == nil {
		return ""
	}
	return g.renderer.DeviceName()
}

func (g *Game) PresentModeName() string {
	if g.renderer == nil {
		return ""
	}
	return g.renderer.PresentModeName()
}

func (g *Game) VRAMBytes() uint64 {
	if g.chunk == nil {
		return 0
	}
	return g.chunk.VRAMBytes()
}

func (g *Game) ChunkRAMBytes() uint64 {
	if g.chunk == nil {
		return 0
	}
	return g.chunk.RAMBytes()
}

func (g *Game) ResidentBrickCount() int {
	if g.chunk == nil {
		return 0
	}
	return g.chunk.ResidentBrickCount()
}

func (g *Game) StreamingStats() vulkan.StreamingStats {
	if g.chunk == nil {
		return vulkan.StreamingStats{}
	}
	return g.chunk.StreamingStats()
}

func (g *Game) CaptureScreenshot(path string) error {
	if g.renderer == nil {
		return fmt.Errorf("renderer is not initialized")
	}
	if g.core.CurrentSVO() == nil {
		return fmt.Errorf("no scene is loaded")
	}
	return g.renderer.CaptureFramePNG(g.Render, path)
}

func (g *Game) Update(delta time.Duration) error {
	g.core.SetHeldActions(mergeSnapshots(keyboardSnapshot(g.window), g.externalHeldActions))
	if err := g.core.Step(delta); err != nil {
		return err
	}
	if g.loadedSceneVersion != g.core.SceneVersion() {
		if g.core.CurrentSVO() == nil {
			if g.chunk != nil {
				if g.renderer != nil {
					if err := g.renderer.WaitIdle(); err != nil {
						return fmt.Errorf("waiting for renderer before clearing active scene: %w", err)
					}
				}
				g.chunk.Close()
				g.chunk = nil
			}
			g.loadedSceneVersion = g.core.SceneVersion()
			g.updateWindowTitle()
			return nil
		}
		if err := g.InitChunk(); err != nil {
			return fmt.Errorf("syncing active scene: %w", err)
		}
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
	if snapshot := g.core.Snapshot(); snapshot.SceneLoaded {
		title = fmt.Sprintf("%s | SVO %d nodes", title, snapshot.NodeCount)
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
	camera := g.core.Camera()
	g.chunk.SetCameraPosition(camera.Position)
	if err := g.chunk.RecordStreaming(frame); err != nil {
		return fmt.Errorf("recording brick streaming uploads: %w", err)
	}

	var occupiedMin [3]float32
	var occupiedMax [3]float32
	if currentSVO := g.core.CurrentSVO(); currentSVO != nil {
		minBounds, maxBounds, ok := currentSVO.OccupiedBounds()
		if ok {
			occupiedMin = [3]float32{float32(minBounds[0]), float32(minBounds[1]), float32(minBounds[2])}
			occupiedMax = [3]float32{float32(maxBounds[0]), float32(maxBounds[1]), float32(maxBounds[2])}
		}
	}

	return g.raytracer.Record(frame, camera, g.chunk.DescriptorSet, occupiedMin, occupiedMax)
}

func (g *Game) currentGeneratorName() string {
	return g.core.CurrentGeneratorName()
}

func defaultKeyBindings() map[input.Action]glfw.Key {
	return map[input.Action]glfw.Key{
		control.ActionMoveForward:  glfw.KeyW,
		control.ActionMoveBackward: glfw.KeyS,
		control.ActionMoveLeft:     glfw.KeyA,
		control.ActionMoveRight:    glfw.KeyD,
		control.ActionYawDown:      glfw.KeyLeft,
		control.ActionYawUp:        glfw.KeyRight,
		control.ActionTurnRight:    glfw.KeyDown,
		control.ActionTurnLeft:     glfw.KeyUp,
		control.ActionMoveAway:     glfw.KeyKPAdd,
		control.ActionMoveCloser:   glfw.KeyKPSubtract,
		control.ActionMoveUp:       glfw.KeyQ,
		control.ActionMoveDown:     glfw.KeyE,
		control.ActionFaster:       glfw.KeyLeftShift,
		control.ActionNextModel:    glfw.KeyF1,
	}
}

func keyboardSnapshot(source *platform.Window) input.Snapshot {
	bindings := defaultKeyBindings()
	snapshot := make(input.Snapshot, len(bindings))
	if source == nil {
		return snapshot
	}
	for action, key := range bindings {
		snapshot[action] = source.IsKeyDown(key)
	}
	return snapshot
}

func mergeSnapshots(values ...input.Snapshot) input.Snapshot {
	merged := make(input.Snapshot)
	for _, snapshot := range values {
		for action, down := range snapshot {
			if !down {
				continue
			}
			merged[action] = true
		}
	}
	return merged
}
