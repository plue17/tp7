package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"tp7/internal/config"
	"tp7/internal/importer"
	"tp7/internal/storage"
)

func main() {
	defaultConfig := "tp7.yaml"
	if exe, err := os.Executable(); err == nil {
		defaultConfig = filepath.Join(filepath.Dir(exe), "tp7.yaml")
	}

	configPath := flag.String("c", defaultConfig, "path to configuration file (YAML)")
	debug := flag.Bool("d", false, "enable debug logging")
	flag.Parse()

	// explicit=true when -c was provided by the user.
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "c" {
			explicit = true
		}
	})

	cfg, err := config.Load(*configPath, explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var lib *storage.Library
	if cfg.Library.Path != "" {
		var err error
		lib, err = storage.Open(cfg.Library.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	entriesCh := make(chan []importer.Entry, 1)

	svc := &importer.Service{
		VendorID:     cfg.MTPDevice.VendorID,
		ProductID:    cfg.MTPDevice.ProductID,
		Debug:        *debug,
		Library:      lib,
		RecordingsCh: entriesCh,
	}

	p := tea.NewProgram(newRootModel(lib, entriesCh), tea.WithAltScreen())

	// Forward OS signals to the bubbletea program so Ctrl+C / SIGTERM quit cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		p.Quit()
	}()

	svc.Start()
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	svc.Stop()
}
