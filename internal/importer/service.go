package importer

import (
	"context"
	"log/slog"
	"time"

	"tp7/internal/storage"
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

	// Library is used to filter already-marked recordings from log output.
	// May be nil, in which case all recordings are shown.
	Library *storage.Library

	// RecordingsCh, if non-nil, receives the filtered list of new recordings
	// each time the device connects. Sends are non-blocking; use a buffered channel.
	RecordingsCh chan<- []Entry

	// DeviceStateCh, if non-nil, receives a State value whenever the device
	// connects (StateConnected) or disconnects (StateSearching).
	// Sends are non-blocking; use a buffered channel.
	DeviceStateCh chan<- State

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

// filterMarked returns only the entries that have not yet been marked in the library.
// If no library is configured, all entries are returned unchanged.
func (s *Service) filterMarked(entries []Entry) []Entry {
	if s.Library == nil {
		return entries
	}
	var result []Entry
	for _, e := range entries {
		marked, err := s.Library.IsMarked(e.Name)
		if err != nil {
			slog.Warn("importer: could not check mark", "name", e.Name, "err", err)
			continue
		}
		if !marked {
			result = append(result, e)
		}
	}
	return result
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
				slog.Debug("importer: device mounted", "path", dev.MountPath)
				slog.Debug("TP7 connected")
				if s.DeviceStateCh != nil {
					select {
					case s.DeviceStateCh <- StateConnected:
					default:
					}
				}
				if entries, err := dev.ListRecordings(); err != nil {
					slog.Warn("importer: could not list recordings", "err", err)
				} else {
					new := s.filterMarked(entries)
					slog.Debug("importer: recordings found", "total", len(entries), "new", len(new))
					for _, e := range new {
						slog.Debug("importer: new recording", "name", e.Name, "size", e.Size)
					}
					if s.RecordingsCh != nil {
						select {
						case s.RecordingsCh <- new:
						default:
						}
					}
				}

			case StateConnected:
				exists := dev.Exists()
				slog.Debug("importer: connection check", "path", dev.MountPath, "exists", exists)
				if !exists {
					slog.Debug("importer: device unmounted", "path", dev.MountPath)
					slog.Debug("TP7 disconnected")
					dev = nil
					state = StateSearching
					if s.DeviceStateCh != nil {
						select {
						case s.DeviceStateCh <- StateSearching:
						default:
						}
					}
				}
			}
		}
	}
}
