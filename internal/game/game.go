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

type Options struct {
	Headless     bool
	HiddenWindow bool
	TickRateHz   int
}

type Game struct {
	options  Options
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

func New(options Options) *Game {
	options = normalizeOptions(options)
	return &Game{
		options:             options,
		core:                engine.NewCore(engine.DefaultGeneratorCatalog(), engine.Config{TickRateHz: options.TickRateHz}),
		externalHeldActions: make(input.Snapshot),
	}
}

func normalizeOptions(options Options) Options {
	if options.TickRateHz <= 0 {
		options.TickRateHz = 60
	}
	if options.Headless {
		options.HiddenWindow = false
	}
	return options
}

func (g *Game) Start() error {
	if g == nil || g.options.Headless {
		return nil
	}
	return g.InitWindowed(g.options.HiddenWindow)
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
	if err := g.Start(); err != nil {
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
		g.RecordFrame(delta)
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

func (g *Game) PollEvents() {
	if g == nil || g.window == nil {
		return
	}
	g.window.PollEvents()
}

func (g *Game) IsIconified() bool {
	if g == nil || g.window == nil {
		return false
	}
	return g.window.IsIconified()
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

func (g *Game) SetSelectedEditMaterial(name string) error {
	if g == nil {
		return fmt.Errorf("game is not initialized")
	}
	return g.core.SetSelectedEditMaterial(name)
}

func (g *Game) SelectedEditMaterialName() string {
	if g == nil {
		return ""
	}
	return g.core.SelectedEditMaterialName()
}

func (g *Game) EditAtCursor(mode engine.EditMode, normalizedX, normalizedY float32) (engine.EditResult, error) {
	if g == nil {
		return engine.EditResult{}, fmt.Errorf("game is not initialized")
	}
	sample := engine.CursorSample{
		NormalizedX: normalizedX,
		NormalizedY: normalizedY,
	}
	if g.window != nil {
		sample.ViewportWidth, sample.ViewportHeight = g.window.FramebufferSize()
	} else {
		sample.ViewportWidth = vulkan.DefaultWindowWidth
		sample.ViewportHeight = vulkan.DefaultWindowHeight
	}
	return g.core.EditAtCursor(mode, sample)
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
	g.core.SetCursorSample(cursorSample(g.window))
	g.core.SetHeldActions(mergeSnapshots(keyboardSnapshot(g.window), mouseSnapshot(g.window), g.externalHeldActions))
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
		if g.renderer == nil {
			g.loadedSceneVersion = g.core.SceneVersion()
			return nil
		}
		if g.chunk != nil && g.chunk.QueueSceneUpdate(g.core.CurrentSVO()) {
			g.loadedSceneVersion = g.core.SceneVersion()
			return nil
		}
		if err := g.InitChunk(); err != nil {
			return fmt.Errorf("syncing active scene: %w", err)
		}
	}

	return nil
}

func (g *Game) RecordFrame(delta time.Duration) {
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
	if materialName := g.core.SelectedEditMaterialName(); materialName != "" {
		title = fmt.Sprintf("%s | %s", title, materialName)
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
	if err := g.chunk.RecordSceneUpdate(frame); err != nil {
		return fmt.Errorf("recording scene update: %w", err)
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
		control.ActionPreviousMaterial: glfw.KeyLeftBracket,
		control.ActionNextMaterial: glfw.KeyRightBracket,
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

func mouseSnapshot(source *platform.Window) input.Snapshot {
	snapshot := make(input.Snapshot, 2)
	if source == nil {
		return snapshot
	}
	snapshot[control.ActionPlaceCube] = source.IsMouseButtonDown(glfw.MouseButtonLeft)
	snapshot[control.ActionRemoveCube] = source.IsMouseButtonDown(glfw.MouseButtonRight)
	return snapshot
}

func cursorSample(source *platform.Window) engine.CursorSample {
	sample := engine.CursorSample{
		NormalizedX:   0.5,
		NormalizedY:   0.5,
		ViewportWidth: vulkan.DefaultWindowWidth,
		ViewportHeight: vulkan.DefaultWindowHeight,
	}
	if source == nil {
		return sample
	}
	windowWidth, windowHeight := source.Size()
	framebufferWidth, framebufferHeight := source.FramebufferSize()
	cursorX, cursorY := source.CursorPosition()
	if framebufferWidth <= 0 {
		framebufferWidth = vulkan.DefaultWindowWidth
	}
	if framebufferHeight <= 0 {
		framebufferHeight = vulkan.DefaultWindowHeight
	}
	return cursorSampleFromMetrics(windowWidth, windowHeight, framebufferWidth, framebufferHeight, cursorX, cursorY)
}

func clampNormalized(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
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
