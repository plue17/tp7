package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const markedDir = ".marked"

// MarkAction describes what was done with a voice memo.
type MarkAction string

const (
	ActionIgnore MarkAction = "ignore"
	ActionCopied MarkAction = "copied"
)

// Mark is the content written to the YAML file inside .marked/.
type Mark struct {
	Action MarkAction `yaml:"action,omitempty"`
	// Topic is only set when Action == ActionCopied.
	Topic string `yaml:"topic,omitempty"`
	// SourcePath holds the original device path when Action == ActionIgnore.
	SourcePath string `yaml:"source_path,omitempty"`
	// Size holds the file size in bytes (stored for display purposes).
	Size int64 `yaml:"size,omitempty"`
	// DisplayName is an optional human-readable name preferred over the raw filename.
	DisplayName string `yaml:"display_name,omitempty"`
}

// Library is a collection of topics anchored to a directory on disk.
type Library struct {
	Path string
}

// Topic is a named section of a library, backed by a subdirectory.
// Its directory is always filepath.Join(library.Path, topic.Name).
type Topic struct {
	Name string
}

// Open opens the library rooted at path. If the directory does not yet exist
// it is created automatically. The hidden .marked subdirectory is also created.
func Open(path string) (*Library, error) {
	if err := os.MkdirAll(filepath.Join(path, markedDir), 0o755); err != nil {
		return nil, fmt.Errorf("opening library at %q: %w", path, err)
	}
	return &Library{Path: path}, nil
}

// CreateTopic creates a new topic with the given name inside the library.
// The topic is backed by a flat subdirectory of the library root.
func (l *Library) CreateTopic(name string) (*Topic, error) {
	if name == "" {
		return nil, fmt.Errorf("topic name must not be empty")
	}
	topicPath := filepath.Join(l.Path, name)
	if err := os.Mkdir(topicPath, 0o755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("topic %q already exists", name)
		}
		return nil, fmt.Errorf("creating topic %q: %w", name, err)
	}
	return &Topic{Name: name}, nil
}

// Topics returns all topics found in the library (one per subdirectory).
func (l *Library) Topics() ([]*Topic, error) {
	entries, err := os.ReadDir(l.Path)
	if err != nil {
		return nil, fmt.Errorf("reading library at %q: %w", l.Path, err)
	}
	var topics []*Topic
	for _, e := range entries {
		if e.IsDir() && e.Name() != markedDir {
			topics = append(topics, &Topic{Name: e.Name()})
		}
	}
	return topics, nil
}

// TopicFiles returns the names of all files inside the given topic's directory.
func (l *Library) TopicFiles(topicName string) ([]string, error) {
	dir := filepath.Join(l.Path, topicName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading topic %q: %w", topicName, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// FilePath returns the absolute path to a file inside a topic.
func (l *Library) FilePath(topicName, filename string) string {
	return filepath.Join(l.Path, topicName, filename)
}

// markFilePath returns the path to the YAML mark file for the given voice memo filename.
func (l *Library) markFilePath(filename string) string {
	return filepath.Join(l.Path, markedDir, filename+".yaml")
}

// CopyToTopic copies srcPath into the given topic directory, then marks the
// file as ActionCopied. The filename inside the topic is the base name of srcPath.
func (l *Library) CopyToTopic(topicName, srcPath string) error {
	filename := filepath.Base(srcPath)
	dst := filepath.Join(l.Path, topicName, filename)

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", srcPath, err)
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return fmt.Errorf("copying to topic %q: %w", topicName, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing destination: %w", err)
	}

	return l.MarkFile(filename, ActionCopied, topicName, srcPath, 0)
}

// MarkFile records a decision for a voice memo file.
// For ActionCopied, topic must be the topic name; for ActionIgnore it is ignored.
// sourcePath may be set to the original device path when action is ActionIgnore.
// Any existing DisplayName is preserved.
func (l *Library) MarkFile(filename string, action MarkAction, topic, sourcePath string, size int64) error {
	if filename == "" {
		return fmt.Errorf("filename must not be empty")
	}
	// Preserve an existing display name if one has been set.
	var displayName string
	if existing, err := l.ReadMark(filename); err == nil && existing != nil {
		displayName = existing.DisplayName
	}
	m := Mark{Action: action, SourcePath: sourcePath, Size: size, DisplayName: displayName}
	if action == ActionCopied {
		m.Topic = topic
	}
	data, err := yaml.Marshal(&m)
	if err != nil {
		return fmt.Errorf("marshalling mark for %q: %w", filename, err)
	}
	if err := os.WriteFile(l.markFilePath(filename), data, 0o644); err != nil {
		return fmt.Errorf("writing mark for %q: %w", filename, err)
	}
	return nil
}

// IsMarked reports whether a voice memo file has already been marked with a real action.
// A mark that only stores a display name (empty action) is not considered a real mark.
func (l *Library) IsMarked(filename string) (bool, error) {
	data, err := os.ReadFile(l.markFilePath(filename))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking mark for %q: %w", filename, err)
	}
	var m Mark
	if err := yaml.Unmarshal(data, &m); err != nil {
		return false, fmt.Errorf("parsing mark for %q: %w", filename, err)
	}
	return m.Action != "", nil
}

// ReadMark returns the stored Mark for a voice memo file.
func (l *Library) ReadMark(filename string) (*Mark, error) {
	data, err := os.ReadFile(l.markFilePath(filename))
	if err != nil {
		return nil, fmt.Errorf("reading mark for %q: %w", filename, err)
	}
	var m Mark
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing mark for %q: %w", filename, err)
	}
	return &m, nil
}

// IgnoredEntry holds the name, original device path, and size of an ignored recording.
type IgnoredEntry struct {
	Name        string
	SourcePath  string
	Size        int64
	DisplayName string
}

// ListIgnored returns all recordings that have been marked with ActionIgnore.
func (l *Library) ListIgnored() ([]IgnoredEntry, error) {
	entries, err := os.ReadDir(filepath.Join(l.Path, markedDir))
	if err != nil {
		return nil, fmt.Errorf("reading marks: %w", err)
	}
	var result []IgnoredEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		markName := e.Name()
		if filepath.Ext(markName) != ".yaml" {
			continue
		}
		original := strings.TrimSuffix(markName, ".yaml")
		m, err := l.ReadMark(original)
		if err != nil {
			continue
		}
		if m.Action == ActionIgnore {
			result = append(result, IgnoredEntry{Name: original, SourcePath: m.SourcePath, Size: m.Size, DisplayName: m.DisplayName})
		}
	}
	return result, nil
}

// RenameFile sets or clears the display name for a voice memo file.
// If no mark exists yet, a display-name-only record is created (action stays empty,
// so IsMarked still returns false and the file remains visible in tab 2).
func (l *Library) RenameFile(filename, displayName string) error {
	if filename == "" {
		return fmt.Errorf("filename must not be empty")
	}
	// Read existing mark (if any) to preserve action and other fields.
	var m Mark
	if existing, err := l.ReadMark(filename); err == nil && existing != nil {
		m = *existing
	}
	m.DisplayName = strings.TrimSpace(displayName)
	data, err := yaml.Marshal(&m)
	if err != nil {
		return fmt.Errorf("marshalling mark for %q: %w", filename, err)
	}
	if err := os.WriteFile(l.markFilePath(filename), data, 0o644); err != nil {
		return fmt.Errorf("writing mark for %q: %w", filename, err)
	}
	return nil
}

// LoadDisplayNames reads marks for the given filenames and returns a map of
// filename -> DisplayName for entries that have a non-empty display name.
func (l *Library) LoadDisplayNames(filenames []string) map[string]string {
	dn := make(map[string]string)
	for _, f := range filenames {
		m, err := l.ReadMark(f)
		if err == nil && m != nil && m.DisplayName != "" {
			dn[f] = m.DisplayName
		}
	}
	return dn
}

// UnmarkFile removes the decision record for a voice memo file.
// If no record exists the call is a no-op.
func (l *Library) UnmarkFile(filename string) error {
	if err := os.Remove(l.markFilePath(filename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing mark for %q: %w", filename, err)
	}
	return nil
}

// MoveToTopic moves a file from one topic directory to another and updates the mark.
// It uses os.Rename for an atomic move within the same filesystem, falling back to
// a copy-then-delete if the source and destination are on different filesystems.
func (l *Library) MoveToTopic(srcTopicName, dstTopicName, filename string) error {
	src := filepath.Join(l.Path, srcTopicName, filename)
	dst := filepath.Join(l.Path, dstTopicName, filename)

	if err := os.Rename(src, dst); err != nil {
		// Cross-device move: copy then remove.
		srcF, err2 := os.Open(src)
		if err2 != nil {
			return fmt.Errorf("moving %q: %w", filename, err)
		}
		defer srcF.Close()
		dstF, err2 := os.Create(dst)
		if err2 != nil {
			return fmt.Errorf("moving %q: %w", filename, err)
		}
		defer dstF.Close()
		if _, err2 = io.Copy(dstF, srcF); err2 != nil {
			return fmt.Errorf("moving %q: %w", filename, err)
		}
		if err2 = dstF.Close(); err2 != nil {
			return fmt.Errorf("moving %q: %w", filename, err)
		}
		if err2 = os.Remove(src); err2 != nil {
			return fmt.Errorf("moving %q: %w", filename, err)
		}
	}

	return l.MarkFile(filename, ActionCopied, dstTopicName, "", 0)
}

// RemoveFromTopic deletes a file from a topic's directory.
func (l *Library) RemoveFromTopic(topicName, filename string) error {
	path := filepath.Join(l.Path, topicName, filename)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing %q from topic %q: %w", filename, topicName, err)
	}
	return nil
}
