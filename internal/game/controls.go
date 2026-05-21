package game

import (
	"strings"

	"Gogoxel/internal/engine"
)

func ManualControlsSummary() string {
	return "Controls: WASD move, Q/E move up/down, arrow keys look, Shift accelerates, F1 switches scene, Left click places a block, hold Left click while moving to paint a stroke, Right click removes a block, [ and ] cycle block material. Materials: " + strings.Join(engine.EditMaterialNames(), ", ") + ". The current material appears in the window title."
}

func cursorSampleFromMetrics(windowWidth, windowHeight, framebufferWidth, framebufferHeight int, cursorX, cursorY float64) engine.CursorSample {
	sample := engine.CursorSample{
		NormalizedX:   0.5,
		NormalizedY:   0.5,
		ViewportWidth:  framebufferWidth,
		ViewportHeight: framebufferHeight,
	}
	if sample.ViewportWidth <= 0 {
		sample.ViewportWidth = framebufferWidth
	}
	if sample.ViewportHeight <= 0 {
		sample.ViewportHeight = framebufferHeight
	}
	if sample.ViewportWidth <= 0 {
		sample.ViewportWidth = 1
	}
	if sample.ViewportHeight <= 0 {
		sample.ViewportHeight = 1
	}
	if windowWidth <= 0 || windowHeight <= 0 {
		return sample
	}
	sample.NormalizedX = clampNormalized(float32(cursorX / float64(windowWidth)))
	sample.NormalizedY = clampNormalized(float32(cursorY / float64(windowHeight)))
	return sample
}