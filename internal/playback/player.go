// Package playback controls audio playback of a single file at a time using
// an external command (aplay). Only one file plays at a time; starting a new
// file stops the previous one.
//
// # Future: seeking
//
// Seek support (forward / backward) is planned but not yet implemented. The
// API is designed to accommodate it: Play returns a channel so the caller can
// react when playback ends, and the Player struct owns the process lifecycle.
package playback

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

// ErrStopped is delivered on the done channel when Stop was called (or when a
// new Play preempted a previous one).
var ErrStopped = errors.New("playback stopped")

// Player manages playback of a single audio file at a time.
// It is safe for concurrent use.
type Player struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	file       string
	stoppedPtr *bool // points into the active Play call's stack frame
}

// Play starts playing the audio file at path using aplay(1). Any currently
// playing file is stopped first (its done channel receives ErrStopped).
//
// The returned channel is buffered (capacity 1). It receives:
//   - nil   – playback reached end-of-file naturally
//   - [ErrStopped] – Stop was called or a new Play preempted this one
//   - another error – the player command failed
//
// The channel is closed after the value is sent.
func (p *Player) Play(path string) (<-chan error, error) {
	p.mu.Lock()
	p.killLocked()

	stopped := false
	cmd := exec.Command("aplay", path)
	p.cmd = cmd
	p.file = path
	p.stoppedPtr = &stopped
	p.mu.Unlock()

	if err := cmd.Start(); err != nil {
		p.mu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
			p.file = ""
			p.stoppedPtr = nil
		}
		p.mu.Unlock()
		return nil, fmt.Errorf("starting playback of %q: %w", path, err)
	}

	done := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()

		p.mu.Lock()
		wasStopped := stopped
		if p.cmd == cmd {
			p.cmd = nil
			p.file = ""
			p.stoppedPtr = nil
		}
		p.mu.Unlock()

		switch {
		case wasStopped:
			done <- ErrStopped
		case waitErr != nil:
			done <- waitErr
		default:
			done <- nil
		}
		close(done)
	}()

	return done, nil
}

// Stop terminates any ongoing playback immediately.
// If nothing is playing, Stop is a no-op.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killLocked()
}

// killLocked stops the current process. Must be called with p.mu held.
func (p *Player) killLocked() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if p.stoppedPtr != nil {
		*p.stoppedPtr = true
	}
	_ = p.cmd.Process.Kill()
}

// IsPlaying reports whether a file is currently playing.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil
}

// File returns the path of the currently playing file, or "" if idle.
func (p *Player) File() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.file
}

// Pid returns the OS process ID of the currently playing process, or 0 if idle.
func (p *Player) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
