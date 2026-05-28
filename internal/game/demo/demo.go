// Package demo wires every Gogoxel subsystem into one small playable
// game: "Build the Beacon". The player has limited time to place a
// target list of voxels at specific coordinates with the matching
// material. Each correct placement scores points and plays an audio
// cue; an undo restores the previous state. When all targets are
// filled, the run ends in a win and a snapshot is written.
//
// The session is deterministic and headless-runnable so the gameplay
// loop is fully covered by unit tests. A `gogoxel demo` CLI subcommand
// drives it from main.
package demo

import (
	"bytes"
	"fmt"
	"image/color"
	"time"

	"Gogoxel/internal/actor"
	"Gogoxel/internal/audio"
	"Gogoxel/internal/control/bindings"
	"Gogoxel/internal/ecs"
	"Gogoxel/internal/editor"
	"Gogoxel/internal/engine/frustum"
	"Gogoxel/internal/fluid"
	"Gogoxel/internal/input"
	"Gogoxel/internal/net"
	"Gogoxel/internal/physics"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/profiler"
	"Gogoxel/internal/render/computerays"
	"Gogoxel/internal/render/gi"
	"Gogoxel/internal/render/material"
	"Gogoxel/internal/render/postfx"
	"Gogoxel/internal/render/shadows"
	"Gogoxel/internal/render/taa"
	"Gogoxel/internal/save"
	"Gogoxel/internal/ui"
	"Gogoxel/internal/vulkan/asynccompute"
	"Gogoxel/internal/world/dag"
)

// Coord is a voxel coord in the demo's local space.
type Coord struct{ X, Y, Z int32 }

// Target is one voxel the player must place to win.
type Target struct {
	At       Coord
	Material uint8
	Filled   bool
}

// Result is the terminal session outcome.
type Result uint8

const (
	ResultRunning Result = iota
	ResultWin
	ResultLoss
)

// Config is the demo run configuration.
type Config struct {
	Targets     []Target
	TimeBudget  time.Duration
	StartingHP  float32
	WorldCells  [3]int // for the fluid sub-tick demo
}

// DefaultConfig returns the "build the beacon" challenge: place 4 red
// voxels forming a vertical column.
func DefaultConfig() Config {
	return Config{
		Targets: []Target{
			{At: Coord{0, 0, 0}, Material: 1},
			{At: Coord{0, 1, 0}, Material: 1},
			{At: Coord{0, 2, 0}, Material: 1},
			{At: Coord{0, 3, 0}, Material: 2},
		},
		TimeBudget: 30 * time.Second,
		StartingHP: 100,
		WorldCells: [3]int{8, 8, 8},
	}
}

// HUD is what the renderer would paint for the current frame.
type HUD struct {
	Frame    ui.Frame
	ScoreStr string
}

// Session is the live demo game state.
type Session struct {
	cfg          Config
	player       *actor.Actor
	world        *actor.Manager
	ecsWorld     *ecs.World
	physWorld    physics.World
	editor       editor.Session
	audio        audio.Driver
	bindings     bindings.Map
	palette      material.Palette
	asyncQ       asynccompute.Queue
	dedup        *dag.Dedup
	fluidGrid    *fluid.Grid
	postfx       postfx.Settings
	taaSettings  taa.Settings
	giSettings   gi.Settings
	sun          shadows.Sun
	tiles        []computerays.Tile

	elapsed   time.Duration
	score     int
	result    Result
	placed    []Coord
	undoStack []editor.Op

	logs bytes.Buffer
}

// New returns a ready-to-tick Session.
func New(cfg Config) *Session {
	s := &Session{
		cfg:         cfg,
		world:       actor.NewManager(),
		ecsWorld:    ecs.NewWorld(),
		physWorld:   physics.World{Gravity: physics.Vec3{Y: -9.81}},
		audio:       audio.NopDriver{},
		palette:     material.Default(),
		dedup:       dag.NewDedup(),
		fluidGrid:   fluid.NewGrid(cfg.WorldCells[0], cfg.WorldCells[1], cfg.WorldCells[2]),
		postfx:      postfx.Default(),
		taaSettings: taa.Default(),
		giSettings:  gi.Default(),
		sun:         shadows.Default(),
	}

	id := s.world.Spawn(actor.Transform{Position: [3]float32{0, 16, 8}}, cfg.StartingHP, nil)
	s.player = s.world.Get(id)

	s.physWorld.Bodies = append(s.physWorld.Bodies, physics.Body{
		Position:    physics.Vec3{X: s.player.Transform.Position[0], Y: s.player.Transform.Position[1], Z: s.player.Transform.Position[2]},
		InverseMass: 1,
	})

	s.bindings.Add(bindings.Binding{Action: input.Action("jump"), Sources: []bindings.Source{{Key: "Space"}}})
	s.bindings.Add(bindings.Binding{Action: input.Action("place"), Sources: []bindings.Source{{Key: "Mouse1", IsMouse: true}}})

	s.tiles = computerays.Plan(1280, 720, 64, 64, nil)
	s.asyncQ.Push(asynccompute.Item{Kind: asynccompute.KindBrickUpload, Bytes: 4096})
	s.dedup.Intern(dag.HashWords([]uint32{0, 0, 0}), func() uint32 { return 0 })

	return s
}

// Tick advances the session by dt. Returns the current Result.
func (s *Session) Tick(dt time.Duration) Result {
	defer profiler.Zone("demo.Tick")()

	if s.result != ResultRunning {
		return s.result
	}
	s.elapsed += dt
	if s.elapsed >= s.cfg.TimeBudget {
		s.result = ResultLoss
		s.audio.Submit(audio.Source{Gain: 1, Pitch: 0.5, Position: audio.Vec3{}})
		return s.result
	}

	dtSec := float32(dt.Seconds())
	s.physWorld.Tick(dtSec)
	s.world.Tick(float64(dtSec))
	s.ecsWorld.Tick(float64(dtSec))

	// Fluid: a single cell flip to validate the grid is wired.
	c := s.fluidGrid.At(0, 0, 0)
	c.Settled = true

	profiler.FrameMark()
	return s.result
}

// PlaceVoxel records a voxel placement attempt. Awards score when it
// matches the next unfilled target.
func (s *Session) PlaceVoxel(at Coord, mat uint8) {
	if s.result != ResultRunning {
		return
	}
	op := &placeOp{session: s, at: at, mat: mat}
	s.editor.Do(op)
	s.audio.Submit(audio.Source{Gain: 1, Pitch: 1, Position: audio.Vec3{X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z)}})

	for i := range s.cfg.Targets {
		t := &s.cfg.Targets[i]
		if !t.Filled && t.At == at && t.Material == mat {
			t.Filled = true
			s.score += 100
			break
		}
	}
	if s.allFilled() {
		s.result = ResultWin
	}
}

// Undo reverses the most recent placement.
func (s *Session) Undo() bool { return s.editor.Undo() }

// Score returns the current player score.
func (s *Session) Score() int { return s.score }

// Result returns the current outcome.
func (s *Session) ResultNow() Result { return s.result }

// Placed returns the live list of placed voxel coords.
func (s *Session) Placed() []Coord { return append([]Coord(nil), s.placed...) }

// RemainingTargets returns how many targets still need to be filled.
func (s *Session) RemainingTargets() int {
	n := 0
	for _, t := range s.cfg.Targets {
		if !t.Filled {
			n++
		}
	}
	return n
}

// HUD renders the per-frame command list. Doesn't allocate beyond the
// command-list backing slice between frames if you re-use the same
// Session.
func (s *Session) HUD() HUD {
	var f ui.Frame
	f.DrawRect(ui.Rect{X: 8, Y: 8, W: 220, H: 48}, color.RGBA{0, 0, 0, 180})
	f.DrawText(16, 24, color.RGBA{255, 255, 255, 255},
		fmt.Sprintf("score %d   remaining %d", s.score, s.RemainingTargets()))
	f.DrawText(16, 44, color.RGBA{200, 200, 200, 255},
		fmt.Sprintf("t %.1fs / %.1fs", s.elapsed.Seconds(), s.cfg.TimeBudget.Seconds()))
	return HUD{Frame: f, ScoreStr: fmt.Sprintf("%d", s.score)}
}

// Snapshot writes the end-of-game save header to a buffer and returns
// its bytes. Real chunk-delta serialization is a follow-up (issue #26).
func (s *Session) Snapshot() ([]byte, error) {
	var buf bytes.Buffer
	if err := save.WriteHeader(&buf, save.Header{Kind: save.KindSnapshot}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Scoreboard encodes a network-message scoreboard payload. Validates
// the internal/net envelope is wired end-to-end.
func (s *Session) Scoreboard() net.Message {
	return net.Message{
		Kind:    net.KindSnapshot,
		Seq:     1,
		Payload: []byte(fmt.Sprintf("score=%d result=%d", s.score, s.result)),
	}
}

// FrustumCullingDemo returns true if a far-away AABB is correctly
// culled — proves the frustum primitive is reachable from gameplay.
func (s *Session) FrustumCullingDemo() bool {
	cam := platform.Camera{Position: [3]float32{0, 0, 0}, YawDeg: 0, PitchDeg: 0, FovDeg: 60}
	fr := frustum.FromCamera(cam, 16.0/9.0, 0.1, 100.0)
	// A box well behind the camera must NOT be contained.
	return !fr.ContainsAABB([3]float32{-1000, -1, -1}, [3]float32{-990, 1, 1})
}

func (s *Session) allFilled() bool {
	for _, t := range s.cfg.Targets {
		if !t.Filled {
			return false
		}
	}
	return true
}

// placeOp is the editor.Op for a single voxel placement (used for
// undo). Apply pushes onto the session's placed list; Undo pops.
type placeOp struct {
	session *Session
	at      Coord
	mat     uint8
}

func (o *placeOp) Apply() { o.session.placed = append(o.session.placed, o.at) }
func (o *placeOp) Undo() {
	n := len(o.session.placed)
	if n == 0 {
		return
	}
	o.session.placed = o.session.placed[:n-1]
	for i := range o.session.cfg.Targets {
		t := &o.session.cfg.Targets[i]
		if t.At == o.at && t.Material == o.mat && t.Filled {
			t.Filled = false
			o.session.score -= 100
			break
		}
	}
}
