package main

import (
	"fmt"
	"io"
	"time"

	"Gogoxel/internal/game/demo"
)

// runDemo plays the headless "Build the Beacon" demo to completion,
// applying scripted placements. Returns a process exit code.
func runDemo(out io.Writer) int {
	cfg := demo.DefaultConfig()
	s := demo.New(cfg)

	fmt.Fprintln(out, "gogoxel demo: Build the Beacon")
	fmt.Fprintf(out, "  targets: %d   time budget: %s\n", len(cfg.Targets), cfg.TimeBudget)

	for _, t := range cfg.Targets {
		s.Tick(100 * time.Millisecond)
		s.PlaceVoxel(demo.Coord{X: t.At.X, Y: t.At.Y, Z: t.At.Z}, t.Material)
		fmt.Fprintf(out, "  placed (%d,%d,%d) mat=%d → score %d\n",
			t.At.X, t.At.Y, t.At.Z, t.Material, s.Score())
	}

	hud := s.HUD()
	fmt.Fprintf(out, "  HUD: %d commands, score=%s\n", len(hud.Frame.Commands()), hud.ScoreStr)

	snap, err := s.Snapshot()
	if err != nil {
		fmt.Fprintf(out, "  snapshot error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "  snapshot: %d bytes written\n", len(snap))

	switch s.ResultNow() {
	case demo.ResultWin:
		fmt.Fprintln(out, "  RESULT: win 🎉")
		return 0
	case demo.ResultLoss:
		fmt.Fprintln(out, "  RESULT: loss")
		return 2
	default:
		fmt.Fprintln(out, "  RESULT: incomplete")
		return 3
	}
}
