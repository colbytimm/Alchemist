package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
)

func (m Model) newHistoryEntry(scope []string) history.Entry {
	return newEntry(m.accounts.active, scope, m.editor.Value())
}

func newEntry(account string, scope []string, text string) history.Entry {
	return history.Entry{
		Time:    time.Now().UTC(),
		Profile: account,
		Scope:   scope,
		Query:   text,
	}
}

// recordSuccess appends the current run with the statistics of its first
// page, which is when the run is known to have produced anything.
func (m Model) recordSuccess(stats adapter.Stats) tea.Cmd {
	entry := m.historyEntry
	entry.OK = true
	entry.Rows = stats.RowCount
	entry.RequestCharge = stats.RequestCharge
	entry.ElapsedMillis = stats.Elapsed.Milliseconds()
	return m.record(entry)
}

func (m Model) recordFailure(err error) tea.Cmd {
	entry := m.historyEntry
	entry.Error = err.Error()
	return m.record(entry)
}

// openHistoryOrRefuse asks for the active account's log. With no account
// there is no log to ask for, and the overlay says why.
func (m Model) openHistoryOrRefuse() (Model, tea.Cmd) {
	if m.accounts.active == "" {
		m.historyPane = m.historyPane.SetEntries("", nil, time.Now()).Fail(errNoAccount)
		m.overlay = overlayHistory
		return m, nil
	}
	return m, m.loadHistory()
}

// openHistory shows the log as it arrived. The overlay opens on the
// response rather than on the key press, so it never shows a stale list —
// nor one of an account the session has since left.
func (m Model) openHistory(msg HistoryLoadedMsg) Model {
	if msg.Account != m.accounts.active {
		return m
	}
	m.historyPane = m.historyPane.SetEntries(msg.Account, msg.Entries, time.Now())
	m.overlay = overlayHistory
	return m
}

func (m Model) failHistory(msg ErrMsg) Model {
	if msg.Account != m.accounts.active {
		return m
	}
	m.historyPane = m.historyPane.SetEntries(msg.Account, nil, time.Now()).Fail(msg.Err)
	m.overlay = overlayHistory
	return m
}

// handleHistoryKey drives the overlay. While the filter line has the
// keyboard, typed characters narrow the list — which is why q cannot quit
// there, and only the arrow keys move the cursor. Everything else works
// the same with or without a filter.
func (m Model) handleHistoryKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.historyPane.Filtering() && typesIntoBuffer(msg) {
		return m.filterUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeHistory(), nil
	case key.Matches(msg, m.keys.History):
		m.overlay = overlayNone
	case key.Matches(msg, m.keys.Up):
		m.historyPane = m.historyPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.historyPane = m.historyPane.CursorDown()
	case key.Matches(msg, m.keys.Filter):
		m.historyPane = m.historyPane.StartFilter()
	case key.Matches(msg, m.keys.Recall):
		model, _ := m.recall()
		return model, nil
	case key.Matches(msg, m.keys.Rerun):
		return m.rerun()
	case key.Matches(msg, m.keys.SaveQuery):
		return m.saveHistoryEntry(), nil
	case m.historyPane.Filtering():
		return m.filterUpdate(msg)
	}
	return m, nil
}

func (m Model) closeHistory() Model {
	if m.historyPane.Filtering() {
		m.historyPane = m.historyPane.ClearFilter()
		return m
	}
	m.overlay = overlayNone
	return m
}

func (m Model) filterUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.historyPane, cmd = m.historyPane.Update(msg)
	return m, cmd
}

func (m Model) recall() (Model, bool) {
	entry, ok := m.historyPane.Selected()
	if !ok {
		return m, false
	}
	m.editor = m.editor.SetValue(entry.Query)
	if entry.Kind != history.KindBatch && entry.Kind != history.KindUpdate { // they name their own target, which is no scope for what runs next
		m = m.setScope(m.accounts.active, entry.Scope)
	}
	m.recalledName = ""
	m.overlay = overlayNone
	return m.setFocus(focusEditor), true
}

func (m Model) rerun() (Model, tea.Cmd) {
	recalled, ok := m.recall()
	if !ok {
		return m, nil
	}
	return recalled.startRun()
}
