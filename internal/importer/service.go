package importer

import (
	"context"
	"log/slog"
	"time"
)

// State represents the current state of the Service.
type State int

const (
	StateSearching State = iota // no device connected, polling for it
	StateConnected              // device found and open
)

// Service polls for an MTP device and tracks its connection state.
// Use Start to launch the background goroutine and Stop to shut it down.
type Service struct {
	// VendorID and ProductID identify the target USB device (hex strings,
	// e.g. "2367" and "0019").
	VendorID  string
	ProductID string

	// PollInterval controls how often the service checks for the device.
	// Defaults to 5 seconds if zero.
	PollInterval time.Duration

	// Debug enables verbose debug logging via slog.
	Debug bool

	cancel context.CancelFunc
	done   chan struct{}
}

// Start launches the background goroutine. It returns immediately.
// Calling Start on an already running Service is a no-op.
func (s *Service) Start() {
	if s.cancel != nil {
		return
	}

	if s.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	interval := s.PollInterval
	if interval == 0 {
		interval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})

	go s.run(ctx, interval)
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (s *Service) Stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel = nil
}

func (s *Service) run(ctx context.Context, interval time.Duration) {
	defer close(s.done)

	var dev *DeviceMount
	state := StateSearching

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			switch state {
			case StateSearching:
				found, err := FindDeviceMount(s.VendorID, s.ProductID)
				if err != nil {
					slog.Debug("importer: searching for device", "err", err)
					continue
				}
				dev = found
				state = StateConnected
				slog.Info("importer: device mounted", "path", dev.MountPath)

			case StateConnected:
				exists := dev.Exists()
				slog.Debug("importer: connection check", "path", dev.MountPath, "exists", exists)
				if !exists {
					slog.Info("importer: device unmounted", "path", dev.MountPath)
					dev = nil
					state = StateSearching
				}
			}
		}
	}
}
