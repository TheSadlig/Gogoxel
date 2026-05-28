package assets

import (
	"bytes"
	"io"
	"testing"
)

type fakeImp struct{}

func (fakeImp) Extensions() []string             { return []string{".fake"} }
func (fakeImp) Import(r io.Reader) (any, error)  { b, err := io.ReadAll(r); return b, err }

func TestRegisterFind(t *testing.T) {
	Register(fakeImp{})
	imp, err := Find("foo/bar.FAKE")
	if err != nil || imp == nil {
		t.Fatalf("Find failed: %v", err)
	}
	out, err := imp.Import(bytes.NewReader([]byte("hi")))
	if err != nil || string(out.([]byte)) != "hi" {
		t.Fatalf("import failed: %v / %v", out, err)
	}
}

func TestFindMissing(t *testing.T) {
	if _, err := Find("foo.unknown"); err != ErrNoImporter {
		t.Fatalf("expected ErrNoImporter, got %v", err)
	}
}
