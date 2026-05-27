package vulkan

import (
	"testing"

	vk "github.com/vulkan-go/vulkan"
)

func TestChunkGigabufferAllocateAndCoalesce(t *testing.T) {
	gigabuffer := &chunkGigabuffer{
		capacity:  4096,
		alignment: 256,
		freeList:  []chunkGigabufferSpan{{offset: 0, size: 4096}},
	}

	firstOffset, firstSize, err := gigabuffer.Allocate(513)
	if err != nil {
		t.Fatalf("Allocate(first) error = %v", err)
	}
	if firstOffset != 0 {
		t.Fatalf("firstOffset = %d, want 0", firstOffset)
	}
	if firstSize != 768 {
		t.Fatalf("firstSize = %d, want 768", firstSize)
	}

	secondOffset, secondSize, err := gigabuffer.Allocate(1024)
	if err != nil {
		t.Fatalf("Allocate(second) error = %v", err)
	}
	if secondOffset != 768 {
		t.Fatalf("secondOffset = %d, want 768", secondOffset)
	}
	if secondSize != 1024 {
		t.Fatalf("secondSize = %d, want 1024", secondSize)
	}

	gigabuffer.Free(firstOffset, firstSize)
	gigabuffer.Free(secondOffset, secondSize)

	if len(gigabuffer.freeList) != 1 {
		t.Fatalf("len(freeList) = %d, want 1", len(gigabuffer.freeList))
	}
	if got := gigabuffer.freeList[0]; got != (chunkGigabufferSpan{offset: 0, size: 4096}) {
		t.Fatalf("free span = %+v, want %+v", got, chunkGigabufferSpan{offset: 0, size: 4096})
	}
}

func TestChunkGigabufferAllocateExhausted(t *testing.T) {
	gigabuffer := &chunkGigabuffer{
		capacity:  1024,
		alignment: vk.DeviceSize(256),
		freeList:  []chunkGigabufferSpan{{offset: 0, size: 1024}},
	}

	if _, _, err := gigabuffer.Allocate(1025); err == nil {
		t.Fatalf("Allocate oversized expected error")
	}
}
