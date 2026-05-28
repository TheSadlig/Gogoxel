// Package ui is the immediate-mode HUD/UI primitives layer. See issue #29.
//
// The first slice defines a minimal command-list draw API. A real
// backend (text shaping, atlas) will be added in a follow-up; for now
// the package validates the command-buffer ergonomics and supports
// recording widgets in tests.
package ui

import "image/color"

// Rect is a screen-space rectangle in pixels.
type Rect struct{ X, Y, W, H int32 }

// CommandKind is the discriminator for Command.
type CommandKind uint8

const (
	CmdRect CommandKind = iota + 1
	CmdText
)

// Command is one entry in the immediate-mode command list.
type Command struct {
	Kind  CommandKind
	Rect  Rect
	Color color.RGBA
	Text  string
}

// Frame accumulates draw commands for one UI frame. Zero value is
// usable; call Reset between frames to keep the backing slice.
type Frame struct{ cmds []Command }

// Reset clears the frame, preserving the backing slice.
func (f *Frame) Reset() { f.cmds = f.cmds[:0] }

// Commands returns the recorded command list.
func (f *Frame) Commands() []Command { return f.cmds }

// DrawRect records a filled rectangle.
func (f *Frame) DrawRect(r Rect, c color.RGBA) {
	f.cmds = append(f.cmds, Command{Kind: CmdRect, Rect: r, Color: c})
}

// DrawText records a text string at (x,y).
func (f *Frame) DrawText(x, y int32, c color.RGBA, s string) {
	f.cmds = append(f.cmds, Command{Kind: CmdText, Rect: Rect{X: x, Y: y}, Color: c, Text: s})
}
