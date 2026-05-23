package game

import (
	"fmt"
	"testing"
	"time"

	"Gogoxel/internal/control"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
)

type streamingFlightMeasurement struct {
	maxPending     int
	maxBudget      int
	finalPending   int
	residentCount  int
	averageFrameMs float64
	p95FrameMs     float64
	sampleCount    int
}

func TestHiddenWindowFastStreamingMeasurement(t *testing.T) {
	g := New(Options{HiddenWindow: true, TickRateHz: 60})
	if err := g.Start(); err != nil {
		t.Skipf("hidden-window renderer unavailable: %v", err)
	}
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	normal, err := measureStreamingFlight(g, []input.Action{control.ActionMoveForward}, 120)
	if err != nil {
		t.Fatalf("normal-speed measurement failed: %v", err)
	}
	fast, err := measureStreamingFlight(g, []input.Action{control.ActionMoveForward, control.ActionFaster}, 120)
	if err != nil {
		t.Fatalf("fast-speed measurement failed: %v", err)
	}

	t.Logf("normal flight: max_pending=%d max_budget=%d final_pending=%d resident=%d avg_frame_ms=%.3f p95_frame_ms=%.3f samples=%d",
		normal.maxPending,
		normal.maxBudget,
		normal.finalPending,
		normal.residentCount,
		normal.averageFrameMs,
		normal.p95FrameMs,
		normal.sampleCount,
	)
	t.Logf("fast flight: max_pending=%d max_budget=%d final_pending=%d resident=%d avg_frame_ms=%.3f p95_frame_ms=%.3f samples=%d",
		fast.maxPending,
		fast.maxBudget,
		fast.finalPending,
		fast.residentCount,
		fast.averageFrameMs,
		fast.p95FrameMs,
		fast.sampleCount,
	)

	if fast.maxBudget <= normal.maxBudget {
		t.Fatalf("expected fast flight to raise upload budget above normal flight: normal=%d fast=%d", normal.maxBudget, fast.maxBudget)
	}
	if fast.finalPending != 0 {
		t.Fatalf("expected fast flight to settle back to zero pending bricks after release, got %d", fast.finalPending)
	}
	if fast.maxPending >= 512 {
		t.Fatalf("expected fast flight backlog to stay below 512 pending bricks, got %d", fast.maxPending)
	}
	if fast.sampleCount == 0 {
		t.Fatal("expected frame samples during fast flight")
	}
}

func measureStreamingFlight(g *Game, actions []input.Action, frames int) (streamingFlightMeasurement, error) {
	if err := preparePerlinStreamingFlight(g); err != nil {
		return streamingFlightMeasurement{}, err
	}
	window := &frameWindow{}
	for _, action := range actions {
		g.PressAction(action)
	}
	defer func() {
		for _, action := range actions {
			g.ReleaseAction(action)
		}
	}()

	measurement := streamingFlightMeasurement{}
	for frame := 0; frame < frames; frame++ {
		if err := stepAndRecord(g, window); err != nil {
			return streamingFlightMeasurement{}, fmt.Errorf("step frame %d: %w", frame, err)
		}
		streaming := g.StreamingStats()
		if streaming.PendingDesiredCount > measurement.maxPending {
			measurement.maxPending = streaming.PendingDesiredCount
		}
		if streaming.UploadBudget > measurement.maxBudget {
			measurement.maxBudget = streaming.UploadBudget
		}
	}
	for _, action := range actions {
		g.ReleaseAction(action)
	}
	if err := waitForStreamingSettled(g, window, 600); err != nil {
		return streamingFlightMeasurement{}, err
	}
	streaming := g.StreamingStats()
	windowSnapshot := window.Snapshot()
	measurement.finalPending = streaming.PendingDesiredCount
	measurement.residentCount = streaming.ResidentCount
	measurement.averageFrameMs = windowSnapshot.AverageFrameTime.Seconds() * 1000
	measurement.p95FrameMs = windowSnapshot.P95FrameTime.Seconds() * 1000
	measurement.sampleCount = windowSnapshot.SampleCount
	return measurement, nil
}

func preparePerlinStreamingFlight(g *Game) error {
	if err := g.Reset(); err != nil {
		return err
	}
	g.SetCamera(platform.Camera{Position: [3]float32{256, 256, 160}, YawDeg: 45, PitchDeg: -12, FovDeg: 60})
	if err := g.LoadGenerator("Perlin Terrain"); err != nil {
		return err
	}
	return waitForStreamingSettled(g, nil, 1200)
}

func waitForStreamingSettled(g *Game, window *frameWindow, maxFrames int) error {
	for frame := 0; frame < maxFrames; frame++ {
		streaming := g.StreamingStats()
		snapshot := g.Snapshot()
		if snapshot.SceneLoaded && streaming.DesiredReady && streaming.PendingDesiredCount == 0 {
			return nil
		}
		if err := stepAndRecord(g, window); err != nil {
			return fmt.Errorf("settle frame %d: %w", frame, err)
		}
	}
	streaming := g.StreamingStats()
	return fmt.Errorf("streaming did not settle after %d frames (pending=%d desired_ready=%t)", maxFrames, streaming.PendingDesiredCount, streaming.DesiredReady)
}

func stepAndRecord(g *Game, window *frameWindow) error {
	startedAt := time.Now()
	if err := g.StepFrame(g.TickDuration()); err != nil {
		return err
	}
	if window != nil {
		window.Record(time.Since(startedAt))
	}
	return nil
}
