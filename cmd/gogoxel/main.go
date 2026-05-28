package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"strings"

	"Gogoxel/internal/automation/grpcserver"
	"Gogoxel/internal/game"
	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/vulkan"

	"google.golang.org/grpc"
)

func main() {
	automationMode := flag.Bool("automation", false, "run the engine in automation mode")
	listenAddress := flag.String("listen", "127.0.0.1:50051", "automation gRPC listen address")
	liveListenAddress := flag.String("automation-listen", "", "serve automation gRPC while running the engine session")
	headless := flag.Bool("headless", false, "run automation without initializing GLFW or Vulkan")
	hiddenWindow := flag.Bool("hidden-window", false, "hide the window when the session initializes the renderer")
	tickRateHz := flag.Int("tick-rate", 60, "fixed automation simulation tick rate")
	artifactDir := flag.String("artifact-dir", "artifacts/automation", "directory for automation artifacts")
	chunkRoot := flag.String("chunk-root", "", "directory containing generated chunk maps for runtime loading")
	generateMap := flag.String("generate-map", "", "generate a named chunk map into --chunk-root and exit")
	generateChunkX := flag.Int("generate-chunk-x", 0, "chunk-space X center for --generate-map")
	generateChunkY := flag.Int("generate-chunk-y", 0, "chunk-space Y center for --generate-map")
	generateChunkRange := flag.Int("generate-chunk-range", 0, "chunk-space range for --generate-map")
	validate := flag.Bool("validate", false, "enable Vulkan validation layers (VK_LAYER_KHRONOS_validation + VK_EXT_debug_utils)")
	flag.Parse()

	if *validate || isTruthyEnv(os.Getenv("GOGOXEL_VALIDATE")) {
		vulkan.EnableValidationLayers()
	}

	if strings.TrimSpace(*generateMap) != "" {
		if strings.TrimSpace(*chunkRoot) == "" {
			log.Fatal("--chunk-root is required with --generate-map")
		}
		generator, ok := generators.LookupDefaultChunkGenerator(*generateMap)
		if !ok {
			log.Fatalf("unknown chunk generator %q", *generateMap)
		}
		request := generators.BuildRequest{ChunkX: *generateChunkX, ChunkY: *generateChunkY, ChunkRange: *generateChunkRange}.Normalized()
		mapDir := generators.GeneratedChunkMapDir(*chunkRoot, generator.Name())
		if err := generators.GenerateChunkMapWindow(mapDir, generator, request); err != nil {
			log.Fatal(err)
		}
		log.Printf("generated chunk map %q at %s for center=(%d,%d) range=%d", generator.Name(), mapDir, request.ChunkX, request.ChunkY, request.ChunkRange)
		return
	}

	if *automationMode {
		if err := runSession(*listenAddress, game.HostOptions{
			Headless:     *headless,
			HiddenWindow: *hiddenWindow,
			AutomationExposed: true,
			TickRateHz:   *tickRateHz,
			ArtifactDir:  *artifactDir,
			ChunkRoot:    *chunkRoot,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	if strings.TrimSpace(*liveListenAddress) == "" && (*headless || *hiddenWindow) {
		log.Fatal("headless and hidden-window session modes require --automation or --automation-listen")
	}

	if err := runSession(*liveListenAddress, game.HostOptions{
		Headless:     *headless,
		HiddenWindow: *hiddenWindow,
		Live:         true,
		AutomationExposed: strings.TrimSpace(*liveListenAddress) != "",
		TickRateHz:   *tickRateHz,
		ArtifactDir:  *artifactDir,
		ChunkRoot:    *chunkRoot,
	}); err != nil {
		log.Fatal(err)
	}
}

func runSession(listenAddress string, options game.HostOptions) error {
	host := game.NewHost(options)
	var (
		listener   net.Listener
		server     *grpc.Server
		serverDone chan error
	)
	if strings.TrimSpace(listenAddress) != "" {
		var err error
		listener, err = net.Listen("tcp", listenAddress)
		if err != nil {
			return err
		}
		defer listener.Close()

		server = grpc.NewServer()
		grpcserver.Register(server, host)
		serverDone = make(chan error, 1)
		go func() {
			serverDone <- server.Serve(listener)
		}()

		log.Printf("automation gRPC listening on %s", listener.Addr())
	}
	if !options.Headless && !options.HiddenWindow {
		log.Printf("%s", game.ManualControlsSummary())
	}
	runErr := host.Run(context.Background())
	if server != nil {
		server.GracefulStop()
		serveErr := <-serverDone
		if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			return serveErr
		}
	}
	return runErr
}

// isTruthyEnv returns true for "1", "true", "yes" (case-insensitive). Used
// to honor GOGOXEL_VALIDATE=1 / GOGOXEL_TRACE=1 style flags.
func isTruthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
