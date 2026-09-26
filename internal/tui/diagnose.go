package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// diagnoseDelay is the pause in typing after which the buffer is checked.
// The lexical checks need no pause: they come with every edit's highlight.
const diagnoseDelay = 150 * time.Millisecond

type diagnoseMsg struct {
	diagnosis int
}

// diagnoseAfterPause asks for the check numbered diagnosis once typing
// pauses. Every edit numbers a new one, so of a burst of keys only the last
// one's check is still wanted when it arrives.
func diagnoseAfterPause(diagnosis int) tea.Cmd {
	return tea.Tick(diagnoseDelay, func(time.Time) tea.Msg { return diagnoseMsg{diagnosis: diagnosis} })
}

func (m Model) diagnose(msg diagnoseMsg) Model {
	if msg.diagnosis != m.diagnosis {
		return m
	}
	m.editor = m.editor.Diagnose()
	return m
}
