package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"Gogoxel/internal/engine"
	"Gogoxel/internal/game"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/session"
)

// demoObjective tracks how far the player has gotten through the
// "Build the Beacon" challenge. It is owner-thread-safe (the engine
// invokes Record from the host owner thread) and read-safe from the
// briefing/progress goroutine via the mutex.
type demoObjective struct {
	mu          sync.Mutex
	placed      int
	removed     int
	lastMat     string
	won         atomic.Bool
	winAt       time.Time
	placedCoord [3]uint32
}

// Record is the engine.Core OnEdit hook. It tallies edits and trips
// the win flag once the player has placed at least 4 voxels with at
// least 2 distinct materials (the "beacon" pattern).
func (o *demoObjective) Record(r engine.EditResult, materialsSeen *map[string]struct{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !r.Changed {
		return
	}
	if r.MaterialName == "" {
		o.removed++
		return
	}
	o.placed++
	o.lastMat = r.MaterialName
	o.placedCoord = r.TargetVoxel
	(*materialsSeen)[r.MaterialName] = struct{}{}
	if o.placed >= 4 && len(*materialsSeen) >= 2 && !o.won.Load() {
		o.won.Store(true)
		o.winAt = time.Now()
	}
}

// runDemo launches the actual interactive "Build the Beacon" demo: it
// makes sure a perlin terrain chunk map exists on disk, prints a clear
// mission briefing + control summary, then hands off to the live engine
// session loop so the player can walk around, place voxels, remove
// them, and complete the challenge in a real Vulkan window. A
// background goroutine watches the OnEdit-driven scoreboard and prints
// a win banner the moment the criteria are met.
func runDemo(out io.Writer) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(out)
	headless := fs.Bool("headless", false, "run the demo session headlessly (no Vulkan window)")
	hidden := fs.Bool("hidden-window", false, "open the window but do not show it (smoke-test mode)")
	exitAfter := fs.Duration("exit-after", 0, "exit the session automatically after this duration (0 = run until window closed)")
	chunkRootFlag := fs.String("chunk-root", "", "override the perlin terrain chunk root")
	autoplay := fs.Bool("autoplay", false, "scripted player: drives WaitUntilReady + EditAtCursor to complete the objective without input")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}

	chunkRoot := *chunkRootFlag
	if chunkRoot == "" {
		chunkRoot = os.Getenv("GOGOXEL_DEMO_CHUNK_ROOT")
	}
	if chunkRoot == "" {
		if _, err := os.Stat(filepath.Join("terrain", "perlin-terrain", "manifest.json")); err == nil {
			chunkRoot = "terrain"
		}
	}
	if chunkRoot == "" {
		tmp, err := os.MkdirTemp("", "gogoxel-demo-*")
		if err != nil {
			fmt.Fprintf(out, "demo: tempdir: %v\n", err)
			return 1
		}
		gen := generators.NewPerlinGenerator(1, 2)
		mapDir := generators.GeneratedChunkMapDir(tmp, gen.Name())
		req := generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: 2}.Normalized()
		fmt.Fprintf(out, "demo: generating perlin terrain in %s ...\n", mapDir)
		if err := generators.GenerateChunkMapWindow(mapDir, gen, req); err != nil {
			fmt.Fprintf(out, "demo: generate: %v\n", err)
			return 1
		}
		chunkRoot = tmp
	}

	printBriefing(out)

	host := game.NewHost(game.HostOptions{
		Live:         true,
		Headless:     *headless,
		HiddenWindow: *hidden,
		TickRateHz:   60,
		ChunkRoot:    chunkRoot,
	})
	if !*headless {
		fmt.Fprintln(out, game.ManualControlsSummary())
	}

	obj := &demoObjective{}
	materialsSeen := map[string]struct{}{}
	hookErr := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register the OnEdit hook once the host is ready, then poll
	// progress every second and print scoreboard updates.
	go func() {
		// Try for up to 30s to attach; under headless smoke tests this
		// can complete before the host is fully ready.
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if err := host.SetOnEdit(ctx, func(r engine.EditResult) {
				obj.Record(r, &materialsSeen)
			}); err == nil {
				hookErr <- nil
				break
			}
			select {
			case <-ctx.Done():
				hookErr <- ctx.Err()
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastPlaced := -1
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				obj.mu.Lock()
				p, lm := obj.placed, obj.lastMat
				obj.mu.Unlock()
				if obj.won.Load() {
					if lastPlaced != -2 {
						fmt.Fprintf(out, "\n🎉 BEACON COMPLETE! placed=%d  materials=%d  → you win\n\n",
							p, len(materialsSeen))
						lastPlaced = -2
					}
				} else if p != lastPlaced {
					fmt.Fprintf(out, "  progress: placed=%d  current_material=%q  materials_used=%d/2\n",
						p, lm, len(materialsSeen))
					lastPlaced = p
				}
			}
		}
	}()

	if *exitAfter > 0 {
		go func() {
			time.Sleep(*exitAfter)
			cancel()
		}()
	}

	if *autoplay {
		go autoplayDemo(ctx, host, out, &cancel)
	}

	err := host.Run(ctx)
	if err != nil && err != context.Canceled {
		fmt.Fprintf(out, "demo: session ended with error: %v\n", err)
		return 1
	}
	if obj.won.Load() {
		fmt.Fprintln(out, "demo: result = WIN 🎉")
	} else {
		fmt.Fprintf(out, "demo: result = incomplete (placed=%d, materials=%d)\n",
			obj.placed, len(materialsSeen))
	}
	select {
	case <-hookErr:
	default:
	}
	return 0
}

func printBriefing(out io.Writer) {
	fmt.Fprintln(out, "=============================================")
	fmt.Fprintln(out, " GOGOXEL DEMO — Build the Beacon")
	fmt.Fprintln(out, "=============================================")
	fmt.Fprintln(out, "Objective:")
	fmt.Fprintln(out, "  Place at least 4 voxels using at least 2 different materials.")
	fmt.Fprintln(out, "  The classic beacon: 3 of one material stacked, 1 of another on top.")
	fmt.Fprintln(out, "Use [ and ] to switch the active material; left-click to place,")
	fmt.Fprintln(out, "right-click to remove. Walk around with WASD + mouse-look.")
	fmt.Fprintln(out, "Progress is printed every ~2 s; close the window or ESC to end.")
	fmt.Fprintln(out, "=============================================")
}

// autoplayDemo drives a headless scripted player that achieves the
// objective: wait for readiness, then place 5 voxels with two
// different materials at slightly different normalized cursor
// positions so the engine's raycast resolves distinct target voxels.
func autoplayDemo(ctx context.Context, host *game.Host, out io.Writer, cancel *context.CancelFunc) {
_, err := host.WaitUntilReady(ctx, session.WaitCriteria{
RequireSceneLoaded: true,
MaxTicks:           600,
})
if err != nil {
fmt.Fprintf(out, "autoplay: wait-until-ready: %v\n", err)
return
}
mats := engine.EditMaterialNames()
steps := []struct {
material string
x, y     float32
}{
{mats[0], 0.50, 0.55},
{mats[0], 0.52, 0.55},
{mats[0], 0.54, 0.55},
{mats[0], 0.56, 0.55},
{mats[1 % len(mats)], 0.50, 0.50},
}
for i, s := range steps {
if err := host.SetSelectedMaterial(ctx, s.material); err != nil {
fmt.Fprintf(out, "autoplay: set-material: %v\n", err)
return
}
r, err := host.EditAtCursor(ctx, session.EditModePlace,
session.CursorPosition{NormalizedX: s.x, NormalizedY: s.y})
if err != nil {
fmt.Fprintf(out, "autoplay: edit %d: %v\n", i, err)
return
}
fmt.Fprintf(out, "autoplay: step %d → changed=%v target=%v mat=%q\n",
i+1, r.Changed, r.TargetVoxel, r.MaterialName)
select {
case <-ctx.Done():
return
case <-time.After(150 * time.Millisecond):
}
}
// Give the OnEdit hook a moment to flip the win flag, then end.
time.Sleep(2500 * time.Millisecond)
if cancel != nil {
(*cancel)()
}
}
