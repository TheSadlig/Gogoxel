// Package save defines the persistent-world snapshot+journal data
// model. See issue #26.
//
// The wire format is intentionally minimal in this first slice:
//   - Snapshot: full-world point-in-time dump (header + sections).
//   - Journal:  append-only log of edits since the most recent snapshot.
// Loading replays the journal on top of the snapshot. A future slice
// will compact the journal back into a new snapshot on a configurable
// cadence.
package save

import (
	"encoding/binary"
	"errors"
	"io"
)

// Magic identifies a Gogoxel save stream.
var Magic = [4]byte{'G', 'X', 'S', 'V'}

// FormatVersion is the on-disk format version. Bump on any incompatible
// change.
const FormatVersion uint32 = 1

// Header is written at the start of every save stream.
type Header struct {
	Magic   [4]byte
	Version uint32
	Kind    Kind // Snapshot or Journal
}

// Kind disambiguates the two stream types.
type Kind uint32

const (
	KindSnapshot Kind = 1
	KindJournal  Kind = 2
)

// ErrBadMagic is returned when a stream does not begin with Magic.
var ErrBadMagic = errors.New("save: bad magic")

// ErrUnsupportedVersion is returned when a stream uses a newer format
// than this build understands.
var ErrUnsupportedVersion = errors.New("save: unsupported version")

// WriteHeader emits a Header to w using little-endian encoding.
func WriteHeader(w io.Writer, h Header) error {
	if h.Magic == ([4]byte{}) {
		h.Magic = Magic
	}
	if h.Version == 0 {
		h.Version = FormatVersion
	}
	return binary.Write(w, binary.LittleEndian, h)
}

// ReadHeader parses a Header from r and validates magic + version.
func ReadHeader(r io.Reader) (Header, error) {
	var h Header
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return Header{}, err
	}
	if h.Magic != Magic {
		return Header{}, ErrBadMagic
	}
	if h.Version != FormatVersion {
		return Header{}, ErrUnsupportedVersion
	}
	return h, nil
}
