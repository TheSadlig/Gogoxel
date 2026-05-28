// Package asynccompute is the foundation of the async-compute upload
// queue strategy. See issue #16.
package asynccompute

// Kind describes the work-item type.
type Kind uint8

const (
	KindBrickUpload      Kind = 1
	KindSceneCopy        Kind = 2
	KindLightingDispatch Kind = 3
)

// Item is one piece of work to submit on the async compute queue.
type Item struct {
	Kind     Kind
	Priority int32
	Bytes    uint32
	Payload  any
}

// Queue is a simple in-process FIFO. Concrete backends (separate
// VkQueue, dedicated transfer thread) implement the same surface.
type Queue struct{ items []Item }

func (q *Queue) Push(i Item) { q.items = append(q.items, i) }

func (q *Queue) PopN(n int) []Item {
	if n > len(q.items) {
		n = len(q.items)
	}
	out := append([]Item(nil), q.items[:n]...)
	q.items = q.items[n:]
	return out
}

func (q *Queue) Len() int { return len(q.items) }
