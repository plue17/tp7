package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_CreatesDirectoryIfMissing(t *testing.T) {
	dir := t.TempDir()
	newLib := filepath.Join(dir, "mylib")

	lib, err := Open(newLib)
	if err != nil {
		t.Fatalf("Open: unexpected error: %v", err)
	}
	if lib.Path != newLib {
		t.Errorf("Path = %q, want %q", lib.Path, newLib)
	}
	if _, err := os.Stat(newLib); os.IsNotExist(err) {
		t.Errorf("directory was not created")
	}
}

func TestOpen_ExistingDirectory(t *testing.T) {
	dir := t.TempDir()

	lib, err := Open(dir)
	if err != nil {
		t.Fatalf("Open on existing dir: unexpected error: %v", err)
	}
	if lib.Path != dir {
		t.Errorf("Path = %q, want %q", lib.Path, dir)
	}
}

func TestCreateTopic_Success(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	topic, err := lib.CreateTopic("photos")
	if err != nil {
		t.Fatalf("CreateTopic: unexpected error: %v", err)
	}
	if topic.Name != "photos" {
		t.Errorf("Name = %q, want %q", topic.Name, "photos")
	}
	if _, err := os.Stat(filepath.Join(lib.Path, "photos")); os.IsNotExist(err) {
		t.Errorf("topic directory was not created")
	}
}

func TestCreateTopic_EmptyName(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := lib.CreateTopic(""); err == nil {
		t.Error("expected error for empty topic name, got nil")
	}
}

func TestCreateTopic_Duplicate(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := lib.CreateTopic("music"); err != nil {
		t.Fatalf("first CreateTopic: %v", err)
	}
	if _, err := lib.CreateTopic("music"); err == nil {
		t.Error("expected error for duplicate topic, got nil")
	}
}

func TestOpen_CreatesMarkedDir(t *testing.T) {
	dir := t.TempDir()
	lib, err := Open(filepath.Join(dir, "lib"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	marked := filepath.Join(lib.Path, ".marked")
	if _, err := os.Stat(marked); os.IsNotExist(err) {
		t.Errorf(".marked directory was not created")
	}
}

func TestMarkFile_Ignore(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := lib.MarkFile("rec001.wav", ActionIgnore, ""); err != nil {
		t.Fatalf("MarkFile: %v", err)
	}

	marked, err := lib.IsMarked("rec001.wav")
	if err != nil {
		t.Fatalf("IsMarked: %v", err)
	}
	if !marked {
		t.Error("expected file to be marked")
	}

	m, err := lib.ReadMark("rec001.wav")
	if err != nil {
		t.Fatalf("ReadMark: %v", err)
	}
	if m.Action != ActionIgnore {
		t.Errorf("Action = %q, want %q", m.Action, ActionIgnore)
	}
	if m.Topic != "" {
		t.Errorf("Topic = %q, want empty", m.Topic)
	}
}

func TestMarkFile_Copied(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := lib.MarkFile("rec002.wav", ActionCopied, "music"); err != nil {
		t.Fatalf("MarkFile: %v", err)
	}

	m, err := lib.ReadMark("rec002.wav")
	if err != nil {
		t.Fatalf("ReadMark: %v", err)
	}
	if m.Action != ActionCopied {
		t.Errorf("Action = %q, want %q", m.Action, ActionCopied)
	}
	if m.Topic != "music" {
		t.Errorf("Topic = %q, want %q", m.Topic, "music")
	}
}

func TestIsMarked_NotMarked(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	marked, err := lib.IsMarked("unknown.wav")
	if err != nil {
		t.Fatalf("IsMarked: %v", err)
	}
	if marked {
		t.Error("expected file to not be marked")
	}
}

func TestMarkFile_EmptyFilename(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := lib.MarkFile("", ActionIgnore, ""); err == nil {
		t.Error("expected error for empty filename, got nil")
	}
}

func TestTopics_ExcludesMarkedDir(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := lib.CreateTopic("sounds"); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	topics, err := lib.Topics()
	if err != nil {
		t.Fatalf("Topics: %v", err)
	}
	for _, tp := range topics {
		if tp.Name == ".marked" {
			t.Error("Topics() must not include .marked")
		}
	}
	if len(topics) != 1 {
		t.Errorf("expected 1 topic, got %d", len(topics))
	}
}

func TestTopics_Empty(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	topics, err := lib.Topics()
	if err != nil {
		t.Fatalf("Topics: unexpected error: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("expected 0 topics, got %d", len(topics))
	}
}

func TestTopics_ListsSubdirectories(t *testing.T) {
	lib, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		if _, err := lib.CreateTopic(n); err != nil {
			t.Fatalf("CreateTopic(%q): %v", n, err)
		}
	}

	// also create a regular file — must not appear as a topic
	if err := os.WriteFile(filepath.Join(lib.Path, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	topics, err := lib.Topics()
	if err != nil {
		t.Fatalf("Topics: unexpected error: %v", err)
	}
	if len(topics) != len(names) {
		t.Fatalf("expected %d topics, got %d", len(names), len(topics))
	}

	got := make(map[string]bool, len(topics))
	for _, tp := range topics {
		got[tp.Name] = true
	}
	for _, n := range names {
		if !got[n] {
			t.Errorf("topic %q missing from Topics()", n)
		}
	}
}
