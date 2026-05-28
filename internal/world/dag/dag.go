// Package dag is the foundation of SVO-DAG compression. See issue #15.
//
// The idea: identical subtrees in the SVO can share a single payload by
// content-addressing nodes. The first slice provides a hash helper and
// a small dedup map so future slices can plug into the SVO build
// pipeline without re-deriving the contract.
package dag

import (
	"encoding/binary"
	"hash/fnv"
)

// NodeKey is a 64-bit content hash of a node payload.
type NodeKey uint64

// Hash returns the FNV-1a hash of payload as a NodeKey.
func Hash(payload []byte) NodeKey {
	h := fnv.New64a()
	_, _ = h.Write(payload)
	return NodeKey(h.Sum64())
}

// HashWords hashes a uint32 slice as little-endian bytes (allocation-
// free in the steady-state hot path).
func HashWords(words []uint32) NodeKey {
	h := fnv.New64a()
	var buf [4]byte
	for _, w := range words {
		binary.LittleEndian.PutUint32(buf[:], w)
		_, _ = h.Write(buf[:])
	}
	return NodeKey(h.Sum64())
}

// Dedup is a content-addressed node table.
type Dedup struct{ table map[NodeKey]uint32 }

// NewDedup returns an empty dedup table.
func NewDedup() *Dedup { return &Dedup{table: make(map[NodeKey]uint32)} }

// Intern returns the canonical index for the given hash, allocating a
// new index via next() on a miss.
func (d *Dedup) Intern(k NodeKey, next func() uint32) uint32 {
	if i, ok := d.table[k]; ok {
		return i
	}
	i := next()
	d.table[k] = i
	return i
}

// Len returns the number of distinct nodes.
func (d *Dedup) Len() int { return len(d.table) }
