package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"Gogoxel/internal/engine"
	"Gogoxel/internal/game"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/session"
)

// minecraftStats accumulates per-edit telemetry. Tallied on the host
// owner thread via OnEdit; read by the progress goroutine under mu.
type minecraftStats struct {
	mu       sync.Mutex
	placed   int
	removed  int
	lastMat  string
	closed   atomic.Bool
}

func (s *minecraftStats) record(mode engine.EditMode, r engine.EditResult) {
	if !r.Changed {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == engine.EditModeRemove {
		s.removed++
	} else {
		s.placed++
		s.lastMat = r.MaterialName
	}
}

func (s *minecraftStats) snapshot() (placed, removed int, last string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placed, s.removed, s.lastMat
}

// runDemo launches a very small Minecraft-like clone: a freshly-
// generated tiny perlin world, a first-person camera, mouse-look,
// WASD movement, place/remove blocks, and material switching. Stats
// are printed every few seconds to show the engine is alive.
func runDemo(out io.Writer) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(out)
	headless := fs.Bool("headless", false, "run the demo session headlessly (no Vulkan window)")
	hidden := fs.Bool("hidden-window", false, "open the window but do not show it (smoke-test mode)")
	exitAfter := fs.Duration("exit-after", 0, "exit the session automatically after this duration (0 = run until window closed)")
	chunkRange := fs.Int("chunk-range", 1, "perlin terrain chunk radius (0 = 1×1, 1 = 3×3, 2 = 5×5)")
	seed := fs.Uint64("seed", 1, "perlin terrain seed")
	chunkRootFlag := fs.String("chunk-root", "", "reuse an existing perlin terrain chunk root instead of generating a fresh one")
	autoplay := fs.Bool("autoplay", false, "scripted player demo: places a few blocks to prove the loop works (used by CI smoke tests)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}

	chunkRoot := *chunkRootFlag
	if chunkRoot == "" {
		tmp, err := os.MkdirTemp("", "gogoxel-demo-*")
		if err != nil {
			fmt.Fprintf(out, "demo: tempdir: %v\n", err)
			return 1
		}
		gen := generators.NewPerlinGenerator(*seed, *seed+1)
		mapDir := generators.GeneratedChunkMapDir(tmp, gen.Name())
		req := generators.BuildRequest{ChunkX: 0, ChunkY: 0, ChunkRange: *chunkRange}.Normalized()
		fmt.Fprintf(out, "demo: generating tiny perlin world (range=%d, seed=%d) in %s …\n",
			*chunkRange, *seed, mapDir)
		t0 := time.Now()
		if err := generators.GenerateChunkMapWindow(mapDir, gen, req); err != nil {
			fmt.Fprintf(out, "demo: generate: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "demo: world generated in %s\n", time.Since(t0).Round(time.Millisecond))
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

	stats := &minecraftStats{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Per-edit hook + periodic activity print + enable mouse-look.
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if err := host.SetOnEdit(ctx, stats.record); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		if !*headless && !*autoplay {
			_ = host.SetMouseLook(ctx, true)
		}
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		lastP, lastR := -1, -1
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				p, r, m := stats.snapshot()
				if p != lastP || r != lastR {
					fmt.Fprintf(out, "  hud: placed=%d  mined=%d  current=%q\n", p, r, m)
					lastP, lastR = p, r
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
		go autoplayMinecraft(ctx, host, out, cancel)
	}

	err := host.Run(ctx)
	if err != nil && err != context.Canceled {
		fmt.Fprintf(out, "demo: session ended with error: %v\n", err)
		return 1
	}
	p, r, _ := stats.snapshot()
	fmt.Fprintf(out, "demo: session closed. placed=%d  mined=%d\n", p, r)
	return 0
}

func printBriefing(out io.Writer) {
	fmt.Fprintln(out, "=============================================")
	fmt.Fprintln(out, " GOGOXEL — tiny Minecraft-like clone")
	fmt.Fprintln(out, "=============================================")
	fmt.Fprintln(out, "  Controls:")
	fmt.Fprintln(out, "    WASD      — walk")
	fmt.Fprintln(out, "    Mouse     — look around (cursor is captured)")
	fmt.Fprintln(out, "    Q / E     — fly up / down")
	fmt.Fprintln(out, "    Shift     — sprint")
	fmt.Fprintln(out, "    Left clk  — place block")
	fmt.Fprintln(out, "    Right clk — mine block")
	fmt.Fprintln(out, "    [ / ]     — previous / next material")
	fmt.Fprintln(out, "    Close window to quit")
	fmt.Fprintln(out, "=============================================")
}

// autoplayMinecraft drives a tiny scripted player: wait for the scene
// to load, place one block of each material, then mine one block, then
// exit. Used by the CI smoke test to prove the play loop end-to-end.
func autoplayMinecraft(ctx context.Context, host *game.Host, out io.Writer, cancel context.CancelFunc) {
	_, err := host.WaitUntilReady(ctx, session.WaitCriteria{
		RequireSceneLoaded: true,
		MaxTicks:           600,
	})
	if err != nil {
		fmt.Fprintf(out, "autoplay: wait-until-ready: %v\n", err)
		return
	}
	mats := engine.EditMaterialNames()
	for i, m := range mats {
		x := 0.50 + float32(i)*0.02
		y := float32(0.55)
		if err := host.SetSelectedMaterial(ctx, m); err != nil {
			fmt.Fprintf(out, "autoplay: set-material %q: %v\n", m, err)
			return
		}
		r, err := host.EditAtCursor(ctx, session.EditModePlace,
			session.CursorPosition{NormalizedX: x, NormalizedY: y})
		if err != nil {
			fmt.Fprintf(out, "autoplay: place %q: %v\n", m, err)
			return
		}
		fmt.Fprintf(out, "autoplay: placed %q → changed=%v target=%v\n", m, r.Changed, r.TargetVoxel)
		select {
		case <-ctx.Done():
			return
		case <-time.After(120 * time.Millisecond):
		}
	}
	r, err := host.EditAtCursor(ctx, session.EditModeRemove,
		session.CursorPosition{NormalizedX: 0.50, NormalizedY: 0.55})
	if err == nil {
		fmt.Fprintf(out, "autoplay: mined block → changed=%v target=%v\n", r.Changed, r.TargetVoxel)
	}
	time.Sleep(1500 * time.Millisecond)
	cancel()
}
