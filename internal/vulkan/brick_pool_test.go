package vulkan

import "testing"

func TestBrickPoolAllocateSkipsReservedZeroSlot(t *testing.T) {
	pool := &brickPool{allocated: make([]bool, brickPoolCapacity)}
	pool.allocated[0] = true

	first, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() first error = %v", err)
	}
	if first != 1 {
		t.Fatalf("Allocate() first slot = %d, want 1", first)
	}

	second, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() second error = %v", err)
	}
	if second != 2 {
		t.Fatalf("Allocate() second slot = %d, want 2", second)
	}

	pool.Free(first)

	reused, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate() reused error = %v", err)
	}
	if reused != first {
		t.Fatalf("Allocate() reused slot = %d, want %d", reused, first)
	}
}

func TestBrickPoolFreeKeepsZeroSlotReserved(t *testing.T) {
	pool := &brickPool{allocated: make([]bool, brickPoolCapacity)}
	pool.allocated[0] = true

	pool.Free(0)
	if !pool.allocated[0] {
		t.Fatal("Free(0) released reserved air slot")
	}
}

func TestBrickPoolSlotCoordRoundTrip(t *testing.T) {
	testCases := []struct {
		slot uint32
		x    uint32
		y    uint32
		z    uint32
	}{
		{slot: 0, x: 0, y: 0, z: 0},
		{slot: 1, x: 1, y: 0, z: 0},
		{slot: 63, x: 63, y: 0, z: 0},
		{slot: 64, x: 0, y: 1, z: 0},
		{slot: 520, x: 8, y: 8, z: 0},
		{slot: brickPoolCapacity - 1, x: brickPoolGridEdge - 1, y: brickPoolGridEdge - 1, z: brickPoolGridEdge - 1},
	}

	for _, testCase := range testCases {
		x, y, z, err := brickPoolSlotCoord(testCase.slot)
		if err != nil {
			t.Fatalf("brickPoolSlotCoord(%d) error = %v", testCase.slot, err)
		}
		if x != testCase.x || y != testCase.y || z != testCase.z {
			t.Fatalf("brickPoolSlotCoord(%d) = (%d,%d,%d), want (%d,%d,%d)", testCase.slot, x, y, z, testCase.x, testCase.y, testCase.z)
		}

		slot, err := brickPoolCoordSlot(x, y, z)
		if err != nil {
			t.Fatalf("brickPoolCoordSlot(%d,%d,%d) error = %v", x, y, z, err)
		}
		if slot != testCase.slot {
			t.Fatalf("brickPoolCoordSlot(%d,%d,%d) = %d, want %d", x, y, z, slot, testCase.slot)
		}
	}
}

func TestBrickPoolCoordSlotRejectsOutOfRange(t *testing.T) {
	if _, err := brickPoolCoordSlot(brickPoolGridEdge, 0, 0); err == nil {
		t.Fatal("brickPoolCoordSlot() error = nil, want out-of-range error")
	}
	if _, _, _, err := brickPoolSlotCoord(brickPoolCapacity); err == nil {
		t.Fatal("brickPoolSlotCoord() error = nil, want out-of-range error")
	}
}