package game

import "testing"

func TestCursorSampleFromMetricsUsesTopLeftUVSpace(t *testing.T) {
	sample := cursorSampleFromMetrics(200, 100, 400, 200, 0, 0)
	if sample.NormalizedX != 0 {
		t.Fatalf("expected NormalizedX to be 0 at left edge, got %v", sample.NormalizedX)
	}
	if sample.NormalizedY != 0 {
		t.Fatalf("expected NormalizedY to be 0 at top edge, got %v", sample.NormalizedY)
	}
	if sample.ViewportWidth != 400 || sample.ViewportHeight != 200 {
		t.Fatalf("expected framebuffer dimensions 400x200, got %dx%d", sample.ViewportWidth, sample.ViewportHeight)
	}
}

func TestCursorSampleFromMetricsUsesWindowCoordinates(t *testing.T) {
	sample := cursorSampleFromMetrics(200, 100, 400, 200, 100, 50)
	if sample.NormalizedX != 0.5 || sample.NormalizedY != 0.5 {
		t.Fatalf("expected centered cursor sample, got (%v, %v)", sample.NormalizedX, sample.NormalizedY)
	}
}
