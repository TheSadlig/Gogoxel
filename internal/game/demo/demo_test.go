package demo

import (
	"testing"
	"time"

	"Gogoxel/internal/save"
)

func TestNewSessionStartsRunning(t *testing.T) {
	s := New(DefaultConfig())
	if s.ResultNow() != ResultRunning {
		t.Fatalf("new session should be Running, got %d", s.ResultNow())
	}
	if s.RemainingTargets() != 4 {
		t.Fatalf("expected 4 remaining, got %d", s.RemainingTargets())
	}
}

func TestPlaceVoxelScores(t *testing.T) {
	s := New(DefaultConfig())
	s.PlaceVoxel(Coord{0, 0, 0}, 1)
	if s.Score() != 100 {
		t.Fatalf("score = %d, want 100", s.Score())
	}
	// Wrong material — no score.
	s.PlaceVoxel(Coord{0, 1, 0}, 99)
	if s.Score() != 100 {
		t.Fatalf("score should not change on mismatch, got %d", s.Score())
	}
}

func TestWinCondition(t *testing.T) {
	s := New(DefaultConfig())
	for _, tg := range s.cfg.Targets {
		s.PlaceVoxel(tg.At, tg.Material)
	}
	if s.ResultNow() != ResultWin {
		t.Fatalf("expected ResultWin, got %d", s.ResultNow())
	}
	if s.Score() != 400 {
		t.Fatalf("score = %d, want 400", s.Score())
	}
}

func TestLossOnTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TimeBudget = 100 * time.Millisecond
	s := New(cfg)
	if r := s.Tick(200 * time.Millisecond); r != ResultLoss {
		t.Fatalf("expected ResultLoss, got %d", r)
	}
}

func TestUndoReversesScore(t *testing.T) {
	s := New(DefaultConfig())
	s.PlaceVoxel(Coord{0, 0, 0}, 1)
	if !s.Undo() {
		t.Fatalf("Undo returned false")
	}
	if s.Score() != 0 {
		t.Fatalf("score after undo = %d, want 0", s.Score())
	}
	if s.RemainingTargets() != 4 {
		t.Fatalf("undo failed to free target: %d remaining", s.RemainingTargets())
	}
}

func TestSnapshotHeader(t *testing.T) {
	s := New(DefaultConfig())
	b, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 4 {
		t.Fatalf("snapshot too short: %d bytes", len(b))
	}
	if b[0] != save.Magic[0] {
		t.Fatalf("snapshot magic byte 0 = %x", b[0])
	}
}

func TestScoreboardMessage(t *testing.T) {
	s := New(DefaultConfig())
	m := s.Scoreboard()
	if len(m.Payload) == 0 {
		t.Fatalf("empty scoreboard payload")
	}
}

func TestFrustumCullingDemoCullsBehind(t *testing.T) {
	s := New(DefaultConfig())
	if !s.FrustumCullingDemo() {
		t.Fatalf("frustum should cull a box behind the camera")
	}
}

func TestHUDRendersCommands(t *testing.T) {
	s := New(DefaultConfig())
	s.PlaceVoxel(Coord{0, 0, 0}, 1)
	hud := s.HUD()
	if len(hud.Frame.Commands()) < 2 {
		t.Fatalf("hud expected ≥2 commands, got %d", len(hud.Frame.Commands()))
	}
}

func TestTickAdvancesTime(t *testing.T) {
	s := New(DefaultConfig())
	s.Tick(50 * time.Millisecond)
	if s.elapsed != 50*time.Millisecond {
		t.Fatalf("elapsed = %v", s.elapsed)
	}
}
