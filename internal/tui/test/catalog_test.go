package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

// Fixture nodes from the mock adapter's catalog.
const (
	firstDatabase  = "sales"
	firstContainer = "orders"
)

func TestRootLoadsOnInit(t *testing.T) {
	conn := newConnection(t)

	view := newLoadedModel(t, conn).View()

	assert.Equal(t, 1, conn.calls[""])
	assert.Contains(t, view, firstDatabase)
	assert.Contains(t, view, "telemetry")
}

func TestRootLoadPrefetchesChildrenToSettleChevrons(t *testing.T) {
	conn := newConnection(t)

	newLoadedModel(t, conn)

	assert.Equal(t, 1, conn.calls[firstDatabase],
		"whether a database is worth expanding is only knowable by listing it")
	assert.Equal(t, 1, conn.calls["telemetry"])
}

func TestExpandLoadsChildrenOnceAndCachesThem(t *testing.T) {
	conn := newConnection(t)
	m := newLoadedModel(t, conn)

	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	require.Equal(t, 1, conn.calls[firstDatabase])
	assert.Contains(t, m.View(), firstContainer)

	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.NotContains(t, m.View(), firstContainer, "a second enter collapses the node")

	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Equal(t, 1, conn.calls[firstDatabase], "re-expanding must reuse the cached children")
	assert.Contains(t, m.View(), firstContainer)
}

func TestRefreshFetchesTheNodeAgain(t *testing.T) {
	conn := newConnection(t)
	m := newLoadedModel(t, conn)

	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	require.Equal(t, 1, conn.calls[firstDatabase])

	m = pressAll(t, m, keyRune('r'))
	assert.Equal(t, 2, conn.calls[firstDatabase])
	assert.Contains(t, m.View(), firstContainer)
}

func TestSpaceExpandsLikeEnter(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	m = pressAll(t, m, keySpace())

	assert.Contains(t, m.View(), firstContainer)
}

func TestSelectingAContainerChangesScope(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))
	m = pressAll(t, m, keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown))

	m, msgs := press(t, m, keyMsg(tea.KeyEnter))

	require.True(t, hasMsg[tui.ScopeChangedMsg](msgs), "selecting a container announces the scope")
	assert.Contains(t, m.View(), firstDatabase+"."+firstContainer, "status bar shows the active scope")
}

func TestSelectingADatabaseLeavesScopeAlone(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	_, msgs := press(t, m, keyMsg(tea.KeyEnter))

	assert.False(t, hasMsg[tui.ScopeChangedMsg](msgs), "only a container sets the query scope")
}

func TestExpandingAContainerShowsItsPartitionKey(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	m = pressAll(t, m, keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))

	view := m.View()
	assert.Contains(t, view, "partitionKey")
	assert.Contains(t, view, "/customerId", "the key's paths are readable, not truncated behind the label")
}

func TestChildrenFailureRendersUnderTheNode(t *testing.T) {
	m := newLoadedModel(t, newConnection(t, mock.WithError(mock.OpChildren)))

	m, msgs := press(t, m, keyMsg(tea.KeyEnter))

	// The pane is 28 columns wide, so the message wraps; assert on the word
	// that identifies it rather than on where the wrap falls.
	require.True(t, hasMsg[tui.ErrMsg](msgs))
	assert.Contains(t, m.View(), "injected")
	assert.Contains(t, m.View(), firstDatabase, "the tree survives a failed load")
}

func TestRootFailureRendersInTheCatalog(t *testing.T) {
	m := newLoadedModel(t, newConnection(t, mock.WithError(mock.OpRoot)))

	assert.Contains(t, m.View(), "injected")
}

// A failed opening fetch leaves no node to refresh, so without this the
// catalog would stay empty for the rest of the session.
func TestAFailedRootLoadCanBeRetried(t *testing.T) {
	conn := newConnection(t)
	conn.failRoot = 1
	m := newLoadedModel(t, conn)
	require.Contains(t, m.View(), "unreachable")

	m = pressAll(t, m, keyRune('r'))

	assert.Equal(t, 2, conn.calls[""], "r should ask for the top level again")
	assert.Contains(t, m.View(), firstDatabase, "the retry populates the tree")
	assert.NotContains(t, m.View(), "unreachable", "the failure clears once the retry lands")
}

func TestCursorStopsAtTheEndsOfTheTree(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	top := m.View()
	m = pressAll(t, m, keyMsg(tea.KeyUp))
	assert.Equal(t, top, m.View(), "the cursor cannot move above the first node")

	m = pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown))
	bottom := m.View()
	m = pressAll(t, m, keyMsg(tea.KeyDown))
	assert.Equal(t, bottom, m.View(), "the cursor cannot move past the last node")
}
