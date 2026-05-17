package vulkan

import "runtime"

func withPinnedValue[T any](value *T, fn func() error) error {
	if value == nil {
		return fn()
	}

	var pinner runtime.Pinner
	pinner.Pin(value)
	defer pinner.Unpin()

	return fn()
}

func withPinnedSlice[T any](values []T, fn func() error) error {
	if len(values) == 0 {
		return fn()
	}

	var pinner runtime.Pinner
	pinner.Pin(&values[0])
	defer pinner.Unpin()

	return fn()
}