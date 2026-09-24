package panes_test

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func databaseDelete() panes.Confirm {
	return panes.NewDatabaseDelete(theme.Icons(), "sales").SetSize(dialogWidth, dialogHeight)
}

func containerDelete() panes.Confirm {
	return panes.NewContainerDelete(theme.Icons(), []string{"sales", "orders"}).
		SetSize(dialogWidth, dialogHeight)
}

func typeName(c panes.Confirm, text string) panes.Confirm {
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return c
}

func TestDatabaseDeleteSpellsOutWhatGoes(t *testing.T) {
	view := plain(databaseDelete().View())

	assert.Contains(t, view, " Delete database ")
	assert.Contains(t, view, "sales")
	assert.Contains(t, view, "every container")
	assert.Contains(t, view, "cannot be undone")
	assert.Contains(t, view, "Type the database name to confirm")
	assert.Contains(t, view, "enter delete")
}

func TestContainerDeleteNamesTheContainerInFull(t *testing.T) {
	view := plain(containerDelete().View())

	assert.Contains(t, view, " Delete container ")
	assert.Contains(t, view, "sales.orders")
	assert.Contains(t, view, "Type the container name to confirm")
}

func TestConfirmFillsItsFrameExactly(t *testing.T) {
	view := databaseDelete().View()

	assert.Equal(t, dialogWidth, lipgloss.Width(view))
	assert.Equal(t, dialogHeight, lipgloss.Height(view))
}

func TestConfirmWaitsForTheNameTypedBackExactly(t *testing.T) {
	tests := []struct {
		name  string
		typed string
		want  bool
	}{
		{name: "nothing typed", typed: "", want: false},
		{name: "a prefix", typed: "sale", want: false},
		{name: "the wrong case", typed: "Sales", want: false},
		{name: "a trailing space", typed: "sales ", want: false},
		{name: "another database", typed: "telemetry", want: false},
		{name: "the name itself", typed: "sales", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, typeName(databaseDelete(), tt.typed).Confirmed())
		})
	}
}

func TestContainerDeleteAsksForTheContainerNameAlone(t *testing.T) {
	require.False(t, typeName(containerDelete(), "sales.orders").Confirmed(),
		"the prompt asks for the container, not the path it is rendered under")
	assert.True(t, typeName(containerDelete(), "orders").Confirmed())
}

func TestConfirmShowsWhyTheDeletionWasRefused(t *testing.T) {
	c := typeName(databaseDelete(), "sales").Fail(errors.New("cosmos: delete database: 403 Forbidden"))

	view := plain(c.View())
	assert.Contains(t, view, "403 Forbidden")
	assert.Contains(t, view, "> sales", "what was typed survives the refusal")
}
