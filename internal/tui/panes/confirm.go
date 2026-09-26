package panes

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	databaseDeleteTitle  = "Delete database"
	containerDeleteTitle = "Delete container"
	confirmPrompt        = "> "
	deleteHint           = "enter delete · esc cancel"
)

// nameField is where a name is typed back before something irreversible
// runs. Every such confirmation in the TUI is this one field.
type nameField struct {
	input    textinput.Model
	expected string
}

func newNameField(expected string) nameField {
	input := newInput("", "")
	input.Prompt = confirmPrompt
	input.PromptStyle = theme.HintStyle()
	input.Focus()
	return nameField{input: input, expected: expected}
}

func (f nameField) setWidth(width int) nameField {
	f.input.Width = max(width-len(confirmPrompt)-1, 1)
	return f
}

func (f nameField) update(msg tea.KeyMsg) (nameField, tea.Cmd) {
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

// matches reports whether what was typed is the name asked for, character
// for character: a near miss is a different resource.
func (f nameField) matches() bool {
	return f.input.Value() == f.expected
}

func (f nameField) view() string {
	return f.input.View()
}

// Confirm asks for a name back before something irreversible runs. Like the
// input it wraps, its value receiver hides shared pointers, so a caller must
// keep every Confirm it is handed.
type Confirm struct {
	frame       frame
	icons       theme.IconSet
	consequence string
	prompt      string
	name        nameField
	failure     string
	submitting  bool
}

func NewDatabaseDelete(icons theme.IconSet, name string) Confirm {
	return Confirm{
		frame: frame{title: databaseDeleteTitle, focused: true},
		icons: icons,
		consequence: "Deleting " + name + " removes every container in it and all of their " +
			"documents. This cannot be undone.",
		prompt: "Type the database name to confirm:",
		name:   newNameField(name),
	}
}

func NewContainerDelete(icons theme.IconSet, path []string) Confirm {
	name := path[len(path)-1]
	return Confirm{
		frame: frame{title: containerDeleteTitle, focused: true},
		icons: icons,
		consequence: "Deleting " + strings.Join(path, ".") + " removes every document in it. " +
			"This cannot be undone.",
		prompt: "Type the container name to confirm:",
		name:   newNameField(name),
	}
}

func (c Confirm) SetSize(width, height int) Confirm {
	c.frame = c.frame.size(width, height)
	width, _ = c.frame.inner()
	c.name = c.name.setWidth(width)
	return c
}

func (c Confirm) Update(msg tea.KeyMsg) (Confirm, tea.Cmd) {
	var cmd tea.Cmd
	c.name, cmd = c.name.update(msg)
	c.failure = ""
	return c, cmd
}

func (c Confirm) Confirmed() bool {
	return c.name.matches()
}

// Submitting reports whether the deletion this dialog asked for is in flight.
func (c Confirm) Submitting() bool {
	return c.submitting
}

// StartSubmitting marks the deletion as sent, so a second enter cannot send
// it again while the first is still out.
func (c Confirm) StartSubmitting() Confirm {
	c.submitting = true
	c.failure = ""
	return c
}

// Fail shows why the deletion was refused and keeps what was typed.
func (c Confirm) Fail(err error) Confirm {
	c.failure = err.Error()
	c.submitting = false
	return c
}

func (c Confirm) View() string {
	width, _ := c.frame.inner()
	lines := styleAll(theme.TextStyle(), wrapText(c.consequence, width))
	lines = append(lines, "", theme.TextStyle().Render(c.prompt), c.name.view(), "")
	lines = append(lines, styleAll(theme.ErrorStyle(), failureLines(c.failureText(), width))...)
	return c.frame.renderWithHint(lines, theme.HintStyle().Render(deleteHint))
}

func (c Confirm) failureText() string {
	if c.failure == "" {
		return ""
	}
	return c.icons.Failure + " " + c.failure
}
