// Package playback manages audio playback of WAV files using the beep library.
// Only one file plays at a time; starting a new file stops the previous one.
package playback

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/wav"
)

// ErrStopped is delivered on the done channel when Stop was called (or when a
// new Play preempted a previous one).
var ErrStopped = errors.New("playback stopped")

// The speaker is initialized exactly once with the sample rate of the first
// file played. Subsequent files with a different rate are resampled.
var (
	speakerOnce sync.Once
	speakerRate beep.SampleRate
	speakerErr  error
)

func initSpeaker(rate beep.SampleRate) error {
	speakerOnce.Do(func() {
		speakerRate = rate
		speakerErr = speaker.Init(rate, rate.N(100*time.Millisecond))
	})
	return speakerErr
}

// playSession represents a single active playback session.
type playSession struct {
	done       chan error
	ctrl       *beep.Ctrl
	streamer   beep.StreamSeekCloser
	file       *os.File
	sampleRate beep.SampleRate
	once       sync.Once
}

// finish delivers err on the done channel exactly once, then closes resources.
func (s *playSession) finish(err error) {
	s.once.Do(func() {
		s.done <- err
		close(s.done)
		_ = s.streamer.Close()
		_ = s.file.Close()
	})
}

// Player manages playback of a single audio file at a time.
// It is safe for concurrent use.
type Player struct {
	mu   sync.Mutex
	sess *playSession
	path string
}

// Play starts playing the WAV file at path. Any currently playing file is
// stopped first (its done channel receives ErrStopped).
//
// The returned channel is buffered (capacity 1). It receives:
//   - nil        – playback reached end-of-file naturally
//   - ErrStopped – Stop was called or a new Play preempted this one
//   - another error – decoding or speaker initialisation failed
//
// The channel is closed after the value is sent.
func (p *Player) Play(path string) (<-chan error, error) {
	// Preempt any current session.
	p.mu.Lock()
	prev := p.sess
	p.sess = nil
	p.path = ""
	p.mu.Unlock()

	if prev != nil {
		speaker.Clear()
		prev.finish(ErrStopped)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", path, err)
	}

	streamer, format, err := wav.Decode(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("decoding %q: %w", path, err)
	}

	if err := initSpeaker(format.SampleRate); err != nil {
		_ = streamer.Close()
		_ = f.Close()
		return nil, fmt.Errorf("initialising speaker: %w", err)
	}

	// Resample if the file's rate differs from the speaker's rate.
	var baseStreamer beep.Streamer = streamer
	if format.SampleRate != speakerRate {
		baseStreamer = beep.Resample(4, format.SampleRate, speakerRate, streamer)
	}
	ctrl := &beep.Ctrl{Streamer: baseStreamer}

	sess := &playSession{
		done:       make(chan error, 1),
		ctrl:       ctrl,
		streamer:   streamer,
		file:       f,
		sampleRate: format.SampleRate,
	}

	p.mu.Lock()
	p.sess = sess
	p.path = path
	p.mu.Unlock()

	speaker.Play(beep.Seq(ctrl, beep.Callback(func() {
		p.mu.Lock()
		isCurrent := p.sess == sess
		if isCurrent {
			p.sess = nil
			p.path = ""
		}
		p.mu.Unlock()

		if isCurrent {
			sess.finish(nil)
		}
		// If not current, Stop() already called finish(ErrStopped).
	})))

	return sess.done, nil
}

// Stop terminates any ongoing playback immediately.
// If nothing is playing, Stop is a no-op.
func (p *Player) Stop() {
	p.mu.Lock()
	sess := p.sess
	p.sess = nil
	p.path = ""
	p.mu.Unlock()

	if sess != nil {
		speaker.Clear()
		sess.finish(ErrStopped)
	}
}

// Seek moves the playback position by offset (positive = forward, negative = backward).
// It is a no-op when nothing is playing. Clamps to [0, end-of-file].
func (p *Player) Seek(offset time.Duration) {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return
	}
	speaker.Lock()
	delta := sess.sampleRate.N(offset)
	newPos := sess.streamer.Position() + delta
	if newPos < 0 {
		newPos = 0
	}
	if l := sess.streamer.Len(); newPos > l {
		newPos = l
	}
	_ = sess.streamer.Seek(newPos)
	speaker.Unlock()
}

// Pause suspends audio output without losing the playback position.
// It is a no-op when nothing is playing or already paused.
func (p *Player) Pause() {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return
	}
	speaker.Lock()
	sess.ctrl.Paused = true
	speaker.Unlock()
}

// Resume continues paused playback. It is a no-op when not paused.
func (p *Player) Resume() {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return
	}
	speaker.Lock()
	sess.ctrl.Paused = false
	speaker.Unlock()
}

// IsPaused reports whether playback is currently paused.
func (p *Player) IsPaused() bool {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return false
	}
	speaker.Lock()
	paused := sess.ctrl.Paused
	speaker.Unlock()
	return paused
}

// IsPlaying reports whether a file is currently playing.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sess != nil
}

// File returns the path of the currently playing file, or "" if idle.
func (p *Player) File() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.path
}

// Position returns the current playback position. Returns 0 when idle.
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return 0
	}
	speaker.Lock()
	pos := sess.sampleRate.D(sess.streamer.Position())
	speaker.Unlock()
	return pos
}

// Duration returns the total duration of the currently playing file.
// Returns 0 when idle.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return 0
	}
	speaker.Lock()
	dur := sess.sampleRate.D(sess.streamer.Len())
	speaker.Unlock()
	return dur
}
