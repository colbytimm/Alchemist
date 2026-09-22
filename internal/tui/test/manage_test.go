package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// The dialog titles as they appear inside the top border.
const (
	databaseFormTitle    = " New database "
	containerFormTitle   = " New container "
	throughputFormTitle  = " Throughput "
	databaseDeleteTitle  = " Delete database "
	containerDeleteTitle = " Delete container "
)

func createDatabase(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	return pressAll(t, m, keyRune('n'), keyText(name), keyMsg(tea.KeyEnter))
}

func createContainer(t *testing.T, m tea.Model, name, partitionKey string) tea.Model {
	t.Helper()
	return pressAll(t, m, keyRune('c'), keyText(name), keyMsg(tea.KeyTab), keyText(partitionKey), keyMsg(tea.KeyEnter))
}

// newUnmanagedModel is a session whose connection manages nothing, the way a
// backend implementing neither optional interface leaves it.
func newUnmanagedModel(t *testing.T, conn adapter.Connection) tea.Model {
	t.Helper()
	m := newModelWith(t, tui.Options{Connection: conn})
	model, _ := settle(m, m.Init())
	return model
}

func TestCreatingADatabaseAddsItAndMovesTheCursorOntoIt(t *testing.T) {
	conn := newConnection(t)

	m := createDatabase(t, newLoadedModel(t, conn), "hr")

	view := plain(m.View())
	assert.Contains(t, view, "hr")
	assert.Contains(t, view, "created hr")
	assert.Equal(t, 2, conn.calls[""], "the top level is reloaded once")

	dialog := plain(pressAll(t, m, keyRune('c')).View())
	assert.Contains(t, dialog, containerFormTitle)
	assert.Contains(t, dialog, "hr", "a new container would land in the database just created")
}

func TestCreatingAContainerReloadsOnlyItsDatabase(t *testing.T) {
	conn := newConnection(t)
	m := pressAll(t, newLoadedModel(t, conn), keyMsg(tea.KeyEnter)) // expand sales

	m = createContainer(t, m, "shipments", "/tenantId, /customerId")

	assert.Contains(t, plain(m.View()), "shipments")
	assert.Equal(t, 2, conn.calls[firstDatabase], "the changed database is read again")
	assert.Equal(t, 1, conn.calls["telemetry"], "nothing else is")
	assert.Equal(t, 1, conn.calls[""], "nor the top level")

	require.Len(t, conn.containers, 1)
	assert.Equal(t, firstDatabase, conn.containers[0].Database)
	assert.Equal(t, []string{"/tenantId", "/customerId"}, conn.containers[0].PartitionKeys,
		"a hierarchical key is typed as one field and split on the commas")
}

func TestTheCursorLandsOnTheContainerJustCreated(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newConnection(t)), keyMsg(tea.KeyEnter))
	m = createContainer(t, m, "shipments", "/tenantId")

	m, msgs := press(t, m, keyMsg(tea.KeyEnter))

	require.True(t, hasMsg[tui.ScopeChangedMsg](msgs), "the cursor is on a container")
	assert.Contains(t, plain(m.View()), firstDatabase+".shipments")
}

func TestDeletingADatabaseNeedsItsNameTypedBackExactly(t *testing.T) {
	conn := newConnection(t)
	m := newLoadedModel(t, conn)

	nearMiss := pressAll(t, m, keyRune('d'), keyText("Sales"), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(nearMiss.View()), databaseDeleteTitle, "a near miss deletes nothing")
	assert.Equal(t, 1, conn.calls[""])

	deleted := pressAll(t, nearMiss, keyMsg(tea.KeyCtrlU), keyText(firstDatabase), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(deleted.View()), "deleted "+firstDatabase)
	assert.Equal(t, 2, conn.calls[""], "the top level is read again")
	survivor := plain(pressAll(t, deleted, keyRune('d')).View())
	assert.Contains(t, survivor, "telemetry", "the cursor falls to the nearest surviving node")
	assert.NotContains(t, survivor, firstDatabase, "the deleted database is gone from the tree")
}

func TestDeletingAContainerLeavesTheCursorOnItsDatabase(t *testing.T) {
	m := selectContainer(t, newLoadedModel(t, newConnection(t)))

	m = pressAll(t, m, keyRune('d'), keyText(firstContainer), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "deleted "+firstDatabase+"."+firstContainer)
	assert.Contains(t, plain(pressAll(t, m, keyRune('d')).View()), databaseDeleteTitle,
		"the cursor rests on the surviving parent")
}

func TestDeletingTheScopedContainerClearsTheScope(t *testing.T) {
	m := selectContainer(t, newLoadedModel(t, newConnection(t)))
	require.Contains(t, plain(m.View()), firstDatabase+"."+firstContainer)

	m = pressAll(t, m, keyRune('d'), keyText(firstContainer))
	m, msgs := press(t, m, keyMsg(tea.KeyEnter))

	require.True(t, hasMsg[tui.ScopeChangedMsg](msgs))
	assert.Contains(t, plain(m.View()), "no scope",
		"a scope naming a container that is gone would lie about what the next run reads")
}

func TestARefusedCreateKeepsTheFormOpenWithTheServicesWords(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpCreateContainer))
	m := pressAll(t, newLoadedModel(t, conn), keyMsg(tea.KeyEnter))

	m = createContainer(t, m, "shipments", "/tenantId")

	view := plain(m.View())
	assert.Contains(t, view, containerFormTitle)
	assert.Contains(t, view, "injected create_container error")
	assert.Contains(t, view, "shipments", "the name survives, so a retry is one edit away")
}

// A double tap on enter puts both presses in before either command runs, and
// nothing but the dialog itself stands between the second and a second create.
func TestEnterTwiceSendsOneMutation(t *testing.T) {
	conn := newConnection(t)
	m := pressAll(t, newLoadedModel(t, conn), keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyRune('c'), keyText("shipments"), keyMsg(tea.KeyTab), keyText("/tenantId"))

	sent, first := m.Update(keyMsg(tea.KeyEnter))
	_, second := sent.Update(keyMsg(tea.KeyEnter))

	assert.Nil(t, second, "the second enter asks for nothing while the first is out")
	settle(sent, tea.Batch(first, second))
	assert.Len(t, conn.containers, 1, "the adapter is asked once")
}

func TestAChangeFromADialogLeftBehindLeavesTheNextOneAlone(t *testing.T) {
	m := selectContainer(t, newLoadedModel(t, newConnection(t)))
	deleting, deleted := pressAll(t, m, keyRune('d'), keyText(firstContainer)).Update(keyMsg(tea.KeyEnter))

	m = pressAll(t, deleting, keyMsg(tea.KeyEscape), keyRune('n'))
	require.Contains(t, plain(m.View()), databaseFormTitle)
	m, _ = settle(m, deleted)

	assert.Contains(t, plain(m.View()), databaseFormTitle, "the dialog on screen is not the one that asked")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEscape)).View()),
		"deleted "+firstDatabase+"."+firstContainer, "the change itself still landed behind it")
}

// Two dialogs of the same kind on different nodes are the case an operation
// name alone cannot tell apart.
func TestADeleteLeavesTheConfirmOpenedForAnotherContainerAlone(t *testing.T) {
	m := selectContainer(t, newLoadedModel(t, newConnection(t)))
	deleting, deleted := pressAll(t, m, keyRune('d'), keyText(firstContainer)).Update(keyMsg(tea.KeyEnter))

	m = pressAll(t, deleting, keyMsg(tea.KeyEscape), keyMsg(tea.KeyDown), keyRune('d'))
	require.Contains(t, plain(m.View()), firstDatabase+".customers")
	m, _ = settle(m, deleted)

	view := plain(m.View())
	assert.Contains(t, view, containerDeleteTitle, "the dialog for the other container stays up")
	assert.Contains(t, view, firstDatabase+".customers")
}

func TestARefusalFromADialogLeftBehindIsNotShownOnTheNextOne(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpCreateContainer))
	m := selectContainer(t, newLoadedModel(t, conn))
	creating, refused := pressAll(t, m, keyRune('c'), keyText("shipments"), keyMsg(tea.KeyTab), keyText("/tenantId")).
		Update(keyMsg(tea.KeyEnter))

	m = pressAll(t, creating, keyMsg(tea.KeyEscape), keyRune('t'))
	require.Contains(t, plain(m.View()), throughputFormTitle)
	m, _ = settle(m, refused)

	view := plain(m.View())
	assert.Contains(t, view, throughputFormTitle)
	assert.NotContains(t, view, "injected", "the refusal belongs to a dialog that is gone")
}

func TestARefusedCreateCanBeRetriedOnceTheNameIsCorrected(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newConnection(t)), keyMsg(tea.KeyEnter))
	m = createContainer(t, m, firstContainer, "/tenantId")
	require.Contains(t, plain(m.View()), "already exists")

	m = pressAll(t, m, keyMsg(tea.KeyShiftTab), keyMsg(tea.KeyCtrlU), keyText("shipments"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "created "+firstDatabase+".shipments")
}

func TestTheNoticeAChangeLeavesRetiresItself(t *testing.T) {
	m := createDatabase(t, newLoadedModel(t, newConnection(t)), "hr")
	require.Contains(t, plain(m.View()), "created hr")

	m, _ = m.Update(panes.NoticeExpiredMsg{Notice: 1})

	assert.NotContains(t, plain(m.View()), "created hr",
		"a notice left standing reads as a screen that is stuck")
}

func TestSubmittingAnEmptyRequiredFieldKeepsTheFormOpen(t *testing.T) {
	conn := newConnection(t)

	m := pressAll(t, newLoadedModel(t, conn), keyRune('n'), keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, databaseFormTitle)
	assert.Contains(t, view, "name the database")
	assert.Equal(t, 1, conn.calls[""], "nothing reached the adapter")
}

func TestEscapeLeavesADialogWithoutChangingAnything(t *testing.T) {
	conn := newConnection(t)
	m := pressAll(t, newLoadedModel(t, conn), keyRune('n'), keyText("hr"))

	m, msgs := press(t, m, keyMsg(tea.KeyEscape))

	assert.Empty(t, msgs, "cancelling issues no command")
	assert.NotContains(t, plain(m.View()), databaseFormTitle)
	assert.Equal(t, 1, conn.calls[""], "the tree was left alone")
}

func TestTypedCharactersBelongToTheFieldNotTheGlobalKeys(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newConnection(t)), keyRune('n'))

	typed, quit := m.Update(keyRune('q'))
	assert.Nil(t, quit, "q is part of a name here, not the quit key")
	typed, opened := typed.Update(keyRune('d'))
	assert.Nil(t, opened)

	view := plain(typed.View())
	assert.Contains(t, view, databaseFormTitle, "d did not open a delete dialog")
	assert.Contains(t, view, "qd")
}

func TestThroughputDialogOpensOnWhatIsProvisionedAndReplacesIt(t *testing.T) {
	conn := newConnection(t)
	m := pressAll(t, selectContainer(t, newLoadedModel(t, conn)), keyRune('t'))

	view := plain(m.View())
	require.Contains(t, view, throughputFormTitle)
	assert.Contains(t, view, firstDatabase+"."+firstContainer)
	assert.Contains(t, view, "manual")
	assert.Contains(t, view, "400")

	m = pressAll(t, m, keySpace(), keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText("4000"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "throughput set on "+firstDatabase+"."+firstContainer)
	require.Len(t, conn.provisions, 1)
	assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 4000}, conn.provisions[0])
}

func TestThroughputIsNotSetOnAContainerDrawingOnItsDatabase(t *testing.T) {
	conn := newConnection(t)
	m := pressAll(t, newLoadedModel(t, conn), keyMsg(tea.KeyEnter))
	m = createContainer(t, m, "shipments", "/tenantId") // the default mode draws on the database

	m = pressAll(t, m, keyRune('t'), keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, throughputFormTitle, "the dialog stays open and says why")
	assert.Contains(t, view, "shared")
	assert.Empty(t, conn.provisions, "nothing the service would refuse reaches it")
}

func TestAFailedThroughputReadStillOpensTheDialog(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpThroughput))

	m := pressAll(t, selectContainer(t, newLoadedModel(t, conn)), keyRune('t'))

	view := plain(m.View())
	assert.Contains(t, view, throughputFormTitle)
	assert.Contains(t, view, "injected throughput error")
}

// The reload a create asks for must overtake a read of the same node that
// started before the create, or the new container never reaches the screen.
func TestAStaleReadCannotUndoWhatACreateJustAdded(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newConnection(t)), keyMsg(tea.KeyEnter))
	m = createContainer(t, m, "shipments", "/tenantId")
	require.Contains(t, plain(m.View()), "shipments")

	stale := tui.CatalogLoadedMsg{
		Parent: []string{firstDatabase},
		Nodes:  []adapter.Node{{Kind: adapter.NodeContainer, Name: firstContainer, Path: []string{firstDatabase, firstContainer}}},
		Token:  1, // the read that settled this database before the create
	}
	m, _ = settle(m.Update(stale))

	// The status bar names the new container too, so the tree is asked about
	// the sibling the stale response leaves out instead.
	assert.Contains(t, plain(m.View()), "customers",
		"a superseded response must not overwrite the list that replaced it")
}

func TestASessionThatManagesNothingOffersNothing(t *testing.T) {
	m := newUnmanagedModel(t, newConnection(t))
	settled := m.View()

	for _, binding := range []rune{'n', 'c', 'd', 't'} {
		assert.Equal(t, settled, pressAll(t, m, keyRune(binding)).View(), "%c should do nothing", binding)
	}

	help := plain(pressAll(t, m, keyRune('?')).View())
	for _, description := range []string{"new database", "new container", "delete node", "throughput"} {
		assert.NotContains(t, help, description)
	}
}

func TestABackendWithNoThroughputStillCreatesAndDeletes(t *testing.T) {
	conn := newConnection(t)
	manageWithoutThroughput := func(c adapter.Connection) tui.Management {
		return tui.Management{Admin: managed(c).Admin}
	}
	m := newModelWith(t, tui.Options{Connection: conn, Manage: manageWithoutThroughput})
	m, _ = settle(m, m.Init())

	assert.Contains(t, plain(pressAll(t, m, keyRune('n')).View()), databaseFormTitle)
	assert.NotContains(t, plain(pressAll(t, m, keyRune('?')).View()), "throughput")
}
