package storage

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const markedDir = ".marked"
const tagsFile = ".tags.yaml"

// ParseFilenameTime extracts the recording timestamp from a filename of the
// form YYYY-MM-DD_HHMMSS_NNN.ext. Returns the zero Time and false on failure.
func ParseFilenameTime(filename string) (time.Time, bool) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	parts := strings.SplitN(base, "_", 3)
	if len(parts) < 2 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02_150405", parts[0]+"_"+parts[1])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// DefaultDisplayName parses the recording timestamp encoded in the filename
// (format YYYY-MM-DD_HHMMSS_NNN.ext) and returns it as "YYYY-MM-DD HH:MM:SS".
// Returns an empty string if the filename does not match the expected pattern.
func DefaultDisplayName(filename string) string {
	t, ok := ParseFilenameTime(filename)
	if !ok {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// WavDuration reads the audio duration from a WAV file by parsing the RIFF/fmt/data
// chunks. Returns an error for non-WAV files or malformed headers.
func WavDuration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Verify the RIFF/WAVE header (12 bytes).
	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0, fmt.Errorf("reading RIFF header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, fmt.Errorf("%q: not a WAV file", path)
	}

	var byteRate uint32
	var dataSize uint32
	foundData := false

	for !foundData {
		var id [4]byte
		var size uint32
		if _, err := io.ReadFull(f, id[:]); err != nil {
			return 0, fmt.Errorf("reading chunk id: %w", err)
		}
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			return 0, fmt.Errorf("reading chunk size: %w", err)
		}
		switch string(id[:]) {
		case "fmt ":
			if size < 16 {
				return 0, fmt.Errorf("fmt chunk too small (%d bytes)", size)
			}
			var buf [16]byte
			if _, err := io.ReadFull(f, buf[:]); err != nil {
				return 0, fmt.Errorf("reading fmt chunk: %w", err)
			}
			byteRate = binary.LittleEndian.Uint32(buf[8:12])
			if extra := int64(size) - 16; extra > 0 {
				if _, err := f.Seek(extra, io.SeekCurrent); err != nil {
					return 0, fmt.Errorf("seeking past fmt extras: %w", err)
				}
			}
		case "data":
			dataSize = size
			foundData = true
		default:
			if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
				return 0, fmt.Errorf("seeking past chunk %q: %w", id, err)
			}
		}
	}
	if byteRate == 0 {
		return 0, fmt.Errorf("%q: WAV byte rate is zero", path)
	}
	return time.Duration(float64(dataSize) / float64(byteRate) * float64(time.Second)), nil
}

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
	// DurationSecs holds the audio duration in seconds (WAV files only).
	DurationSecs float64 `yaml:"duration_secs,omitempty"`
	// MP3Name holds the filename of the converted MP3 (same directory, base name only).
	MP3Name string `yaml:"mp3_name,omitempty"`
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
	sort.Slice(files, func(i, j int) bool { return files[i] > files[j] })
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

	if err := l.MarkFile(filename, ActionCopied, topicName, srcPath, 0); err != nil {
		return err
	}
	l.storeDuration(filename, dst)
	return nil
}

// StoreMP3Name records the converted MP3 filename in the mark file for filename.
// Errors are silently ignored — this is best-effort metadata.
func (l *Library) StoreMP3Name(filename, mp3Name string) {
	m, err := l.ReadMark(filename)
	if err != nil || m == nil {
		slog.Debug("storage: StoreMP3Name: could not read mark", "filename", filename, "err", err)
		return
	}
	m.MP3Name = mp3Name
	if data, err := yaml.Marshal(m); err == nil {
		if writeErr := os.WriteFile(l.markFilePath(filename), data, 0o644); writeErr != nil {
			slog.Debug("storage: StoreMP3Name: could not write mark", "filename", filename, "err", writeErr)
		}
	} else {
		slog.Debug("storage: StoreMP3Name: could not marshal mark", "filename", filename, "err", err)
	}
}

// storeDuration tries to read the WAV duration for path and write it to the mark file.
// Errors are silently ignored — duration is best-effort.
func (l *Library) storeDuration(filename, wavPath string) {
	dur, err := WavDuration(wavPath)
	if err != nil {
		slog.Debug("storage: storeDuration: could not read WAV duration", "path", wavPath, "err", err)
		return
	}
	if dur <= 0 {
		return
	}
	m, err := l.ReadMark(filename)
	if err != nil || m == nil {
		slog.Debug("storage: storeDuration: could not read mark", "filename", filename, "err", err)
		return
	}
	m.DurationSecs = dur.Seconds()
	if data, err := yaml.Marshal(m); err == nil {
		if writeErr := os.WriteFile(l.markFilePath(filename), data, 0o644); writeErr != nil {
			slog.Debug("storage: storeDuration: could not write mark", "filename", filename, "err", writeErr)
		}
	} else {
		slog.Debug("storage: storeDuration: could not marshal mark", "filename", filename, "err", err)
	}
}

// MarkFile records a decision for a voice memo file.
// For ActionCopied, topic must be the topic name; for ActionIgnore it is ignored.
// sourcePath may be set to the original device path when action is ActionIgnore.
// Any existing DisplayName is preserved.
func (l *Library) MarkFile(filename string, action MarkAction, topic, sourcePath string, size int64) error {
	if filename == "" {
		return fmt.Errorf("filename must not be empty")
	}
	// Preserve existing display name and duration from any prior mark.
	var displayName string
	var durationSecs float64
	if existing, err := l.ReadMark(filename); err == nil && existing != nil {
		displayName = existing.DisplayName
		durationSecs = existing.DurationSecs
	}
	if displayName == "" {
		displayName = DefaultDisplayName(filename)
	}
	m := Mark{Action: action, SourcePath: sourcePath, Size: size, DisplayName: displayName, DurationSecs: durationSecs}
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
			slog.Warn("storage: ListIgnored: could not read mark", "filename", original, "err", err)
			continue
		}
		if m.Action == ActionIgnore {
			result = append(result, IgnoredEntry{Name: original, SourcePath: m.SourcePath, Size: m.Size, DisplayName: m.DisplayName})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name > result[j].Name })
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
// For MP3 files the display name is looked up from the corresponding WAV mark.
func (l *Library) LoadDisplayNames(filenames []string) map[string]string {
	dn := make(map[string]string)
	for _, f := range filenames {
		markName := f
		// For MP3 files look up the WAV mark (same base name, .wav extension).
		if strings.EqualFold(filepath.Ext(f), ".mp3") {
			markName = f[:len(f)-len(filepath.Ext(f))] + ".wav"
		}
		m, err := l.ReadMark(markName)
		if err == nil && m != nil && m.DisplayName != "" {
			dn[f] = m.DisplayName
		}
	}
	return dn
}

// LoadDurations returns a map of filename -> audio duration for the given topic files.
// If a mark does not yet contain a duration, it attempts to read it from the WAV file
// on disk and updates the mark in place (best-effort).
// For MP3 files the duration is looked up from the corresponding WAV mark (same base name).
func (l *Library) LoadDurations(topicName string, filenames []string) map[string]time.Duration {
	result := make(map[string]time.Duration)
	for _, f := range filenames {
		markName := f
		// For MP3 files look up the WAV mark (same base name, .wav extension).
		if strings.EqualFold(filepath.Ext(f), ".mp3") {
			markName = f[:len(f)-len(filepath.Ext(f))] + ".wav"
		}
		m, err := l.ReadMark(markName)
		if err != nil || m == nil {
			continue
		}
		if m.DurationSecs > 0 {
			result[f] = time.Duration(m.DurationSecs * float64(time.Second))
			continue
		}
		// Not yet stored — read from the WAV file and persist.
		wavPath := filepath.Join(l.Path, topicName, markName)
		dur, err := WavDuration(wavPath)
		if err != nil || dur <= 0 {
			continue
		}
		result[f] = dur
		m.DurationSecs = dur.Seconds()
		if data, err := yaml.Marshal(m); err == nil {
			_ = os.WriteFile(l.markFilePath(markName), data, 0o644)
		}
	}
	return result
}

// UnmarkFile removes the decision record for a voice memo file.
// If no record exists the call is a no-op.
func (l *Library) UnmarkFile(filename string) error {
	if err := os.Remove(l.markFilePath(filename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing mark for %q: %w", filename, err)
	}
	return nil
}

// RenameTopic renames a topic directory on disk and updates every mark file
// that was recorded with the old topic name (ActionCopied entries).
func (l *Library) RenameTopic(oldName, newName string) error {
	if oldName == "" || newName == "" {
		return fmt.Errorf("topic names must not be empty")
	}
	oldPath := filepath.Join(l.Path, oldName)
	newPath := filepath.Join(l.Path, newName)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("topic %q already exists", newName)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming topic %q to %q: %w", oldName, newName, err)
	}
	// Update every mark file that still references the old topic name.
	marksDir := filepath.Join(l.Path, markedDir)
	entries, err := os.ReadDir(marksDir)
	if err != nil {
		return nil // non-fatal; directory rename already succeeded
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		filename := strings.TrimSuffix(e.Name(), ".yaml")
		m, err := l.ReadMark(filename)
		if err != nil || m == nil {
			continue
		}
		if m.Action == ActionCopied && m.Topic == oldName {
			m.Topic = newName
			data, err := yaml.Marshal(m)
			if err != nil {
				continue
			}
			_ = os.WriteFile(l.markFilePath(filename), data, 0o644)
		}
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

// DeleteTopic removes a topic directory and all files in it, and clears the
// mark for every file that was stored inside the topic.
func (l *Library) DeleteTopic(name string) error {
	// List files before removal so we can unmark them afterwards.
	files, _ := l.TopicFiles(name)
	topicPath := filepath.Join(l.Path, name)
	if err := os.RemoveAll(topicPath); err != nil {
		return fmt.Errorf("deleting topic %q: %w", name, err)
	}
	for _, f := range files {
		_ = l.UnmarkFile(f)
	}
	return nil
}

// RemoveFromTopic deletes a file from a topic's directory.
func (l *Library) RemoveFromTopic(topicName, filename string) error {
	path := filepath.Join(l.Path, topicName, filename)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing %q from topic %q: %w", filename, topicName, err)
	}
	return nil
}

// HasTranscript reports whether a .txt transcript file exists alongside the
// given audio file in the topic directory.
func (l *Library) HasTranscript(topicName, filename string) bool {
	base := filename[:len(filename)-len(filepath.Ext(filename))]
	txtPath := filepath.Join(l.Path, topicName, base+".txt")
	_, err := os.Stat(txtPath)
	return err == nil
}

// WriteTranscript writes text as a .txt file next to the audio file in the topic.
func (l *Library) WriteTranscript(topicName, filename, text string) error {
	base := filename[:len(filename)-len(filepath.Ext(filename))]
	txtPath := filepath.Join(l.Path, topicName, base+".txt")
	return os.WriteFile(txtPath, []byte(text), 0o644)
}

// TagMode describes how a tag keyword is matched against transcript text.
type TagMode string

const (
	// TagModeWord matches the keyword only as a complete word (uses \b word boundaries).
	TagModeWord TagMode = "word"
	// TagModeContains matches the keyword as a substring anywhere in the text.
	TagModeContains TagMode = "contains"
)

// Tag is a library-wide keyword with an associated match mode and display color.
// Color is 0 for the default color, or 1–9 for a user-selected palette color.
type Tag struct {
	Keyword string  `yaml:"keyword"`
	Mode    TagMode `yaml:"mode"`
	Color   int     `yaml:"color,omitempty"`
}

// tagsFilePath returns the path to the central tags file for the library.
func (l *Library) tagsFilePath() string {
	return filepath.Join(l.Path, tagsFile)
}

// GetGlobalTags reads the library-wide list of tags.
// Returns nil when no tags have been defined yet.
func (l *Library) GetGlobalTags() ([]Tag, error) {
	data, err := os.ReadFile(l.tagsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading tags: %w", err)
	}
	var tags []Tag
	if err := yaml.Unmarshal(data, &tags); err != nil {
		return nil, fmt.Errorf("parsing tags: %w", err)
	}
	return tags, nil
}

// SetGlobalTags writes the library-wide list of tags.
// An empty or nil slice removes the tags file entirely.
func (l *Library) SetGlobalTags(tags []Tag) error {
	if len(tags) == 0 {
		if err := os.Remove(l.tagsFilePath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing tags file: %w", err)
		}
		return nil
	}
	data, err := yaml.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshalling tags: %w", err)
	}
	return os.WriteFile(l.tagsFilePath(), data, 0o644)
}
