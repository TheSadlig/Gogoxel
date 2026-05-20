package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"

	"Gogoxel/internal/automation"
	"Gogoxel/internal/automation/grpcserver"
	"Gogoxel/internal/game"

	"google.golang.org/grpc"
)

func main() {
	automationMode := flag.Bool("automation", false, "run the engine in automation mode")
	listenAddress := flag.String("listen", "127.0.0.1:50051", "automation gRPC listen address")
	headless := flag.Bool("headless", false, "run automation without initializing GLFW or Vulkan")
	hiddenWindow := flag.Bool("hidden-window", true, "hide the window when automation mode initializes the renderer")
	tickRateHz := flag.Int("tick-rate", 60, "fixed automation simulation tick rate")
	artifactDir := flag.String("artifact-dir", "artifacts/automation", "directory for automation artifacts")
	flag.Parse()

	if *automationMode {
		if err := runAutomation(*listenAddress, automation.Options{
			Headless:     *headless,
			HiddenWindow: *hiddenWindow,
			TickRateHz:   *tickRateHz,
			ArtifactDir:  *artifactDir,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := game.New().Run(); err != nil {
		log.Fatal(err)
	}
}

func runAutomation(listenAddress string, options automation.Options) error {
	host := automation.NewHost(options)
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := grpc.NewServer()
	grpcserver.Register(server, host)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()

	log.Printf("automation gRPC listening on %s", listener.Addr())
	runErr := host.Run(context.Background())
	server.GracefulStop()
	serveErr := <-serverDone
	if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
		return serveErr
	}
	return runErr
}
