package panes

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	saveTitle       = "Save query"
	renameTitle     = "Rename query"
	namePlaceholder = "name"
	accountLabel    = "account  "
	scopeLabel      = "scope    "
	queryLabel      = "query    "
	noSavedScope    = "none"
	nameRule        = "letters, digits, spaces, . - and _."
	saveNameHint    = nameRule + " End the name with ! to replace a saved query of that name."
)

// SaveDraft is what the save prompt was opened for.
type SaveDraft struct {
	Account string
	Text    string
	Scope   []string
	// Name seeds the field: the query being renamed, in a rename.
	Name string
}

// SaveTarget is what the save prompt would do as typed.
type SaveTarget struct {
	Account string
	Name    string
	Replace bool
}

// SavePrompt asks for the name to save a query under, or to rename one to.
// Like the input it wraps, its value receiver hides shared pointers, so a
// caller must keep every SavePrompt it is handed.
type SavePrompt struct {
	frame    frame
	hints    help.Model
	keys     []key.Binding
	input    textinput.Model
	draft    SaveDraft
	renaming bool
	failure  string
	saving   bool
}

func NewSavePrompt(keys []key.Binding) SavePrompt {
	hints := help.New()
	return SavePrompt{
		frame: frame{title: saveTitle, focused: true},
		hints: hints,
		keys:  keys,
		input: newInput(namePlaceholder, ""),
	}
}

func (p SavePrompt) SetSize(width, height int) SavePrompt {
	p.frame = p.frame.size(width, height)
	width, _ = p.frame.inner()
	p.input.Width = max(width-1, 1) // the cursor takes a cell past the text
	p.hints.Width = width
	return p
}

// OpenSave asks for a name to save draft under. The overwrite mark is never
// seeded: replacing is always typed.
func (p SavePrompt) OpenSave(draft SaveDraft) SavePrompt {
	p.frame.title = saveTitle
	p.renaming = false
	return p.open(draft)
}

// OpenRename asks for the new name of the query draft names.
func (p SavePrompt) OpenRename(draft SaveDraft) SavePrompt {
	p.frame.title = renameTitle
	p.renaming = true
	return p.open(draft)
}

func (p SavePrompt) open(draft SaveDraft) SavePrompt {
	p.draft = draft
	p.input.SetValue(draft.Name)
	p.input.CursorEnd()
	p.input.Focus()
	p.failure = ""
	p.saving = false
	return p
}

func (p SavePrompt) Renaming() bool {
	return p.renaming
}

func (p SavePrompt) Draft() SaveDraft {
	return p.draft
}

// Target reads a trailing overwrite mark as replace, except in a rename,
// which never replaces: there the mark stays in the name, to be refused.
func (p SavePrompt) Target() SaveTarget {
	typed := strings.TrimSpace(p.input.Value())
	if p.renaming {
		return SaveTarget{Account: p.draft.Account, Name: typed}
	}
	name, replace := strings.CutSuffix(typed, overwriteMark)
	return SaveTarget{Account: p.draft.Account, Name: strings.TrimSpace(name), Replace: replace}
}

func (p SavePrompt) Update(msg tea.KeyMsg) (SavePrompt, tea.Cmd) {
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.failure = ""
	return p, cmd
}

func (p SavePrompt) Saving() bool {
	return p.saving
}

func (p SavePrompt) StartSaving() SavePrompt {
	p.saving = true
	return p
}

// Fail shows why the save was refused and keeps the name, so it can be
// corrected rather than retyped.
func (p SavePrompt) Fail(err error) SavePrompt {
	p.failure = err.Error()
	p.saving = false
	return p
}

func (p SavePrompt) View() string {
	width, _ := p.frame.inner()
	lines := []string{inputView(p.input), p.field(accountLabel, p.draft.Account)}
	if !p.renaming {
		lines = append(lines, p.field(scopeLabel, draftScope(p.draft.Scope)), p.field(queryLabel, firstLine(p.draft.Text)))
	}
	lines = append(lines, "")
	lines = append(lines, styleAll(theme.HintStyle(), wrapText(p.hint(), width))...)
	lines = append(lines, styleAll(theme.ErrorStyle(), failureLines(p.failure, width))...)
	return p.frame.renderWithHint(lines, themedHelp(p.hints).ShortHelpView(p.keys))
}

// hint leaves the overwrite mark out of a rename, which never replaces.
func (p SavePrompt) hint() string {
	if p.renaming {
		return nameRule
	}
	return saveNameHint
}

func (p SavePrompt) field(label, value string) string {
	return theme.HintStyle().Render(label) + theme.TextStyle().Render(value)
}

func draftScope(scope []string) string {
	if len(scope) == 0 {
		return noSavedScope
	}
	return scopeText(scope)
}
