package tui_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui"
)

const (
	snapshotsTitle    = " Snapshots · "
	diffTitle         = " Diff · "
	snapshotItemCount = 25
)

// newSnapshotModel is a session keeping snapshots under root, with the
// cursor on sales.orders.
func newSnapshotModel(t *testing.T, conn *recordingConnection, root string) tea.Model {
	t.Helper()
	return newSnapshotModelWith(t, conn, tui.Options{Manage: managed, SampleFields: true, Snapshots: root})
}

func newSnapshotModelWith(t *testing.T, conn *recordingConnection, opts tui.Options) tea.Model {
	t.Helper()
	m := newModelWith(t, conn, opts)
	model, _ := settle(m, m.Init())
	return selectContainer(t, model)
}

// wide shows a status bar with room for a notice beside a job's field.
func wide(m tea.Model) tea.Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: testHeight})
	return m
}

func newSnapshotConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	return newConnection(t, append([]mock.Option{mock.WithItemCount(ordersPath, snapshotItemCount)}, opts...)...)
}

// beginSnapshot presses s on the node under the cursor, types note and
// presses enter, and returns the first step not yet run.
func beginSnapshot(t *testing.T, m tea.Model, note string) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, m, keyRune('s'), keyText(note))
	require.Contains(t, plain(m.View()), "Note (optional)")
	return m.Update(keyMsg(tea.KeyEnter))
}

// takeSnapshot takes a snapshot of the node under the cursor to the end,
// and closes the overlay.
func takeSnapshot(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m, cmd := beginSnapshot(t, m, "")
	m, _ = settle(m, cmd)
	require.Contains(t, plain(m.View()), "snapshot taken")
	return pressAll(t, m, keyMsg(tea.KeyEscape))
}

// step delivers what cmd produces and returns the commands that produced,
// not yet run: one step of a capture at a time.
func step(m tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	var next []tea.Cmd
	for _, msg := range messages(cmd) {
		var produced tea.Cmd
		m, produced = m.Update(msg)
		next = append(next, produced)
	}
	return m, tea.Batch(next...)
}

func snapshotsOf(t *testing.T, root string) []snapshot.Record {
	t.Helper()
	store, err := snapshot.Open(snapshot.Location{Root: root, Account: mock.Name, Database: firstDatabase, Container: firstContainer})
	require.NoError(t, err)
	return store.Snapshots()
}

func TestSOnAContainerCapturesPageByPageAndListsTheSnapshot(t *testing.T) {
	root := t.TempDir()
	m, cmd := beginSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), root), "nightly")

	steps := 0
	for cmd != nil {
		m, cmd = step(m, cmd)
		steps++
		m = pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyUp))
		require.Contains(t, plain(m.View()), snapshotsTitle, "keys between pages leave the overlay working")
	}

	assert.Greater(t, steps, 3, "one step per page")
	view := plain(m.View())
	assert.Contains(t, view, "snapshot taken")
	assert.Contains(t, view, "first snapshot")
	assert.Contains(t, view, "note: nightly")
	assert.Contains(t, view, "1 snapshot · ")
	require.Len(t, snapshotsOf(t, root), 1)
	assert.Equal(t, int64(snapshotItemCount), snapshotsOf(t, root)[0].Items)
}

func TestTheEmptyOverlaySaysWhatSDoesAndWhereAndThatItIsNotEncrypted(t *testing.T) {
	root := t.TempDir()
	m := pressAll(t, newSnapshotModel(t, newSnapshotConnection(t), root), keyRune('v'))

	view := strings.Join(strings.Fields(strings.ReplaceAll(plain(m.View()), "│", "")), " ")
	assert.Contains(t, view, "No snapshots of sales.orders yet")
	assert.Contains(t, view, "reads every item once")
	assert.Contains(t, view, "not encrypted")
}

func TestADiffListsTheItemsThatChangedAndTheirFields(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := takeSnapshot(t, newSnapshotModel(t, conn, t.TempDir()))
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"item-00003","customerId":"pk-3","n":3,"status":"shipped"}`)))
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"new","customerId":"pk-1","n":99}`)))
	require.NoError(t, conn.store.DeleteItem(ordersPath, "item-00004", adapter.PartitionKey{json.RawMessage(`"pk-4"`)}))
	m = takeSnapshot(t, m)

	m = pressAll(t, m, keyRune('v'), keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, diffTitle+"sales.orders")
	assert.Contains(t, view, "+1 added   −1 removed   ~1 modified")
	assert.Contains(t, view, "+ pk-1")
	assert.Contains(t, view, "− pk-4")
	assert.Regexp(t, `~ pk-3 +item-00003 +status`, view, "the fields column is read for the rows on screen")
	item := pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	itemView := plain(item.View())
	assert.Contains(t, itemView, "Item · pk-3 / item-00003 · modified")
	assert.Contains(t, itemView, `+   "status": "shipped"`)
	assert.Contains(t, itemView, "1 field · modified")
	assert.Contains(t, plain(pressAll(t, item, keyMsg(tea.KeyEscape)).View()), diffTitle, "esc goes back one level")
}

func TestXMidCaptureKeepsNothingAndAFreshSWorks(t *testing.T) {
	root := t.TempDir()
	m, cmd := beginSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), root), "")
	m, cmd = step(m, cmd)
	m, cmd = step(m, cmd)

	m = pressAll(t, m, keyRune('x'))
	assert.Contains(t, plain(m.View()), "capture cancelled")
	m, _ = settle(m, cmd)

	assert.Empty(t, snapshotsOf(t, root), "the late page is dropped and nothing published")
	m, cmd = m.Update(keyRune('s'))
	m, _ = settle(m, cmd)
	m, cmd = m.Update(keyMsg(tea.KeyEnter))
	m, _ = settle(m, cmd)
	assert.Contains(t, plain(m.View()), "snapshot taken", "the lock was released")
	assert.Len(t, snapshotsOf(t, root), 1)
}

func TestWhileACaptureRunsOtherJobsWaitAndQueriesDoNot(t *testing.T) {
	conn := newSnapshotConnection(t)
	m, _ := beginSnapshot(t, newSnapshotModel(t, conn, t.TempDir()), "")
	m = wide(pressAll(t, m, keyMsg(tea.KeyEscape)))

	again := pressAll(t, m, keyRune('s'))
	assert.Contains(t, statusBar(again), "a snapshot is running: snapshots wait for it (v)")
	clone := pressAll(t, m, keyRune('y'))
	assert.Contains(t, statusBar(clone), "a snapshot is running: clones wait for it (v)")
	queried := runQuery(t, m, "SELECT * FROM c")
	assert.NotEmpty(t, conn.queries, "a SELECT runs")
	assert.Contains(t, statusBar(queried), "snapshot mock/sales.orders")
}

func TestAClonesSlotRefusesS(t *testing.T) {
	conn := newCloneConnection(t)
	m := newModelWith(t, conn, tui.Options{Manage: managed, Snapshots: t.TempDir()})
	m, _ = settle(m, m.Init())
	m, _ = confirmClone(t, reviewClone(t, m), mock.Name)
	m = wide(pressAll(t, m, keyMsg(tea.KeyEscape)))

	m = pressAll(t, m, keyRune('s'))

	assert.Contains(t, statusBar(m), "a clone is running: snapshots wait for it (y)")
	assert.NotContains(t, plain(m.View()), snapshotsTitle)
}

func TestTheFirstQuitMidCaptureWarnsAndTheSecondQuitsKeepingNothing(t *testing.T) {
	root := t.TempDir()
	m, _ := beginSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), root), "")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	warned, cmd := m.Update(keyRune('q'))
	assert.Nil(t, cmd)
	assert.Contains(t, plain(warned.View()), "A snapshot is running. Quit again to cancel it and quit; nothing will be kept.")
	_, cmd = warned.Update(keyRune('q'))

	require.NotNil(t, cmd)
	assert.Empty(t, snapshotsOf(t, root))
}

func TestAQuitMidStepLeavesTheStoreUnlocked(t *testing.T) {
	root := t.TempDir()
	conn := newSnapshotConnection(t)
	m, cmd := beginSnapshot(t, newSnapshotModel(t, conn, root), "")
	m, step := step(m, cmd)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = pressAll(t, m, keyRune('q'))
	_, quit := m.Update(keyRune('q'))
	require.NotNil(t, quit)
	messages(step) // the step in flight ends after the session has gone, and nothing reads its message

	store, err := snapshot.Open(snapshot.Location{Root: root, Account: mock.Name, Database: firstDatabase, Container: firstContainer})
	require.NoError(t, err)
	capture, err := store.Begin(snapshot.Source{Container: ordersPath, Items: conn.scanner}, snapshot.CaptureOptions{})
	require.NoError(t, err, "the step released the lock itself")
	require.NoError(t, capture.Abort())
	assert.Empty(t, snapshotsOf(t, root))
}

func TestAHiddenCaptureShowsInTheStatusBarAcrossAccountsAndVReopensIt(t *testing.T) {
	o := newOpener(t)
	o.options["prod"] = []mock.Option{mock.WithItemCount(ordersPath, snapshotItemCount)}
	root := t.TempDir()
	m := newAccountsModel(t, o, tui.Options{Snapshots: root})
	m = pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter), keyRune('s'))
	m, cmd := m.Update(keyMsg(tea.KeyEnter))
	m, cmd = step(m, cmd)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = switchTo(t, m, "staging")
	assert.Contains(t, statusBar(m), "snapshot prod/sales.orders")
	refused := pressAll(t, highlight(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "prod"), keyRune('x'))
	assert.Contains(t, plain(refused.View()), "a snapshot is using prod: cancel it first (v in the catalog)")
	reopened := pressAll(t, m, keyRune('v'))
	assert.Contains(t, plain(reopened.View()), snapshotsTitle+"prod · sales.orders")

	m, _ = settle(reopened, cmd)
	stored, err := snapshot.Open(snapshot.Location{Root: root, Account: "prod", Database: firstDatabase, Container: firstContainer})
	require.NoError(t, err)
	assert.Len(t, stored.Snapshots(), 1, "it publishes under the account it began on")
	staging := pressAll(t, pressAll(t, m, keyMsg(tea.KeyEscape)), keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown), keyRune('v'))
	assert.Contains(t, plain(staging.View()), snapshotsTitle+"staging · sales.orders")
	assert.Contains(t, plain(staging.View()), "No snapshots", "an overlay lists its own account's snapshots only")
}

func TestAHiddenCaptureThatEndsSaysSoUntilSeen(t *testing.T) {
	m, cmd := beginSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), t.TempDir()), "")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m, _ = settle(m, cmd)

	assert.Contains(t, statusBar(m), "snapshot done (v)")
	m = pressAll(t, m, keyRune('v'), keyMsg(tea.KeyEscape))
	assert.NotContains(t, statusBar(m), "snapshot done")
}

func TestMarksKeepTheLastTwoAndEnterDiffsThemOrTheCursorsParent(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := newSnapshotModel(t, conn, t.TempDir())
	for i := range 3 {
		require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"extra-`+string(rune('a'+i))+`","customerId":"pk-0"}`)))
		m = takeSnapshot(t, m)
	}
	m = pressAll(t, m, keyRune('v'))

	marked := pressAll(t, m, keySpace(), keyMsg(tea.KeyDown), keySpace(), keyMsg(tea.KeyDown), keySpace())
	assert.Equal(t, 2, strings.Count(plain(marked.View()), "●"))
	diffed := pressAll(t, marked, keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(diffed.View()), "+1 added", "the second and third snapshots, one item apart")

	parent := pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(parent.View()), "+1 added")
	first := pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(first.View()), "nothing to compare")
}

func TestTheDiffFiltersByKindAndIdAndExports(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newSnapshotConnection(t)
	m := takeSnapshot(t, newSnapshotModel(t, conn, t.TempDir()))
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"item-00003","customerId":"pk-3","n":30}`)))
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"new","customerId":"pk-1"}`)))
	m = pressAll(t, takeSnapshot(t, m), keyRune('v'), keyMsg(tea.KeyEnter))

	added := pressAll(t, m, keyMsg(tea.KeyTab))
	assert.Contains(t, plain(added.View()), "added · 1 changes")
	assert.NotContains(t, plain(added.View()), "item-00003")
	filtered := pressAll(t, m, keyRune('/'), keyText("00003"))
	assert.NotContains(t, plain(filtered.View()), "+ pk-1")
	assert.Contains(t, plain(filtered.View()), "item-00003")

	exported := pressAll(t, m, keyMsg(tea.KeyCtrlE), keyMsg(tea.KeyCtrlU), keyText("diff.json"), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(exported.View()), diffTitle)
	assert.FileExists(t, "diff.json")
	csv := pressAll(t, m, keyMsg(tea.KeyCtrlE), keyMsg(tea.KeyTab), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(csv.View()), diffTitle)
	matches, err := filepath.Glob("*.csv")
	require.NoError(t, err)
	assert.Len(t, matches, 1)
	again := pressAll(t, m, keyMsg(tea.KeyCtrlE), keyMsg(tea.KeyCtrlU), keyText("diff.json"), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(again.View()), "file exists")
}

func TestASnapshotsItemsExport(t *testing.T) {
	t.Chdir(t.TempDir())
	m := takeSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), t.TempDir()))

	m = pressAll(t, m, keyRune('v'), keyMsg(tea.KeyCtrlE), keyMsg(tea.KeyCtrlU), keyText("items.jsonl"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "exported to items.jsonl")
	data, err := os.ReadFile("items.jsonl")
	require.NoError(t, err)
	assert.Equal(t, snapshotItemCount, strings.Count(string(data), "\n"))
}

func TestDAsksFirstAndEscKeepsTheSnapshot(t *testing.T) {
	root := t.TempDir()
	m := takeSnapshot(t, newSnapshotModel(t, newSnapshotConnection(t), root))
	m = pressAll(t, m, keyRune('v'), keyRune('d'))
	require.Contains(t, plain(m.View()), "Delete snapshot")

	kept := pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(kept.View()), snapshotsTitle)
	assert.Len(t, snapshotsOf(t, root), 1)

	deleted := pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(deleted.View()), "No snapshots")
	assert.Empty(t, snapshotsOf(t, root))
}

func TestWithoutAnItemScannerSAndVDoNothing(t *testing.T) {
	m := newSnapshotModelWith(t, newSnapshotConnection(t), tui.Options{Snapshots: t.TempDir()})

	m = pressAll(t, m, keyRune('s'), keyRune('v'))

	assert.NotContains(t, plain(m.View()), snapshotsTitle)
	help := plain(pressAll(t, m, keyRune('?')).View())
	assert.NotContains(t, help, "take snapshot")
	assert.NotContains(t, help, "snapshots")
}

func TestAThrottledPageIsRetriedAndTheSixthInARowFails(t *testing.T) {
	tests := []struct {
		name      string
		throttles int
		wantTaken bool
	}{
		{name: "five in a row are waited out", throttles: 5, wantTaken: true},
		{name: "the sixth fails the capture", throttles: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			conn := newSnapshotConnection(t)
			conn.throttleScans, conn.scanRetryAfter = tt.throttles, time.Millisecond
			m, cmd := beginSnapshot(t, newSnapshotModel(t, conn, root), "")

			m, _ = settle(m, cmd)

			if tt.wantTaken {
				assert.Len(t, snapshotsOf(t, root), 1)
				return
			}
			assert.Contains(t, plain(m.View()), "capture failed")
			assert.Empty(t, snapshotsOf(t, root))
		})
	}
}

func TestKeysInsideTheOverlayNeverReachTheCatalog(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := takeSnapshot(t, newSnapshotModel(t, conn, t.TempDir()))
	m = pressAll(t, m, keyRune('v'))

	for _, typed := range []tea.KeyMsg{keyRune('d'), keyMsg(tea.KeyEscape), keyRune('x'), keyRune('s'), keyMsg(tea.KeyEscape)} {
		m = pressAll(t, m, typed)
		assert.Contains(t, plain(m.View()), snapshotsTitle, "after %s", typed)
	}
	assert.Empty(t, conn.deleted, "d deletes a snapshot there, never the container")
	_, cmd := m.Update(keyRune('q'))
	assert.NotNil(t, cmd, "q still quits")
}

func TestADefinitionChangeShowsARowAndIsMarkedInTheList(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := takeSnapshot(t, newSnapshotModel(t, conn, t.TempDir()))
	require.NoError(t, conn.editor.SetThroughput(t.Context(), ordersPath, adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 800}))
	m = takeSnapshot(t, m)

	m = pressAll(t, m, keyRune('v'))
	assert.Contains(t, plain(m.View()), "+0 −0 ~0 def")
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	view := plain(m.View())
	assert.Contains(t, view, "definition: 1 setting")
	assert.Contains(t, view, "definition   throughput: 400 RU/s manual → 800 RU/s manual")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEnter)).View()), "Definition · sales.orders")
}

func TestASizeChangeAloneIsNoDefinitionChange(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := takeSnapshot(t, newSnapshotModel(t, conn, t.TempDir()))
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"new","customerId":"pk-1"}`)))
	m = takeSnapshot(t, m)

	m = pressAll(t, m, keyRune('v'))

	assert.NotContains(t, plain(m.View()), " def")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEnter)).View()), "definition unchanged")
}

func TestWithoutADefinitionReaderTheDiffSaysSo(t *testing.T) {
	conn := newSnapshotConnection(t)
	manage := func(c adapter.Connection) tui.Management {
		management := managed(c)
		management.Definitions = nil
		return management
	}
	m := newSnapshotModelWith(t, conn, tui.Options{Manage: manage, Snapshots: t.TempDir()})
	m = takeSnapshot(t, takeSnapshot(t, m))

	m = pressAll(t, m, keyRune('v'), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "definition not captured")
}

func TestVOnADatabaseListsItsSnapshotsAndDiffsThemByContainer(t *testing.T) {
	conn := newSnapshotConnection(t)
	m := pressAll(t, newSnapshotModel(t, conn, t.TempDir()), keyMsg(tea.KeyUp))
	takeDatabase := func(m tea.Model) tea.Model {
		m = pressAll(t, m, keyRune('s'))
		m, cmd := m.Update(keyMsg(tea.KeyEnter))
		m, _ = settle(m, cmd)
		return pressAll(t, m, keyMsg(tea.KeyEscape))
	}
	m = takeDatabase(m)
	require.NoError(t, conn.store.PutItem(ordersPath, json.RawMessage(`{"id":"new","customerId":"pk-1"}`)))
	m = takeDatabase(m)

	m = pressAll(t, m, keyRune('v'))
	assert.Contains(t, plain(m.View()), snapshotsTitle+"mock · sales")
	assert.Contains(t, plain(m.View()), "2 containers")
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(m.View()), "orders")
	assert.Regexp(t, `orders +\+1 −0 ~0`, plain(m.View()))
	m = pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(m.View()), diffTitle+"sales.orders")
	back := pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Regexp(t, `orders +\+1`, plain(back.View()), "esc goes back to the database's diff")
}
