package storage

import (
	"fmt"
	"os"
	"path/filepath"

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
	Action MarkAction `yaml:"action"`
	// Topic is only set when Action == ActionCopied.
	Topic string `yaml:"topic,omitempty"`
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

// markFilePath returns the path to the YAML mark file for the given voice memo filename.
func (l *Library) markFilePath(filename string) string {
	return filepath.Join(l.Path, markedDir, filename+".yaml")
}

// MarkFile records a decision for a voice memo file.
// For ActionCopied, topic must be the topic name; for ActionIgnore it is ignored.
func (l *Library) MarkFile(filename string, action MarkAction, topic string) error {
	if filename == "" {
		return fmt.Errorf("filename must not be empty")
	}
	m := Mark{Action: action}
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

// IsMarked reports whether a voice memo file has already been marked.
func (l *Library) IsMarked(filename string) (bool, error) {
	_, err := os.Stat(l.markFilePath(filename))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking mark for %q: %w", filename, err)
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
