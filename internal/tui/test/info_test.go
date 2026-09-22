package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

// The overlay title as it appears inside the top border.
const infoTitle = " Info "

func openInfo(t *testing.T, conn *recordingConnection) tea.Model {
	t.Helper()
	return pressAll(t, selectContainer(t, newLoadedModel(t, conn)), keyRune('i'))
}

func TestInfoOpensOnTheSelectedNodeWithOneRequest(t *testing.T) {
	conn := newConnection(t)

	m := openInfo(t, conn)

	view := plain(m.View())
	assert.Contains(t, view, infoTitle)
	assert.Contains(t, view, firstDatabase+"."+firstContainer)
	assert.Contains(t, view, "4.2 MB", "the adapter's answer is on screen")
	assert.Equal(t, [][]string{{firstDatabase, firstContainer}}, conn.inspected)
}

func TestInfoWithNothingSelectedDoesNothing(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpRoot))
	m := newLoadedModel(t, conn)

	_, cmd := m.Update(keyRune('i'))

	assert.Nil(t, cmd)
	assert.NotContains(t, plain(m.View()), infoTitle)
	assert.Empty(t, conn.inspected)
}

func TestInfoRefreshAsksAboutTheSameNodeAndLeavesTheTreeAlone(t *testing.T) {
	conn := newConnection(t)
	m := openInfo(t, conn)
	before := conn.calls[firstDatabase]

	m = pressAll(t, m, keyRune('r'))

	assert.Contains(t, plain(m.View()), infoTitle, "the overlay stays up")
	assert.Equal(t, [][]string{{firstDatabase, firstContainer}, {firstDatabase, firstContainer}}, conn.inspected)
	assert.Equal(t, before, conn.calls[firstDatabase], "r inside the overlay refreshes nothing behind it")
}

func TestInfoClosesOnEscapeAndOnItsOwnKey(t *testing.T) {
	conn := newConnection(t)
	m := openInfo(t, conn)

	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEscape)).View()), catalogTitle)
	assert.Contains(t, plain(pressAll(t, m, keyRune('i')).View()), catalogTitle)
}

func TestInfoDoesNotQuitOnQButStillOnCtrlC(t *testing.T) {
	m := openInfo(t, newConnection(t))

	_, cmd := m.Update(keyRune('q'))
	assert.Nil(t, cmd)

	_, cmd = m.Update(keyMsg(tea.KeyCtrlC))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestReopeningANodeAlreadyReadCostsNothing(t *testing.T) {
	conn := newConnection(t)
	m := openInfo(t, conn)

	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyRune('i'))

	assert.Contains(t, plain(m.View()), "4.2 MB")
	assert.Len(t, conn.inspected, 1, "the second opening is served from memory")
}

func TestInfoScrollsWithTheArrowKeys(t *testing.T) {
	m := openInfo(t, newConnection(t))
	m, _ = m.Update(tea.WindowSizeMsg{Width: testWidth, Height: 8})

	top := m.View()
	scrolled := pressAll(t, m, keyMsg(tea.KeyDown))
	assert.NotEqual(t, top, scrolled.View())
	assert.Equal(t, top, pressAll(t, scrolled, keyMsg(tea.KeyUp)).View())
}

func TestASharedContainerShowsTheInheritedNoteNotAnError(t *testing.T) {
	m := pressAll(t, newLoadedModel(t, newConnection(t)),
		keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown), keyRune('i')) // telemetry.events

	view := plain(m.View())
	require.Contains(t, view, "telemetry.events")
	assert.Contains(t, view, "Inherited from database telemetry")
}

func TestAFailedInspectShowsTheFailureAndKeepsTheSessionAlive(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpInspect))

	m := openInfo(t, conn)

	view := plain(m.View())
	assert.Contains(t, view, infoTitle)
	assert.Contains(t, view, firstContainer, "the header drawn from the tree survives")
	assert.Contains(t, view, "injected inspect error")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEscape)).View()), catalogTitle)
}

func TestASessionWithNoInspectorOffersNoInfo(t *testing.T) {
	m := newUnmanagedModel(t, newConnection(t))
	settled := m.View()

	assert.Equal(t, settled, pressAll(t, m, keyRune('i')).View())
	assert.NotContains(t, plain(pressAll(t, m, keyRune('?')).View()), "node info")
}
