package vulkan

import (
	"testing"

	vk "github.com/vulkan-go/vulkan"
)

func TestAlignDeviceSizeHandlesPowerOfTwoAlignment(t *testing.T) {
	if got, want := alignDeviceSize(vk.DeviceSize(13), vk.DeviceSize(4)), vk.DeviceSize(16); got != want {
		t.Fatalf("alignDeviceSize(13, 4) = %d, want %d", got, want)
	}
}

func TestAlignDeviceSizeHandlesNonPowerOfTwoAlignment(t *testing.T) {
	if got, want := alignDeviceSize(vk.DeviceSize(5), vk.DeviceSize(6)), vk.DeviceSize(6); got != want {
		t.Fatalf("alignDeviceSize(5, 6) = %d, want %d", got, want)
	}
	if got, want := alignDeviceSize(vk.DeviceSize(7), vk.DeviceSize(6)), vk.DeviceSize(12); got != want {
		t.Fatalf("alignDeviceSize(7, 6) = %d, want %d", got, want)
	}
}

func TestStagingRingCanAllocateReturnsFalseWhenRemainingSpaceIsTooSmall(t *testing.T) {
	ring := &stagingRing{size: stagingRingBytesPerSlot, offset: stagingRingBytesPerSlot - 492}
	if stagingRingCanAllocate(ring, 512, 4) {
		t.Fatal("stagingRingCanAllocate() = true, want false when the ring only has 492 bytes left")
	}
}

func TestStagingRingCanAllocateReturnsTrueWhenAllocationFits(t *testing.T) {
	ring := &stagingRing{size: stagingRingBytesPerSlot, offset: 64 * 1024}
	if !stagingRingCanAllocate(ring, 512, 4) {
		t.Fatal("stagingRingCanAllocate() = false, want true for a fitting brick upload")
	}
}
