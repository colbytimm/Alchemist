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
	destinationLabel  = "saves to "
	exportHint        = "type a name, or a path such as ~/exports/orders.csv; missing folders are created. " +
		"End it with ! to replace an existing file."
	overwriteMark = "!"
)

// ExportFormat is a format the prompt offers, named by its extension.
type ExportFormat struct {
	Label     string
	Extension string
}

var resultFormats = []ExportFormat{
	{Label: "JSON", Extension: export.ExtJSON},
	{Label: "CSV", Extension: export.ExtCSV},
}

type ExportTarget struct {
	Path      string
	Overwrite bool
}

// ExportPrompt asks where to write the result set. Like the input it wraps,
// its value receiver hides shared pointers, so a caller must keep every
// ExportPrompt it is handed.
type ExportPrompt struct {
	frame       frame
	hints       help.Model
	keys        []key.Binding
	input       textinput.Model
	defaultName string
	formats     []ExportFormat
	failure     string
	saving      bool
}

func NewExportPrompt(keys []key.Binding) ExportPrompt {
	hints := help.New()
	return ExportPrompt{
		frame:       frame{title: exportTitle, focused: true},
		hints:       hints,
		keys:        keys,
		input:       newInput(defaultExportName, ""),
		defaultName: defaultExportName,
		formats:     resultFormats,
	}
}

// WithFormats offers formats, cycled in order, and suggests defaultName.
func (p ExportPrompt) WithFormats(title, defaultName string, formats ...ExportFormat) ExportPrompt {
	p.frame.title = title
	p.defaultName, p.formats = defaultName, formats
	p.input.Placeholder = defaultName
	return p
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
	p.input.SetValue(p.defaultName)
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

// SwitchFormat renames the file to the next format, keeping the rest of
// what was typed. An extension that names no format is part of the name.
func (p ExportPrompt) SwitchFormat() ExportPrompt {
	target := p.Target()
	if target.Path == "" {
		target.Path = p.defaultName
	}
	base, current := p.splitFormat(target.Path)
	name := base + p.nextExtension(current)
	if target.Overwrite {
		name += overwriteMark
	}
	p.input.SetValue(name)
	p.input.CursorEnd()
	p.failure = ""
	return p
}

func (p ExportPrompt) splitFormat(path string) (base, extension string) {
	extension = filepath.Ext(path)
	if p.formatIndex(extension) < 0 {
		return path, ""
	}
	return strings.TrimSuffix(path, extension), extension
}

// formatIndex is the place among the formats of the one extension names,
// or -1.
func (p ExportPrompt) formatIndex(extension string) int {
	for i, format := range p.formats {
		if strings.EqualFold(extension, format.Extension) {
			return i
		}
	}
	return -1
}

// nextExtension is the format after current, or, when current names none,
// the one after the suggested name's: a switch always changes the format
// the prompt shows as chosen.
func (p ExportPrompt) nextExtension(current string) string {
	i := p.formatIndex(current)
	if i < 0 {
		i = p.formatIndex(filepath.Ext(p.defaultName))
	}
	return p.formats[(i+1)%len(p.formats)].Extension
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
	width, _ := p.frame.inner()
	lines := []string{inputView(p.input)}
	lines = append(lines, styleAll(theme.TextStyle(), p.destinationLines(width))...)
	lines = append(lines, p.formatLine(), "")
	lines = append(lines, styleAll(theme.HintStyle(), wrapText(exportHint, width))...)
	lines = append(lines, styleAll(theme.ErrorStyle(), failureLines(p.failure, width))...)
	return p.frame.renderWithHint(lines, themedHelp(p.hints).ShortHelpView(p.keys))
}

// destinationLines spell out where the name as typed resolves to, so a bare
// name is seen to land in the working directory and a path is seen to work.
func (p ExportPrompt) destinationLines(width int) []string {
	path, err := export.ResolvePath(p.Target().Path)
	if err != nil {
		return nil
	}
	return wrapText(destinationLabel+path, width)
}

// formatLine marks the format the name as typed will export to, and marks
// none when its extension names no format.
func (p ExportPrompt) formatLine() string {
	current := filepath.Ext(p.Target().Path)
	line := theme.HintStyle().Render(formatLabel)
	for _, format := range p.formats {
		style := theme.TextStyle()
		if strings.EqualFold(current, format.Extension) {
			style = theme.SelectedStyle()
		}
		line += style.Render(format.Label) + formatGap
	}
	return line
}
