package generators

import (
	"testing"
)

// FuzzParseVOX asserts that the MagicaVoxel .vox parser returns a typed
// error or a model — never panics — for any byte input.
func FuzzParseVOX(f *testing.F) {
	// Seeds: empty, header-only, garbage, partially-valid.
	f.Add([]byte{})
	f.Add([]byte("VOX "))
	f.Add([]byte("VOX \x96\x00\x00\x00"))
	// "MAIN" chunk with zero-content / zero-children — minimal envelope.
	f.Add([]byte("VOX \x96\x00\x00\x00MAIN\x00\x00\x00\x00\x00\x00\x00\x00"))
	// Truncated chunk header.
	f.Add([]byte("VOX \x96\x00\x00\x00MAI"))
	// Bogus chunk advertising a huge content size — parser must reject.
	f.Add([]byte("VOX \x96\x00\x00\x00MAIN\xff\xff\xff\xff\x00\x00\x00\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = parseVOX(data)
	})
}

// FuzzParseRSVO asserts that the bundled rsvo importer never panics on
// arbitrary bytes.
func FuzzParseRSVO(f *testing.F) {
	f.Add([]byte{})
	// "VOXR" magic with zero version and no payload.
	f.Add([]byte("VOXR\x00\x00\x00\x00"))
	// Truncated header.
	f.Add([]byte("VOXR"))
	// Header advertising bogus payload sizes.
	f.Add([]byte("VOXR\x00\x00\x00\x00\xff\xff\xff\xff\xff\xff\xff\xff"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = parseRSVO(data)
	})
}
