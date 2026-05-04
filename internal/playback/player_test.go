package playback_test

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"tp7/internal/playback"
)

// testWAV returns the absolute path to the shared test WAV file.
func testWAV(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile: .../internal/playback/player_test.go
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "tests", "material", "2026-04-27_001208_000.wav")
}

// TestPlay_Stop starts the player and stops it immediately.
// The done channel must receive ErrStopped.
func TestPlay_Stop(t *testing.T) {
	var p playback.Player
	done, err := p.Play(testWAV(t))
	if err != nil {
		t.Fatalf("Play returned error: %v", err)
	}

	p.Stop()

	select {
	case playErr := <-done:
		if !errors.Is(playErr, playback.ErrStopped) {
			t.Errorf("expected ErrStopped, got: %v", playErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for done after Stop()")
	}

	if p.IsPlaying() {
		t.Error("IsPlaying() should be false after Stop()")
	}
	if p.File() != "" {
		t.Errorf("File() should be empty after Stop(), got %q", p.File())
	}
}

// TestPlay_Toggle starts, stops, then starts again to confirm reuse.
func TestPlay_Toggle(t *testing.T) {
	var p playback.Player
	wav := testWAV(t)

	// First play → stop
	done1, err := p.Play(wav)
	if err != nil {
		t.Fatalf("first Play: %v", err)
	}
	p.Stop()
	if playErr := <-done1; !errors.Is(playErr, playback.ErrStopped) {
		t.Fatalf("first done: expected ErrStopped, got %v", playErr)
	}

	// Second play → stop (player must be reusable)
	done2, err := p.Play(wav)
	if err != nil {
		t.Fatalf("second Play: %v", err)
	}
	p.Stop()
	if playErr := <-done2; !errors.Is(playErr, playback.ErrStopped) {
		t.Fatalf("second done: expected ErrStopped, got %v", playErr)
	}
}

// TestPlay_Preempt starts a file and immediately starts another.
// The first done channel must receive ErrStopped; the second can be stopped normally.
func TestPlay_Preempt(t *testing.T) {
	var p playback.Player
	wav := testWAV(t)

	done1, err := p.Play(wav)
	if err != nil {
		t.Fatalf("first Play: %v", err)
	}

	// Start a second play before the first finishes – this preempts the first.
	done2, err := p.Play(wav)
	if err != nil {
		t.Fatalf("second Play: %v", err)
	}

	select {
	case playErr := <-done1:
		if !errors.Is(playErr, playback.ErrStopped) {
			t.Errorf("preempted play: expected ErrStopped, got %v", playErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for preempted done1")
	}

	p.Stop()
	select {
	case playErr := <-done2:
		if !errors.Is(playErr, playback.ErrStopped) {
			t.Errorf("second play after stop: expected ErrStopped, got %v", playErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for done2 after Stop()")
	}
}

// TestStop_WhenIdle ensures Stop() is safe to call when nothing is playing.
func TestStop_WhenIdle(t *testing.T) {
	var p playback.Player
	p.Stop() // must not panic or block
}

// TestPlay_StopAfter2s plays a WAV for 2 seconds, then stops it.
// After done is drained, IsPlaying must be false.
func TestPlay_StopAfter2s(t *testing.T) {
	var p playback.Player
	done, err := p.Play(testWAV(t))
	if err != nil {
		t.Fatalf("Play returned error: %v", err)
	}

	if !p.IsPlaying() {
		t.Fatal("IsPlaying() should be true immediately after Play")
	}

	// Let it play for 2 seconds, then stop.
	time.Sleep(2 * time.Second)
	p.Stop()

	select {
	case playErr := <-done:
		if !errors.Is(playErr, playback.ErrStopped) {
			t.Errorf("expected ErrStopped, got: %v", playErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for done after Stop()")
	}

	if p.IsPlaying() {
		t.Error("IsPlaying() should be false after Stop()")
	}
	if p.File() != "" {
		t.Errorf("File() should be empty after Stop(), got %q", p.File())
	}
}
