package transcript

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Segment is a single timed block of a transcript.
type Segment struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// Transcript holds all segments of a parsed transcript file.
type Transcript struct {
	Segments []Segment
}

// At returns the segment whose time range contains pos.
// If pos falls between two segments the next segment is returned.
// Returns nil when no segment covers or follows pos.
func (t *Transcript) At(pos time.Duration) *Segment {
	for i := range t.Segments {
		s := &t.Segments[i]
		if pos <= s.End {
			return s
		}
	}
	return nil
}

// ParseFile reads and parses a transcript file from disk.
func ParseFile(path string) (*Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening transcript %q: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads and parses a transcript from r.
// Each non-empty line must have the form:
//
//	[HH:MM:SS.mmm --> HH:MM:SS.mmm] Text…
func Parse(r io.Reader) (*Transcript, error) {
	var tr Transcript
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		seg, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		tr.Segments = append(tr.Segments, seg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript: %w", err)
	}
	return &tr, nil
}

// parseLine parses one transcript line of the form:
// [HH:MM:SS.mmm --> HH:MM:SS.mmm] Text…
func parseLine(line string) (Segment, error) {
	if len(line) == 0 || line[0] != '[' {
		return Segment{}, fmt.Errorf("expected line to start with '[', got %q", line)
	}
	end := strings.Index(line, "]")
	if end < 0 {
		return Segment{}, fmt.Errorf("missing closing ']' in %q", line)
	}
	timeRange := line[1:end]
	text := strings.TrimSpace(line[end+1:])

	parts := strings.SplitN(timeRange, " --> ", 2)
	if len(parts) != 2 {
		return Segment{}, fmt.Errorf("expected 'start --> end', got %q", timeRange)
	}
	start, err := parseDuration(strings.TrimSpace(parts[0]))
	if err != nil {
		return Segment{}, fmt.Errorf("parsing start time: %w", err)
	}
	endD, err := parseDuration(strings.TrimSpace(parts[1]))
	if err != nil {
		return Segment{}, fmt.Errorf("parsing end time: %w", err)
	}
	return Segment{Start: start, End: endD, Text: text}, nil
}

// parseDuration parses a timestamp of the form HH:MM:SS.mmm into a time.Duration.
func parseDuration(s string) (time.Duration, error) {
	// Expected: HH:MM:SS.mmm
	var h, m, sec, ms int
	n, err := fmt.Sscanf(s, "%d:%d:%d.%d", &h, &m, &sec, &ms)
	if err != nil || n != 4 {
		return 0, fmt.Errorf("invalid timestamp %q (expected HH:MM:SS.mmm)", s)
	}
	d := time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second +
		time.Duration(ms)*time.Millisecond
	return d, nil
}
