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
