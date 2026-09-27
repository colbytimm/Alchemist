package panes

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	snapshotsTitle = "Snapshots"
	takenLayout    = "2006-01-02 15:04"
	takenWidth     = 16
	windowWidth    = 7
	itemsWidth     = 11
	changesWidth   = 22
	newDataWidth   = 9
	// markedLimit is how many rows can be marked for a diff; a further mark
	// replaces the older of the two.
	markedLimit = 2
	notePrompt  = "Note (optional): "
)

// CaptureEnd is how a capture shown in the overlay ended.
type CaptureEnd int

const (
	CaptureRunning CaptureEnd = iota
	CaptureDone
	CaptureFailed
	CaptureCancelled
)

// CaptureStatus is the capture the overlay's top row reports. Container
// names the container in progress of a database snapshot.
type CaptureStatus struct {
	Container string
	Progress  snapshot.Progress
	End       CaptureEnd
	Err       error
	// Warning is said under the row: what quitting now would do.
	Warning string
}

// snapshotsState is what the overlay is asking for, if anything.
type snapshotsState int

const (
	snapshotsListing snapshotsState = iota
	snapshotsNaming
	snapshotsConfirmingDelete
)

// SnapshotsKeys are the bindings the overlay's hint lines show.
type SnapshotsKeys struct {
	List    []key.Binding
	Capture []key.Binding
	Prompt  []key.Binding
	Confirm []key.Binding
}

// Snapshots is the overlay listing the snapshots of one container, or the
// database snapshots of one database, newest first. Like the input it
// wraps, its value receiver hides shared pointers, so a caller must keep
// every Snapshots it is handed.
type Snapshots struct {
	frame   frame
	icons   theme.IconSet
	hints   help.Model
	keys    SnapshotsKeys
	scope   string
	dir     string
	loading bool
	records []snapshot.Record
	changed map[string]bool
	groups  []snapshot.Group
	usage   string
	cursor  int
	marked  []string
	capture *CaptureStatus
	state   snapshotsState
	note    textinput.Model
	failure string
	notice  string
}

func NewSnapshots(icons theme.IconSet, keys SnapshotsKeys) Snapshots {
	hints := help.New()
	note := newInput("", "")
	note.Prompt = notePrompt
	return Snapshots{frame: frame{title: snapshotsTitle, focused: true}, icons: icons, hints: hints, keys: keys, note: note}
}

func (s Snapshots) SetSize(width, height int) Snapshots {
	s.frame = s.frame.size(width, height)
	width, _ = s.frame.inner()
	s.hints.Width = width
	s.note.Width = max(width-lipgloss.Width(notePrompt)-1, 1)
	return s
}

// Open starts the overlay over for the store scope names on account, kept
// under dir, while its list is read.
func (s Snapshots) Open(account, scope, dir string) Snapshots {
	s.frame.title = snapshotsTitle + " · " + account + " · " + scope
	s.scope, s.dir = scope, dir
	s.loading = true
	s.records, s.changed, s.groups, s.usage = nil, nil, nil, ""
	s.cursor, s.marked, s.failure, s.notice = 0, nil, "", ""
	s.state = snapshotsListing
	return s
}

// SetNotice says something about the last key, in place of the note line,
// until the cursor moves or the list is read again.
func (s Snapshots) SetNotice(notice string) Snapshots {
	s.notice = notice
	return s
}

// SetRecords lists a container's snapshots, oldest first as the store
// gives them; changed names those whose definition differs from their
// parent's. Marks on snapshots still listed survive a reload.
func (s Snapshots) SetRecords(records []snapshot.Record, changed map[string]bool, usage string) Snapshots {
	s.loading, s.failure, s.notice = false, "", ""
	s.records = slices.Clone(records)
	slices.Reverse(s.records)
	s.changed, s.usage = changed, usage
	s.marked = slices.DeleteFunc(s.marked, func(id string) bool {
		return !slices.ContainsFunc(s.records, func(r snapshot.Record) bool { return r.ID == id })
	})
	s.cursor = min(s.cursor, max(len(s.records)-1, 0))
	return s
}

// SetGroups lists a database's snapshots, oldest first as the store gives
// them.
func (s Snapshots) SetGroups(groups []snapshot.Group, usage string) Snapshots {
	s.loading, s.failure, s.notice = false, "", ""
	s.groups = slices.Clone(groups)
	slices.Reverse(s.groups)
	s.usage = usage
	s.marked = slices.DeleteFunc(s.marked, func(id string) bool {
		return !slices.ContainsFunc(s.groups, func(g snapshot.Group) bool { return g.ID == id })
	})
	s.cursor = min(s.cursor, max(len(s.groups)-1, 0))
	return s
}

func (s Snapshots) Fail(err error) Snapshots {
	s.loading = false
	s.failure = err.Error()
	return s
}

// SetCapture shows the capture of this store in the top row; nil shows
// none.
func (s Snapshots) SetCapture(status *CaptureStatus) Snapshots {
	s.capture = status
	return s
}

func (s Snapshots) rows() int {
	if s.groups != nil {
		return len(s.groups)
	}
	return len(s.records)
}

func (s Snapshots) CursorUp() Snapshots {
	s.cursor, s.notice = max(s.cursor-1, 0), ""
	return s
}

func (s Snapshots) CursorDown() Snapshots {
	s.cursor, s.notice = min(s.cursor+1, max(s.rows()-1, 0)), ""
	return s
}

// Selected is the id of the row under the cursor.
func (s Snapshots) Selected() (string, bool) {
	return s.idAt(s.cursor)
}

func (s Snapshots) idAt(row int) (string, bool) {
	switch {
	case row < 0 || row >= s.rows():
		return "", false
	case s.groups != nil:
		return s.groups[row].ID, true
	}
	return s.records[row].ID, true
}

// ToggleMark marks the row under the cursor, or clears its mark; a third mark
// replaces the older of two.
func (s Snapshots) ToggleMark() Snapshots {
	id, ok := s.Selected()
	if !ok {
		return s
	}
	if i := slices.Index(s.marked, id); i >= 0 {
		s.marked = slices.Delete(slices.Clone(s.marked), i, i+1)
		return s
	}
	s.marked = append(slices.Clone(s.marked), id)
	if len(s.marked) > markedLimit {
		s.marked = s.marked[1:]
	}
	return s
}

// Pair is what enter diffs: the two marked rows, older first, or with
// fewer than two marked, the row under the cursor and the one before it.
// ok is false when there is nothing to compare it with.
func (s Snapshots) Pair() (from, to string, ok bool) {
	if len(s.marked) == markedLimit {
		first, second := s.marked[0], s.marked[1]
		return min(first, second), max(first, second), true
	}
	to, ok = s.Selected()
	if !ok {
		return "", "", false
	}
	from, ok = s.idAt(s.cursor + 1)
	return from, to, ok
}

func (s Snapshots) Marked() []string { return slices.Clone(s.marked) }

// PromptNote asks for a note before a snapshot is taken.
func (s Snapshots) PromptNote() Snapshots {
	s.state = snapshotsNaming
	s.note.Reset()
	s.note.Focus()
	return s
}

func (s Snapshots) Naming() bool { return s.state == snapshotsNaming }

func (s Snapshots) Note() string { return strings.TrimSpace(s.note.Value()) }

func (s Snapshots) UpdateNote(msg tea.KeyMsg) (Snapshots, tea.Cmd) {
	var cmd tea.Cmd
	s.note, cmd = s.note.Update(msg)
	return s, cmd
}

// ConfirmDelete asks before the snapshot under the cursor is deleted.
func (s Snapshots) ConfirmDelete() Snapshots {
	if _, ok := s.Selected(); ok {
		s.state = snapshotsConfirmingDelete
	}
	return s
}

func (s Snapshots) ConfirmingDelete() bool { return s.state == snapshotsConfirmingDelete }

// Settle ends a prompt or a confirmation.
func (s Snapshots) Settle() Snapshots {
	s.state = snapshotsListing
	s.note.Blur()
	return s
}

func (s Snapshots) View() string {
	width, height := s.frame.inner()
	top := s.topLines(width)
	footer := s.footerLines(width)
	hints := packHints(s.hints, s.hintKeys(), width)
	body := s.body(width, max(height-len(top)-len(footer)-len(hints)-1, 0))
	lines := append(append(top, body...), "")
	return s.frame.renderWithHints(append(lines, footer...), hints)
}

func (s Snapshots) topLines(width int) []string {
	var lines []string
	if s.capture != nil {
		lines = append(lines, s.captureLine(width))
		if s.capture.Warning != "" {
			lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(s.capture.Warning, width))...)
		}
	}
	if s.groups != nil {
		return append(lines, theme.HintStyle().Render(fmt.Sprintf("    %-*s  %-*s  %s", takenWidth, "Taken (UTC)", windowWidth, "Window", "Containers")))
	}
	return append(lines, theme.HintStyle().Render(fmt.Sprintf("    %-*s  %-*s  %*s  %-*s  %*s", takenWidth, "Taken (UTC)",
		windowWidth, "Window", itemsWidth, "Items", changesWidth, "Changes", newDataWidth, "New data")))
}

func (s Snapshots) captureLine(width int) string {
	status := s.capture
	switch status.End {
	case CaptureFailed:
		return theme.ErrorStyle().Render(fit(s.icons.Failure+" capture failed: "+status.Err.Error(), width))
	case CaptureCancelled:
		return theme.HintStyle().Render(fit("capture cancelled: nothing was kept", width))
	case CaptureDone:
		return theme.SuccessStyle().Render(fit(s.icons.Success+" snapshot taken", width))
	}
	p := status.Progress
	where := ""
	if status.Container != "" {
		where = status.Container + " "
	}
	return theme.TextStyle().Render(fit(fmt.Sprintf("%s capturing %s%s…  %s items · %s RU · %s read · %s new", s.icons.SpinnerFrames[0], where, p.Phase,
		FormatCount(p.Items), FormatCharge(p.RequestCharge), FormatBytes(p.BytesRead), FormatBytes(p.BytesStored)), width))
}

func (s Snapshots) body(width, height int) []string {
	switch {
	case s.failure != "":
		return wrapText(theme.ErrorStyle().Render(s.icons.Failure+" "+s.failure), width)
	case s.loading:
		return []string{theme.HintStyle().Render("reading snapshots…")}
	case s.rows() == 0:
		return styleAll(theme.HintStyle(), wrapText(s.emptyText(), width))
	}
	start, end := windowBounds(s.rows(), s.cursor, height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, s.row(i, width))
	}
	return lines
}

func (s Snapshots) emptyText() string {
	return fmt.Sprintf("No snapshots of %s yet. s takes one: the first reads every item once, and later ones "+
		"read every key and only what changed. Snapshots are kept in %s and are not encrypted.", s.scope, s.dir)
}

func (s Snapshots) row(i, width int) string {
	id, _ := s.idAt(i)
	mark := "  "
	if slices.Contains(s.marked, id) {
		mark = s.icons.Marked + " "
	}
	cursor := "  "
	style := theme.TextStyle()
	if i == s.cursor {
		cursor, style = "› ", theme.SelectedStyle()
	}
	if s.groups != nil {
		return style.Render(fit(mark+cursor+groupColumns(s.groups[i]), width))
	}
	return style.Render(fit(mark+cursor+s.recordColumns(s.records[i]), width))
}

func (s Snapshots) recordColumns(r snapshot.Record) string {
	changes := "first snapshot"
	if r.Parent != "" {
		changes = fmt.Sprintf("+%s %s%s ~%s", FormatCount(r.Added), s.icons.Removed, FormatCount(r.Removed), FormatCount(r.Modified))
	}
	if s.changed[r.ID] {
		changes += " def"
	}
	return fmt.Sprintf("%-*s  %-*s  %*s  %-*s  %*s", takenWidth, r.Time().Format(takenLayout), windowWidth, windowText(r.Window()),
		itemsWidth, FormatCount(r.Items), changesWidth, changes, newDataWidth, FormatBytes(r.StoredBytes))
}

func groupColumns(g snapshot.Group) string {
	failed := 0
	for _, member := range g.Containers {
		if member.Error != "" {
			failed++
		}
	}
	members := fmt.Sprintf("%d containers", len(g.Containers))
	if failed > 0 {
		members += fmt.Sprintf(", %d failed", failed)
	}
	taken, err := time.Parse("20060102T150405Z", g.ID)
	if err != nil {
		taken = g.Started
	}
	return fmt.Sprintf("%-*s  %-*s  %s", takenWidth, taken.Format(takenLayout), windowWidth, windowText(g.Finished.Sub(g.Started)), members)
}

func windowText(d time.Duration) string {
	return d.Round(time.Second).String()
}

func (s Snapshots) footerLines(width int) []string {
	switch s.state {
	case snapshotsNaming:
		return []string{promptedInputView(s.note)}
	case snapshotsConfirmingDelete:
		id, _ := s.Selected()
		return []string{theme.ErrorStyle().Render(fit("Delete snapshot "+id+"? Its data goes when no other snapshot needs it.", width))}
	}
	if s.notice != "" {
		return []string{theme.ErrorStyle().Render(fit(s.notice, width)), theme.HintStyle().Render(fit(s.usage, width))}
	}
	note := ""
	if s.groups == nil && s.cursor < len(s.records) && s.records[s.cursor].Note != "" {
		note = "note: " + s.records[s.cursor].Note
	}
	if s.groups != nil && s.cursor < len(s.groups) && s.groups[s.cursor].Note != "" {
		note = "note: " + s.groups[s.cursor].Note
	}
	return []string{theme.TextStyle().Render(fit(note, width)), theme.HintStyle().Render(fit(s.usage, width))}
}

func (s Snapshots) hintKeys() []key.Binding {
	switch {
	case s.state == snapshotsNaming:
		return s.keys.Prompt
	case s.state == snapshotsConfirmingDelete:
		return s.keys.Confirm
	case s.capture != nil && s.capture.End == CaptureRunning:
		return s.keys.Capture
	}
	return s.keys.List
}
