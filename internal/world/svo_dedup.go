package world

import "math/bits"

// SVDAG-style deduplication for the flat SVO node array.
//
// Two non-brick subtrees with identical structure (identical payloads and
// identical recursive children) share the same physical child block after
// dedup, collapsing many duplicate "all-air" and "uniform stone" subtrees
// that natural terrain produces. Brick leaves are NEVER deduplicated — each
// brick owns unique voxel data and is referenced by a unique NodeIndex.
//
// The shader is unaffected: deduplicated nodes are still reached through
// the same childPointer indirection, the same parent simply shares its
// children with another parent.

const (
	fnv1aOffset uint64 = 0xcbf29ce484222325
	fnv1aPrime  uint64 = 0x100000001b3
)

func fnv1aMix(h, x uint64) uint64 {
	h ^= x
	h *= fnv1aPrime
	return h
}

// DedupNodes hash-conses identical subtree blocks and rewrites
// childPointer values so duplicates share a single backing block.
// It rewrites s.nodes and s.bricks in place. Returns the number of
// nodes after dedup (always <= len(s.nodes) on entry). Does NOT
// rebuild storageWords — the caller is expected to do that.
//
// Determinism: bottom-up hashing uses FNV-1a over a fixed traversal
// order, so identical input SVOs produce byte-identical post-dedup
// arrays. The shader and screenshots are unaffected modulo storage-
// buffer size.
func (s *SVO) DedupNodes() int {
	if s == nil || len(s.nodes) <= 1 {
		if s != nil {
			return len(s.nodes)
		}
		return 0
	}

	// Pass 1 — compute subtree hashes bottom-up. Brick leaves use a salt
	// over their original index so any block containing brick descendants
	// becomes globally unique (impossible to dedup → bricks are safe).
	hashes := make([]uint64, len(s.nodes))
	computed := make([]bool, len(s.nodes))

	var hashOf func(idx uint32) uint64
	hashOf = func(idx uint32) uint64 {
		if computed[idx] {
			return hashes[idx]
		}
		// Mark before recursing so accidental cycles cannot infinite-loop
		// (the flatten path is a DAG by construction, but defence-in-depth).
		computed[idx] = true
		node := s.nodes[idx]
		h := fnv1aOffset
		if node.isBrickLeaf() {
			h = fnv1aMix(h, 0xB91CCB91CC)
			h = fnv1aMix(h, uint64(idx)) // per-brick uniqueness
			h = fnv1aMix(h, uint64(node.payload))
			hashes[idx] = h
			return h
		}
		cm := uint8(node.payload & childMaskMask)
		if cm == 0 || node.childPointer == 0 {
			h = fnv1aMix(h, 0x1EAF1EAF)
			h = fnv1aMix(h, uint64(node.payload))
			hashes[idx] = h
			return h
		}
		h = fnv1aMix(h, 0xB1AC0DE)
		h = fnv1aMix(h, uint64(node.payload))
		count := bits.OnesCount8(cm)
		for i := 0; i < count; i++ {
			h = fnv1aMix(h, hashOf(node.childPointer+uint32(i)))
		}
		hashes[idx] = h
		return h
	}
	for idx := range s.nodes {
		hashOf(uint32(idx))
	}

	// Pass 2 — emit a new compacted node array in BFS order from the root.
	// Identical child-blocks (keyed by the hash of the ordered child
	// subtree hashes) are emitted once and shared by all parents.
	type queueEntry struct {
		oldIdx uint32
		newIdx uint32
	}
	newNodes := make([]SvoNode, 0, len(s.nodes))
	// nodeRemap is dense; -1 sentinel via len(s.nodes)+1 == "not assigned".
	nodeRemap := make([]uint32, len(s.nodes))
	remapValid := make([]bool, len(s.nodes))
	childBlockMap := make(map[uint64]uint32)
	queue := make([]queueEntry, 0, 64)

	// Root is always at logical index 0.
	newNodes = append(newNodes, s.nodes[0])
	nodeRemap[0] = 0
	remapValid[0] = true
	queue = append(queue, queueEntry{oldIdx: 0, newIdx: 0})

	for qi := 0; qi < len(queue); qi++ {
		entry := queue[qi]
		oldNode := s.nodes[entry.oldIdx]
		// Brick leaves: keep childPointer (resident slot, or 0 when
		// nonresident) untouched. Their "subtree" is themselves.
		if oldNode.isBrickLeaf() {
			newNodes[entry.newIdx].childPointer = oldNode.childPointer
			continue
		}
		cm := uint8(oldNode.payload & childMaskMask)
		if cm == 0 || oldNode.childPointer == 0 {
			// Pure leaf — childPointer already copied from oldNode.
			continue
		}
		count := bits.OnesCount8(cm)

		blockHash := fnv1aOffset
		blockHash = fnv1aMix(blockHash, 0xB10CB10C)
		blockHash = fnv1aMix(blockHash, uint64(count))
		for i := 0; i < count; i++ {
			blockHash = fnv1aMix(blockHash, hashes[oldNode.childPointer+uint32(i)])
		}
		if existing, ok := childBlockMap[blockHash]; ok {
			// Children already emitted at `existing` via a sibling parent
			// with the same child block; alias them. Brick-salt guarantees
			// blocks reachable from this aliasing branch are brick-free,
			// so brick.NodeIndex never points into the aliased-away copy.
			newNodes[entry.newIdx].childPointer = existing
			continue
		}
		// Emit a fresh child block.
		childBase := uint32(len(newNodes))
		for i := 0; i < count; i++ {
			childOldIdx := oldNode.childPointer + uint32(i)
			newNodes = append(newNodes, s.nodes[childOldIdx])
			nodeRemap[childOldIdx] = childBase + uint32(i)
			remapValid[childOldIdx] = true
			queue = append(queue, queueEntry{oldIdx: childOldIdx, newIdx: childBase + uint32(i)})
		}
		childBlockMap[blockHash] = childBase
		newNodes[entry.newIdx].childPointer = childBase
	}

	s.nodes = newNodes

	// Pass 3 — remap brick.NodeIndex through the new array. Bricks whose
	// old index was never visited (orphaned by malformed input) are dropped.
	if len(s.bricks) > 0 {
		kept := s.bricks[:0]
		for _, brick := range s.bricks {
			if int(brick.NodeIndex) >= len(remapValid) || !remapValid[brick.NodeIndex] {
				continue
			}
			brick.NodeIndex = nodeRemap[brick.NodeIndex]
			kept = append(kept, brick)
		}
		s.bricks = kept
	}

	return len(s.nodes)
}
