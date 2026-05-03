package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"tp7/internal/config"
	"tp7/internal/importer"
)

func main() {
	defaultConfig := "tp7.yaml"
	if exe, err := os.Executable(); err == nil {
		defaultConfig = filepath.Join(filepath.Dir(exe), "tp7.yaml")
	}

	configPath := flag.String("c", defaultConfig, "path to configuration file (YAML)")
	debug := flag.Bool("d", false, "enable debug logging")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	svc := &importer.Service{
		VendorID:  cfg.MTPDevice.VendorID,
		ProductID: cfg.MTPDevice.ProductID,
		Debug:     *debug,
	}

	svc.Start()
	fmt.Println("service started, press Ctrl+C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("shutting down...")
	svc.Stop()
}
