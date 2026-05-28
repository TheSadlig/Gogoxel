package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// runDoctor performs lightweight environment checks intended to be runnable
// on every supported platform (Linux, Windows, macOS) without GPU access.
// It prints a table of {check, status, detail} rows and exits non-zero if
// any required check fails. It is intentionally side-effect-free besides
// printing — never touches the Vulkan loader directly.
//
// This is the minimum slice referenced by issue #23's acceptance criteria.
// Subcommands listed in issue #7 (new, run, build, asset, shader, ...) are
// follow-ups.
func runDoctor(out io.Writer) int {
	failed := 0
	report := func(name string, ok bool, detail string) {
		status := "OK"
		if !ok {
			status = "FAIL"
			failed++
		}
		fmt.Fprintf(out, "%-28s %-6s %s\n", name, status, detail)
	}

	fmt.Fprintf(out, "gogoxel doctor — environment report\n\n")
	fmt.Fprintf(out, "%-28s %-6s %s\n", "CHECK", "STATUS", "DETAIL")
	fmt.Fprintf(out, "%-28s %-6s %s\n", strings.Repeat("-", 28), strings.Repeat("-", 6), strings.Repeat("-", 30))

	// Go runtime and target OS/arch.
	report("go-runtime", true, fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))

	// glslangValidator: required to build shaders via `make shaders`.
	if path, err := exec.LookPath("glslangValidator"); err == nil {
		report("glslangValidator", true, path)
	} else {
		report("glslangValidator", false, "not on PATH — install Vulkan SDK or glslang-tools")
	}

	// Vulkan loader (best-effort, OS-specific).
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/usr/lib/x86_64-linux-gnu/libvulkan.so.1"); err == nil {
			report("vulkan-loader", true, "/usr/lib/x86_64-linux-gnu/libvulkan.so.1")
		} else if _, err := os.Stat("/usr/lib/libvulkan.so.1"); err == nil {
			report("vulkan-loader", true, "/usr/lib/libvulkan.so.1")
		} else {
			report("vulkan-loader", false, "libvulkan.so.1 not found — apt install libvulkan-dev")
		}
	case "darwin":
		if _, err := os.Stat("/usr/local/lib/libvulkan.dylib"); err == nil {
			report("vulkan-loader", true, "/usr/local/lib/libvulkan.dylib")
		} else if _, err := os.Stat("/opt/homebrew/lib/libvulkan.dylib"); err == nil {
			report("vulkan-loader", true, "/opt/homebrew/lib/libvulkan.dylib (MoltenVK)")
		} else {
			report("vulkan-loader", false, "libvulkan.dylib not found — brew install molten-vk vulkan-loader")
		}
	case "windows":
		if sdk := os.Getenv("VULKAN_SDK"); sdk != "" {
			report("vulkan-loader", true, "VULKAN_SDK="+sdk)
		} else {
			report("vulkan-loader", false, "VULKAN_SDK env var not set — install Vulkan SDK (choco install vulkan-sdk)")
		}
	default:
		report("vulkan-loader", false, "unsupported OS: "+runtime.GOOS)
	}

	// pkg-config (used by the Makefile to find GLFW/Vulkan flags on Unix).
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("pkg-config"); err == nil {
			report("pkg-config", true, "")
		} else {
			report("pkg-config", false, "not on PATH — install via package manager")
		}
	}

	fmt.Fprintln(out)
	if failed == 0 {
		fmt.Fprintln(out, "All checks passed.")
		return 0
	}
	fmt.Fprintf(out, "%d check(s) failed.\n", failed)
	return 1
}
