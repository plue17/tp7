package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/plue17/tp7/internal/converter"
	"github.com/plue17/tp7/internal/importer"
	"github.com/plue17/tp7/internal/playback"
	"github.com/plue17/tp7/internal/storage"
	"github.com/plue17/tp7/internal/transcriber"
	"github.com/plue17/tp7/internal/transcript"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleSelected  = lipgloss.NewStyle().Reverse(true)
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleFileName  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))  // bright white
	styleTopic     = lipgloss.NewStyle().Foreground(lipgloss.Color("166")) // dark orange
	styleTab       = lipgloss.NewStyle().Padding(0, 1)
	styleActiveTab = lipgloss.NewStyle().Padding(0, 1).Bold(true).Underline(true)
	styleDialog    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	styleDialogErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleMultiSel  = lipgloss.NewStyle().Bold(true)
	stylePlayIcon  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")) // bright green
	styleTag       = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))            // cyan
)

// ── tabs ──────────────────────────────────────────────────────────────────────

type tab int

const (
	tabLibrary tab = iota
	tabImport
	tabIgnored
)

// ── messages ──────────────────────────────────────────────────────────────────

type entriesMsg []importer.Entry

type deviceStateMsg struct {
	state importer.State
}

type playbackDoneMsg struct {
	path string
	err  error
}
type playbackTickMsg struct{}

type loadTopicsMsg struct {
	names []string
	err   error
}

type loadFilesMsg struct {
	topicName    string
	files        []string
	displayNames map[string]string
	durations    map[string]time.Duration
	transcripts  map[string]bool
	tags         map[string][]string
	err          error
}

type createTopicMsg struct {
	name string
	err  error
}

type importActionDoneMsg struct {
	entryName string
	topicName string // non-empty when action was a copy
	err       error
}

type importTopicsLoadedMsg struct {
	entries []importer.Entry
	topics  []string
	err     error
}

type loadIgnoredMsg struct {
	entries []storage.IgnoredEntry
	err     error
}

type ignoredTopicsLoadedMsg struct {
	entries    []storage.IgnoredEntry // batch mode; nil = single
	filename   string                 // single mode
	sourcePath string                 // single mode
	topics     []string
	err        error
}

type ignoredFileDoneMsg struct {
	filename   string
	sourcePath string // non-empty when action was unmark
	size       int64  // file size, set when action was unmark
	topicName  string // non-empty = was copied to a topic
	err        error
}

type libFileDoneMsg struct {
	topicName string
	fileName  string
	isIgnore  bool
	err       error
}

type batchImportDoneMsg struct {
	topicName string   // non-empty when batch copy
	removed   []string // entry names removed from 2
	errMsg    string
}

type batchIgnoredDoneMsg struct {
	topicName string                 // non-empty when copied to a topic
	removed   []storage.IgnoredEntry // entries removed from 3
	errMsg    string
}

type convScanDoneMsg struct{ count int }

type transcribeUploadedMsg struct {
	topicName string
	filename  string
	filePath  string
	jobID     string
	busy      bool
	err       error
}

type transcribePollMsg struct {
	topicName  string
	filename   string
	filePath   string
	jobID      string
	done       bool
	transcript string
	err        error
}

type transcriptWrittenMsg struct {
	topicName string
	filename  string
	err       error
}

type transcribeQueueItem struct {
	topicName string
	filename  string
	filePath  string
}

// ── rename dialog ─────────────────────────────────────────────────────────────

type renameDialog struct {
	active    bool
	filename  string // the file being renamed (raw filename on disk)
	topicName string // only set in tab 1 (library)
	input     string
	errMsg    string
}

type renameDoneMsg struct {
	filename    string
	displayName string
	err         error
}

// ── tag dialog ────────────────────────────────────────────────────────────────

// tagDialogMode tracks which sub-screen the global tag dialog is showing.
type tagDialogMode int

const (
	tagDialogView tagDialogMode = iota // list of global tag keywords
	tagDialogAdd                       // text input for new keyword
)

// tagDialog manages the library-wide tag keyword list.
// Tags are global: a voice memo is labelled with a tag when that keyword
// appears anywhere in its transcript.
type tagDialog struct {
	active bool
	tags   []string // working copy of the global keyword list
	cursor int
	mode   tagDialogMode
	input  string
	errMsg string
}

type globalTagsLoadedMsg struct {
	tags []string
	err  error
}

type setGlobalTagsDoneMsg struct {
	tags []string
	err  error
}

func loadGlobalTagsCmd(lib *storage.Library) tea.Cmd {
	return func() tea.Msg {
		tags, err := lib.GetGlobalTags()
		return globalTagsLoadedMsg{tags: tags, err: err}
	}
}

func setGlobalTagsCmd(lib *storage.Library, tags []string) tea.Cmd {
	return func() tea.Msg {
		err := lib.SetGlobalTags(tags)
		return setGlobalTagsDoneMsg{tags: tags, err: err}
	}
}

type importDisplayNamesMsg struct {
	names map[string]string
}

func renameFileCmd(lib *storage.Library, filename, displayName string) tea.Cmd {
	return func() tea.Msg {
		err := lib.RenameFile(filename, displayName)
		return renameDoneMsg{filename: filename, displayName: displayName, err: err}
	}
}

func loadImportDisplayNamesCmd(lib *storage.Library, names []string) tea.Cmd {
	return func() tea.Msg {
		return importDisplayNamesMsg{names: lib.LoadDisplayNames(names)}
	}
}

// timeGroup returns the group label for a recording timestamp relative to now.
// Groups (exclusive, first match wins):
//
//	Today, Yesterday, Last 7 Days, This Month, Last Month, This Year, Older
func timeGroup(t, now time.Time) string {
	y, m, d := now.Date()
	todayStart := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(todayStart):
		return "Today"
	case !t.Before(todayStart.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(todayStart.AddDate(0, 0, -7)):
		return "Last 7 Days"
	case t.Year() == y && t.Month() == m:
		return "This Month"
	case t.Year() == y && t.Month() == m-1 || (m == 1 && t.Year() == y-1 && t.Month() == 12):
		return "Last Month"
	case t.Year() == y:
		return "This Year"
	default:
		return "Older"
	}
}

// groupHeader renders a subtle section header for a time group.
func groupHeader(label string) string {
	return styleDim.Render("  ── "+label+" ──") + "\n"
}

// formatDuration formats a time.Duration as hh:mm:ss. Returns "" for zero/negative durations.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// formatLabel builds the display label for a voice memo.
// If displayName is a user-chosen name (different from the default timestamp),
// it returns "displayName (timestamp)". Otherwise just the timestamp, or the
// raw filename if no timestamp can be parsed.
func formatLabel(filename, displayName string) string {
	ts := storage.DefaultDisplayName(filename)
	if displayName == "" || displayName == ts {
		if ts != "" {
			return ts
		}
		return filename
	}
	if ts != "" {
		return displayName + " (" + ts + ")"
	}
	return displayName
}

// formatLabelAligned is like formatLabel but right-aligns the timestamp (and an
// optional suffix) within availWidth columns: name left-aligned, right portion
// flush to the right edge.  suffix (e.g. WAV duration "01:23:45") is shown two
// spaces after the timestamp.  Pass an empty string to suppress the suffix.
// Falls back to formatLabel when availWidth <= 0 or no timestamp can be parsed.
func formatLabelAligned(filename, displayName string, availWidth int, suffix string) string {
	ts := storage.DefaultDisplayName(filename)
	if availWidth <= 0 || ts == "" {
		label := formatLabel(filename, displayName)
		if suffix != "" {
			label += "  " + suffix
		}
		return label
	}
	right := ts
	if suffix != "" {
		right = ts + "  " + suffix
	}
	isUserName := displayName != "" && displayName != ts
	name := displayName
	if !isUserName {
		name = "no description"
	}
	gap := availWidth - lipgloss.Width(name) - lipgloss.Width(right)
	if gap < 1 {
		// Truncate name to make room for at least one space + right portion.
		name = string([]rune(name)[:max(0, availWidth-lipgloss.Width(right)-1)])
		gap = 1
	}
	return name + strings.Repeat(" ", gap) + right
}

// formatLabelAlignedStyled is like formatLabelAligned but renders the name in
// bright white and the right portion (timestamp + duration) in dim/gray.
func formatLabelAlignedStyled(filename, displayName string, availWidth int, suffix string) string {
	ts := storage.DefaultDisplayName(filename)
	if availWidth <= 0 || ts == "" {
		label := formatLabel(filename, displayName)
		if suffix != "" {
			return styleFileName.Render(label) + styleDim.Render("  "+suffix)
		}
		return styleFileName.Render(label)
	}
	right := ts
	if suffix != "" {
		right = ts + "  " + suffix
	}
	isUserName := displayName != "" && displayName != ts
	name := displayName
	if !isUserName {
		name = "no description"
	}
	gap := availWidth - lipgloss.Width(name) - lipgloss.Width(right)
	if gap < 1 {
		name = string([]rune(name)[:max(0, availWidth-lipgloss.Width(right)-1)])
		gap = 1
	}
	return styleFileName.Render(name) + strings.Repeat(" ", gap) + styleDim.Render(right)
}

// preferredName returns the display name from the map when present,
// then falls back to the timestamp parsed from the filename, then the raw filename.
func preferredName(filename string, displayNames map[string]string) string {
	return formatLabel(filename, displayNames[filename])
}

// formatLabelWithTags is like formatLabelAligned but inserts raw tag text
// (e.g. " [foo] [bar]") between the name and the right-aligned portion.
func formatLabelWithTags(filename, displayName string, tags []string, availWidth int, suffix string) string {
	ts := storage.DefaultDisplayName(filename)
	rawTags := ""
	for _, t := range tags {
		rawTags += " [" + t + "]"
	}
	if availWidth <= 0 || ts == "" {
		label := formatLabel(filename, displayName)
		if suffix != "" {
			return label + rawTags + "  " + suffix
		}
		return label + rawTags
	}
	right := ts
	if suffix != "" {
		right = ts + "  " + suffix
	}
	isUserName := displayName != "" && displayName != ts
	name := displayName
	if !isUserName {
		name = "no description"
	}
	gap := availWidth - lipgloss.Width(name) - lipgloss.Width(rawTags) - lipgloss.Width(right)
	if gap < 1 {
		available := availWidth - lipgloss.Width(rawTags) - lipgloss.Width(right) - 1
		if available < 0 {
			available = 0
		}
		name = string([]rune(name)[:max(0, available)])
		gap = 1
	}
	return name + rawTags + strings.Repeat(" ", gap) + right
}

// formatLabelWithTagsStyled is like formatLabelAlignedStyled but renders tags
// in cyan between the (white) name and the (dim) right-aligned portion.
func formatLabelWithTagsStyled(filename, displayName string, tags []string, availWidth int, suffix string) string {
	ts := storage.DefaultDisplayName(filename)
	rawTags := ""
	for _, t := range tags {
		rawTags += " [" + t + "]"
	}
	if availWidth <= 0 || ts == "" {
		label := formatLabel(filename, displayName)
		var sb strings.Builder
		sb.WriteString(styleFileName.Render(label))
		for _, t := range tags {
			sb.WriteString(" " + styleTag.Render("["+t+"]"))
		}
		if suffix != "" {
			sb.WriteString(styleDim.Render("  " + suffix))
		}
		return sb.String()
	}
	right := ts
	if suffix != "" {
		right = ts + "  " + suffix
	}
	isUserName := displayName != "" && displayName != ts
	name := displayName
	if !isUserName {
		name = "no description"
	}
	gap := availWidth - lipgloss.Width(name) - lipgloss.Width(rawTags) - lipgloss.Width(right)
	if gap < 1 {
		available := availWidth - lipgloss.Width(rawTags) - lipgloss.Width(right) - 1
		if available < 0 {
			available = 0
		}
		name = string([]rune(name)[:max(0, available)])
		gap = 1
	}
	var sb strings.Builder
	sb.WriteString(styleFileName.Render(name))
	for _, t := range tags {
		sb.WriteString(" " + styleTag.Render("["+t+"]"))
	}
	sb.WriteString(strings.Repeat(" ", gap))
	sb.WriteString(styleDim.Render(right))
	return sb.String()
}

// ── new-topic dialog ─────────────────────────────────────────────────────────

type newTopicDialog struct {
	active bool
	input  string
	errMsg string
}

func createTopicCmd(lib *storage.Library, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := lib.CreateTopic(name)
		return createTopicMsg{name: name, err: err}
	}
}

func loadImportTopicsCmd(lib *storage.Library, entries []importer.Entry) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			return importTopicsLoadedMsg{entries: entries, err: err}
		}
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = t.Name
		}
		return importTopicsLoadedMsg{entries: entries, topics: names}
	}
}

func ignoreEntryCmd(lib *storage.Library, entry importer.Entry) tea.Cmd {
	return func() tea.Msg {
		err := lib.MarkFile(entry.Name, storage.ActionIgnore, "", entry.Path, entry.Size)
		return importActionDoneMsg{entryName: entry.Name, err: err}
	}
}

func copyEntryCmd(lib *storage.Library, entry importer.Entry, topicName string) tea.Cmd {
	return func() tea.Msg {
		err := lib.CopyToTopic(topicName, entry.Path)
		if err == nil && converter.IsAvailable() {
			destPath := lib.FilePath(topicName, entry.Name)
			if convErr := converter.ConvertWAVToMP3(destPath); convErr != nil {
				slog.Warn("converter: conversion failed", "path", destPath, "err", convErr)
			} else {
				mp3Name := entry.Name[:len(entry.Name)-len(filepath.Ext(entry.Name))] + ".mp3"
				lib.StoreMP3Name(entry.Name, mp3Name)
				if rmErr := os.Remove(destPath); rmErr != nil {
					slog.Warn("converter: removing WAV failed", "path", destPath, "err", rmErr)
				}
			}
		}
		return importActionDoneMsg{entryName: entry.Name, topicName: topicName, err: err}
	}
}

func loadIgnoredCmd(lib *storage.Library) tea.Cmd {
	return func() tea.Msg {
		entries, err := lib.ListIgnored()
		return loadIgnoredMsg{entries: entries, err: err}
	}
}

func removeFromTopicCmd(lib *storage.Library, topicName, filename string) tea.Cmd {
	return func() tea.Msg {
		err := lib.RemoveFromTopic(topicName, filename)
		if err == nil {
			err = lib.UnmarkFile(filename)
		}
		return libFileDoneMsg{topicName: topicName, fileName: filename, isIgnore: false, err: err}
	}
}

func ignoreInTopicCmd(lib *storage.Library, topicName, filename string) tea.Cmd {
	return func() tea.Msg {
		// Carry over the device path and size from the existing mark (set when
		// the file was copied from the device) so Tab 3 can still play/copy it.
		var sourcePath string
		var size int64
		if m, err := lib.ReadMark(filename); err == nil && m != nil {
			sourcePath = m.SourcePath
			size = m.Size
		}
		err := lib.RemoveFromTopic(topicName, filename)
		if err == nil {
			err = lib.MarkFile(filename, storage.ActionIgnore, "", sourcePath, size)
		}
		return libFileDoneMsg{topicName: topicName, fileName: filename, isIgnore: true, err: err}
	}
}

func unmarkFileCmd(lib *storage.Library, filename, sourcePath string, size int64) tea.Cmd {
	return func() tea.Msg {
		err := lib.UnmarkFile(filename)
		return ignoredFileDoneMsg{filename: filename, sourcePath: sourcePath, size: size, err: err}
	}
}

func loadIgnoredTopicsCmd(lib *storage.Library, filename, sourcePath string) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			return ignoredTopicsLoadedMsg{filename: filename, sourcePath: sourcePath, err: err}
		}
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = t.Name
		}
		return ignoredTopicsLoadedMsg{filename: filename, sourcePath: sourcePath, topics: names}
	}
}

func loadIgnoredBatchTopicsCmd(lib *storage.Library, entries []storage.IgnoredEntry) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			return ignoredTopicsLoadedMsg{entries: entries, err: err}
		}
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = t.Name
		}
		return ignoredTopicsLoadedMsg{entries: entries, topics: names}
	}
}

func copyFromIgnoredCmd(lib *storage.Library, filename, sourcePath, topicName string) tea.Cmd {
	return func() tea.Msg {
		if sourcePath == "" {
			return ignoredFileDoneMsg{
				filename:  filename,
				topicName: topicName,
				err:       fmt.Errorf("source path unknown – copy file via tab 2"),
			}
		}
		err := lib.CopyToTopic(topicName, sourcePath)
		if err == nil && converter.IsAvailable() {
			destPath := lib.FilePath(topicName, filepath.Base(sourcePath))
			if convErr := converter.ConvertWAVToMP3(destPath); convErr != nil {
				slog.Warn("converter: conversion failed", "path", destPath, "err", convErr)
			} else {
				wavBase := filepath.Base(sourcePath)
				mp3Name := wavBase[:len(wavBase)-len(filepath.Ext(wavBase))] + ".mp3"
				lib.StoreMP3Name(filename, mp3Name)
				if rmErr := os.Remove(destPath); rmErr != nil {
					slog.Warn("converter: removing WAV failed", "path", destPath, "err", rmErr)
				}
			}
		}
		return ignoredFileDoneMsg{filename: filename, topicName: topicName, err: err}
	}
}

// ── commands ──────────────────────────────────────────────────────────────────

func playCmd(p *playback.Player, path string) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("playCmd: starting", "path", path)
		done, err := p.Play(path)
		if err != nil {
			slog.Debug("playCmd: Play() error", "path", path, "err", err)
			return playbackDoneMsg{path: path, err: err}
		}
		err = <-done
		slog.Debug("playCmd: done", "path", path, "err", err)
		return playbackDoneMsg{path: path, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return playbackTickMsg{}
	})
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// ── playerState ───────────────────────────────────────────────────────────────

// playerState encapsulates audio playback state for a single tab model.
// Use newPlayerState() to construct; the zero value is not ready.
type playerState struct {
	player      *playback.Player
	playingPath string
	playPos     time.Duration
	playDur     time.Duration
	paused      bool
}

func newPlayerState() playerState {
	return playerState{player: &playback.Player{}}
}

// toggle starts playback of path, or stops if path is already playing.
// Returns the tea.Cmd to run (nil when toggling off).
func (ps *playerState) toggle(path string) tea.Cmd {
	if path == "" {
		slog.Debug("playerState.toggle: empty path, ignoring")
		return nil
	}
	if ps.playingPath == path {
		if ps.paused {
			slog.Debug("playerState.toggle: resuming", "path", path)
			ps.player.Resume()
			ps.paused = false
			return tickCmd()
		}
		slog.Debug("playerState.toggle: pausing", "path", path)
		ps.player.Pause()
		ps.paused = true
		return nil
	}
	slog.Debug("playerState.toggle: starting", "path", path, "prev", ps.playingPath)
	ps.player.Stop()
	ps.playingPath = path
	ps.playPos = 0
	ps.playDur = 0
	ps.paused = false
	return tea.Batch(playCmd(ps.player, path), tickCmd())
}

// stop stops any current playback and clears state.
func (ps *playerState) stop() {
	slog.Debug("playerState.stop", "was", ps.playingPath)
	ps.player.Stop()
	ps.playingPath = ""
	ps.playPos = 0
	ps.playDur = 0
	ps.paused = false
}

// seek moves playback by offset (positive = forward, negative = backward).
func (ps *playerState) seek(offset time.Duration) {
	ps.player.Seek(offset)
}

// onTick reads current position/duration. Returns true if still playing.
func (ps *playerState) onTick() bool {
	if ps.playingPath == "" {
		return false
	}
	ps.playPos = ps.player.Position()
	ps.playDur = ps.player.Duration()
	slog.Debug("playerState.onTick", "pos", ps.playPos, "dur", ps.playDur, "path", ps.playingPath)
	return true
}

// onDone clears playback state when playback ends.
// path is the path from the playbackDoneMsg; it is compared against
// ps.playingPath so that a stale ErrStopped from a preempted session
// does not clear the state of a newly started session.
func (ps *playerState) onDone(path string) {
	slog.Debug("playerState.onDone", "msgPath", path, "curPath", ps.playingPath)
	if path != ps.playingPath {
		slog.Debug("playerState.onDone: stale, ignoring")
		return
	}
	ps.playingPath = ""
	ps.playPos = 0
	ps.playDur = 0
	ps.paused = false
}

// isPlaying reports whether path is the currently playing file.
func (ps playerState) isPlaying(path string) bool {
	return path != "" && ps.playingPath == path
}

// timeLabel returns "pos / dur" or "" when idle.
func (ps playerState) timeLabel() string {
	if ps.playingPath == "" {
		return ""
	}
	return fmtDuration(ps.playPos) + " / " + fmtDuration(ps.playDur)
}

// progressBar returns a fixed-width bar like [████████░░░░░░░░░░░░] of the given width.
// Returns "" when idle or duration is zero.
func (ps playerState) progressBar(width int) string {
	if ps.playingPath == "" || ps.playDur <= 0 || width <= 0 {
		return ""
	}
	ratio := float64(ps.playPos) / float64(ps.playDur)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// loadTranscriptForFile tries to parse the transcript file for a given audio
// file. Returns nil when no transcript exists or when parsing fails.
func loadTranscriptForFile(lib *storage.Library, topicName, filename string) *transcript.Transcript {
	base := filename[:len(filename)-len(filepath.Ext(filename))]
	txtPath := lib.FilePath(topicName, base+".txt")
	tr, err := transcript.ParseFile(txtPath)
	if err != nil {
		slog.Debug("tab1: transcript load failed", "path", txtPath, "err", err)
		return nil
	}
	return tr
}

// wrapWords wraps plain text to at most maxCols runes per line and returns at
// most maxLines lines joined by newlines. If text is empty, it returns a
// string of maxLines empty lines (to hold layout space).
func wrapWords(text string, maxCols, maxLines int) string {
	if maxCols < 1 {
		maxCols = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return strings.Repeat("\n", maxLines-1)
	}
	var lines []string
	current := ""
	for _, w := range words {
		if len(lines) >= maxLines {
			break
		}
		if current == "" {
			current = w
		} else if len(current)+1+len(w) <= maxCols {
			current += " " + w
		} else {
			lines = append(lines, current)
			if len(lines) >= maxLines {
				break
			}
			current = w
		}
	}
	if current != "" && len(lines) < maxLines {
		lines = append(lines, current)
	}
	// Pad to maxLines so layout stays stable.
	for len(lines) < maxLines {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func loadTopicsCmd(lib *storage.Library) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			return loadTopicsMsg{err: err}
		}
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = t.Name
		}
		return loadTopicsMsg{names: names}
	}
}

func loadFilesCmd(lib *storage.Library, topicName string) tea.Cmd {
	return func() tea.Msg {
		files, err := lib.TopicFiles(topicName)
		if err != nil {
			return loadFilesMsg{topicName: topicName, err: err}
		}
		// Build a set of base names that have an MP3 variant so that the
		// corresponding WAV file can be hidden (show only the MP3).
		mp3Bases := make(map[string]struct{})
		for _, f := range files {
			if strings.EqualFold(filepath.Ext(f), ".mp3") {
				base := f[:len(f)-len(filepath.Ext(f))]
				mp3Bases[strings.ToLower(base)] = struct{}{}
			}
		}
		// Build a set of base names that have a .txt transcript.
		txtBases := make(map[string]struct{})
		for _, f := range files {
			if strings.EqualFold(filepath.Ext(f), ".txt") {
				base := f[:len(f)-len(filepath.Ext(f))]
				txtBases[strings.ToLower(base)] = struct{}{}
			}
		}
		filtered := files[:0:0]
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f))
			if ext == ".txt" {
				continue // shown as indicator badge, not as a list item
			}
			if ext == ".wav" {
				base := f[:len(f)-len(filepath.Ext(f))]
				if _, hasMp3 := mp3Bases[strings.ToLower(base)]; hasMp3 {
					continue // skip WAV — MP3 variant will be shown instead
				}
			}
			filtered = append(filtered, f)
		}
		transcripts := make(map[string]bool)
		for _, f := range filtered {
			base := strings.ToLower(f[:len(f)-len(filepath.Ext(f))])
			if _, ok := txtBases[base]; ok {
				transcripts[f] = true
			}
		}
		dn := lib.LoadDisplayNames(filtered)
		durs := lib.LoadDurations(topicName, filtered)
		// Match global tag keywords against each file's transcript (case-insensitive).
		globalTags, _ := lib.GetGlobalTags()
		tags := make(map[string][]string)
		if len(globalTags) > 0 {
			for _, f := range filtered {
				if !transcripts[f] {
					continue
				}
				base := f[:len(f)-len(filepath.Ext(f))]
				content, err := os.ReadFile(filepath.Join(lib.Path, topicName, base+".txt"))
				if err != nil {
					continue
				}
				lower := strings.ToLower(string(content))
				var matched []string
				for _, tag := range globalTags {
					if strings.Contains(lower, strings.ToLower(tag)) {
						matched = append(matched, tag)
					}
				}
				if len(matched) > 0 {
					tags[f] = matched
				}
			}
		}
		return loadFilesMsg{topicName: topicName, files: filtered, displayNames: dn, durations: durs, transcripts: transcripts, tags: tags}
	}
}

// selRange returns a map with all indices between a and b (inclusive).
func selRange(a, b int) map[int]bool {
	sel := make(map[int]bool)
	if a > b {
		a, b = b, a
	}
	for i := a; i <= b; i++ {
		sel[i] = true
	}
	return sel
}

func batchIgnoreCmd(lib *storage.Library, entries []importer.Entry) tea.Cmd {
	return func() tea.Msg {
		var removed []string
		var errs []string
		for _, e := range entries {
			if err := lib.MarkFile(e.Name, storage.ActionIgnore, "", e.Path, e.Size); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name, err))
			} else {
				removed = append(removed, e.Name)
			}
		}
		return batchImportDoneMsg{removed: removed, errMsg: strings.Join(errs, "\n")}
	}
}

func batchCopyCmd(lib *storage.Library, entries []importer.Entry, topicName string) tea.Cmd {
	return func() tea.Msg {
		var removed []string
		var errs []string
		for _, e := range entries {
			if err := lib.CopyToTopic(topicName, e.Path); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name, err))
			} else {
				removed = append(removed, e.Name)
				if converter.IsAvailable() {
					destPath := lib.FilePath(topicName, e.Name)
					if convErr := converter.ConvertWAVToMP3(destPath); convErr != nil {
						slog.Warn("converter: batch conversion failed", "path", destPath, "err", convErr)
					} else {
						mp3Name := e.Name[:len(e.Name)-len(filepath.Ext(e.Name))] + ".mp3"
						lib.StoreMP3Name(e.Name, mp3Name)
						if rmErr := os.Remove(destPath); rmErr != nil {
							slog.Warn("converter: removing WAV failed", "path", destPath, "err", rmErr)
						}
					}
				}
			}
		}
		return batchImportDoneMsg{topicName: topicName, removed: removed, errMsg: strings.Join(errs, "\n")}
	}
}

func batchUnmarkCmd(lib *storage.Library, entries []storage.IgnoredEntry) tea.Cmd {
	return func() tea.Msg {
		var removed []storage.IgnoredEntry
		var errs []string
		for _, e := range entries {
			if err := lib.UnmarkFile(e.Name); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name, err))
			} else {
				removed = append(removed, e)
			}
		}
		return batchIgnoredDoneMsg{removed: removed, errMsg: strings.Join(errs, "\n")}
	}
}

func batchCopyFromIgnoredCmd(lib *storage.Library, entries []storage.IgnoredEntry, topicName string) tea.Cmd {
	return func() tea.Msg {
		var removed []storage.IgnoredEntry
		var errs []string
		for _, e := range entries {
			if e.SourcePath == "" {
				errs = append(errs, fmt.Sprintf("%s: source path unknown", e.Name))
				continue
			}
			if err := lib.CopyToTopic(topicName, e.SourcePath); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name, err))
			} else {
				removed = append(removed, e)
				if converter.IsAvailable() {
					destPath := lib.FilePath(topicName, filepath.Base(e.SourcePath))
					if convErr := converter.ConvertWAVToMP3(destPath); convErr != nil {
						slog.Warn("converter: batch conversion failed", "path", destPath, "err", convErr)
					} else {
						wavBase := filepath.Base(e.SourcePath)
						mp3Name := wavBase[:len(wavBase)-len(filepath.Ext(wavBase))] + ".mp3"
						lib.StoreMP3Name(e.Name, mp3Name)
						if rmErr := os.Remove(destPath); rmErr != nil {
							slog.Warn("converter: removing WAV failed", "path", destPath, "err", rmErr)
						}
					}
				}
			}
		}
		return batchIgnoredDoneMsg{topicName: topicName, removed: removed, errMsg: strings.Join(errs, "\n")}
	}
}

// ── library confirm dialog ──────────────────────────────────────────────────

type libConfirmMode int

const (
	libConfirmNone        libConfirmMode = iota
	libConfirmDelete                     // remove from topic + unmark
	libConfirmIgnore                     // remove from topic + mark as ignored
	libConfirmDeleteTopic                // delete entire topic and all its files
)

type libConfirmDialog struct {
	mode      libConfirmMode
	topicName string
	fileName  string
	errMsg    string
}

// ── library move dialog ─────────────────────────────────────────────────────

type libMoveDialog struct {
	active    bool
	topicName string // source topic
	fileName  string
	topics    []string // available destination topics (source excluded)
	cursor    int
	errMsg    string
}

type libMoveTopicsLoadedMsg struct {
	topicName string
	fileName  string
	topics    []string
	err       error
}

type moveFileDoneMsg struct {
	srcTopic string
	dstTopic string
	fileName string
	err      error
}

func loadMoveTopicsCmd(lib *storage.Library, topicName, fileName string) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			return libMoveTopicsLoadedMsg{topicName: topicName, fileName: fileName, err: err}
		}
		var names []string
		for _, t := range topics {
			if t.Name != topicName {
				names = append(names, t.Name)
			}
		}
		return libMoveTopicsLoadedMsg{topicName: topicName, fileName: fileName, topics: names}
	}
}

func moveFileCmd(lib *storage.Library, srcTopic, dstTopic, fileName string) tea.Cmd {
	return func() tea.Msg {
		err := lib.MoveToTopic(srcTopic, dstTopic, fileName)
		return moveFileDoneMsg{srcTopic: srcTopic, dstTopic: dstTopic, fileName: fileName, err: err}
	}
}

// ── library topic-rename dialog ─────────────────────────────────────────────

type topicRenameDialog struct {
	active  bool
	oldName string
	input   string
	errMsg  string
}

type renameTopicDoneMsg struct {
	oldName string
	newName string
	err     error
}

type deleteTopicDoneMsg struct {
	topicName string
	err       error
}

func renameTopicCmd(lib *storage.Library, oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		err := lib.RenameTopic(oldName, newName)
		return renameTopicDoneMsg{oldName: oldName, newName: newName, err: err}
	}
}

func deleteTopicCmd(lib *storage.Library, name string) tea.Cmd {
	return func() tea.Msg {
		err := lib.DeleteTopic(name)
		return deleteTopicDoneMsg{topicName: name, err: err}
	}
}

func transcribeUploadCmd(client *transcriber.Client, topicName, filename, filePath string) tea.Cmd {
	return func() tea.Msg {
		for {
			result, err := client.Upload(filePath)
			if err != nil {
				return transcribeUploadedMsg{topicName: topicName, filename: filename, filePath: filePath, err: err}
			}
			if !result.Busy {
				return transcribeUploadedMsg{topicName: topicName, filename: filename, filePath: filePath, jobID: result.JobID}
			}
			// 503 – server busy, wait and retry
			time.Sleep(5 * time.Second)
		}
	}
}

func transcribePollCmd(client *transcriber.Client, topicName, filename, filePath, jobID string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(5 * time.Second)
		result, err := client.Status(jobID)
		return transcribePollMsg{
			topicName:  topicName,
			filename:   filename,
			filePath:   filePath,
			jobID:      jobID,
			done:       result.Done,
			transcript: result.Transcript,
			err:        err,
		}
	}
}

func transcriptWriteCmd(lib *storage.Library, topicName, filename, text string) tea.Cmd {
	return func() tea.Msg {
		err := lib.WriteTranscript(topicName, filename, text)
		return transcriptWrittenMsg{topicName: topicName, filename: filename, err: err}
	}
}

// transcribeNextCmd pops the first item from the queue and starts its upload.
// It returns nil when the queue is empty or no transcriber is configured.
func transcribeNextCmd(m *libraryModel) tea.Cmd {
	if m.transcriber == nil || len(m.transcribeQueue) == 0 {
		return nil
	}
	item := m.transcribeQueue[0]
	m.transcribeQueue = m.transcribeQueue[1:]
	m.pendingJobs[item.filename] = "" // mark as in-flight before cmd returns
	return transcribeUploadCmd(m.transcriber, item.topicName, item.filename, item.filePath)
}

// ── library model (1) ────────────────────────────────────────────────────────

type topicNode struct {
	name         string
	expanded     bool
	files        []string
	displayNames map[string]string
	durations    map[string]time.Duration
	transcripts  map[string]bool
	tags         map[string][]string
	loaded       bool
}

// flatRow is a single visible line in the library tree.
type flatRow struct {
	isTopic  bool
	topicIdx int
	fileIdx  int // only meaningful when !isTopic
}

type libraryModel struct {
	lib              *storage.Library
	topics           []topicNode
	cursor           int
	height           int
	width            int
	status           string
	dialog           newTopicDialog
	confirm          libConfirmDialog
	move             libMoveDialog
	topicRename      topicRenameDialog
	rename           renameDialog
	tagDlg           tagDialog
	globalTags       []string // library-wide tag keyword list
	ps               playerState
	transcriber      *transcriber.Client
	pendingJobs      map[string]string // filename -> jobID
	failedJobs       map[string]bool   // filename -> true
	activeTranscript *transcript.Transcript
	transcribeQueue  []transcribeQueueItem // files waiting for auto-transcription
}

func newLibraryModel(lib *storage.Library, tc *transcriber.Client) libraryModel {
	return libraryModel{
		lib:         lib,
		status:      "Loading library…",
		ps:          newPlayerState(),
		transcriber: tc,
		pendingJobs: make(map[string]string),
		failedJobs:  make(map[string]bool),
	}
}

func (m libraryModel) Init() tea.Cmd {
	if m.lib == nil {
		return nil
	}
	return tea.Batch(loadTopicsCmd(m.lib), loadGlobalTagsCmd(m.lib))
}

func (m libraryModel) buildRows() []flatRow {
	var rows []flatRow
	for i, t := range m.topics {
		rows = append(rows, flatRow{isTopic: true, topicIdx: i})
		if t.expanded {
			for j := range t.files {
				rows = append(rows, flatRow{isTopic: false, topicIdx: i, fileIdx: j})
			}
		}
	}
	return rows
}

// refreshTopic refreshes the file list for a topic if expanded, or marks it stale otherwise.
func (m libraryModel) refreshTopic(topicName string) (libraryModel, tea.Cmd) {
	for i := range m.topics {
		if m.topics[i].name == topicName {
			if m.topics[i].expanded {
				return m, loadFilesCmd(m.lib, topicName)
			}
			m.topics[i].loaded = false
			break
		}
	}
	return m, nil
}

func (m libraryModel) handleDialogKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dialog = newTopicDialog{}
	case "enter":
		name := strings.TrimSpace(m.dialog.input)
		if name == "" {
			m.dialog.errMsg = "Name must not be empty"
			return m, nil
		}
		m.dialog.errMsg = ""
		return m, createTopicCmd(m.lib, name)
	case "backspace", "ctrl+h":
		if len(m.dialog.input) > 0 {
			runes := []rune(m.dialog.input)
			m.dialog.input = string(runes[:len(runes)-1])
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.dialog.input += string(r)
		}
	}
	return m, nil
}

func (m libraryModel) handleConfirmKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch msg.String() {
	case "j", "enter":
		m.confirm.errMsg = ""
		switch m.confirm.mode {
		case libConfirmDelete:
			return m, removeFromTopicCmd(m.lib, m.confirm.topicName, m.confirm.fileName)
		case libConfirmIgnore:
			return m, ignoreInTopicCmd(m.lib, m.confirm.topicName, m.confirm.fileName)
		case libConfirmDeleteTopic:
			return m, deleteTopicCmd(m.lib, m.confirm.topicName)
		}
	case "n", "esc":
		m.confirm = libConfirmDialog{}
	}
	return m, nil
}

func (m libraryModel) handleNormalKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	rows := m.buildRows()
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.ps.stop()
		}
	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
			m.ps.stop()
		}
	case "enter":
		if m.cursor < len(rows) {
			row := rows[m.cursor]
			if row.isTopic {
				t := &m.topics[row.topicIdx]
				t.expanded = !t.expanded
				if t.expanded && !t.loaded {
					return m, loadFilesCmd(m.lib, t.name)
				}
			}
		}
	case "left":
		m.ps.seek(-5 * time.Second)
	case "right":
		m.ps.seek(5 * time.Second)
	case "shift+left":
		m.ps.seek(-30 * time.Second)
	case "shift+right":
		m.ps.seek(30 * time.Second)
	case "ctrl+left":
		m.ps.seek(-60 * time.Second)
	case "ctrl+right":
		m.ps.seek(60 * time.Second)
	case " ":
		if m.cursor < len(rows) {
			row := rows[m.cursor]
			if !row.isTopic {
				filename := m.topics[row.topicIdx].files[row.fileIdx]
				topicName := m.topics[row.topicIdx].name
				path := m.lib.FilePath(topicName, filename)
				slog.Debug("tab1 space: toggle", "path", path)
				// Load transcript when starting a new file; clear when stopping.
				if m.ps.playingPath != path {
					m.activeTranscript = loadTranscriptForFile(m.lib, topicName, filename)
				} else if m.ps.paused {
					// resuming – keep existing transcript
				} else {
					// pausing – keep existing transcript
				}
				if cmd := m.ps.toggle(path); cmd != nil {
					return m, cmd
				} else if m.ps.playingPath == "" {
					m.activeTranscript = nil
				}
			}
		}
	case "n":
		if m.lib != nil {
			m.dialog = newTopicDialog{active: true}
		}
	case "R":
		if m.lib != nil && m.cursor < len(rows) && rows[m.cursor].isTopic {
			row := rows[m.cursor]
			m.topicRename = topicRenameDialog{
				active:  true,
				oldName: m.topics[row.topicIdx].name,
				input:   m.topics[row.topicIdx].name,
			}
		}
	case "r":
		if m.lib != nil && m.cursor < len(rows) && !rows[m.cursor].isTopic {
			row := rows[m.cursor]
			filename := m.topics[row.topicIdx].files[row.fileIdx]
			current := preferredName(filename, m.topics[row.topicIdx].displayNames)
			m.rename = renameDialog{
				active:    true,
				filename:  filename,
				topicName: m.topics[row.topicIdx].name,
				input:     current,
			}
		}
	case "t":
		if m.lib != nil {
			tagsCopy := make([]string, len(m.globalTags))
			copy(tagsCopy, m.globalTags)
			m.tagDlg = tagDialog{
				active: true,
				tags:   tagsCopy,
				mode:   tagDialogView,
			}
		}
	case "m":
		if m.lib != nil && m.cursor < len(rows) && !rows[m.cursor].isTopic {
			row := rows[m.cursor]
			return m, loadMoveTopicsCmd(m.lib, m.topics[row.topicIdx].name, m.topics[row.topicIdx].files[row.fileIdx])
		}
	case "i":
		if m.lib != nil && m.cursor < len(rows) && !rows[m.cursor].isTopic {
			row := rows[m.cursor]
			m.confirm = libConfirmDialog{
				mode:      libConfirmIgnore,
				topicName: m.topics[row.topicIdx].name,
				fileName:  m.topics[row.topicIdx].files[row.fileIdx],
			}
		}
	case "delete":
		if m.lib != nil && m.cursor < len(rows) {
			row := rows[m.cursor]
			if row.isTopic {
				m.confirm = libConfirmDialog{
					mode:      libConfirmDeleteTopic,
					topicName: m.topics[row.topicIdx].name,
				}
			} else {
				m.confirm = libConfirmDialog{
					mode:      libConfirmDelete,
					topicName: m.topics[row.topicIdx].name,
					fileName:  m.topics[row.topicIdx].files[row.fileIdx],
				}
			}
		}
	}
	return m, nil
}

func (m libraryModel) handleTopicRenameKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.topicRename = topicRenameDialog{}
	case "enter":
		name := strings.TrimSpace(m.topicRename.input)
		if name == "" {
			m.topicRename.errMsg = "Name must not be empty"
			return m, nil
		}
		if name == m.topicRename.oldName {
			m.topicRename = topicRenameDialog{}
			return m, nil
		}
		m.topicRename.errMsg = ""
		return m, renameTopicCmd(m.lib, m.topicRename.oldName, name)
	case "backspace", "ctrl+h":
		if len(m.topicRename.input) > 0 {
			runes := []rune(m.topicRename.input)
			m.topicRename.input = string(runes[:len(runes)-1])
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.topicRename.input += string(r)
		}
	}
	return m, nil
}

func (m libraryModel) handleMoveKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.move = libMoveDialog{}
	case "up", "k":
		if m.move.cursor > 0 {
			m.move.cursor--
		}
	case "down", "j":
		if m.move.cursor < len(m.move.topics)-1 {
			m.move.cursor++
		}
	case "enter":
		if len(m.move.topics) == 0 {
			return m, nil
		}
		dst := m.move.topics[m.move.cursor]
		src := m.move.topicName
		file := m.move.fileName
		m.move = libMoveDialog{}
		m.ps.stop()
		return m, moveFileCmd(m.lib, src, dst, file)
	}
	return m, nil
}

func (m libraryModel) handleRenameKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rename = renameDialog{}
	case "enter":
		name := strings.TrimSpace(m.rename.input)
		if name == "" {
			m.rename.errMsg = "Name must not be empty"
			return m, nil
		}
		m.rename.errMsg = ""
		return m, renameFileCmd(m.lib, m.rename.filename, name)
	case "backspace", "ctrl+h":
		if len(m.rename.input) > 0 {
			runes := []rune(m.rename.input)
			m.rename.input = string(runes[:len(runes)-1])
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.rename.input += string(r)
		}
	}
	return m, nil
}

func (m libraryModel) handleTagKey(msg tea.KeyMsg) (libraryModel, tea.Cmd) {
	switch m.tagDlg.mode {
	case tagDialogView:
		switch msg.String() {
		case "esc":
			m.tagDlg = tagDialog{}
		case "up", "k":
			if m.tagDlg.cursor > 0 {
				m.tagDlg.cursor--
			}
		case "down", "j":
			if m.tagDlg.cursor < len(m.tagDlg.tags)-1 {
				m.tagDlg.cursor++
			}
		case "a", "enter":
			m.tagDlg.mode = tagDialogAdd
			m.tagDlg.input = ""
			m.tagDlg.errMsg = ""
		case "delete", "d":
			if len(m.tagDlg.tags) > 0 {
				idx := m.tagDlg.cursor
				newTags := make([]string, 0, len(m.tagDlg.tags)-1)
				newTags = append(newTags, m.tagDlg.tags[:idx]...)
				newTags = append(newTags, m.tagDlg.tags[idx+1:]...)
				m.tagDlg.tags = newTags
				if m.tagDlg.cursor >= len(m.tagDlg.tags) && m.tagDlg.cursor > 0 {
					m.tagDlg.cursor--
				}
				m.tagDlg.errMsg = ""
				return m, setGlobalTagsCmd(m.lib, m.tagDlg.tags)
			}
		}
	case tagDialogAdd:
		switch msg.String() {
		case "esc":
			m.tagDlg.mode = tagDialogView
			m.tagDlg.input = ""
			m.tagDlg.errMsg = ""
		case "enter":
			tag := strings.TrimSpace(m.tagDlg.input)
			if tag == "" {
				m.tagDlg.errMsg = "Tag must not be empty"
				return m, nil
			}
			for _, existing := range m.tagDlg.tags {
				if existing == tag {
					m.tagDlg.errMsg = "Tag already exists"
					return m, nil
				}
			}
			m.tagDlg.tags = append(m.tagDlg.tags, tag)
			m.tagDlg.cursor = len(m.tagDlg.tags) - 1
			m.tagDlg.input = ""
			m.tagDlg.errMsg = ""
			m.tagDlg.mode = tagDialogView
			return m, setGlobalTagsCmd(m.lib, m.tagDlg.tags)
		case "backspace", "ctrl+h":
			if len(m.tagDlg.input) > 0 {
				runes := []rune(m.tagDlg.input)
				m.tagDlg.input = string(runes[:len(runes)-1])
			}
		default:
			if r := msg.Runes; len(r) > 0 {
				m.tagDlg.input += string(r)
			}
		}
	}
	return m, nil
}

func (m libraryModel) update(msg tea.Msg) (libraryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case loadTopicsMsg:
		if msg.err != nil {
			slog.Warn("tab1: loadTopics error", "err", msg.err)
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		// preserve expanded/files state and cursor position across reloads
		existing := make(map[string]topicNode, len(m.topics))
		for _, t := range m.topics {
			existing[t.name] = t
		}
		var cursorName string
		if m.cursor < len(m.topics) {
			cursorName = m.topics[m.cursor].name
		}
		m.topics = make([]topicNode, len(msg.names))
		for i, n := range msg.names {
			if prev, ok := existing[n]; ok {
				m.topics[i] = prev
			} else {
				m.topics[i] = topicNode{name: n}
			}
		}
		// restore cursor to same topic name if possible
		m.cursor = 0
		for i, t := range m.topics {
			if t.name == cursorName {
				m.cursor = i
				break
			}
		}
		if len(m.topics) == 0 {
			m.status = "No topics"
		} else {
			m.status = fmt.Sprintf("%d Topic(s)", len(m.topics))
		}

	case loadFilesMsg:
		if msg.err != nil {
			slog.Warn("tab1: loadFiles error", "topic", msg.topicName, "err", msg.err)
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				m.topics[i].files = msg.files
				m.topics[i].displayNames = msg.displayNames
				m.topics[i].durations = msg.durations
				m.topics[i].transcripts = msg.transcripts
				m.topics[i].tags = msg.tags
				m.topics[i].loaded = true
				break
			}
		}
		// Enqueue files that have no transcript yet and are not already in flight.
		// Only MP3 files are transcribed — WAV files are skipped because they
		// will be converted to MP3 first, and only the MP3 gets transcribed.
		if m.transcriber != nil {
			for _, f := range msg.files {
				if !strings.EqualFold(filepath.Ext(f), ".mp3") {
					continue // only transcribe MP3s
				}
				if msg.transcripts[f] {
					continue // already transcribed
				}
				if _, pending := m.pendingJobs[f]; pending {
					continue // already in flight
				}
				alreadyQueued := false
				for _, qi := range m.transcribeQueue {
					if qi.filename == f {
						alreadyQueued = true
						break
					}
				}
				if !alreadyQueued {
					m.transcribeQueue = append(m.transcribeQueue, transcribeQueueItem{
						topicName: msg.topicName,
						filename:  f,
						filePath:  m.lib.FilePath(msg.topicName, f),
					})
				}
			}
			// Start processing if nothing is currently in flight.
			if len(m.pendingJobs) == 0 {
				return m, transcribeNextCmd(&m)
			}
		}

	case importActionDoneMsg:
		if msg.topicName == "" {
			return m, nil // ignore action was not a copy
		}
		return m.refreshTopic(msg.topicName)

	case ignoredFileDoneMsg:
		if msg.topicName == "" || msg.err != nil {
			return m, nil
		}
		return m.refreshTopic(msg.topicName)

	case batchImportDoneMsg:
		if msg.topicName == "" {
			return m, nil
		}
		return m.refreshTopic(msg.topicName)

	case batchIgnoredDoneMsg:
		if msg.topicName == "" {
			return m, nil
		}
		return m.refreshTopic(msg.topicName)

	case libFileDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: libFile action error", "topic", msg.topicName, "file", msg.fileName, "err", msg.err)
			m.confirm.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.confirm = libConfirmDialog{}
		return m.refreshTopic(msg.topicName)

	case createTopicMsg:
		if msg.err != nil {
			slog.Warn("tab1: createTopic error", "name", msg.name, "err", msg.err)
			m.dialog.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.dialog = newTopicDialog{} // close
		return m, loadTopicsCmd(m.lib)

	case libMoveTopicsLoadedMsg:
		if msg.err != nil {
			slog.Warn("tab1: loadMoveTopics error", "err", msg.err)
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		if len(msg.topics) == 0 {
			m.status = "No other topics to move to"
			return m, nil
		}
		m.move = libMoveDialog{
			active:    true,
			topicName: msg.topicName,
			fileName:  msg.fileName,
			topics:    msg.topics,
		}

	case moveFileDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: moveFile error", "src", msg.srcTopic, "dst", msg.dstTopic, "file", msg.fileName, "err", msg.err)
			m.move.errMsg = fmt.Sprintf("Error: %v", msg.err)
			m.move.active = true // re-show dialog with error
			return m, nil
		}
		// Reload both affected topics.
		m, cmd1 := m.refreshTopic(msg.srcTopic)
		m, cmd2 := m.refreshTopic(msg.dstTopic)
		return m, tea.Batch(cmd1, cmd2)

	case renameDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: renameFile error", "filename", msg.filename, "err", msg.err)
			m.rename.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.rename = renameDialog{}
		// Update the in-memory display name for the renamed file.
		for i := range m.topics {
			for _, f := range m.topics[i].files {
				if f == msg.filename {
					if m.topics[i].displayNames == nil {
						m.topics[i].displayNames = make(map[string]string)
					}
					if msg.displayName == msg.filename {
						delete(m.topics[i].displayNames, msg.filename)
					} else {
						m.topics[i].displayNames[msg.filename] = msg.displayName
					}
					break
				}
			}
		}

	case globalTagsLoadedMsg:
		if msg.err != nil {
			slog.Warn("tab1: loadGlobalTags error", "err", msg.err)
			return m, nil
		}
		m.globalTags = msg.tags

	case setGlobalTagsDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: setGlobalTags error", "err", msg.err)
			m.tagDlg.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.globalTags = msg.tags
		// Reload all expanded topics so tag matches are recomputed.
		var cmds []tea.Cmd
		for _, t := range m.topics {
			if t.expanded {
				cmds = append(cmds, loadFilesCmd(m.lib, t.name))
			}
		}
		return m, tea.Batch(cmds...)

	case renameTopicDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: renameTopic error", "old", msg.oldName, "new", msg.newName, "err", msg.err)
			m.topicRename.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.topicRename = topicRenameDialog{}
		// Update the topic name in-memory.
		for i := range m.topics {
			if m.topics[i].name == msg.oldName {
				m.topics[i].name = msg.newName
				break
			}
		}
		return m, loadTopicsCmd(m.lib)

	case deleteTopicDoneMsg:
		if msg.err != nil {
			slog.Warn("tab1: deleteTopic error", "topic", msg.topicName, "err", msg.err)
			m.confirm.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.confirm = libConfirmDialog{}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				m.ps.stop()
				m.topics = append(m.topics[:i], m.topics[i+1:]...)
				if m.cursor >= len(m.topics) && m.cursor > 0 {
					m.cursor--
				}
				break
			}
		}
		m.status = fmt.Sprintf("%d Topic(s)", len(m.topics))
		return m, loadTopicsCmd(m.lib)

	case playbackTickMsg:
		if m.ps.onTick() {
			return m, tickCmd()
		}

	case playbackDoneMsg:
		m.ps.onDone(msg.path)
		if msg.err != nil && msg.err != playback.ErrStopped {
			slog.Warn("tab1: playback error", "path", msg.path, "err", msg.err)
			m.status = fmt.Sprintf("Playback error: %v", msg.err)
		}
		if m.ps.playingPath == "" {
			m.activeTranscript = nil
		}

	case transcribeUploadedMsg:
		if msg.err != nil {
			slog.Warn("tab1: transcribeUpload error", "filename", msg.filename, "err", msg.err)
			m.failedJobs[msg.filename] = true
			delete(m.pendingJobs, msg.filename)
			return m, transcribeNextCmd(&m)
		}
		m.pendingJobs[msg.filename] = msg.jobID
		return m, transcribePollCmd(m.transcriber, msg.topicName, msg.filename, msg.filePath, msg.jobID)

	case transcribePollMsg:
		if msg.err != nil {
			slog.Warn("tab1: transcribePoll error", "filename", msg.filename, "err", msg.err)
			m.failedJobs[msg.filename] = true
			delete(m.pendingJobs, msg.filename)
			return m, transcribeNextCmd(&m)
		}
		if !msg.done {
			return m, transcribePollCmd(m.transcriber, msg.topicName, msg.filename, msg.filePath, msg.jobID)
		}
		return m, transcriptWriteCmd(m.lib, msg.topicName, msg.filename, msg.transcript)

	case transcriptWrittenMsg:
		delete(m.pendingJobs, msg.filename)
		if msg.err != nil {
			slog.Warn("tab1: transcriptWrite error", "filename", msg.filename, "err", msg.err)
			m.failedJobs[msg.filename] = true
			return m, transcribeNextCmd(&m)
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].transcripts == nil {
					m.topics[i].transcripts = make(map[string]bool)
				}
				m.topics[i].transcripts[msg.filename] = true
				break
			}
		}
		return m, transcribeNextCmd(&m)

	case tea.KeyMsg:
		if m.dialog.active {
			return m.handleDialogKey(msg)
		}
		if m.confirm.mode != libConfirmNone {
			return m.handleConfirmKey(msg)
		}
		if m.move.active {
			return m.handleMoveKey(msg)
		}
		if m.topicRename.active {
			return m.handleTopicRenameKey(msg)
		}
		if m.rename.active {
			return m.handleRenameKey(msg)
		}
		if m.tagDlg.active {
			return m.handleTagKey(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

func (m libraryModel) view() string {
	if m.lib == nil {
		return styleDim.Render("No library configured.") + "\n"
	}
	rows := m.buildRows()
	listHeight := m.height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	start := m.cursor - listHeight/2
	if start < 0 {
		start = 0
	}
	if start+listHeight > len(rows) {
		start = max(0, len(rows)-listHeight)
	}
	var out string
	for i, linesLeft := start, listHeight; i < len(rows) && linesLeft > 0; i++ {
		row := rows[i]
		var line string
		if row.isTopic {
			t := m.topics[row.topicIdx]
			arrow := "▶"
			if t.expanded {
				arrow = "▼"
			}
			line = styleTopic.Render(fmt.Sprintf("%s %s", arrow, t.name))
		} else {
			f := m.topics[row.topicIdx].files[row.fileIdx]
			prefix := "  "
			availWidth := m.width - 5 - lipgloss.Width(prefix)
			durStr := formatDuration(m.topics[row.topicIdx].durations[f])
			// Transcript badge: [T] done, [~] pending, [E] error.
			// Always reserve 4 chars so all rows stay aligned.
			badge := "    "
			if m.topics[row.topicIdx].transcripts[f] {
				badge = " [T]"
			} else if _, ok := m.pendingJobs[f]; ok {
				badge = " [~]"
			} else if m.failedJobs[f] {
				badge = " [E]"
			}
			durStr += badge
			fileTags := m.topics[row.topicIdx].tags[f]
			if i == m.cursor {
				label := formatLabelWithTags(f, m.topics[row.topicIdx].displayNames[f], fileTags, availWidth, durStr)
				line = prefix + label
			} else {
				label := formatLabelWithTagsStyled(f, m.topics[row.topicIdx].displayNames[f], fileTags, availWidth, durStr)
				line = prefix + label
			}
		}
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if !row.isTopic {
			out += line + "\n"
		} else {
			out += line + "\n"
		}
		linesLeft--
	}
	return out
}

func (m libraryModel) overlayContent() string {
	if m.dialog.active {
		return m.renderDialog()
	}
	if m.confirm.mode != libConfirmNone {
		return m.renderConfirmDialog()
	}
	if m.move.active {
		return m.renderMoveDialog()
	}
	if m.topicRename.active {
		return m.renderTopicRenameDialog()
	}
	if m.rename.active {
		return m.renderRenameDialog()
	}
	if m.tagDlg.active {
		return m.renderTagDialog()
	}
	return ""
}

func (m libraryModel) renderDialog() string {
	prompt := "New topic: " + m.dialog.input + "█"
	var body string
	if m.dialog.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.dialog.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return styleDialog.Render(body)
}
func (m libraryModel) renderConfirmDialog() string {
	var action string
	switch m.confirm.mode {
	case libConfirmDelete:
		action = "Remove file from topic and delete mark?"
	case libConfirmIgnore:
		action = "Remove file from topic and ignore?"
	case libConfirmDeleteTopic:
		action = fmt.Sprintf("Delete topic %q and all its files?", m.confirm.topicName)
	}
	body := fmt.Sprintf("%s\n\n%s\n\n%s",
		action,
		styleDim.Render(m.confirm.topicName+"/"+m.confirm.fileName),
		styleDim.Render("j / Enter confirm  •  n / Esc cancel"),
	)
	if m.confirm.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.confirm.errMsg)
	}
	return styleDialog.Render(body)
}

func (m libraryModel) renderTopicRenameDialog() string {
	prompt := "Rename topic: " + m.topicRename.input + "█"
	var body string
	if m.topicRename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.topicRename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return styleDialog.Render(body)
}

func (m libraryModel) renderMoveDialog() string {
	label := preferredName(m.move.fileName, nil)
	body := fmt.Sprintf("Move %s to which topic?\n\n", styleDim.Render(label))
	if len(m.move.topics) == 0 {
		body += styleDim.Render("No other topics available.")
	} else {
		for i, t := range m.move.topics {
			line := "  " + t
			if i == m.move.cursor {
				body += styleSelected.Render(line) + "\n"
			} else {
				body += line + "\n"
			}
		}
		body += "\n" + styleDim.Render("↑/↓ select  •  Enter move  •  Esc cancel")
	}
	if m.move.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.move.errMsg)
	}
	return styleDialog.Render(body)
}

func (m libraryModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return styleDialog.Render(body)
}

func (m libraryModel) renderTagDialog() string {
	header := styleTitle.Render("Global Tag Keywords")
	var body string

	if m.tagDlg.mode == tagDialogView {
		body = header + "\n" + styleDim.Render("Files with a transcript are labelled when the keyword appears in it.") + "\n\n"
		if len(m.tagDlg.tags) == 0 {
			body += styleDim.Render("  (no tags yet)") + "\n"
		} else {
			for i, tag := range m.tagDlg.tags {
				if i == m.tagDlg.cursor {
					body += "  ▶ " + styleTag.Render(tag) + "\n"
				} else {
					body += styleDim.Render("  • "+tag) + "\n"
				}
			}
		}
		body += "\n"
		if m.tagDlg.errMsg != "" {
			body += styleDialogErr.Render(m.tagDlg.errMsg)
		} else {
			var hints string
			if len(m.tagDlg.tags) > 0 {
				hints = "↑/↓ select  •  a add  •  d / del remove  •  Esc close"
			} else {
				hints = "a add  •  Esc close"
			}
			body += styleDim.Render(hints)
		}
	} else { // tagDialogAdd
		body = header + "\n\n"
		for _, tag := range m.tagDlg.tags {
			body += styleDim.Render("  • "+tag) + "\n"
		}
		if len(m.tagDlg.tags) > 0 {
			body += "\n"
		}
		body += "New keyword: " + m.tagDlg.input + "█\n"
		if m.tagDlg.errMsg != "" {
			body += styleDialogErr.Render(m.tagDlg.errMsg)
		} else {
			body += styleDim.Render("Enter confirm  •  Esc cancel")
		}
	}

	return styleDialog.Render(body)
}

// ── import dialog ─────────────────────────────────────────────────────────────

type importDialogMode int

const (
	importDialogNone   importDialogMode = iota
	importDialogIgnore                  // confirm ignore
	importDialogCopy                    // choose topic
)

type importDialog struct {
	mode        importDialogMode
	entries     []importer.Entry // always set when dialog is open (len >= 1)
	topics      []string
	topicCursor int
	errMsg      string
}

// ── import model (2) ─────────────────────────────────────────────────────────

type importModel struct {
	lib           *storage.Library
	entries       []importer.Entry
	cursor        int
	sel           map[int]bool
	anchor        int
	height        int
	width         int
	status        string
	entriesCh     <-chan []importer.Entry
	deviceStateCh <-chan importer.State
	dialog        importDialog
	copying       bool
	ps            playerState
	rename        renameDialog
	displayNames  map[string]string
}

func newImportModel(lib *storage.Library, ch <-chan []importer.Entry, stateCh <-chan importer.State) importModel {
	return importModel{lib: lib, entriesCh: ch, deviceStateCh: stateCh, status: "Please connect TP-7…", ps: newPlayerState()}
}

func (m importModel) Init() tea.Cmd {
	return tea.Batch(m.awaitEntries(), m.awaitDeviceState())
}

func (m importModel) awaitEntries() tea.Cmd {
	ch := m.entriesCh
	return func() tea.Msg { return entriesMsg(<-ch) }
}

func (m importModel) awaitDeviceState() tea.Cmd {
	ch := m.deviceStateCh
	return func() tea.Msg { return deviceStateMsg{state: <-ch} }
}

// selectedEntries returns the selected entries (or the cursor entry if no selection).
func (m importModel) selectedEntries() []importer.Entry {
	if len(m.sel) == 0 {
		if m.cursor < len(m.entries) {
			return []importer.Entry{m.entries[m.cursor]}
		}
		return nil
	}
	entries := make([]importer.Entry, 0, len(m.sel))
	for i, e := range m.entries {
		if m.sel[i] {
			entries = append(entries, e)
		}
	}
	return entries
}

func (m importModel) handleIgnoreDialogKey(msg tea.KeyMsg) (importModel, tea.Cmd) {
	switch msg.String() {
	case "j", "enter":
		m.dialog.errMsg = ""
		if len(m.dialog.entries) == 1 {
			return m, ignoreEntryCmd(m.lib, m.dialog.entries[0])
		}
		return m, batchIgnoreCmd(m.lib, m.dialog.entries)
	case "n", "esc":
		m.dialog = importDialog{}
	}
	return m, nil
}

func (m importModel) handleCopyDialogKey(msg tea.KeyMsg) (importModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.dialog.topicCursor > 0 {
			m.dialog.topicCursor--
		}
	case "down", "j":
		if m.dialog.topicCursor < len(m.dialog.topics)-1 {
			m.dialog.topicCursor++
		}
	case "enter":
		topic := m.dialog.topics[m.dialog.topicCursor]
		entries := m.dialog.entries
		n := len(entries)
		m.dialog = importDialog{}
		m.copying = true
		m.status = fmt.Sprintf("Copying %d file(s) to %s…", n, topic)
		if n == 1 {
			return m, copyEntryCmd(m.lib, entries[0], topic)
		}
		return m, batchCopyCmd(m.lib, entries, topic)
	case "esc":
		m.dialog = importDialog{}
	}
	return m, nil
}

func (m importModel) handleNormalKey(msg tea.KeyMsg) (importModel, tea.Cmd) {
	if m.copying {
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		m.sel = nil
		if m.cursor > 0 {
			m.cursor--
			m.ps.stop()
		}
		m.anchor = m.cursor
	case "down", "j":
		m.sel = nil
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.ps.stop()
		}
		m.anchor = m.cursor
	case "shift+up":
		if m.cursor > 0 {
			m.cursor--
			m.sel = selRange(m.anchor, m.cursor)
		}
	case "shift+down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.sel = selRange(m.anchor, m.cursor)
		}
	case "a":
		if len(m.entries) > 0 {
			m.sel = selRange(0, len(m.entries)-1)
			m.anchor = m.cursor
		}
	case "d":
		m.sel = nil
	case " ":
		if !m.copying && m.cursor < len(m.entries) {
			path := m.entries[m.cursor].Path
			slog.Debug("tab2 space: toggle", "path", path, "cursor", m.cursor, "entries", len(m.entries))
			if cmd := m.ps.toggle(path); cmd != nil {
				return m, cmd
			}
		}
	case "left":
		m.ps.seek(-5 * time.Second)
	case "right":
		m.ps.seek(5 * time.Second)
	case "shift+left":
		m.ps.seek(-30 * time.Second)
	case "shift+right":
		m.ps.seek(30 * time.Second)
	case "ctrl+left":
		m.ps.seek(-60 * time.Second)
	case "ctrl+right":
		m.ps.seek(60 * time.Second)
	case "i":
		if m.lib != nil && len(m.entries) > 0 {
			m.dialog = importDialog{
				mode:    importDialogIgnore,
				entries: m.selectedEntries(),
			}
		}
	case "c":
		if m.lib != nil && len(m.entries) > 0 {
			return m, loadImportTopicsCmd(m.lib, m.selectedEntries())
		}
	case "r":
		if m.lib != nil && m.cursor < len(m.entries) {
			filename := m.entries[m.cursor].Name
			current := preferredName(filename, m.displayNames)
			m.rename = renameDialog{
				active:   true,
				filename: filename,
				input:    current,
			}
		}
	}
	return m, nil
}

func (m importModel) handleRenameKey(msg tea.KeyMsg) (importModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rename = renameDialog{}
	case "enter":
		name := strings.TrimSpace(m.rename.input)
		if name == "" {
			m.rename.errMsg = "Name must not be empty"
			return m, nil
		}
		m.rename.errMsg = ""
		return m, renameFileCmd(m.lib, m.rename.filename, name)
	case "backspace", "ctrl+h":
		if len(m.rename.input) > 0 {
			runes := []rune(m.rename.input)
			m.rename.input = string(runes[:len(runes)-1])
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.rename.input += string(r)
		}
	}
	return m, nil
}

func (m importModel) update(msg tea.Msg) (importModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case playbackTickMsg:
		if m.ps.onTick() {
			return m, tickCmd()
		}

	case playbackDoneMsg:
		m.ps.onDone(msg.path)
		if msg.err != nil && msg.err != playback.ErrStopped {
			slog.Warn("tab2: playback error", "path", msg.path, "err", msg.err)
			m.status = fmt.Sprintf("Playback error: %v", msg.err)
		}

	case entriesMsg:
		m.entries = []importer.Entry(msg)
		m.sel = nil
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))
		names := make([]string, len(m.entries))
		for i, e := range m.entries {
			names[i] = e.Name
		}
		return m, tea.Batch(m.awaitEntries(), loadImportDisplayNamesCmd(m.lib, names))

	case deviceStateMsg:
		if msg.state == importer.StateSearching {
			m.status = "Please connect TP-7…"
			m.entries = nil
			m.sel = nil
			m.cursor = 0
			m.ps.stop()
		}
		return m, m.awaitDeviceState()

	case importTopicsLoadedMsg:
		if msg.err != nil {
			slog.Warn("tab2: loadImportTopics error", "err", msg.err)
			m.dialog = importDialog{}
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		if len(msg.topics) == 0 {
			m.dialog = importDialog{}
			m.status = "No topics – create a topic in tab 1 first"
			return m, nil
		}
		m.dialog = importDialog{
			mode:    importDialogCopy,
			entries: msg.entries,
			topics:  msg.topics,
		}

	case importActionDoneMsg:
		if msg.err != nil {
			slog.Warn("tab2: importAction error", "entry", msg.entryName, "topic", msg.topicName, "err", msg.err)
			if m.copying {
				m.copying = false
				m.status = fmt.Sprintf("Error: %v", msg.err)
			} else {
				m.dialog.errMsg = fmt.Sprintf("Error: %v", msg.err)
			}
			return m, nil
		}
		m.copying = false
		m.dialog = importDialog{}
		m.sel = nil
		for i, e := range m.entries {
			if e.Name == msg.entryName {
				if m.ps.isPlaying(e.Path) {
					m.ps.stop()
				}
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				break
			}
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))

	case batchImportDoneMsg:
		if msg.errMsg != "" {
			slog.Warn("tab2: batchImport partial/full error", "topic", msg.topicName, "removed", len(msg.removed), "err", msg.errMsg)
		}
		if msg.errMsg != "" && len(msg.removed) == 0 {
			if m.copying {
				m.copying = false
				m.status = fmt.Sprintf("Error: %s", msg.errMsg)
			} else {
				m.dialog.errMsg = msg.errMsg
			}
			return m, nil
		}
		m.copying = false
		m.dialog = importDialog{}
		m.sel = nil
		removed := make(map[string]bool, len(msg.removed))
		for _, name := range msg.removed {
			removed[name] = true
		}
		// Stop playback if the playing file is among those being removed.
		for _, e := range m.entries {
			if removed[e.Name] && m.ps.isPlaying(e.Path) {
				m.ps.stop()
				break
			}
		}
		var remaining []importer.Entry
		for _, e := range m.entries {
			if !removed[e.Name] {
				remaining = append(remaining, e)
			}
		}
		m.entries = remaining
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		if msg.errMsg != "" {
			m.status = fmt.Sprintf("Error: %s", msg.errMsg)
		} else {
			m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))
		}

	case batchIgnoredDoneMsg:
		// 3 batch-unmark: add removed entries back to 2 if they have a source path.
		if msg.topicName != "" {
			return m, nil // was a copy, not an unmark
		}
		existing := make(map[string]bool, len(m.entries))
		for _, e := range m.entries {
			existing[e.Name] = true
		}
		for _, e := range msg.removed {
			if e.SourcePath != "" && !existing[e.Name] {
				m.entries = append(m.entries, importer.Entry{Name: e.Name, Path: e.SourcePath, Size: e.Size})
			}
		}
		if len(msg.removed) > 0 {
			m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))
		}

	case ignoredFileDoneMsg:
		// An ignored file was un-marked in 3 — add it back to the import list.
		if msg.err != nil || msg.topicName != "" || msg.sourcePath == "" {
			return m, nil
		}
		for _, e := range m.entries {
			if e.Name == msg.filename {
				return m, nil // already present
			}
		}
		m.entries = append(m.entries, importer.Entry{Name: msg.filename, Path: msg.sourcePath, Size: msg.size})
		m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))

	case importDisplayNamesMsg:
		m.displayNames = msg.names

	case renameDoneMsg:
		if msg.err != nil {
			m.rename.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.rename = renameDialog{}
		if m.displayNames == nil {
			m.displayNames = make(map[string]string)
		}
		if msg.displayName == msg.filename {
			delete(m.displayNames, msg.filename)
		} else {
			m.displayNames[msg.filename] = msg.displayName
		}

	case tea.KeyMsg:
		if m.rename.active {
			return m.handleRenameKey(msg)
		}
		switch m.dialog.mode {
		case importDialogIgnore:
			return m.handleIgnoreDialogKey(msg)
		case importDialogCopy:
			return m.handleCopyDialogKey(msg)
		default:
			return m.handleNormalKey(msg)
		}
	}
	return m, nil
}

func (m importModel) view() string {
	listHeight := m.height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	start := m.cursor - listHeight/2
	if start < 0 {
		start = 0
	}
	if start+listHeight > len(m.entries) {
		start = max(0, len(m.entries)-listHeight)
	}
	var out string
	for i, linesLeft := start, listHeight; i < len(m.entries) && linesLeft > 0; i++ {
		e := m.entries[i]
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		sizeStr := formatSize(e.Size)
		const sizeColWidth = 9 // enough for "1023.9 MB"
		availW := m.width - 5 - lipgloss.Width(prefix) - 2 - sizeColWidth
		if m.ps.isPlaying(e.Path) {
			prefix = "▶ "
		}
		sizeField := fmt.Sprintf("%*s", sizeColWidth, sizeStr)
		if i == m.cursor {
			label := formatLabelAligned(e.Name, m.displayNames[e.Name], availW, "")
			out += styleSelected.Render(prefix+label+"  "+sizeField) + "\n"
		} else if m.sel[i] {
			label := formatLabelAligned(e.Name, m.displayNames[e.Name], availW, "")
			out += styleMultiSel.Render(prefix+label+"  "+sizeField) + "\n"
		} else {
			label := formatLabelAlignedStyled(e.Name, m.displayNames[e.Name], availW, "")
			out += prefix + label + styleDim.Render("  "+sizeField) + "\n"
		}
		linesLeft--
	}
	return out
}

func (m importModel) overlayContent() string {
	if m.dialog.mode != importDialogNone {
		return m.renderDialog()
	}
	if m.rename.active {
		return m.renderRenameDialog()
	}
	return ""
}

func (m importModel) renderDialog() string {
	var body string
	switch m.dialog.mode {
	case importDialogIgnore:
		var label string
		if len(m.dialog.entries) == 1 {
			label = m.dialog.entries[0].Name
		} else {
			label = fmt.Sprintf("%d recordings", len(m.dialog.entries))
		}
		body = fmt.Sprintf(
			"Ignore recording(s)?\n\n"+
				styleDim.Render("%s")+"\n\n"+
				"The file(s) will not be copied and will\n"+
				"no longer appear in the list.\n\n"+
				styleDim.Render("j / Enter confirm  •  n / Esc cancel"),
			label,
		)
	case importDialogCopy:
		var label string
		if len(m.dialog.entries) == 1 {
			label = m.dialog.entries[0].Name
		} else {
			label = fmt.Sprintf("%d recordings", len(m.dialog.entries))
		}
		body = fmt.Sprintf("Copy to which topic?\n\n"+styleDim.Render("%s")+"\n\n",
			label)
		for i, t := range m.dialog.topics {
			line := "  " + t
			if i == m.dialog.topicCursor {
				body += styleSelected.Render(line) + "\n"
			} else {
				body += line + "\n"
			}
		}
		body += "\n" + styleDim.Render("↑/↓ select  •  Enter copy  •  Esc cancel")
	}
	if m.dialog.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.dialog.errMsg)
	}
	return styleDialog.Render(body)
}

func (m importModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return styleDialog.Render(body)
}

// ── ignored model (3) ───────────────────────────────────────────────────────

type ignoredDialogMode int

const (
	ignoredDialogNone   ignoredDialogMode = iota
	ignoredDialogDelete                   // confirm unmark
	ignoredDialogCopy                     // pick topic
)

type ignoredDialog struct {
	mode       ignoredDialogMode
	filename   string                 // single mode
	sourcePath string                 // single mode
	size       int64                  // single mode
	entries    []storage.IgnoredEntry // batch mode (len > 1)
	topics     []string
	cursor     int
	errMsg     string
}

type ignoredModel struct {
	lib     *storage.Library
	entries []storage.IgnoredEntry
	cursor  int
	sel     map[int]bool
	anchor  int
	height  int
	width   int
	status  string
	dialog  ignoredDialog
	copying bool
	ps      playerState
	rename  renameDialog
}

func newIgnoredModel(lib *storage.Library) ignoredModel {
	return ignoredModel{lib: lib, status: "Loading ignored files…", ps: newPlayerState()}
}

func (m ignoredModel) Init() tea.Cmd {
	if m.lib == nil {
		return nil
	}
	return loadIgnoredCmd(m.lib)
}

// selectedIgnoredEntries returns the selected entries (or the cursor entry if no selection).
func (m ignoredModel) selectedIgnoredEntries() []storage.IgnoredEntry {
	if len(m.sel) == 0 {
		if m.cursor < len(m.entries) {
			return []storage.IgnoredEntry{m.entries[m.cursor]}
		}
		return nil
	}
	entries := make([]storage.IgnoredEntry, 0, len(m.sel))
	for i, e := range m.entries {
		if m.sel[i] {
			entries = append(entries, e)
		}
	}
	return entries
}

func (m ignoredModel) handleDeleteDialogKey(msg tea.KeyMsg) (ignoredModel, tea.Cmd) {
	switch msg.String() {
	case "j", "enter":
		m.dialog.errMsg = ""
		if len(m.dialog.entries) > 1 {
			return m, batchUnmarkCmd(m.lib, m.dialog.entries)
		}
		return m, unmarkFileCmd(m.lib, m.dialog.filename, m.dialog.sourcePath, m.dialog.size)
	case "n", "esc":
		m.dialog = ignoredDialog{}
	}
	return m, nil
}

func (m ignoredModel) handleCopyDialogKey(msg tea.KeyMsg) (ignoredModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.dialog.cursor > 0 {
			m.dialog.cursor--
		}
	case "down", "j":
		if m.dialog.cursor < len(m.dialog.topics)-1 {
			m.dialog.cursor++
		}
	case "enter":
		topic := m.dialog.topics[m.dialog.cursor]
		entries := m.dialog.entries
		filename := m.dialog.filename
		sourcePath := m.dialog.sourcePath
		n := len(entries)
		if n == 0 {
			n = 1
		}
		m.dialog = ignoredDialog{}
		m.copying = true
		m.status = fmt.Sprintf("Copying %d file(s) to %s…", n, topic)
		if len(entries) > 1 {
			return m, batchCopyFromIgnoredCmd(m.lib, entries, topic)
		}
		return m, copyFromIgnoredCmd(m.lib, filename, sourcePath, topic)
	case "esc":
		m.dialog = ignoredDialog{}
	}
	return m, nil
}

func (m ignoredModel) handleNormalKey(msg tea.KeyMsg) (ignoredModel, tea.Cmd) {
	if m.copying {
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		m.sel = nil
		if m.cursor > 0 {
			m.cursor--
			m.ps.stop()
		}
		m.anchor = m.cursor
	case "down", "j":
		m.sel = nil
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.ps.stop()
		}
		m.anchor = m.cursor
	case "shift+up":
		if m.cursor > 0 {
			m.cursor--
			m.sel = selRange(m.anchor, m.cursor)
		}
	case "shift+down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.sel = selRange(m.anchor, m.cursor)
		}
	case "a":
		if len(m.entries) > 0 {
			m.sel = selRange(0, len(m.entries)-1)
			m.anchor = m.cursor
		}
	case "d":
		m.sel = nil
	case " ":
		if !m.copying && m.cursor < len(m.entries) {
			path := m.entries[m.cursor].SourcePath
			slog.Debug("tab3 space: toggle", "path", path, "cursor", m.cursor, "entries", len(m.entries))
			if path == "" {
				m.status = "No file available – was removed from library when ignored"
				return m, nil
			}
			if _, err := os.Stat(path); err != nil {
				m.status = "Device not mounted – cannot play"
				return m, nil
			}
			if cmd := m.ps.toggle(path); cmd != nil {
				return m, cmd
			}
		}
	case "left":
		m.ps.seek(-5 * time.Second)
	case "right":
		m.ps.seek(5 * time.Second)
	case "shift+left":
		m.ps.seek(-30 * time.Second)
	case "shift+right":
		m.ps.seek(30 * time.Second)
	case "ctrl+left":
		m.ps.seek(-60 * time.Second)
	case "ctrl+right":
		m.ps.seek(60 * time.Second)
	case "r":
		if m.lib != nil && m.cursor < len(m.entries) {
			e := m.entries[m.cursor]
			current := e.DisplayName
			if current == "" {
				current = e.Name
			}
			m.rename = renameDialog{
				active:   true,
				filename: e.Name,
				input:    current,
			}
		}
	case "delete":
		if m.lib != nil && len(m.entries) > 0 {
			entries := m.selectedIgnoredEntries()
			if len(entries) > 1 {
				m.dialog = ignoredDialog{
					mode:    ignoredDialogDelete,
					entries: entries,
				}
			} else {
				e := m.entries[m.cursor]
				m.dialog = ignoredDialog{
					mode:       ignoredDialogDelete,
					filename:   e.Name,
					sourcePath: e.SourcePath,
					size:       e.Size,
				}
			}
		}
	case "c":
		if m.lib != nil && len(m.entries) > 0 {
			entries := m.selectedIgnoredEntries()
			// Filter out entries whose source file is not accessible.
			var accessible []storage.IgnoredEntry
			for _, e := range entries {
				if e.SourcePath != "" {
					if _, err := os.Stat(e.SourcePath); err == nil {
						accessible = append(accessible, e)
					}
				}
			}
			if len(accessible) == 0 {
				m.status = "Device not mounted – cannot copy"
				return m, nil
			}
			if len(accessible) > 1 {
				return m, loadIgnoredBatchTopicsCmd(m.lib, accessible)
			}
			e := accessible[0]
			return m, loadIgnoredTopicsCmd(m.lib, e.Name, e.SourcePath)
		}
	}
	return m, nil
}

func (m ignoredModel) handleRenameKey(msg tea.KeyMsg) (ignoredModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rename = renameDialog{}
	case "enter":
		name := strings.TrimSpace(m.rename.input)
		if name == "" {
			m.rename.errMsg = "Name must not be empty"
			return m, nil
		}
		m.rename.errMsg = ""
		return m, renameFileCmd(m.lib, m.rename.filename, name)
	case "backspace", "ctrl+h":
		if len(m.rename.input) > 0 {
			runes := []rune(m.rename.input)
			m.rename.input = string(runes[:len(runes)-1])
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.rename.input += string(r)
		}
	}
	return m, nil
}

func (m ignoredModel) update(msg tea.Msg) (ignoredModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case playbackTickMsg:
		if m.ps.onTick() {
			return m, tickCmd()
		}

	case playbackDoneMsg:
		m.ps.onDone(msg.path)
		if msg.err != nil && msg.err != playback.ErrStopped {
			slog.Warn("tab3: playback error", "path", msg.path, "err", msg.err)
			m.status = fmt.Sprintf("Playback error: %v", msg.err)
		}

	case loadIgnoredMsg:
		if msg.err != nil {
			slog.Warn("tab3: loadIgnored error", "err", msg.err)
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.entries = msg.entries
		m.sel = nil
		if len(m.entries) == 0 {
			m.status = "No ignored files"
		} else {
			m.status = fmt.Sprintf("%d ignored", len(m.entries))
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}

	case libFileDoneMsg:
		// 1 ignored a file – reload so it shows up here
		if msg.isIgnore && msg.err == nil {
			return m, loadIgnoredCmd(m.lib)
		}

	case ignoredTopicsLoadedMsg:
		if msg.err != nil {
			slog.Warn("tab3: loadIgnoredTopics error", "err", msg.err)
			m.dialog = ignoredDialog{}
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		if len(msg.topics) == 0 {
			m.dialog = ignoredDialog{}
			m.status = "No topics – create a topic in tab 1 first"
			return m, nil
		}
		m.dialog = ignoredDialog{
			mode:       ignoredDialogCopy,
			filename:   msg.filename,
			sourcePath: msg.sourcePath,
			entries:    msg.entries,
			topics:     msg.topics,
		}

	case ignoredFileDoneMsg:
		if msg.err != nil {
			slog.Warn("tab3: ignoredFile action error", "filename", msg.filename, "topic", msg.topicName, "err", msg.err)
			if m.copying {
				m.copying = false
				m.status = fmt.Sprintf("Error: %v", msg.err)
			} else {
				m.dialog.errMsg = fmt.Sprintf("Error: %v", msg.err)
			}
			return m, nil
		}
		m.copying = false
		m.dialog = ignoredDialog{}
		m.sel = nil
		for i, e := range m.entries {
			if e.Name == msg.filename {
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				break
			}
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		if len(m.entries) == 0 {
			m.status = "No ignored files"
		} else {
			m.status = fmt.Sprintf("%d ignored", len(m.entries))
		}

	case batchIgnoredDoneMsg:
		if msg.errMsg != "" {
			slog.Warn("tab3: batchIgnored partial/full error", "topic", msg.topicName, "removed", len(msg.removed), "err", msg.errMsg)
		}
		if msg.errMsg != "" && len(msg.removed) == 0 {
			if m.copying {
				m.copying = false
				m.status = fmt.Sprintf("Error: %s", msg.errMsg)
			} else {
				m.dialog.errMsg = msg.errMsg
			}
			return m, nil
		}
		m.copying = false
		m.dialog = ignoredDialog{}
		m.sel = nil
		removed := make(map[string]bool, len(msg.removed))
		for _, e := range msg.removed {
			removed[e.Name] = true
		}
		var remaining []storage.IgnoredEntry
		for _, e := range m.entries {
			if !removed[e.Name] {
				remaining = append(remaining, e)
			}
		}
		m.entries = remaining
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		if msg.errMsg != "" {
			m.status = fmt.Sprintf("Error: %s", msg.errMsg)
		} else if len(m.entries) == 0 {
			m.status = "No ignored files"
		} else {
			m.status = fmt.Sprintf("%d ignored", len(m.entries))
		}

	case renameDoneMsg:
		if msg.err != nil {
			slog.Warn("tab3: renameFile error", "filename", msg.filename, "err", msg.err)
			m.rename.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.rename = renameDialog{}
		// Update the in-memory display name on the matching entry.
		for i := range m.entries {
			if m.entries[i].Name == msg.filename {
				m.entries[i].DisplayName = msg.displayName
				if msg.displayName == msg.filename {
					m.entries[i].DisplayName = ""
				}
				break
			}
		}

	case tea.KeyMsg:
		if m.rename.active {
			return m.handleRenameKey(msg)
		}
		switch m.dialog.mode {
		case ignoredDialogDelete:
			return m.handleDeleteDialogKey(msg)
		case ignoredDialogCopy:
			return m.handleCopyDialogKey(msg)
		default:
			return m.handleNormalKey(msg)
		}
	}
	return m, nil
}

func (m ignoredModel) view() string {
	if m.lib == nil {
		return styleDim.Render("No library configured.") + "\n"
	}
	listHeight := m.height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	start := m.cursor - listHeight/2
	if start < 0 {
		start = 0
	}
	if start+listHeight > len(m.entries) {
		start = max(0, len(m.entries)-listHeight)
	}
	var out string
	for i, linesLeft := start, listHeight; i < len(m.entries) && linesLeft > 0; i++ {
		e := m.entries[i]
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		availW := m.width - 5 - lipgloss.Width(prefix)
		if i == m.cursor || m.sel[i] {
			label := formatLabelAligned(e.Name, e.DisplayName, availW, "")
			var line string
			if e.SourcePath == "" {
				line = prefix + styleDim.Render(label+"  (no file)")
			} else if m.ps.isPlaying(e.SourcePath) {
				line = "▶ " + label
			} else if _, err := os.Stat(e.SourcePath); err != nil {
				line = prefix + styleDim.Render(label+"  (device not mounted)")
			} else {
				line = prefix + label
			}
			if i == m.cursor {
				out += styleSelected.Render(line) + "\n"
			} else {
				out += styleMultiSel.Render(line) + "\n"
			}
		} else {
			label := formatLabelAlignedStyled(e.Name, e.DisplayName, availW, "")
			var line string
			if e.SourcePath == "" {
				line = prefix + label + styleDim.Render("  (no file)")
			} else if m.ps.isPlaying(e.SourcePath) {
				line = "▶ " + label
			} else if _, err := os.Stat(e.SourcePath); err != nil {
				line = prefix + label + styleDim.Render("  (device not mounted)")
			} else {
				line = prefix + label
			}
			out += line + "\n"
		}
		linesLeft--
	}
	return out
}

func (m ignoredModel) overlayContent() string {
	if m.dialog.mode != ignoredDialogNone {
		return m.renderDialog()
	}
	if m.rename.active {
		return m.renderRenameDialog()
	}
	return ""
}

func (m ignoredModel) renderDialog() string {
	var body string
	switch m.dialog.mode {
	case ignoredDialogDelete:
		var label string
		if len(m.dialog.entries) > 1 {
			label = fmt.Sprintf("%d marks", len(m.dialog.entries))
		} else {
			label = m.dialog.filename
		}
		body = fmt.Sprintf("Remove mark(s)?\n\n%s\n\n%s\n\n%s",
			styleDim.Render(label),
			"The file(s) will reappear in tab 2.",
			styleDim.Render("j / Enter confirm  •  n / Esc cancel"),
		)
	case ignoredDialogCopy:
		var label string
		if len(m.dialog.entries) > 1 {
			label = fmt.Sprintf("%d files", len(m.dialog.entries))
		} else {
			label = m.dialog.filename
		}
		body = fmt.Sprintf("Copy to which topic?\n\n%s\n\n",
			styleDim.Render(label))
		for i, t := range m.dialog.topics {
			line := "  " + t
			if i == m.dialog.cursor {
				body += styleSelected.Render(line) + "\n"
			} else {
				body += line + "\n"
			}
		}
		body += "\n" + styleDim.Render("↑/↓ select  •  Enter copy  •  Esc cancel")
	}
	if m.dialog.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.dialog.errMsg)
	}
	return styleDialog.Render(body)
}

func (m ignoredModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return styleDialog.Render(body)
}

// ── root model ────────────────────────────────────────────────────────────────

type rootModel struct {
	active      tab
	library     libraryModel
	imports     importModel
	ignored     ignoredModel
	height      int
	width       int
	showHelp    bool
	ffmpegAvail bool
}

func newRootModel(lib *storage.Library, ch <-chan []importer.Entry, stateCh <-chan importer.State, ffmpegAvail bool, tc *transcriber.Client) rootModel {
	return rootModel{
		active:      tabLibrary,
		library:     newLibraryModel(lib, tc),
		imports:     newImportModel(lib, ch, stateCh),
		ignored:     newIgnoredModel(lib),
		ffmpegAvail: ffmpegAvail,
	}
}

func (m rootModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.library.Init(), m.imports.Init(), m.ignored.Init()}
	if m.ffmpegAvail && m.library.lib != nil {
		cmds = append(cmds, scanConvertCmd(m.library.lib))
	}
	return tea.Batch(cmds...)
}

// scanConvertCmd walks all topics in lib and converts any WAV files that do
// not yet have a corresponding MP3. Runs in the background via tea.Cmd.
func scanConvertCmd(lib *storage.Library) tea.Cmd {
	return func() tea.Msg {
		topics, err := lib.Topics()
		if err != nil {
			slog.Warn("converter: scan: could not list topics", "err", err)
			return convScanDoneMsg{}
		}
		var count int
		for _, t := range topics {
			files, err := lib.TopicFiles(t.Name)
			if err != nil {
				continue
			}
			for _, f := range files {
				if !strings.EqualFold(filepath.Ext(f), ".wav") {
					continue
				}
				wavPath := lib.FilePath(t.Name, f)
				mp3Path := wavPath[:len(wavPath)-len(filepath.Ext(wavPath))] + ".mp3"
				if _, statErr := os.Stat(mp3Path); statErr == nil {
					// MP3 already exists — just remove the WAV if still present.
					if rmErr := os.Remove(wavPath); rmErr != nil && !os.IsNotExist(rmErr) {
						slog.Warn("converter: removing WAV failed", "path", wavPath, "err", rmErr)
					}
					continue
				}
				if convErr := converter.ConvertWAVToMP3(wavPath); convErr != nil {
					slog.Warn("converter: scan conversion failed", "path", wavPath, "err", convErr)
					continue
				}
				mp3Name := f[:len(f)-len(filepath.Ext(f))] + ".mp3"
				lib.StoreMP3Name(f, mp3Name)
				if rmErr := os.Remove(wavPath); rmErr != nil {
					slog.Warn("converter: removing WAV failed", "path", wavPath, "err", rmErr)
				}
				count++
			}
		}
		return convScanDoneMsg{count: count}
	}
}

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		inner := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - headerHeight - footerHeightLib}
		lib, c1 := m.library.update(inner)
		imp, c2 := m.imports.update(inner)
		ign, c3 := m.ignored.update(inner)
		m.library = lib
		m.imports = imp
		m.ignored = ign
		return m, tea.Batch(c1, c2, c3)

	case tea.KeyMsg:
		// Forward everything to active tab when a tab dialog is open.
		if m.active == tabLibrary && (m.library.dialog.active || m.library.confirm.mode != libConfirmNone || m.library.move.active || m.library.topicRename.active || m.library.rename.active) {
			lib, cmd := m.library.update(msg)
			m.library = lib
			return m, cmd
		}
		if m.active == tabImport && (m.imports.dialog.mode != importDialogNone || m.imports.rename.active) {
			imp, cmd := m.imports.update(msg)
			m.imports = imp
			return m, cmd
		}
		if m.active == tabIgnored && (m.ignored.dialog.mode != ignoredDialogNone || m.ignored.rename.active) {
			ign, cmd := m.ignored.update(msg)
			m.ignored = ign
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q", "Q":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			return m, tea.Quit
		case "h", "H":
			m.showHelp = !m.showHelp
			return m, nil
		case "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
		case "1":
			m.imports.ps.stop()
			m.ignored.ps.stop()
			m.active = tabLibrary
			return m, nil
		case "2":
			m.library.ps.stop()
			m.ignored.ps.stop()
			m.active = tabImport
			return m, nil
		case "3":
			m.library.ps.stop()
			m.imports.ps.stop()
			m.active = tabIgnored
			return m, loadIgnoredCmd(m.ignored.lib)
		}
		// When help overlay is open, swallow all remaining keys.
		if m.showHelp {
			return m, nil
		}
		// route key to the active tab only
		switch m.active {
		case tabLibrary:
			lib, cmd := m.library.update(msg)
			m.library = lib
			return m, cmd
		case tabImport:
			imp, cmd := m.imports.update(msg)
			m.imports = imp
			return m, cmd
		case tabIgnored:
			ign, cmd := m.ignored.update(msg)
			m.ignored = ign
			return m, cmd
		}
	case playbackDoneMsg, playbackTickMsg:
		slog.Debug("rootModel: routing playback msg", "active", m.active, "type", fmt.Sprintf("%T", msg))
		switch m.active {
		case tabLibrary:
			lib, cmd := m.library.update(msg)
			m.library = lib
			return m, cmd
		case tabImport:
			imp, cmd := m.imports.update(msg)
			m.imports = imp
			return m, cmd
		case tabIgnored:
			ign, cmd := m.ignored.update(msg)
			m.ignored = ign
			return m, cmd
		}
		return m, nil
	case convScanDoneMsg:
		if msg.count > 0 {
			m.library.status = fmt.Sprintf("Converted %d WAV file(s) to MP3", msg.count)
		}
		return m, nil
	case transcribeUploadedMsg, transcribePollMsg, transcriptWrittenMsg:
		lib, cmd := m.library.update(msg)
		m.library = lib
		return m, cmd
	}
	// data messages (async loads, entries) reach all sub-models
	lib, c1 := m.library.update(msg)
	imp, c2 := m.imports.update(msg)
	ign, c3 := m.ignored.update(msg)
	m.library = lib
	m.imports = imp
	m.ignored = ign
	return m, tea.Batch(c1, c2, c3)
}

// Layout constants — must match the rows rendered in View().
const (
	headerHeight    = 2 // tabBar + blank line
	footerHeight    = 2 // playback line + h-for-help line
	footerHeightLib = 5 // footerHeight + 3 transcript lines
)

func (m rootModel) View() string {
	libTab := styleTab.Render("1 Library")
	impTab := styleTab.Render("2 Import")
	ignTab := styleTab.Render("3 Ignored")
	switch m.active {
	case tabLibrary:
		libTab = styleActiveTab.Render("1 Library")
	case tabImport:
		impTab = styleActiveTab.Render("2 Import")
	case tabIgnored:
		ignTab = styleActiveTab.Render("3 Ignored")
	}
	var status string
	switch m.active {
	case tabLibrary:
		status = m.library.status
	case tabImport:
		if len(m.imports.sel) > 0 {
			status = fmt.Sprintf("%d selected  •  ", len(m.imports.sel)) + m.imports.status
		} else {
			status = m.imports.status
		}
	case tabIgnored:
		if len(m.ignored.sel) > 0 {
			status = fmt.Sprintf("%d selected  •  ", len(m.ignored.sel)) + m.ignored.status
		} else {
			status = m.ignored.status
		}
	}
	tabBar := libTab + "  " + impTab + "  " + ignTab + "  " + styleDim.Render(status)

	var content string
	switch m.active {
	case tabLibrary:
		content = m.library.view()
	case tabImport:
		content = m.imports.view()
	case tabIgnored:
		content = m.ignored.view()
	}

	helpLine := "h for help  •  q quit"
	if !m.ffmpegAvail {
		helpLine += "  •  mp3 conversion disabled - ffmpeg not installed"
	}

	// Transcript block: 3 fixed lines in Tab 1 with word-wrap.
	var transcriptBlock string
	if m.active == tabLibrary {
		transcriptText := ""
		if m.library.activeTranscript != nil {
			seg := m.library.activeTranscript.At(m.library.ps.playPos)
			if seg != nil {
				transcriptText = seg.Text
			}
		}
		wrapped := wrapWords(transcriptText, m.width, 3)
		transcriptBlock = styleDim.Render(wrapped)
	}

	var footer string
	if transcriptBlock != "" {
		footer = transcriptBlock + "\n" + m.playbackLine() + "\n" + styleDim.Render(helpLine)
	} else {
		footer = m.playbackLine() + "\n" + styleDim.Render(helpLine)
	}

	effFooter := footerHeight
	if m.active == tabLibrary {
		effFooter = footerHeightLib
	}
	contentHeight := m.height - headerHeight - effFooter
	if contentHeight < 1 {
		contentHeight = 1
	}
	pinnedContent := lipgloss.NewStyle().Height(contentHeight).Render(content)
	out := tabBar + "\n\n" + pinnedContent + "\n" + footer
	if m.showHelp {
		out = m.renderHelpOverlay(out)
	} else {
		var dlg string
		switch m.active {
		case tabLibrary:
			dlg = m.library.overlayContent()
		case tabImport:
			dlg = m.imports.overlayContent()
		case tabIgnored:
			dlg = m.ignored.overlayContent()
		}
		if dlg != "" {
			out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dlg)
		}
	}
	return out
}

// playbackLine returns a one-line playback status for the active tab's player,
// or an empty string when nothing is playing.
func (m rootModel) playbackLine() string {
	var ps *playerState
	switch m.active {
	case tabLibrary:
		ps = &m.library.ps
	case tabImport:
		ps = &m.imports.ps
	case tabIgnored:
		ps = &m.ignored.ps
	}
	if ps == nil || ps.playingPath == "" {
		return ""
	}
	icon := "▶"
	if ps.paused {
		icon = "⏸"
	}
	line := stylePlayIcon.Render(icon) + "  " + ps.timeLabel()
	if bar := ps.progressBar(20); bar != "" {
		line += "  " + bar
	}
	return styleTitle.Render(line)
}

// helpLines returns the context-sensitive help text for the current tab.
func (m rootModel) helpLines() []string {
	global := []string{
		"Global",
		"  1 / 2 / 3     switch tab",
		"  h             toggle this help",
		"  q  /  Ctrl+C  quit",
		"",
	}
	playback := []string{
		"Playback (all tabs)",
		"  Space          pause / resume",
		"  ←  /  →        seek ±5 s",
		"  Shift+←/→      seek ±30 s",
		"  Ctrl+←/→       seek ±1 min",
		"",
	}
	var specific []string
	switch m.active {
	case tabLibrary:
		specific = []string{
			"Library (Tab 1)",
			"  ↑ / ↓         move cursor",
			"  Enter         expand / collapse topic",
			"  Space         play / pause file",
			"  i             ignore file",
			"  Del           delete file  /  delete topic",
			"  m             move file to topic",
			"  r             change description",
			"  t             manage tag keywords",
			"  n             new topic",
			"  R             rename topic",
		}
	case tabImport:
		specific = []string{
			"Import (Tab 2)",
			"  ↑ / ↓         move cursor",
			"  Shift+↑/↓     extend selection",
			"  a / d         select all / none",
			"  Space         play / pause file",
			"  i             ignore selected",
			"  c             copy to topic",
			"  r             change description",
		}
	case tabIgnored:
		specific = []string{
			"Ignored (Tab 3)",
			"  ↑ / ↓         move cursor",
			"  Shift+↑/↓     extend selection",
			"  a / d         select all / none",
			"  Space         play / pause file",
			"  c             copy to topic",
			"  Del           unmark (remove from ignored)",
			"  r             change description",
		}
	}
	lines := append(global, playback...)
	lines = append(lines, specific...)
	lines = append(lines, "", "  Esc / h  close")
	return lines
}

// renderHelpOverlay places a centered help box on top of the rendered screen.
func (m rootModel) renderHelpOverlay(screen string) string {
	lines := m.helpLines()

	// Find the longest line to determine box width.
	maxLen := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > maxLen {
			maxLen = w
		}
	}
	boxWidth := maxLen + 6 // 2 padding + 2 border on each side
	if boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}

	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
