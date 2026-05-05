package playback_test

import (
	"testing"

	"tp7/internal/playback"
)

// TestStop_WhenIdle_placeholder keeps the package non-empty.
// Tests that require a WAV file have been removed (no test assets in CI).

func TestPlay_Stop(t *testing.T) {
	t.Skip("requires WAV test asset")
}

// TestStop_WhenIdle ensures Stop() is safe to call when nothing is playing.
func TestStop_WhenIdle(t *testing.T) {
	var p playback.Player
	p.Stop() // must not panic or block
}
