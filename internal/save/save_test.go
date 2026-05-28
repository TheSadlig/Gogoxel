package save

import (
	"bytes"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHeader(&buf, Header{Kind: KindSnapshot}); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != KindSnapshot || h.Version != FormatVersion {
		t.Fatalf("header = %+v", h)
	}
}

func TestHeaderBadMagic(t *testing.T) {
	buf := bytes.NewReader([]byte{0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0})
	if _, err := ReadHeader(buf); err != ErrBadMagic {
		t.Fatalf("expected ErrBadMagic, got %v", err)
	}
}
