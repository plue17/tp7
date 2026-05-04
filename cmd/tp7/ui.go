package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp7/internal/importer"
	"tp7/internal/playback"
	"tp7/internal/storage"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleSelected  = lipgloss.NewStyle().Reverse(true)
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleTab       = lipgloss.NewStyle().Padding(0, 1)
	styleActiveTab = lipgloss.NewStyle().Padding(0, 1).Bold(true).Underline(true)
	styleDialog    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	styleDialogErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleMultiSel  = lipgloss.NewStyle().Bold(true)
	stylePlayIcon  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")) // bright green
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

type playbackDoneMsg struct{ err error }
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

// preferredName returns the display name from the map when present,
// then falls back to the timestamp parsed from the filename, then the raw filename.
func preferredName(filename string, displayNames map[string]string) string {
	return formatLabel(filename, displayNames[filename])
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
			return playbackDoneMsg{err: err}
		}
		err = <-done
		slog.Debug("playCmd: done", "path", path, "err", err)
		return playbackDoneMsg{err: err}
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
func (ps *playerState) onDone() {
	slog.Debug("playerState.onDone", "path", ps.playingPath)
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
		dn := lib.LoadDisplayNames(files)
		durs := lib.LoadDurations(topicName, files)
		return loadFilesMsg{topicName: topicName, files: files, displayNames: dn, durations: durs}
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
			}
		}
		return batchIgnoredDoneMsg{topicName: topicName, removed: removed, errMsg: strings.Join(errs, "\n")}
	}
}

// ── library confirm dialog ──────────────────────────────────────────────────

type libConfirmMode int

const (
	libConfirmNone   libConfirmMode = iota
	libConfirmDelete                // remove from topic + unmark
	libConfirmIgnore                // remove from topic + mark as ignored
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

func renameTopicCmd(lib *storage.Library, oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		err := lib.RenameTopic(oldName, newName)
		return renameTopicDoneMsg{oldName: oldName, newName: newName, err: err}
	}
}

// ── library model (1) ────────────────────────────────────────────────────────

type topicNode struct {
	name         string
	expanded     bool
	files        []string
	displayNames map[string]string
	durations    map[string]time.Duration
	loaded       bool
}

// flatRow is a single visible line in the library tree.
type flatRow struct {
	isTopic  bool
	topicIdx int
	fileIdx  int // only meaningful when !isTopic
}

type libraryModel struct {
	lib         *storage.Library
	topics      []topicNode
	cursor      int
	height      int
	width       int
	status      string
	dialog      newTopicDialog
	confirm     libConfirmDialog
	move        libMoveDialog
	topicRename topicRenameDialog
	rename      renameDialog
	ps          playerState
}

func newLibraryModel(lib *storage.Library) libraryModel {
	return libraryModel{lib: lib, status: "Loading library…", ps: newPlayerState()}
}

func (m libraryModel) Init() tea.Cmd {
	if m.lib == nil {
		return nil
	}
	return loadTopicsCmd(m.lib)
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
				if cmd := m.ps.toggle(path); cmd != nil {
					return m, cmd
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
	case "m":
		if m.lib != nil && m.cursor < len(rows) && !rows[m.cursor].isTopic {
			row := rows[m.cursor]
			return m, loadMoveTopicsCmd(m.lib, m.topics[row.topicIdx].name, m.topics[row.topicIdx].files[row.fileIdx])
		}
	case "i", "delete":
		if m.lib != nil && m.cursor < len(rows) && !rows[m.cursor].isTopic {
			row := rows[m.cursor]
			mode := libConfirmIgnore
			if msg.String() == "delete" {
				mode = libConfirmDelete
			}
			m.confirm = libConfirmDialog{
				mode:      mode,
				topicName: m.topics[row.topicIdx].name,
				fileName:  m.topics[row.topicIdx].files[row.fileIdx],
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

func (m libraryModel) update(msg tea.Msg) (libraryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case loadTopicsMsg:
		if msg.err != nil {
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
			m.status = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				m.topics[i].files = msg.files
				m.topics[i].displayNames = msg.displayNames
				m.topics[i].durations = msg.durations
				m.topics[i].loaded = true
				break
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
			m.confirm.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.confirm = libConfirmDialog{}
		return m.refreshTopic(msg.topicName)

	case createTopicMsg:
		if msg.err != nil {
			m.dialog.errMsg = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.dialog = newTopicDialog{} // close
		return m, loadTopicsCmd(m.lib)

	case libMoveTopicsLoadedMsg:
		if msg.err != nil {
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

	case renameTopicDoneMsg:
		if msg.err != nil {
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

	case playbackTickMsg:
		if m.ps.onTick() {
			return m, tickCmd()
		}

	case playbackDoneMsg:
		m.ps.onDone()
		if msg.err != nil && msg.err != playback.ErrStopped {
			m.status = fmt.Sprintf("Playback error: %v", msg.err)
		}

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
	now := time.Now()
	// Pre-compute which file rows start a new time group within their topic.
	lastGroupByTopic := map[int]string{}
	groupStarters := map[int]string{} // row index → group label
	for idx, row := range rows {
		if row.isTopic {
			continue
		}
		f := m.topics[row.topicIdx].files[row.fileIdx]
		grp := ""
		if t, ok := storage.ParseFilenameTime(f); ok {
			grp = timeGroup(t, now)
		}
		if grp != "" && grp != lastGroupByTopic[row.topicIdx] {
			groupStarters[idx] = grp
			lastGroupByTopic[row.topicIdx] = grp
		}
	}
	for i, linesLeft := start, listHeight; i < len(rows) && linesLeft > 0; i++ {
		row := rows[i]
		// Emit group header if this file row starts a new group.
		if !row.isTopic {
			if grp, ok := groupStarters[i]; ok {
				out += groupHeader(grp)
				linesLeft--
				if linesLeft == 0 {
					break
				}
			}
		}
		var line string
		if row.isTopic {
			t := m.topics[row.topicIdx]
			arrow := "▶"
			if t.expanded {
				arrow = "▼"
			}
			line = fmt.Sprintf("%s %s", arrow, t.name)
		} else {
			f := m.topics[row.topicIdx].files[row.fileIdx]
			topicName := m.topics[row.topicIdx].name
			path := m.lib.FilePath(topicName, f)
			prefix := "  └ "
			if m.ps.isPlaying(path) {
				prefix = "  ▶ "
			}
			availWidth := m.width - 5 - lipgloss.Width(prefix)
			durStr := formatDuration(m.topics[row.topicIdx].durations[f])
			label := formatLabelAligned(f, m.topics[row.topicIdx].displayNames[f], availWidth, durStr)
			line = prefix + label
		}
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if !row.isTopic {
			out += styleDim.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
		linesLeft--
	}
	if m.dialog.active {
		out += m.renderDialog()
	}
	if m.confirm.mode != libConfirmNone {
		out += m.renderConfirmDialog()
	}
	if m.move.active {
		out += m.renderMoveDialog()
	}
	if m.topicRename.active {
		out += m.renderTopicRenameDialog()
	}
	if m.rename.active {
		out += m.renderRenameDialog()
	}
	return out
}

func (m libraryModel) renderDialog() string {
	prompt := "New topic: " + m.dialog.input + "█"
	var body string
	if m.dialog.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.dialog.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return "\n" + styleDialog.Render(body) + "\n"
}
func (m libraryModel) renderConfirmDialog() string {
	var action string
	switch m.confirm.mode {
	case libConfirmDelete:
		action = "Remove file from topic and delete mark?"
	case libConfirmIgnore:
		action = "Remove file from topic and ignore?"
	}
	body := fmt.Sprintf("%s\n\n%s\n\n%s",
		action,
		styleDim.Render(m.confirm.topicName+"/"+m.confirm.fileName),
		styleDim.Render("j / Enter confirm  •  n / Esc cancel"),
	)
	if m.confirm.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.confirm.errMsg)
	}
	return "\n" + styleDialog.Render(body) + "\n"
}

func (m libraryModel) renderTopicRenameDialog() string {
	prompt := "Rename topic: " + m.topicRename.input + "█"
	var body string
	if m.topicRename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.topicRename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return "\n" + styleDialog.Render(body) + "\n"
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
	return "\n" + styleDialog.Render(body) + "\n"
}

func (m libraryModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return "\n" + styleDialog.Render(body) + "\n"
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
	lib          *storage.Library
	entries      []importer.Entry
	cursor       int
	sel          map[int]bool
	anchor       int
	height       int
	width        int
	status       string
	entriesCh    <-chan []importer.Entry
	dialog       importDialog
	copying      bool
	ps           playerState
	rename       renameDialog
	displayNames map[string]string
}

func newImportModel(lib *storage.Library, ch <-chan []importer.Entry) importModel {
	return importModel{lib: lib, entriesCh: ch, status: "Waiting for TP-7…", ps: newPlayerState()}
}

func (m importModel) Init() tea.Cmd {
	return m.awaitEntries()
}

func (m importModel) awaitEntries() tea.Cmd {
	ch := m.entriesCh
	return func() tea.Msg { return entriesMsg(<-ch) }
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
		m.ps.onDone()
		if msg.err != nil && msg.err != playback.ErrStopped {
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

	case importTopicsLoadedMsg:
		if msg.err != nil {
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
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				break
			}
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.status = fmt.Sprintf("%d new recording(s)", len(m.entries))

	case batchImportDoneMsg:
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
	now := time.Now()
	lastGroup := ""
	for i, linesLeft := start, listHeight; i < len(m.entries) && linesLeft > 0; i++ {
		e := m.entries[i]
		if t, ok := storage.ParseFilenameTime(e.Name); ok {
			if grp := timeGroup(t, now); grp != lastGroup {
				out += groupHeader(grp)
				lastGroup = grp
				linesLeft--
				if linesLeft == 0 {
					break
				}
			}
		}
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		sizeStr := formatSize(e.Size)
		const sizeColWidth = 9 // enough for "1023.9 MB"
		label := formatLabelAligned(e.Name, m.displayNames[e.Name], m.width-5-lipgloss.Width(prefix)-2-sizeColWidth, "")
		var line string
		if m.ps.isPlaying(e.Path) {
			prefix = "▶ "
			line = prefix + label + "  " + fmt.Sprintf("%*s", sizeColWidth, sizeStr)
		} else {
			line = prefix + label + "  " + fmt.Sprintf("%*s", sizeColWidth, sizeStr)
		}
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if m.sel[i] {
			out += styleMultiSel.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
		linesLeft--
	}
	if m.dialog.mode != importDialogNone {
		out += m.renderDialog()
	}
	if m.rename.active {
		out += m.renderRenameDialog()
	}
	return out
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
	return "\n" + styleDialog.Render(body) + "\n"
}

func (m importModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return "\n" + styleDialog.Render(body) + "\n"
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
		m.ps.onDone()
		if msg.err != nil && msg.err != playback.ErrStopped {
			m.status = fmt.Sprintf("Playback error: %v", msg.err)
		}

	case loadIgnoredMsg:
		if msg.err != nil {
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
	now := time.Now()
	lastGroup := ""
	for i, linesLeft := start, listHeight; i < len(m.entries) && linesLeft > 0; i++ {
		e := m.entries[i]
		if t, ok := storage.ParseFilenameTime(e.Name); ok {
			if grp := timeGroup(t, now); grp != lastGroup {
				out += groupHeader(grp)
				lastGroup = grp
				linesLeft--
				if linesLeft == 0 {
					break
				}
			}
		}
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		label := formatLabelAligned(e.Name, e.DisplayName, m.width-5-lipgloss.Width(prefix), "")
		var line string
		if e.SourcePath == "" {
			line = prefix + styleDim.Render(label+"  (no file)")
		} else if m.ps.isPlaying(e.SourcePath) {
			prefix = "▶ "
			line = prefix + label
		} else if _, err := os.Stat(e.SourcePath); err != nil {
			line = prefix + styleDim.Render(label+"  (device not mounted)")
		} else {
			line = prefix + label
		}
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if m.sel[i] {
			out += styleMultiSel.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
		linesLeft--
	}
	if m.dialog.mode != ignoredDialogNone {
		out += m.renderDialog()
	}
	if m.rename.active {
		out += m.renderRenameDialog()
	}
	return out
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
	return "\n" + styleDialog.Render(body) + "\n"
}

func (m ignoredModel) renderRenameDialog() string {
	prompt := "Rename: " + m.rename.input + "█"
	var body string
	if m.rename.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.rename.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter confirm  •  Esc cancel")
	}
	return "\n" + styleDialog.Render(body) + "\n"
}

// ── root model ────────────────────────────────────────────────────────────────

type rootModel struct {
	active   tab
	library  libraryModel
	imports  importModel
	ignored  ignoredModel
	height   int
	width    int
	showHelp bool
}

func newRootModel(lib *storage.Library, ch <-chan []importer.Entry) rootModel {
	return rootModel{
		active:  tabLibrary,
		library: newLibraryModel(lib),
		imports: newImportModel(lib, ch),
		ignored: newIgnoredModel(lib),
	}
}

func (m rootModel) Init() tea.Cmd {
	return tea.Batch(m.library.Init(), m.imports.Init(), m.ignored.Init())
}

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		inner := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - headerHeight - footerHeight}
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
	headerHeight = 2 // tabBar + blank line
	footerHeight = 2 // playback line + h-for-help line
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

	footer := m.playbackLine() + "\n" + styleDim.Render("h for help  •  q quit")

	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	pinnedContent := lipgloss.NewStyle().Height(contentHeight).Render(content)
	out := tabBar + "\n\n" + pinnedContent + "\n" + footer
	if m.showHelp {
		out = m.renderHelpOverlay(out)
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
			"  Del           delete file",
			"  m             move file to topic",
			"  r             rename file",
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
			"  r             rename file",
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
			"  r             rename file",
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
