// Package editor is the foundation of the in-engine voxel editor. See
// issue #10. The first slice owns editor session state (active brush,
// selection, undo/redo stack). Tool-window UI and gizmos arrive in a
// follow-up using internal/ui.
package editor

// Vec3i is an integer voxel-space vector.
type Vec3i struct{ X, Y, Z int32 }

// Brush describes the current authoring tool.
type Brush struct {
	Material uint8
	Radius   int32 // 0 = single voxel
	Mode     BrushMode
}

// BrushMode chooses additive/subtractive/replace behavior.
type BrushMode uint8

const (
	BrushPlace BrushMode = iota
	BrushErase
	BrushReplace
)

// Op is one reversible editor action.
type Op interface {
	Apply()
	Undo()
}

// Session is the editor's owner-thread state.
type Session struct {
	Brush     Brush
	Selection []Vec3i
	undo      []Op
	redo      []Op
}

// Do applies op and pushes it onto the undo stack, dropping the redo
// stack as usual.
func (s *Session) Do(op Op) {
	op.Apply()
	s.undo = append(s.undo, op)
	s.redo = s.redo[:0]
}

// Undo reverses the most recent Op. Returns false if nothing to undo.
func (s *Session) Undo() bool {
	n := len(s.undo)
	if n == 0 {
		return false
	}
	op := s.undo[n-1]
	s.undo = s.undo[:n-1]
	op.Undo()
	s.redo = append(s.redo, op)
	return true
}

// Redo re-applies the most recently undone Op.
func (s *Session) Redo() bool {
	n := len(s.redo)
	if n == 0 {
		return false
	}
	op := s.redo[n-1]
	s.redo = s.redo[:n-1]
	op.Apply()
	s.undo = append(s.undo, op)
	return true
}
