package panes

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	exportTitle       = "Export"
	defaultExportName = "results" + export.ExtJSON
	formatLabel       = "format  "
	formatGap         = "  "
	exportHint        = "end the name with ! to replace an existing file"
	overwriteMark     = "!"
)

var exportFormats = []struct {
	label     string
	extension string
}{
	{label: "JSON", extension: export.ExtJSON},
	{label: "CSV", extension: export.ExtCSV},
}

type ExportTarget struct {
	Path      string
	Overwrite bool
}

// ExportPrompt asks where to write the result set. Like the input it wraps,
// its value receiver hides shared pointers, so a caller must keep every
// ExportPrompt it is handed.
type ExportPrompt struct {
	frame   frame
	hints   help.Model
	keys    []key.Binding
	input   textinput.Model
	failure string
	saving  bool
}

func NewExportPrompt(keys []key.Binding) ExportPrompt {
	hints := help.New()
	hints.Styles = helpStyles()
	return ExportPrompt{
		frame: frame{title: exportTitle, focused: true},
		hints: hints,
		keys:  keys,
		input: newInput(defaultExportName, ""),
	}
}

func (p ExportPrompt) SetSize(width, height int) ExportPrompt {
	p.frame = p.frame.size(width, height)
	width, _ = p.frame.inner()
	p.input.Width = max(width-1, 1) // the cursor takes a cell past the text
	p.hints.Width = width
	return p
}

// Open starts over from the suggested name: a name typed for the last result
// set is rarely the one wanted for this one.
func (p ExportPrompt) Open() ExportPrompt {
	p.input.SetValue(defaultExportName)
	p.input.CursorEnd()
	p.input.Focus()
	p.failure = ""
	p.saving = false
	return p
}

func (p ExportPrompt) Update(msg tea.KeyMsg) (ExportPrompt, tea.Cmd) {
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.failure = ""
	return p, cmd
}

// SwitchFormat renames the file to the other format, keeping the rest of
// what was typed. An extension that names no format is part of the name.
func (p ExportPrompt) SwitchFormat() ExportPrompt {
	target := p.Target()
	if target.Path == "" {
		target.Path = defaultExportName
	}
	base, current := splitFormat(target.Path)
	name := base + otherExtension(current)
	if target.Overwrite {
		name += overwriteMark
	}
	p.input.SetValue(name)
	p.input.CursorEnd()
	p.failure = ""
	return p
}

func splitFormat(path string) (base, extension string) {
	extension = filepath.Ext(path)
	for _, format := range exportFormats {
		if strings.EqualFold(extension, format.extension) {
			return strings.TrimSuffix(path, extension), extension
		}
	}
	return path, ""
}

func otherExtension(current string) string {
	if strings.EqualFold(current, export.ExtCSV) {
		return export.ExtJSON
	}
	return export.ExtCSV
}

func (p ExportPrompt) Target() ExportTarget {
	name := strings.TrimSpace(p.input.Value())
	path, overwrite := strings.CutSuffix(name, overwriteMark)
	return ExportTarget{Path: strings.TrimSpace(path), Overwrite: overwrite}
}

func (p ExportPrompt) Saving() bool {
	return p.saving
}

func (p ExportPrompt) StartSaving() ExportPrompt {
	p.saving = true
	return p
}

// Fail shows why the export was refused and keeps the name, so it can be
// corrected rather than retyped.
func (p ExportPrompt) Fail(err error) ExportPrompt {
	p.failure = err.Error()
	p.saving = false
	return p
}

func (p ExportPrompt) View() string {
	width, height := p.frame.inner()
	lines := []string{p.input.View(), p.formatLine(), theme.HintStyle().Render(exportHint)}
	lines = append(lines, styleAll(theme.ErrorStyle(), p.failureLines(width))...)
	body := make([]string, max(height-1, len(lines)))
	copy(body, lines)
	return p.frame.render(strings.Join(append(body, p.hints.ShortHelpView(p.keys)), "\n"))
}

// formatLine marks the format the name as typed will export to, and marks
// none when its extension names no format.
func (p ExportPrompt) formatLine() string {
	current := filepath.Ext(p.Target().Path)
	line := theme.HintStyle().Render(formatLabel)
	for _, format := range exportFormats {
		style := theme.TextStyle()
		if strings.EqualFold(current, format.extension) {
			style = theme.SelectedStyle()
		}
		line += style.Render(format.label) + formatGap
	}
	return line
}

func (p ExportPrompt) failureLines(width int) []string {
	if p.failure == "" {
		return nil
	}
	return wrapText(p.failure, width)
}
