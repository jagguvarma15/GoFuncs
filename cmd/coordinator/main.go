package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jagguvarma15/GoFuncs/internal/coordinator"
)

func main() {
	var (
		configPath = flag.String("config", "config/coordinator.yaml", "Path to configuration file")
		address    = flag.String("address", "localhost", "Address to bind to")
		port       = flag.Int("port", 8080, "Port to bind to")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Println("Starting GoFuncs Coordinator in verbose mode")
	}

	// Create coordinator with configuration
	coord, err := coordinator.New(coordinator.Config{
		Address:    *address,
		Port:       *port,
		ConfigPath: *configPath,
		Verbose:    *verbose,
	})
	if err != nil {
		log.Fatalf("Failed to create coordinator: %v", err)
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start coordinator in a goroutine
	errChan := make(chan error, 1)
	go func() {
		log.Printf("Starting coordinator on %s:%d", *address, *port)
		if err := coord.Start(); err != nil {
			errChan <- fmt.Errorf("coordinator failed to start: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down gracefully...", sig)
	case err := <-errChan:
		log.Printf("Coordinator error: %v", err)
	}

	// Graceful shutdown
	if err := coord.Shutdown(); err != nil {
		log.Printf("Error during shutdown: %v", err)
		os.Exit(1)
	}

	log.Println("Coordinator stopped successfully")
}
