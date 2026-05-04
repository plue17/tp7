package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
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
	logFile := flag.String("l", "", "write log output to this file (use with -d for debug info)")
	flag.Parse()

	// explicit=true when -c was provided by the user.
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "c" {
			explicit = true
		}
	})

	// Set up file logging if -l was provided; otherwise discard all log output
	// so nothing leaks into the TUI.
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening log file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		level := slog.LevelInfo
		if *debug {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

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
