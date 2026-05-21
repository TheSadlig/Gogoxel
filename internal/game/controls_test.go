package game

import (
	"strings"
	"testing"
)

func TestCursorSampleFromMetricsUsesWindowCoordinatesForNormalization(t *testing.T) {
	sample := cursorSampleFromMetrics(960, 540, 1920, 1080, 480, 270)
	if got, want := sample.NormalizedX, float32(0.5); got != want {
		t.Fatalf("NormalizedX = %v, want %v", got, want)
	}
	if got, want := sample.NormalizedY, float32(0.5); got != want {
		t.Fatalf("NormalizedY = %v, want %v", got, want)
	}
	if got, want := sample.ViewportWidth, 1920; got != want {
		t.Fatalf("ViewportWidth = %d, want %d", got, want)
	}
	if got, want := sample.ViewportHeight, 1080; got != want {
		t.Fatalf("ViewportHeight = %d, want %d", got, want)
	}
}

// TestCursorSampleYTopDownMatchesShaderUV verifies that GLFW cursor Y (top=0,
// bottom=windowHeight) maps to normalizedY (top=0, bottom=1) without inversion.
// The Vulkan UV convention has uv.y=0 at the screen top; the engine's cursorRay
// does screenY = normalizedY*2-1, so normalizedY=0 must produce screenY=-1 (top).
// Flipping would invert the vertical ray direction, placing blocks in the wrong location.
func TestCursorSampleYTopDownMatchesShaderUV(t *testing.T) {
	// Cursor at the very top of a 100×100 window.
	topSample := cursorSampleFromMetrics(100, 100, 100, 100, 50, 0)
	if got := topSample.NormalizedY; got != 0.0 {
		t.Errorf("cursor at GLFW top (y=0): NormalizedY = %v, want 0 (top of screen)", got)
	}
	// Cursor at the very bottom.
	bottomSample := cursorSampleFromMetrics(100, 100, 100, 100, 50, 100)
	if got := bottomSample.NormalizedY; got != 1.0 {
		t.Errorf("cursor at GLFW bottom (y=H): NormalizedY = %v, want 1 (bottom of screen)", got)
	}
}

func TestManualControlsSummaryMentionsEditBindings(t *testing.T) {
	summary := ManualControlsSummary()
	for _, fragment := range []string{"Left click", "paint a stroke", "Right click", "[", "]", "grass", "sand"} {
		if !strings.Contains(summary, fragment) {
			t.Fatalf("ManualControlsSummary() = %q, want fragment %q", summary, fragment)
		}
	}
}