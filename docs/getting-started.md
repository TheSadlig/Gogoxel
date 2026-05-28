# Getting started

> Status: skeleton — issue #9 will flesh this out.

## Install

```
go install github.com/TheSadlig/Gogoxel/cmd/gogoxel@latest
```

## Verify your environment

```
gogoxel doctor
```

This prints the Go runtime version and confirms that `glslangValidator`,
the Vulkan loader, and `pkg-config` are reachable.

## Run a sample

```
go run ./examples/hello
```
