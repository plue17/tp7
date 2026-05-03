package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp7/internal/importer"
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

type loadTopicsMsg struct {
	names []string
	err   error
}

type loadFilesMsg struct {
	topicName string
	files     []string
	err       error
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
	removed   []string // entry names removed from F2
	errMsg    string
}

type batchIgnoredDoneMsg struct {
	topicName string                 // non-empty when copied to a topic
	removed   []storage.IgnoredEntry // entries removed from F3
	errMsg    string
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
		err := lib.RemoveFromTopic(topicName, filename)
		if err == nil {
			err = lib.MarkFile(filename, storage.ActionIgnore, "", "", 0)
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
				err:       fmt.Errorf("Quellpfad unbekannt – Datei über F2 kopieren"),
			}
		}
		err := lib.CopyToTopic(topicName, sourcePath)
		return ignoredFileDoneMsg{filename: filename, topicName: topicName, err: err}
	}
}

// ── commands ──────────────────────────────────────────────────────────────────

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
		return loadFilesMsg{topicName: topicName, files: files, err: err}
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
				errs = append(errs, fmt.Sprintf("%s: Quellpfad unbekannt", e.Name))
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

// ── library model (F1) ────────────────────────────────────────────────────────

type topicNode struct {
	name     string
	expanded bool
	files    []string
	loaded   bool
}

// flatRow is a single visible line in the library tree.
type flatRow struct {
	isTopic  bool
	topicIdx int
	fileIdx  int // only meaningful when !isTopic
}

type libraryModel struct {
	lib     *storage.Library
	topics  []topicNode
	cursor  int
	height  int
	width   int
	status  string
	dialog  newTopicDialog
	confirm libConfirmDialog
}

func newLibraryModel(lib *storage.Library) libraryModel {
	return libraryModel{lib: lib, status: "Library wird geladen…"}
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

func (m libraryModel) update(msg tea.Msg) (libraryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case loadTopicsMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Fehler: %v", msg.err)
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
			m.status = "Keine Topics vorhanden"
		} else {
			m.status = fmt.Sprintf("%d Topic(s)", len(m.topics))
		}

	case loadFilesMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				m.topics[i].files = msg.files
				m.topics[i].loaded = true
				break
			}
		}

	case importActionDoneMsg:
		if msg.topicName == "" {
			return m, nil // ignore action was not a copy
		}
		var cmds []tea.Cmd
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].expanded {
					cmds = append(cmds, loadFilesCmd(m.lib, msg.topicName))
				} else {
					// mark stale so next expand fetches fresh list
					m.topics[i].loaded = false
				}
				break
			}
		}
		return m, tea.Batch(cmds...)

	case ignoredFileDoneMsg:
		// F3 copied a file to a topic – refresh that topic in F1
		if msg.topicName == "" || msg.err != nil {
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].expanded {
					return m, loadFilesCmd(m.lib, msg.topicName)
				}
				m.topics[i].loaded = false
				break
			}
		}

	case batchImportDoneMsg:
		// F2 batch copy – refresh the destination topic in F1
		if msg.topicName == "" {
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].expanded {
					return m, loadFilesCmd(m.lib, msg.topicName)
				}
				m.topics[i].loaded = false
				break
			}
		}

	case batchIgnoredDoneMsg:
		// F3 batch copy – refresh the destination topic in F1
		if msg.topicName == "" {
			return m, nil
		}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].expanded {
					return m, loadFilesCmd(m.lib, msg.topicName)
				}
				m.topics[i].loaded = false
				break
			}
		}

	case libFileDoneMsg:
		if msg.err != nil {
			m.confirm.errMsg = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		m.confirm = libConfirmDialog{}
		for i := range m.topics {
			if m.topics[i].name == msg.topicName {
				if m.topics[i].expanded {
					return m, loadFilesCmd(m.lib, msg.topicName)
				}
				m.topics[i].loaded = false
				break
			}
		}

	case createTopicMsg:
		if msg.err != nil {
			m.dialog.errMsg = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		m.dialog = newTopicDialog{} // close
		return m, loadTopicsCmd(m.lib)

	case tea.KeyMsg:
		// ── dialog mode ──
		if m.dialog.active {
			switch msg.String() {
			case "esc":
				m.dialog = newTopicDialog{}
			case "enter":
				name := strings.TrimSpace(m.dialog.input)
				if name == "" {
					m.dialog.errMsg = "Name darf nicht leer sein"
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

		// ── confirm dialog mode ──
		if m.confirm.mode != libConfirmNone {
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

		// ── normal mode ──
		rows := m.buildRows()
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(rows)-1 {
				m.cursor++
			}
		case "enter", " ":
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
		case "n":
			if m.lib != nil {
				m.dialog = newTopicDialog{active: true}
			}
		case "r":
			return m, loadTopicsCmd(m.lib)
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
	}
	return m, nil
}

func (m libraryModel) view() string {
	if m.lib == nil {
		return styleDim.Render("Keine Library konfiguriert.") + "\n"
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
	for i := start; i < start+listHeight && i < len(rows); i++ {
		row := rows[i]
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
			line = fmt.Sprintf("  └ %s", f)
		}
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if !row.isTopic {
			out += styleDim.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
	}
	if m.dialog.active {
		out += m.renderDialog()
	}
	if m.confirm.mode != libConfirmNone {
		out += m.renderConfirmDialog()
	}
	return out
}

func (m libraryModel) renderDialog() string {
	prompt := "Neues Topic: " + m.dialog.input + "█"
	var body string
	if m.dialog.errMsg != "" {
		body = prompt + "\n" + styleDialogErr.Render(m.dialog.errMsg)
	} else {
		body = prompt + "\n" + styleDim.Render("Enter bestätigen • Esc abbrechen")
	}
	return "\n" + styleDialog.Render(body) + "\n"
}
func (m libraryModel) renderConfirmDialog() string {
	var action string
	switch m.confirm.mode {
	case libConfirmDelete:
		action = "Datei aus Topic entfernen und Markierung löschen?"
	case libConfirmIgnore:
		action = "Datei aus Topic entfernen und ignorieren?"
	}
	body := fmt.Sprintf("%s\n\n%s\n\n%s",
		action,
		styleDim.Render(m.confirm.topicName+"/"+m.confirm.fileName),
		styleDim.Render("j / Enter bestätigen  •  n / Esc abbrechen"),
	)
	if m.confirm.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.confirm.errMsg)
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

// ── import model (F2) ─────────────────────────────────────────────────────────

type importModel struct {
	lib       *storage.Library
	entries   []importer.Entry
	cursor    int
	sel       map[int]bool
	anchor    int
	height    int
	width     int
	status    string
	entriesCh <-chan []importer.Entry
	dialog    importDialog
}

func newImportModel(lib *storage.Library, ch <-chan []importer.Entry) importModel {
	return importModel{lib: lib, entriesCh: ch, status: "Warte auf TP-7…"}
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

func (m importModel) update(msg tea.Msg) (importModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
	case entriesMsg:
		m.entries = []importer.Entry(msg)
		m.sel = nil
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.status = fmt.Sprintf("%d neue Aufnahme(n)", len(m.entries))
		return m, m.awaitEntries()

	case importTopicsLoadedMsg:
		if msg.err != nil {
			m.dialog = importDialog{}
			m.status = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		if len(msg.topics) == 0 {
			m.dialog = importDialog{}
			m.status = "Keine Topics vorhanden – erst in F1 ein Topic erstellen"
			return m, nil
		}
		m.dialog = importDialog{
			mode:    importDialogCopy,
			entries: msg.entries,
			topics:  msg.topics,
		}

	case importActionDoneMsg:
		if msg.err != nil {
			m.dialog.errMsg = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		m.dialog = importDialog{}
		m.sel = nil
		// remove the handled entry from the list
		for i, e := range m.entries {
			if e.Name == msg.entryName {
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				break
			}
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.status = fmt.Sprintf("%d neue Aufnahme(n)", len(m.entries))

	case batchImportDoneMsg:
		if msg.errMsg != "" && len(msg.removed) == 0 {
			m.dialog.errMsg = msg.errMsg
			return m, nil
		}
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
			m.status = fmt.Sprintf("Fehler: %s", msg.errMsg)
		} else {
			m.status = fmt.Sprintf("%d neue Aufnahme(n)", len(m.entries))
		}

	case batchIgnoredDoneMsg:
		// F3 batch-unmark: add removed entries back to F2 if they have a source path.
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
			m.status = fmt.Sprintf("%d neue Aufnahme(n)", len(m.entries))
		}

	case ignoredFileDoneMsg:
		// An ignored file was un-marked in F3 — add it back to the import list.
		if msg.err != nil || msg.topicName != "" || msg.sourcePath == "" {
			return m, nil
		}
		for _, e := range m.entries {
			if e.Name == msg.filename {
				return m, nil // already present
			}
		}
		m.entries = append(m.entries, importer.Entry{Name: msg.filename, Path: msg.sourcePath, Size: msg.size})
		m.status = fmt.Sprintf("%d neue Aufnahme(n)", len(m.entries))

	case tea.KeyMsg:
		switch m.dialog.mode {
		case importDialogIgnore:
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

		case importDialogCopy:
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
				m.dialog.errMsg = ""
				if len(m.dialog.entries) == 1 {
					return m, copyEntryCmd(m.lib, m.dialog.entries[0], topic)
				}
				return m, batchCopyCmd(m.lib, m.dialog.entries, topic)
			case "esc":
				m.dialog = importDialog{}
			}
			return m, nil

		default: // importDialogNone
			switch msg.String() {
			case "up", "k":
				m.sel = nil
				if m.cursor > 0 {
					m.cursor--
				}
				m.anchor = m.cursor
			case "down", "j":
				m.sel = nil
				if m.cursor < len(m.entries)-1 {
					m.cursor++
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
			}
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
	for i := start; i < start+listHeight && i < len(m.entries); i++ {
		e := m.entries[i]
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		line := fmt.Sprintf("%s%-38s  %8s", prefix, e.Name, formatSize(e.Size))
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if m.sel[i] {
			out += styleMultiSel.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
	}
	if m.dialog.mode != importDialogNone {
		out += m.renderDialog()
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
			label = fmt.Sprintf("%d Aufnahmen", len(m.dialog.entries))
		}
		body = fmt.Sprintf(
			"Aufnahme(n) ignorieren?\n\n"+
				styleDim.Render("%s")+"\n\n"+
				"Die Datei(en) werden nicht kopiert und in Zukunft\n"+
				"nicht mehr in der Liste erscheinen.\n\n"+
				styleDim.Render("j / Enter bestätigen  •  n / Esc abbrechen"),
			label,
		)
	case importDialogCopy:
		var label string
		if len(m.dialog.entries) == 1 {
			label = m.dialog.entries[0].Name
		} else {
			label = fmt.Sprintf("%d Aufnahmen", len(m.dialog.entries))
		}
		body = fmt.Sprintf("In welches Topic kopieren?\n\n"+styleDim.Render("%s")+"\n\n",
			label)
		for i, t := range m.dialog.topics {
			line := "  " + t
			if i == m.dialog.topicCursor {
				body += styleSelected.Render(line) + "\n"
			} else {
				body += line + "\n"
			}
		}
		body += "\n" + styleDim.Render("↑/↓ wählen  •  Enter kopieren  •  Esc abbrechen")
	}
	if m.dialog.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.dialog.errMsg)
	}
	return "\n" + styleDialog.Render(body) + "\n"
}

// ── ignored model (F3) ───────────────────────────────────────────────────────

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
}

func newIgnoredModel(lib *storage.Library) ignoredModel {
	return ignoredModel{lib: lib, status: "Ignorierte Dateien werden geladen…"}
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

func (m ignoredModel) update(msg tea.Msg) (ignoredModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case loadIgnoredMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		m.entries = msg.entries
		m.sel = nil
		if len(m.entries) == 0 {
			m.status = "Keine ignorierten Dateien"
		} else {
			m.status = fmt.Sprintf("%d ignoriert", len(m.entries))
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}

	case libFileDoneMsg:
		// F1 ignored a file – reload so it shows up here
		if msg.isIgnore && msg.err == nil {
			return m, loadIgnoredCmd(m.lib)
		}

	case ignoredTopicsLoadedMsg:
		if msg.err != nil {
			m.dialog = ignoredDialog{}
			m.status = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
		if len(msg.topics) == 0 {
			m.dialog = ignoredDialog{}
			m.status = "Keine Topics vorhanden – erst in F1 ein Topic erstellen"
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
			m.dialog.errMsg = fmt.Sprintf("Fehler: %v", msg.err)
			return m, nil
		}
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
			m.status = "Keine ignorierten Dateien"
		} else {
			m.status = fmt.Sprintf("%d ignoriert", len(m.entries))
		}

	case batchIgnoredDoneMsg:
		if msg.errMsg != "" && len(msg.removed) == 0 {
			m.dialog.errMsg = msg.errMsg
			return m, nil
		}
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
			m.status = fmt.Sprintf("Fehler: %s", msg.errMsg)
		} else if len(m.entries) == 0 {
			m.status = "Keine ignorierten Dateien"
		} else {
			m.status = fmt.Sprintf("%d ignoriert", len(m.entries))
		}

	case tea.KeyMsg:
		switch m.dialog.mode {
		case ignoredDialogDelete:
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

		case ignoredDialogCopy:
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
				m.dialog.errMsg = ""
				if len(m.dialog.entries) > 1 {
					return m, batchCopyFromIgnoredCmd(m.lib, m.dialog.entries, topic)
				}
				return m, copyFromIgnoredCmd(m.lib, m.dialog.filename, m.dialog.sourcePath, topic)
			case "esc":
				m.dialog = ignoredDialog{}
			}
			return m, nil

		default: // ignoredDialogNone
			switch msg.String() {
			case "up", "k":
				m.sel = nil
				if m.cursor > 0 {
					m.cursor--
				}
				m.anchor = m.cursor
			case "down", "j":
				m.sel = nil
				if m.cursor < len(m.entries)-1 {
					m.cursor++
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
			case "r":
				return m, loadIgnoredCmd(m.lib)
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
					if len(entries) > 1 {
						return m, loadIgnoredBatchTopicsCmd(m.lib, entries)
					}
					e := m.entries[m.cursor]
					return m, loadIgnoredTopicsCmd(m.lib, e.Name, e.SourcePath)
				}
			}
		}
	}
	return m, nil
}

func (m ignoredModel) view() string {
	if m.lib == nil {
		return styleDim.Render("Keine Library konfiguriert.") + "\n"
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
	for i := start; i < start+listHeight && i < len(m.entries); i++ {
		prefix := "  "
		if m.sel[i] {
			prefix = "► "
		}
		line := prefix + m.entries[i].Name
		if i == m.cursor {
			out += styleSelected.Render(line) + "\n"
		} else if m.sel[i] {
			out += styleMultiSel.Render(line) + "\n"
		} else {
			out += line + "\n"
		}
	}
	if m.dialog.mode != ignoredDialogNone {
		out += m.renderDialog()
	}
	return out
}

func (m ignoredModel) renderDialog() string {
	var body string
	switch m.dialog.mode {
	case ignoredDialogDelete:
		var label string
		if len(m.dialog.entries) > 1 {
			label = fmt.Sprintf("%d Markierungen", len(m.dialog.entries))
		} else {
			label = m.dialog.filename
		}
		body = fmt.Sprintf("Markierung(en) entfernen?\n\n%s\n\n%s\n\n%s",
			styleDim.Render(label),
			"Die Datei(en) erscheinen wieder in F2.",
			styleDim.Render("j / Enter bestätigen  •  n / Esc abbrechen"),
		)
	case ignoredDialogCopy:
		var label string
		if len(m.dialog.entries) > 1 {
			label = fmt.Sprintf("%d Dateien", len(m.dialog.entries))
		} else {
			label = m.dialog.filename
		}
		body = fmt.Sprintf("In welches Topic kopieren?\n\n%s\n\n",
			styleDim.Render(label))
		for i, t := range m.dialog.topics {
			line := "  " + t
			if i == m.dialog.cursor {
				body += styleSelected.Render(line) + "\n"
			} else {
				body += line + "\n"
			}
		}
		body += "\n" + styleDim.Render("↑/↓ wählen  •  Enter kopieren  •  Esc abbrechen")
	}
	if m.dialog.errMsg != "" {
		body += "\n" + styleDialogErr.Render(m.dialog.errMsg)
	}
	return "\n" + styleDialog.Render(body) + "\n"
}

// ── root model ────────────────────────────────────────────────────────────────

type rootModel struct {
	active  tab
	library libraryModel
	imports importModel
	ignored ignoredModel
	height  int
	width   int
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
		inner := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 3}
		lib, c1 := m.library.update(inner)
		imp, c2 := m.imports.update(inner)
		ign, c3 := m.ignored.update(inner)
		m.library = lib
		m.imports = imp
		m.ignored = ign
		return m, tea.Batch(c1, c2, c3)

	case tea.KeyMsg:
		// Forward everything to active tab when a dialog is open.
		if m.active == tabLibrary && (m.library.dialog.active || m.library.confirm.mode != libConfirmNone) {
			lib, cmd := m.library.update(msg)
			m.library = lib
			return m, cmd
		}
		if m.active == tabImport && m.imports.dialog.mode != importDialogNone {
			imp, cmd := m.imports.update(msg)
			m.imports = imp
			return m, cmd
		}
		if m.active == tabIgnored && m.ignored.dialog.mode != ignoredDialogNone {
			ign, cmd := m.ignored.update(msg)
			m.ignored = ign
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "f1":
			m.active = tabLibrary
			return m, nil
		case "f2":
			m.active = tabImport
			return m, nil
		case "f3":
			m.active = tabIgnored
			return m, loadIgnoredCmd(m.ignored.lib)
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

func (m rootModel) View() string {
	libTab := styleTab.Render("F1 Library")
	impTab := styleTab.Render("F2 Import")
	ignTab := styleTab.Render("F3 Ignoriert")
	switch m.active {
	case tabLibrary:
		libTab = styleActiveTab.Render("F1 Library")
	case tabImport:
		impTab = styleActiveTab.Render("F2 Import")
	case tabIgnored:
		ignTab = styleActiveTab.Render("F3 Ignoriert")
	}
	var status string
	switch m.active {
	case tabLibrary:
		status = m.library.status
	case tabImport:
		if len(m.imports.sel) > 0 {
			status = fmt.Sprintf("%d ausgewählt  •  ", len(m.imports.sel)) + m.imports.status
		} else {
			status = m.imports.status
		}
	case tabIgnored:
		if len(m.ignored.sel) > 0 {
			status = fmt.Sprintf("%d ausgewählt  •  ", len(m.ignored.sel)) + m.ignored.status
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

	var footerParts string
	switch m.active {
	case tabLibrary:
		libRows := m.library.buildRows()
		if len(libRows) > 0 && m.library.cursor < len(libRows) && !libRows[m.library.cursor].isTopic {
			footerParts = "↑/↓ scrollen • i ignorieren • Entf löschen • n neues Topic • r neu laden • q beenden"
		} else {
			footerParts = "↑/↓ scrollen • Enter aufklappen • n neues Topic • r neu laden • q beenden"
		}
	case tabImport:
		if len(m.imports.entries) > 0 && m.imports.dialog.mode == importDialogNone {
			footerParts = "↑/↓ scrollen • Shift+↑/↓ Mehrfachauswahl • a alle • d keine • i ignorieren • c kopieren • q beenden"
		} else {
			footerParts = "↑/↓ scrollen • q beenden"
		}
	case tabIgnored:
		if len(m.ignored.entries) > 0 && m.ignored.dialog.mode == ignoredDialogNone {
			footerParts = "↑/↓ scrollen • Shift+↑/↓ Mehrfachauswahl • a alle • d keine • c in Topic • Entf entmarkieren • r neu laden • q beenden"
		} else {
			footerParts = "↑/↓ scrollen • r neu laden • q beenden"
		}
	}
	footer := styleDim.Render("F1/F2/F3 Tab wechseln • " + footerParts)
	return tabBar + "\n\n" + content + "\n" + footer
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
