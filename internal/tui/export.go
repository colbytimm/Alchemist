package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const homePrefix = "~" + string(filepath.Separator)

func (m Model) openExport() Model {
	if !m.state.loaded() {
		return m
	}
	m.exportPrompt = m.exportPrompt.Open()
	m.overlay = overlayExport
	return m
}

// handleExportKey drives the prompt. Typed characters belong to the file
// name, which is why q cannot quit here. A write in flight holds the prompt
// open, so its outcome always lands on the prompt that asked for it.
func (m Model) handleExportKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.exportPrompt.Saving() {
		return m.handleSavingKey(msg)
	}
	if typesIntoBuffer(msg) {
		return m.exportPromptUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = overlayNone
		return m, nil
	case key.Matches(msg, m.keys.Save):
		return m.saveExport()
	case key.Matches(msg, m.keys.Format):
		m.exportPrompt = m.exportPrompt.SwitchFormat()
		return m, nil
	}
	return m.exportPromptUpdate(msg)
}

func (m Model) handleSavingKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) && !typesIntoBuffer(msg) {
		return m.quit()
	}
	return m, nil
}

func (m Model) saveExport() (Model, tea.Cmd) {
	m.exportPrompt = m.exportPrompt.StartSaving()
	return m, exportResults(m.exportPrompt.Target(), m.results.Fetched())
}

func (m Model) exportPromptUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.exportPrompt, cmd = m.exportPrompt.Update(msg)
	return m, cmd
}

func (m Model) finishExport(msg ExportedMsg) Model {
	m.logger.Info("exported", "path", msg.Path, "rows", msg.Rows)
	m.statusBar = m.statusBar.SetNotice(fmt.Sprintf("exported %d rows to %s", msg.Rows, msg.Path))
	m.overlay = overlayNone
	return m
}

func exportResults(target panes.ExportTarget, fetched adapter.Page) tea.Cmd {
	return func() tea.Msg {
		path, err := expandHome(target.Path)
		if err != nil {
			return ErrMsg{Op: OpExport, Err: err}
		}
		write := export.WriteFile
		if target.Overwrite {
			write = export.OverwriteFile
		}
		if err := write(path, []adapter.Page{fetched}); err != nil {
			return ErrMsg{Op: OpExport, Err: err}
		}
		return ExportedMsg{Path: target.Path, Rows: len(fetched.Rows)}
	}
}

// expandHome resolves a leading ~/, which a shell would have expanded but a
// prompt has to expand for itself.
func expandHome(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, homePrefix)
	if !ok {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("export: resolve ~: %w", err)
	}
	return filepath.Join(home, rest), nil
}
