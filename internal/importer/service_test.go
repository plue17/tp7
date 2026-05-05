package importer

import (
	"testing"

	"github.com/plue17/tp7/internal/storage"
)

func TestFilterMarked_NoLibrary(t *testing.T) {
	svc := &Service{}
	got := svc.filterMarked(sampleEntries)
	if len(got) != len(sampleEntries) {
		t.Errorf("expected %d entries, got %d", len(sampleEntries), len(got))
	}
}

func TestFilterMarked_NoneMarked(t *testing.T) {
	lib, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	svc := &Service{Library: lib}

	got := svc.filterMarked(sampleEntries)
	if len(got) != len(sampleEntries) {
		t.Errorf("expected %d entries, got %d", len(sampleEntries), len(got))
	}
}

func TestFilterMarked_AllMarked(t *testing.T) {
	lib, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, e := range sampleEntries {
		if err := lib.MarkFile(e.Name, storage.ActionIgnore, "", "", 0); err != nil {
			t.Fatalf("MarkFile(%q): %v", e.Name, err)
		}
	}

	svc := &Service{Library: lib}
	got := svc.filterMarked(sampleEntries)
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
}

func TestFilterMarked_SomeMarked(t *testing.T) {
	lib, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// mark the first 3 entries
	marked := sampleEntries[:3]
	for _, e := range marked {
		if err := lib.MarkFile(e.Name, storage.ActionIgnore, "", "", 0); err != nil {
			t.Fatalf("MarkFile(%q): %v", e.Name, err)
		}
	}

	svc := &Service{Library: lib}
	got := svc.filterMarked(sampleEntries)

	want := len(sampleEntries) - len(marked)
	if len(got) != want {
		t.Errorf("expected %d entries, got %d", want, len(got))
	}

	// verify none of the marked names appear in the result
	markedNames := make(map[string]bool)
	for _, e := range marked {
		markedNames[e.Name] = true
	}
	for _, e := range got {
		if markedNames[e.Name] {
			t.Errorf("marked entry %q appeared in result", e.Name)
		}
	}
}
