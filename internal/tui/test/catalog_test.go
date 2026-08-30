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
	catalog := newCatalog(t)
	view := loaded(t, catalog).View()

	assert.Equal(t, 1, catalog.calls[""])
	assert.Contains(t, view, firstDatabase)
	assert.Contains(t, view, "telemetry")
}

func TestExpandLoadsChildrenOnceAndCachesThem(t *testing.T) {
	catalog := newCatalog(t)
	m := loaded(t, catalog)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 1, catalog.calls[firstDatabase])
	assert.Contains(t, m.View(), firstContainer)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotContains(t, m.View(), firstContainer, "a second enter collapses the node")

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, 1, catalog.calls[firstDatabase], "re-expanding must reuse the cached children")
	assert.Contains(t, m.View(), firstContainer)
}

func TestRefreshFetchesTheNodeAgain(t *testing.T) {
	catalog := newCatalog(t)
	m := loaded(t, catalog)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 1, catalog.calls[firstDatabase])

	m, _ = press(t, m, keyRune('r'))
	assert.Equal(t, 2, catalog.calls[firstDatabase])
	assert.Contains(t, m.View(), firstContainer)
}

func TestSpaceExpandsLikeEnter(t *testing.T) {
	m := loaded(t, newCatalog(t))

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	assert.Contains(t, m.View(), firstContainer)
}

func TestSelectingAContainerChangesScope(t *testing.T) {
	m := loaded(t, newCatalog(t))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyDown})

	m, msgs := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	require.True(t, has[tui.ScopeChangedMsg](msgs), "selecting a container announces the scope")
	assert.Contains(t, m.View(), firstDatabase+"."+firstContainer, "status bar shows the active scope")
}

func TestSelectingADatabaseLeavesScopeAlone(t *testing.T) {
	m := loaded(t, newCatalog(t))

	_, msgs := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.False(t, has[tui.ScopeChangedMsg](msgs), "only a container sets the query scope")
}

func TestExpandingAContainerShowsItsPartitionKey(t *testing.T) {
	m := loaded(t, newCatalog(t))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyDown})

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Contains(t, m.View(), "partitionKey")
}

func TestChildrenFailureRendersUnderTheNode(t *testing.T) {
	catalog := newCatalog(t, mock.WithError(mock.OpChildren))
	m := loaded(t, catalog)

	m, msgs := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// The pane is 28 columns wide, so the message wraps; assert on the word
	// that identifies it rather than on where the wrap falls.
	require.True(t, has[tui.ErrMsg](msgs))
	assert.Contains(t, m.View(), "injected")
	assert.Contains(t, m.View(), firstDatabase, "the tree survives a failed load")
}

func TestRootFailureRendersInTheCatalog(t *testing.T) {
	m := loaded(t, newCatalog(t, mock.WithError(mock.OpRoot)))

	assert.Contains(t, m.View(), "injected")
}

func TestCursorStopsAtTheEndsOfTheTree(t *testing.T) {
	m := loaded(t, newCatalog(t))

	top := m.View()
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, top, m.View(), "the cursor cannot move above the first node")

	for i := 0; i < 5; i++ {
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	bottom := m.View()
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, bottom, m.View(), "the cursor cannot move past the last node")
}
