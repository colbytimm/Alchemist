package tui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The clone overlays' titles as they appear inside the top border.
const (
	cloneContainerTitle = " Clone container "
	cloneDatabaseTitle  = " Clone database "
	cloneProgressTitle  = " Clone · "
	cloneItemCount      = 25
)

var copyPath = []string{firstDatabase, firstContainer + "-copy"}

// newCloneConnection serves a mock whose sales.orders holds 25 items.
func newCloneConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	return newConnection(t, append([]mock.Option{mock.WithItemCount(ordersPath, cloneItemCount)}, opts...)...)
}

// openCloneForm puts the cursor on sales.orders and presses y.
func openCloneForm(t *testing.T, m tea.Model) (tea.Model, []tea.Msg) {
	t.Helper()
	return press(t, selectContainer(t, m), keyRune('y'))
}

// reviewClone takes the form as it stands to the review.
func reviewClone(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m, _ = openCloneForm(t, m)
	return pressAll(t, m, keyMsg(tea.KeyEnter))
}

// confirmClone types account at the review and presses enter, and returns
// the first step's command not yet run: the clone is in flight until the test
// runs it.
func confirmClone(t *testing.T, m tea.Model, account string) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, m, keyText(account))
	return m.Update(keyMsg(tea.KeyEnter))
}

// runSteps delivers what cmd produces and every step that follows, until
// the clone has nothing left to do.
func runSteps(m tea.Model, cmd tea.Cmd) tea.Model {
	m, _ = settle(m, cmd)
	return m
}

// cloneAll runs a same-account clone of sales.orders with the form's
// defaults, start to finish.
func cloneAll(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m, cmd := confirmClone(t, reviewClone(t, m), mock.Name)
	return runSteps(m, cmd)
}

func storedIn(t *testing.T, conn *recordingConnection, path []string) []string {
	t.Helper()
	var ids []string
	for _, item := range conn.store.Items(path) {
		ids = append(ids, itemID(t, item))
	}
	return ids
}

func TestYOnAContainerOpensThePromptAndReadsTheSourceOnce(t *testing.T) {
	m, msgs := openCloneForm(t, newLoadedModel(t, newCloneConnection(t)))

	view := plain(m.View())
	assert.Contains(t, view, cloneContainerTitle)
	assert.Contains(t, view, "mock / sales.orders")
	assert.Contains(t, view, "about 25 items")
	assert.Contains(t, view, "partition key /customerId")
	assert.Contains(t, view, "orders-copy", "a copy beside its source is named as one")
	prepared := 0
	for _, msg := range msgs {
		if _, ok := msg.(tui.ClonePreparedMsg); ok {
			prepared++
		}
	}
	assert.Equal(t, 1, prepared)
}

func TestYOnADatabaseOpensTheDatabasePrompt(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newCloneConnection(t)), keyRune('y'))

	view := plain(m.View())
	assert.Contains(t, view, cloneDatabaseTitle)
	assert.Contains(t, view, "2 containers")
	assert.Contains(t, view, "sales-copy")
	assert.NotContains(t, view, "Database        ", "a database clone names no database to land in")
}

func TestWithNoDefinitionReaderYDoesNothing(t *testing.T) {
	m := selectContainer(t, newUnmanagedModel(t, newCloneConnection(t)))

	m = pressAll(t, m, keyRune('y'))

	assert.NotContains(t, plain(m.View()), cloneContainerTitle)
	assert.NotContains(t, plain(pressAll(t, m, keyRune('?')).View()), "clone", "nor is it in the help")
}

func TestTheFormsEnterOpensTheReviewAndWritesNothing(t *testing.T) {
	conn := newCloneConnection(t)

	m := reviewClone(t, newLoadedModel(t, conn))

	view := plain(m.View())
	assert.Contains(t, view, "mock / sales.orders  →  mock / sales.orders-copy")
	assert.Contains(t, view, "Creates container orders-copy at 400 RU/s manual.")
	assert.Contains(t, view, "costs money until it is deleted")
	assert.Contains(t, view, "Copies about 25 items.")
	assert.Contains(t, view, "Not copied: stored procedures")
	assert.Contains(t, view, "Type the target account name to confirm:")
	assert.Empty(t, conn.containers)
	assert.Zero(t, conn.store.Upserts())
}

func TestTheReviewStartsNothingUntilTheAccountNameMatches(t *testing.T) {
	conn := newCloneConnection(t)
	m := reviewClone(t, newLoadedModel(t, conn))

	for _, typed := range []string{"", "Mock", "orders-copy"} {
		attempt := pressAll(t, m, keyText(typed), keyMsg(tea.KeyEnter))
		assert.NotContains(t, plain(attempt.View()), cloneProgressTitle, "typed %q", typed)
	}
	assert.Empty(t, conn.containers)
}

func TestEscFromTheReviewReturnsToTheFormAsItWasLeft(t *testing.T) {
	m, _ := openCloneForm(t, newLoadedModel(t, newCloneConnection(t)))
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText("archive"))

	m = pressAll(t, m, keyMsg(tea.KeyEnter), keyMsg(tea.KeyEscape))

	view := plain(m.View())
	assert.Contains(t, view, cloneContainerTitle)
	assert.Contains(t, view, "archive")
}

func TestATargetThatExistsIsRefusedInTheForm(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := openCloneForm(t, newLoadedModel(t, conn))

	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText("customers"), keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, cloneContainerTitle, "the form stays open")
	assert.Contains(t, view, "already exists")
	assert.Empty(t, conn.containers)
}

func TestASameAccountCloneCopiesEveryItemAndLandsOnTheCopy(t *testing.T) {
	conn := newCloneConnection(t)
	m := cloneAll(t, newLoadedModel(t, conn))

	assert.ElementsMatch(t, storedIn(t, conn, ordersPath), storedIn(t, conn, copyPath))
	view := plain(m.View())
	assert.Contains(t, view, "Done.")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	bar := statusBar(m)
	assert.Contains(t, bar, "cloned 25 items to mock/sales.orders-copy")
	assert.Contains(t, bar, "sales.orders ", "the scope is where it was")
	assert.True(t, strings.HasPrefix(bar, "mock "))
	assert.Contains(t, plain(pressAll(t, m, keyRune('d')).View()), "orders-copy", "the cursor is on the copy")
}

func TestEachPageIssuesExactlyOneNextStep(t *testing.T) {
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, newCloneConnection(t))), mock.Name)

	pages := 0
	for cmd != nil {
		var steps []tea.Msg
		for _, msg := range messages(cmd) {
			switch msg.(type) {
			case tui.CloneTargetCreatedMsg, tui.ClonePageCopiedMsg, tui.CloneFailedMsg:
				steps = append(steps, msg)
			}
		}
		if len(steps) == 0 {
			break
		}
		require.Len(t, steps, 1, "one step at a time")
		if _, ok := steps[0].(tui.ClonePageCopiedMsg); ok {
			pages++
		}
		m, cmd = m.Update(steps[0])
	}

	assert.Equal(t, 3, pages, "25 items in pages of 10")
	assert.Contains(t, plain(m.View()), "Done.")
}

func TestAStepOfAReleasedCloneStartsNothing(t *testing.T) {
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, newCloneConnection(t))), mock.Name)
	created := messages(cmd)
	m = pressAll(t, m, keyRune('x'))
	m, _ = settle(m, func() tea.Msg { return created[0] })
	require.Contains(t, plain(m.View()), "Stopped.")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	_, next := m.Update(created[0])

	assert.Empty(t, messages(next), "a step of a clone that is over is dropped")
}

func TestARunningCloneCanBeHiddenAndLeavesTheSessionUsable(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	require.Contains(t, plain(m.View()), cloneProgressTitle)

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, statusBar(m), "clone mock/sales.orders → mock")
	assert.Contains(t, statusBar(m), "(y)")

	m = runQuery(t, m, "SELECT * FROM c")
	assert.Len(t, conn.queries, 1, "a query runs beside the clone")

	reopened, msgs := press(t, pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab)), keyRune('y'))
	assert.Contains(t, plain(reopened.View()), cloneProgressTitle)
	assert.False(t, hasMsg[tui.ClonePreparedMsg](msgs), "y reopens the view and starts nothing")
}

func TestACloneThatEndsWhileHiddenSaysSoUntilItsViewIsClosed(t *testing.T) {
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, newCloneConnection(t))), mock.Name)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = runSteps(m, cmd)
	assert.Contains(t, statusBar(m), "clone done (y)")

	m = pressAll(t, m, keyRune('y'))
	assert.Contains(t, plain(m.View()), "Done.")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.NotContains(t, statusBar(m), "(y)")

	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyUp), keyRune('y')).View()), cloneContainerTitle,
		"with the clone closed, y opens a prompt again")
}

func TestStoppingLeavesAPartialTargetThatResumesToTheRightTotal(t *testing.T) {
	conn := newCloneConnection(t)
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	created := messages(cmd)
	m, cmd = m.Update(created[0])
	firstPage := messages(cmd)
	m = pressAll(t, m, keyRune('x'))

	m, _ = settle(m, func() tea.Msg { return firstPage[0] })

	view := plain(m.View())
	assert.Contains(t, view, "Stopped.")
	assert.Contains(t, view, "mock/sales.orders-copy holds 10 of about 25 items and is incomplete.")
	assert.Len(t, storedIn(t, conn, copyPath), 10)

	m = pressAll(t, m, keyRune('r'))
	assert.Contains(t, plain(m.View()), "Done.")
	assert.ElementsMatch(t, storedIn(t, conn, ordersPath), storedIn(t, conn, copyPath), "every item once")
}

func TestDeletingAPartialTargetNeedsItsNameAndTouchesNothingElse(t *testing.T) {
	conn := newCloneConnection(t)
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	m, cmd = m.Update(messages(cmd)[0])
	firstPage := messages(cmd)
	m = pressAll(t, m, keyRune('x'))
	m, _ = settle(m, func() tea.Msg { return firstPage[0] })

	m = pressAll(t, m, keyRune('d'))
	require.Contains(t, plain(m.View()), containerDeleteTitle)
	m = pressAll(t, m, keyText("orders-copy"), keyMsg(tea.KeyEnter))

	assert.Equal(t, [][]string{copyPath}, conn.deleted)
	assert.NotContains(t, statusBar(m), "(y)", "a clone whose target is gone lets the slot go")
	assert.Contains(t, statusBar(m), "deleted sales.orders-copy")
}

func TestQuitDuringACloneWarnsFirst(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m, first := m.Update(keyRune('q'))
	assert.NotContains(t, messages(first), tea.QuitMsg{})
	assert.Contains(t, plain(m.View()), "Quit again to stop it and quit")

	_, second := m.Update(keyRune('q'))
	assert.Contains(t, messages(second), tea.QuitMsg{})
	assert.Equal(t, 1, conn.closes)
}

func TestTheDeleteKeyRefusesWhatACloneIsWriting(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = pressAll(t, m, keyMsg(tea.KeyUp), keyRune('d'))

	assert.NotContains(t, plain(m.View()), databaseDeleteTitle)
	wide, _ := m.Update(tea.WindowSizeMsg{Width: 2 * testWidth, Height: testHeight})
	assert.Contains(t, statusBar(wide), "a clone is writing sales: stop it first (y in the catalog)")
}

func TestABatchIntoTheClonesTargetWaitsAndOneIntoItsSourceCommits(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := openCloneForm(t, newLoadedModel(t, conn))
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText("backup"), keyMsg(tea.KeyEnter))
	m, _ = confirmClone(t, m, mock.Name)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	refused := runQuery(t, m, `BEGIN BATCH sales.backup PARTITION "c01"; READ "o1"; COMMIT`)
	assert.Contains(t, plain(refused.View()), "a clone is writing sales.backup: the batch waits")
	assert.Empty(t, conn.batches)

	runQuery(t, m, `BEGIN BATCH sales.orders PARTITION "c01"; READ "o1"; COMMIT`)
	assert.Len(t, conn.batches, 1)
}

func TestWithAnUnknownSizeTheViewShowsNoPercentage(t *testing.T) {
	m, _ := confirmClone(t, reviewClone(t, newLoadedModel(t, newCloneConnection(t, mock.WithUnknownSize()))), mock.Name)

	view := plain(m.View())
	assert.Contains(t, view, cloneProgressTitle)
	assert.NotContains(t, view, "%")
	assert.NotContains(t, view, "about")
}

func TestACloneOfDefinitionOnlyWritesNoItem(t *testing.T) {
	conn := newCloneConnection(t)
	m, _ := openCloneForm(t, newLoadedModel(t, conn))
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyRight), keyMsg(tea.KeyEnter))
	require.Contains(t, plain(m.View()), "Copies the definition only")

	m, cmd := confirmClone(t, m, mock.Name)
	m = runSteps(m, cmd)

	assert.Contains(t, plain(m.View()), "Done.")
	require.Len(t, conn.containers, 1)
	assert.Equal(t, []string{"/customerId"}, conn.containers[0].PartitionKeys)
	assert.Empty(t, storedIn(t, conn, copyPath))
}

func TestADatabaseCloneListsEveryContainer(t *testing.T) {
	conn := newCloneConnection(t)
	m := pressAll(t, newLoadedModel(t, conn), keyRune('y'), keyMsg(tea.KeyEnter))
	require.Contains(t, plain(m.View()), "Creates database sales-copy on mock")

	m, cmd := confirmClone(t, m, mock.Name)
	m = runSteps(m, cmd)

	view := plain(m.View())
	assert.Contains(t, view, "orders  25 items")
	assert.Contains(t, view, "customers  0 items")
	assert.Len(t, storedIn(t, conn, []string{"sales-copy", "orders"}), cloneItemCount)
}

// unansweredAdmin sends a container create that never gets an answer.
type unansweredAdmin struct {
	adapter.CatalogAdmin
}

func (unansweredAdmin) CreateContainer(context.Context, adapter.ContainerSpec) error {
	return fmt.Errorf("create container: no answer: %w", adapter.ErrWriteOutcomeUnknown)
}

func TestACreateWithNoAnswerSaysTheTargetMayExist(t *testing.T) {
	conn := newCloneConnection(t)
	conn.admin = unansweredAdmin{conn.admin}

	m := cloneAll(t, newLoadedModel(t, conn))

	view := plain(m.View())
	assert.Contains(t, view, "Failed.")
	assert.Contains(t, view, "no answer came back, so what it was creating may")
	assert.NotContains(t, view, "Nothing was created")
	assert.NotContains(t, view, "d delete", "nothing known to exist is offered for deletion")
}

func TestTheCloneKeyIsInTheHelpOverlay(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newCloneConnection(t)), keyRune('?'))

	assert.Contains(t, plain(m.View()), "clone")
}

func itemID(t *testing.T, item []byte) string {
	t.Helper()
	var head struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(item, &head))
	return head.ID
}

// refusingAdmin refuses the container creates refuses picks, with the words
// a service would use.
type refusingAdmin struct {
	adapter.CatalogAdmin
	refuses func(adapter.ContainerSpec) bool
}

func (a refusingAdmin) CreateContainer(ctx context.Context, spec adapter.ContainerSpec) error {
	if a.refuses(spec) {
		return errors.New("mock: 400 Bad Request: " + spec.Name + " refused")
	}
	return a.CatalogAdmin.CreateContainer(ctx, spec)
}

func refusing(name string) func(adapter.ContainerSpec) bool {
	return func(spec adapter.ContainerSpec) bool { return spec.Name == name }
}

// cloneSales runs a clone of the whole sales database to sales-copy.
func cloneSales(t *testing.T, conn *recordingConnection) tea.Model {
	t.Helper()
	m := pressAll(t, newLoadedModel(t, conn), keyRune('y'), keyMsg(tea.KeyEnter))
	m, cmd := confirmClone(t, m, mock.Name)
	return runSteps(m, cmd)
}

func TestADatabaseCloneSaysHowManyContainersItLeftBehind(t *testing.T) {
	tests := []struct {
		name   string
		refuse string
		want   string
	}{
		{name: "the second create refused", refuse: "customers", want: "mock/sales-copy holds 1 of 2 containers."},
		{name: "the first create refused", refuse: "orders", want: "mock/sales-copy was created and holds no container yet."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newCloneConnection(t)
			conn.admin = refusingAdmin{CatalogAdmin: conn.admin, refuses: refusing(tt.refuse)}

			m := cloneSales(t, conn)

			view := plain(m.View())
			assert.Contains(t, view, "Failed.")
			assert.Contains(t, view, tt.want)
			assert.NotContains(t, view, "incomplete")
		})
	}
}

func TestStoppingAtAContainerBoundaryCountsOnlyWhatExists(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newCloneConnection(t)), keyRune('y'), keyMsg(tea.KeyEnter))
	m, cmd := confirmClone(t, m, mock.Name)
	for {
		next := messages(cmd)
		require.Len(t, next, 1)
		page, ok := next[0].(tui.ClonePageCopiedMsg)
		if ok && page.Progress.Done {
			m = pressAll(t, m, keyRune('x'))
			m, _ = m.Update(page)
			break
		}
		m, cmd = m.Update(next[0])
	}

	view := plain(m.View())
	assert.Contains(t, view, "Stopped.")
	assert.Contains(t, view, "mock/sales-copy holds 1 of 2 containers.")
}

func TestAResumeAfterTheSkipLimitCountsEachItemOnce(t *testing.T) {
	var keyless []json.RawMessage
	for i := range 101 {
		keyless = append(keyless, json.RawMessage(fmt.Sprintf(`{"id":"k%03d"}`, i)))
	}
	conn := newCloneConnection(t, mock.WithItems(ordersPath, keyless...))
	m := cloneAll(t, newLoadedModel(t, conn))
	require.Contains(t, plain(m.View()), "Failed.")
	require.Contains(t, plain(m.View()), "skipped 101")

	m = pressAll(t, m, keyRune('r'))

	view := plain(m.View())
	assert.Contains(t, view, "Done.")
	assert.Contains(t, view, "skipped 101")
	assert.Len(t, storedIn(t, conn, copyPath), cloneItemCount)
}

func TestAFirstCreateTheServiceRefusesGoesBackToTheForm(t *testing.T) {
	conn := newCloneConnection(t)
	conn.admin = refusingAdmin{CatalogAdmin: conn.admin, refuses: func(spec adapter.ContainerSpec) bool {
		return spec.Throughput.Provisioned()
	}}

	m := cloneAll(t, newLoadedModel(t, conn))

	view := plain(m.View())
	assert.Contains(t, view, cloneContainerTitle, "the form is back")
	assert.Contains(t, view, "400 Bad Request: orders-copy")
	assert.Contains(t, view, "orders-copy", "with its values")
	assert.NotContains(t, statusBar(m), "(y)", "and the slot is free")

	m = pressAll(t, m, keyMsg(tea.KeyShiftTab), keyMsg(tea.KeyRight), keyMsg(tea.KeyRight), keyMsg(tea.KeyEnter))
	m, cmd := confirmClone(t, m, mock.Name)
	m = runSteps(m, cmd)

	assert.Contains(t, plain(m.View()), "Done.")
	require.Len(t, conn.containers, 2)
	assert.False(t, conn.containers[1].Throughput.Provisioned(), "none was one keystroke away")
	assert.Len(t, storedIn(t, conn, copyPath), cloneItemCount)
}

func TestStoppingDuringACreateLetsTheCreateFinish(t *testing.T) {
	conn := newCloneConnection(t)
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)

	m = pressAll(t, m, keyRune('x'))
	m = runSteps(m, cmd)

	view := plain(m.View())
	assert.Contains(t, view, "Stopped.")
	assert.NotContains(t, view, "no answer came back")
	assert.Len(t, conn.containers, 1)
	assert.Contains(t, view, "mock/sales.orders-copy holds 0 of about 25 items and is incomplete.")
}

func TestEverySkippedItemIsNamedInTheLogOnce(t *testing.T) {
	var keyless []json.RawMessage
	for i := range 150 {
		keyless = append(keyless, json.RawMessage(fmt.Sprintf(`{"id":"k%03d"}`, i)))
	}
	conn := newCloneConnection(t, mock.WithItems(ordersPath, keyless...))
	var logged bytes.Buffer
	m := newModelWith(t, conn, tui.Options{Manage: managed, Logger: log.New(&logged)})
	m, _ = settle(m, m.Init())
	m = cloneAll(t, m)
	require.Contains(t, plain(m.View()), "Failed.")

	m = pressAll(t, m, keyRune('r'))

	require.Contains(t, plain(m.View()), "Done.")
	for i := range 150 {
		assert.Equal(t, 1, strings.Count(logged.String(), fmt.Sprintf("id=k%03d ", i)), "k%03d", i)
	}
}

func TestARefusalBehindAnotherOverlayWaitsForY(t *testing.T) {
	conn := newCloneConnection(t)
	conn.admin = refusingAdmin{CatalogAdmin: conn.admin, refuses: func(spec adapter.ContainerSpec) bool {
		return spec.Throughput.Provisioned()
	}}
	m, cmd := confirmClone(t, reviewClone(t, newLoadedModel(t, conn)), mock.Name)
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyRune('?'))

	m = runSteps(m, cmd)

	assert.Contains(t, plain(m.View()), "Catalog", "the help overlay stays up")
	assert.NotContains(t, plain(m.View()), cloneContainerTitle)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, statusBar(m), "clone refused at create (y)")

	m = pressAll(t, m, keyRune('y'))

	view := plain(m.View())
	assert.Contains(t, view, cloneContainerTitle)
	assert.Contains(t, view, "400 Bad Request: orders-copy")
	assert.Contains(t, view, "orders-copy")
}
