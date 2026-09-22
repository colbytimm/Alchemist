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

// Confirm asks for a name back before something irreversible runs. Like the
// input it wraps, its value receiver hides shared pointers, so a caller must
// keep every Confirm it is handed.
type Confirm struct {
	frame       frame
	icons       theme.IconSet
	consequence string
	prompt      string
	expected    string
	input       textinput.Model
	failure     string
	submitting  bool
}

func NewDatabaseDelete(icons theme.IconSet, name string) Confirm {
	return Confirm{
		frame: frame{title: databaseDeleteTitle, focused: true},
		icons: icons,
		consequence: "Deleting " + name + " removes every container in it and all of their " +
			"documents. This cannot be undone.",
		prompt:   "Type the database name to confirm:",
		expected: name,
		input:    confirmInput(),
	}
}

func NewContainerDelete(icons theme.IconSet, path []string) Confirm {
	name := path[len(path)-1]
	return Confirm{
		frame: frame{title: containerDeleteTitle, focused: true},
		icons: icons,
		consequence: "Deleting " + strings.Join(path, ".") + " removes every document in it. " +
			"This cannot be undone.",
		prompt:   "Type the container name to confirm:",
		expected: name,
		input:    confirmInput(),
	}
}

func confirmInput() textinput.Model {
	input := newInput("", "")
	input.Prompt = confirmPrompt
	input.PromptStyle = theme.HintStyle()
	input.Focus()
	return input
}

func (c Confirm) SetSize(width, height int) Confirm {
	c.frame = c.frame.size(width, height)
	width, _ = c.frame.inner()
	c.input.Width = max(width-len(confirmPrompt)-1, 1)
	return c
}

func (c Confirm) Update(msg tea.KeyMsg) (Confirm, tea.Cmd) {
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	c.failure = ""
	return c, cmd
}

// Confirmed reports whether what was typed is the name asked for, character
// for character: a near miss is a different resource.
func (c Confirm) Confirmed() bool {
	return c.input.Value() == c.expected
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
	lines = append(lines, "", theme.TextStyle().Render(c.prompt), c.input.View(), "")
	lines = append(lines, styleAll(theme.ErrorStyle(), failureLines(c.failureText(), width))...)
	return c.frame.renderWithHint(lines, theme.HintStyle().Render(deleteHint))
}

func (c Confirm) failureText() string {
	if c.failure == "" {
		return ""
	}
	return c.icons.Failure + " " + c.failure
}
