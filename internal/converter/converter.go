// Package converter converts WAV files to MP3 using ffmpeg.
package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsAvailable reports whether ffmpeg is found on PATH.
func IsAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ConvertWAVToMP3 converts the WAV file at src to an MP3 in the same
// directory (same name, .mp3 extension) using ffmpeg with VBR quality 2.
// It is a no-op when:
//   - src does not have a .wav extension (case-insensitive)
//   - the target .mp3 already exists
//
// Returns an error when ffmpeg is not available or the conversion fails.
func ConvertWAVToMP3(src string) error {
	if !strings.EqualFold(filepath.Ext(src), ".wav") {
		return nil
	}
	dst := src[:len(src)-len(filepath.Ext(src))] + ".mp3"
	if _, err := os.Stat(dst); err == nil {
		return nil // already exists
	}
	// #nosec G204 — src is a library-internal path, not user-supplied shell input.
	cmd := exec.Command("ffmpeg", "-i", src, "-codec:a", "libmp3lame", "-qscale:a", "2", "-y", dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, out)
	}
	return nil
}
