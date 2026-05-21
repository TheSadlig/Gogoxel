package platform

import (
	"errors"
	"fmt"

	"github.com/go-gl/glfw/v3.3/glfw"
	vk "github.com/vulkan-go/vulkan"
)

type Window struct {
	handle *glfw.Window
}

type WindowOptions struct {
	Visible bool
}

func NewWindow(title string, width, height int) (*Window, error) {
	return NewWindowWithOptions(title, width, height, WindowOptions{Visible: true})
}

func NewWindowWithOptions(title string, width, height int, options WindowOptions) (*Window, error) {
	if err := glfw.Init(); err != nil {
		return nil, fmt.Errorf("initializing GLFW: %w", err)
	}
	if !glfw.VulkanSupported() {
		glfw.Terminate()
		return nil, errors.New("GLFW reports no Vulkan support on this machine")
	}

	procAddr := glfw.GetVulkanGetInstanceProcAddress()
	if procAddr == nil {
		glfw.Terminate()
		return nil, errors.New("GLFW returned a nil Vulkan proc address")
	}
	vk.SetGetInstanceProcAddr(procAddr)
	if err := vk.Init(); err != nil {
		glfw.Terminate()
		return nil, fmt.Errorf("initializing Vulkan loader: %w", err)
	}

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.Resizable, glfw.False)
	if options.Visible {
		glfw.WindowHint(glfw.Visible, glfw.True)
	} else {
		glfw.WindowHint(glfw.Visible, glfw.False)
	}

	handle, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		glfw.Terminate()
		return nil, fmt.Errorf("creating window: %w", err)
	}

	return &Window{handle: handle}, nil
}

func (w *Window) RequiredInstanceExtensions() []string {
	if w.handle == nil {
		return nil
	}
	return w.handle.GetRequiredInstanceExtensions()
}

func (w *Window) PollEvents() {
	glfw.PollEvents()
}

func (w *Window) ShouldClose() bool {
	return w.handle == nil || w.handle.ShouldClose()
}

func (w *Window) IsIconified() bool {
	return w.handle != nil && w.handle.GetAttrib(glfw.Iconified) == 1
}

func (w *Window) SetTitle(title string) {
	if w.handle != nil {
		w.handle.SetTitle(title)
	}
}

func (w *Window) RequestClose() {
	if w.handle != nil {
		w.handle.SetShouldClose(true)
	}
}

func (w *Window) IsKeyDown(key glfw.Key) bool {
	return w.handle != nil && w.handle.GetKey(key) == glfw.Press
}

func (w *Window) IsMouseButtonDown(button glfw.MouseButton) bool {
	return w.handle != nil && w.handle.GetMouseButton(button) == glfw.Press
}

func (w *Window) CursorPosition() (float64, float64) {
	if w.handle == nil {
		return 0, 0
	}
	return w.handle.GetCursorPos()
}

func (w *Window) Size() (int, int) {
	if w.handle == nil {
		return 0, 0
	}
	return w.handle.GetSize()
}

func (w *Window) FramebufferSize() (int, int) {
	if w.handle == nil {
		return 0, 0
	}
	return w.handle.GetFramebufferSize()
}

func (w *Window) CreateSurface(instance vk.Instance) (vk.Surface, error) {
	if w.handle == nil {
		return vk.NullSurface, errors.New("window is not initialized")
	}

	surfacePtr, err := w.handle.CreateWindowSurface(instance, nil)
	if err != nil {
		return vk.NullSurface, fmt.Errorf("creating window surface: %w", err)
	}

	return vk.SurfaceFromPointer(surfacePtr), nil
}

func (w *Window) Close() {
	if w.handle != nil {
		w.handle.Destroy()
		w.handle = nil
	}
	glfw.Terminate()
}
