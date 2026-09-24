package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// openInfo opens the overlay on the node under the cursor. A node already
// read is shown from memory; only a refresh asks the adapter about it again.
func (m Model) openInfo() (Model, tea.Cmd) {
	node, ok := m.catalogPane().SelectedNode()
	if !ok {
		return m, nil
	}
	m.overlay = overlayInfo
	m = m.setActiveInfo(m.activeInfo().Show(node))
	if m.activeInfo().Loaded() {
		return m, nil
	}
	return m.inspectShown()
}

func (m Model) inspectShown() (Model, tea.Cmd) {
	info, tick := m.activeInfo().StartLoading()
	m = m.setActiveInfo(info)
	return m, tea.Batch(tick, m.inspect(info.Node()))
}

// activeInfo is the active account's overlay: each account keeps the details
// it has read, since one path can name a node in several accounts.
func (m Model) activeInfo() panes.Info {
	entry, _ := m.accounts.get(m.accounts.active)
	return entry.info
}

func (m Model) setActiveInfo(info panes.Info) Model {
	if entry, ok := m.accounts.get(m.accounts.active); ok {
		entry.info = info
		m.accounts.put(entry)
	}
	return m
}

// fileDetails keeps what was read in the account it was read for, on screen
// or not — and only on the connection it was read on.
func (m Model) fileDetails(msg DetailsLoadedMsg) Model {
	if entry, ok := m.accounts.get(msg.Account); ok && entry.readOn(msg.attempt) {
		entry.info = entry.info.SetDetails(msg.Path, msg.Details)
		m.accounts.put(entry)
	}
	return m
}

func (m Model) failInspect(msg ErrMsg) Model {
	if entry, ok := m.accounts.get(msg.Account); ok && entry.readOn(msg.attempt) {
		entry.info = entry.info.Fail(msg.Path, msg.Err)
		m.accounts.put(entry)
	}
	return m
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
		m = m.setActiveInfo(m.activeInfo().ScrollUp())
	case key.Matches(msg, m.keys.Down):
		m = m.setActiveInfo(m.activeInfo().ScrollDown())
	case key.Matches(msg, m.keys.Refresh):
		return m.inspectShown()
	}
	return m, nil
}
