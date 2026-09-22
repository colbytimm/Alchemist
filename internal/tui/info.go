package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// openInfo opens the overlay on the node under the cursor. A node already
// read is shown from memory; only a refresh asks the adapter about it again.
func (m Model) openInfo() (Model, tea.Cmd) {
	node, ok := m.catalogPane.SelectedNode()
	if !ok {
		return m, nil
	}
	m.overlay = overlayInfo
	m.info = m.info.Show(node)
	if m.info.Loaded() {
		return m, nil
	}
	return m.inspectShown()
}

func (m Model) inspectShown() (Model, tea.Cmd) {
	var tick tea.Cmd
	m.info, tick = m.info.StartLoading()
	return m, tea.Batch(tick, m.inspect(m.info.Node()))
}

// handleInfoKey keeps every key inside the overlay: r re-inspects the node on
// screen rather than refreshing the tree behind it, and q, a plain letter,
// does not quit out from under a half-read screen the way ctrl+c still does.
func (m Model) handleInfoKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit) && !typesIntoBuffer(msg):
		return m.quit()
	case key.Matches(msg, m.keys.Close), key.Matches(msg, m.keys.Info):
		m.overlay = overlayNone
	case key.Matches(msg, m.keys.Up):
		m.info = m.info.ScrollUp()
	case key.Matches(msg, m.keys.Down):
		m.info = m.info.ScrollDown()
	case key.Matches(msg, m.keys.Refresh):
		return m.inspectShown()
	}
	return m, nil
}
