package transcript

import (
	"strings"
	"testing"
	"time"
)

const exampleInput = `[00:00:00.460 --> 00:00:11.420] Ich habe mich jetzt dazu entschlossen, dass zu dem OP-1-Field nehme ich das Digitakt 2 als Drum-Computer.
[00:00:12.740 --> 00:00:24.660] Dadurch entfällt dieses ganze Drum-Computer-Gedöns auf dem OP-1-Field,
[00:00:24.660 --> 00:00:34.660] weil dieses Kopieren der einzelnen Layers eines Drum-Kits, das erschien mir ein bisschen überkompliziert.
[00:00:35.680 --> 00:00:37.460] Das macht jetzt das Digitakt 2.
`

func TestParse(t *testing.T) {
	tr, err := Parse(strings.NewReader(exampleInput))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(tr.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(tr.Segments))
	}

	first := tr.Segments[0]
	if first.Start != 460*time.Millisecond {
		t.Errorf("first.Start: got %v, want %v", first.Start, 460*time.Millisecond)
	}
	if first.End != 11420*time.Millisecond {
		t.Errorf("first.End: got %v, want %v", first.End, 11420*time.Millisecond)
	}
	if first.Text == "" {
		t.Error("first.Text must not be empty")
	}
}

func TestAt(t *testing.T) {
	tr, _ := Parse(strings.NewReader(exampleInput))

	tests := []struct {
		pos      time.Duration
		wantNil  bool
		wantText string
	}{
		{0, false, "Ich habe mich jetzt"}, // before first segment start → returned (pos <= End)
		{5 * time.Second, false, "Ich habe mich jetzt"},
		{12*time.Second + 740*time.Millisecond, false, "Dadurch entfällt"},
		{35*time.Second + 680*time.Millisecond, false, "Das macht jetzt"},
		{60 * time.Second, true, ""}, // after last segment end
	}

	for _, tc := range tests {
		seg := tr.At(tc.pos)
		if tc.wantNil {
			if seg != nil {
				t.Errorf("At(%v): expected nil, got %q", tc.pos, seg.Text)
			}
			continue
		}
		if seg == nil {
			t.Errorf("At(%v): expected segment, got nil", tc.pos)
			continue
		}
		if !strings.Contains(seg.Text, tc.wantText) {
			t.Errorf("At(%v): text %q does not contain %q", tc.pos, seg.Text, tc.wantText)
		}
	}
}

func TestAtExactTime(t *testing.T) {
	tr, _ := Parse(strings.NewReader(exampleInput))

	// "Gebe mir das Gesprochene zum Zeitpunkt 00:00:28"
	pos := 28 * time.Second
	seg := tr.At(pos)
	if seg == nil {
		t.Fatal("At(28s): expected a segment, got nil")
	}
	want := "Kopieren der einzelnen Layers"
	if !strings.Contains(seg.Text, want) {
		t.Errorf("At(28s): got %q, want it to contain %q", seg.Text, want)
	}
}
