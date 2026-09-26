package panes_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	overlayWidth  = 80
	overlayHeight = 24
)

func binding(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

func snapshotKeys() panes.SnapshotsKeys {
	list := []key.Binding{binding("s", "take snapshot"), binding("space", "mark"), binding("enter", "diff"),
		binding("ctrl+e", "export to file"), binding("d", "delete snapshot"), binding("esc", "close")}
	return panes.SnapshotsKeys{List: list, Capture: list, Prompt: list, Confirm: list}
}

func records(n int) []snapshot.Record {
	start := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	var listed []snapshot.Record
	for i := range n {
		taken := start.Add(time.Duration(i) * 24 * time.Hour)
		record := snapshot.Record{ID: taken.Format("20060102T150405Z"), Started: taken, Finished: taken.Add(48 * time.Second),
			Items: 1000412, Added: 212, Removed: 40, Modified: 9731, StoredBytes: 2 << 20}
		if i > 0 {
			record.Parent = listed[i-1].ID
		}
		listed = append(listed, record)
	}
	return listed
}

func TestTheSnapshotsOverlayFitsTheMinimumTerminal(t *testing.T) {
	pane := panes.NewSnapshots(theme.Icons(), snapshotKeys()).SetSize(overlayWidth, overlayHeight).
		Open("prod", "sales.orders", "/snapshots")
	pane = pane.SetRecords(records(30), map[string]bool{records(30)[29].ID: true}, "30 snapshots · 30.0 GB of items")

	view := plain(pane.View())

	lines := strings.Split(view, "\n")
	require.Len(t, lines, overlayHeight)
	for _, line := range lines {
		assert.LessOrEqual(t, lipgloss.Width(line), overlayWidth)
	}
	assert.Contains(t, view, "2026-10-18 06:00  48s        1,000,412  +212 −40 ~9,731 def")
	assert.NotContains(t, view, "first snapshot", "the oldest is at the bottom, out of view")
	assert.Contains(t, view, "d delete snapshot • esc close", "a hint line too long for one line wraps")
}

func TestTheLatestTwoMarksAreKeptAndPairedOldestFirst(t *testing.T) {
	pane := panes.NewSnapshots(theme.Icons(), snapshotKeys()).SetSize(overlayWidth, overlayHeight).
		Open("prod", "sales.orders", "/snapshots").SetRecords(records(3), nil, "")

	pane = pane.ToggleMark().CursorDown().ToggleMark().CursorDown().ToggleMark()

	from, to, ok := pane.Pair()
	require.True(t, ok)
	assert.Equal(t, []string{records(3)[1].ID, records(3)[0].ID}, pane.Marked())
	assert.Equal(t, records(3)[0].ID, from)
	assert.Equal(t, records(3)[1].ID, to)
}

func TestAnItemDiffFoldsLongRunsOfUnchangedLines(t *testing.T) {
	var fields []string
	for i := range 30 {
		fields = append(fields, fmt.Sprintf(`"f%02d":%d`, i, i))
	}
	before := "{" + strings.Join(fields, ",") + "}"
	after := strings.Replace(before, `"f15":15`, `"f15":99`, 1)
	item := snapshot.ItemChange{Kind: snapshot.Modified, Identity: snapshot.Identity{ID: "o1"}}

	view := plain(panes.NewItemDiff(theme.Icons(), nil).SetSize(overlayWidth, overlayHeight).
		SetItem(item, []byte(before), []byte(after)).View())

	assert.Contains(t, view, `−   "f15": 15,`)
	assert.Contains(t, view, `+   "f15": 99,`)
	assert.Contains(t, view, "… 12 unchanged lines")
	assert.Contains(t, view, "1 field")
}
