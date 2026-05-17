package vulkan

/*
#include <stdlib.h>
*/
import "C"

import (
	"strings"
	"unsafe"
)

type cStringArena struct {
	ptrs []unsafe.Pointer
}

func (a *cStringArena) String(value string) string {
	trimmed := strings.TrimRight(value, "\x00")
	ptr := C.CString(trimmed)
	a.ptrs = append(a.ptrs, unsafe.Pointer(ptr))
	return unsafe.String((*byte)(unsafe.Pointer(ptr)), len(trimmed))
}

func (a *cStringArena) Strings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	converted := make([]string, len(values))
	for index, value := range values {
		converted[index] = a.String(value)
	}

	return converted
}

func (a *cStringArena) Free() {
	if a == nil {
		return
	}

	for _, ptr := range a.ptrs {
		C.free(ptr)
	}
	a.ptrs = nil
}