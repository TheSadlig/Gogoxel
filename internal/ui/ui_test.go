package ui

import (
	"image/color"
	"testing"
)

func TestFrameRecord(t *testing.T) {
	var f Frame
	f.DrawRect(Rect{0, 0, 10, 10}, color.RGBA{255, 0, 0, 255})
	f.DrawText(1, 2, color.RGBA{255, 255, 255, 255}, "hi")
	if len(f.Commands()) != 2 {
		t.Fatalf("got %d cmds, want 2", len(f.Commands()))
	}
	if f.Commands()[1].Kind != CmdText {
		t.Fatalf("kind = %d, want CmdText", f.Commands()[1].Kind)
	}
	f.Reset()
	if len(f.Commands()) != 0 {
		t.Fatalf("Reset failed")
	}
}
