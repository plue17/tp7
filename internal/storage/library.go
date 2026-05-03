package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

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
// it is created automatically.
func Open(path string) (*Library, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
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
		if e.IsDir() {
			topics = append(topics, &Topic{Name: e.Name()})
		}
	}
	return topics, nil
}
