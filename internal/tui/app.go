// Package tui contains the terminal user interface. The root model owns
// layout, focus, and message routing; panes live in their own files.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/internal/theme"
)

// Model is the root TUI model. In iteration 1 it renders the logo shell;
// later iterations grow the catalog, editor, and results panes.
type Model struct {
	width  int
	height int
}

// New returns the initial root model.
func New() Model {
	return Model{}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(theme.Logo())
	b.WriteString("\n\n")
	b.WriteString(theme.TaglineStyle.Render("A terminal IDE for Azure Cosmos DB"))
	b.WriteString("\n")
	b.WriteString(theme.TextStyle.Render(fmt.Sprintf("%s %s (built %s)", app.Name, app.Version, app.BuildDate)))
	b.WriteString("\n\n")
	b.WriteString(theme.HintStyle.Render("press q to quit"))

	content := b.String()
	if m.width == 0 || m.height == 0 {
		return content
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}
