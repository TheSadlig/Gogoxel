package game

import (
	"fmt"
	"time"

	"Gogoxel/internal/vulkan"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const windowTitle = "Gogoxel Vulkan"

type Game struct {
	renderer *vulkan.Renderer
	elapsed  time.Duration

	fpsFrames  int
	fpsElapsed time.Duration
	lastFPS    float64

	cameraPosition [3]float32
}

func New() *Game {
	return &Game{}
}

func (g *Game) Run() error {
	renderer, err := vulkan.New(windowTitle, vulkan.DefaultWindowWidth, vulkan.DefaultWindowHeight)
	if err != nil {
		return err
	}
	defer renderer.Close()

	g.renderer = renderer
	g.updateWindowTitle()
	lastFrame := time.Now()

	for !renderer.ShouldClose() {
		renderer.PollEvents()
		if renderer.IsIconified() {
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

	if g.renderer.IsKeyPressed(glfw.KeyW) {
		println("W pressed")
		g.renderer.RenderingInfo.CameraPosition[2] -= 1 // Move forward
	}
	if g.renderer.IsKeyPressed(glfw.KeyS) {
		println("S pressed")
		g.renderer.RenderingInfo.CameraPosition[2] += 1 // Move backward
	}
	if g.renderer.IsKeyPressed(glfw.KeyA) {
		println("A pressed")
		g.renderer.RenderingInfo.CameraPosition[0] -= 1 // Move left
	}
	if g.renderer.IsKeyPressed(glfw.KeyD) {
		println("D pressed")
		g.renderer.RenderingInfo.CameraPosition[0] += 1 // Move right
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
	if g.renderer == nil {
		return
	}

	title := windowTitle
	if g.lastFPS > 0 {
		title = fmt.Sprintf("%s | %.1f FPS", windowTitle, g.lastFPS)
	}

	g.renderer.SetTitle(title)
}

func (g *Game) Render(frame *vulkan.Frame) error {
	frame.BindDefaultPipeline()
	frame.Draw(3, 1, 0, 0)
	return nil
}
