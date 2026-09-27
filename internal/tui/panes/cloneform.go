package panes

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	cloneContainerTitle = "Clone container"
	cloneDatabaseTitle  = "Clone database"
	cloneFormHint       = "enter review · tab next field · ←/→ change · esc cancel"
	copySuffix          = "-copy"
)

var (
	errNoTargetDatabase  = errors.New("name the database the copy goes in")
	errNoCopyName        = errors.New("name the copy")
	errNoWritableAccount = errors.New("no account can be written to, so there is nowhere to clone to")
	errStillReading      = errors.New("still reading the source")
)

var fidelities = []adapter.DefinitionFidelity{adapter.DefinitionFull, adapter.DefinitionPortable}

// CloneTarget is an account the form offers as a destination, with the state
// the switcher shows it in.
type CloneTarget struct {
	Name  string
	State AccountState
}

// CloneDefaults are what the fields start from for one target account. A
// field the user has changed keeps what they made it.
type CloneDefaults struct {
	Name     string
	Fidelity adapter.DefinitionFidelity
	Capacity clone.Capacity
}

type CloneChoice struct {
	Target   clone.Endpoint
	Content  clone.Content
	Fidelity adapter.DefinitionFidelity
	Capacity clone.Capacity
}

// CloneForm collects where a clone goes and what it carries. Its first block
// describes the source, and fills in once the source has been read. Like
// Form, its value receiver hides shared state: keep every CloneForm it hands
// back.
type CloneForm struct {
	frame      frame
	icons      theme.IconSet
	source     clone.Endpoint
	survey     clone.Survey
	surveyed   bool
	targets    []CloneTarget
	readOnly   []string
	contents   []clone.Content
	capacities []clone.Capacity
	fields     []formField
	focus      int
	status     string
	failure    string
}

// NewCloneForm opens on the source's own account when it is among targets,
// and on the first of them otherwise. readOnly are the accounts left out for
// being read-only; items is whether the source's items can be read.
func NewCloneForm(icons theme.IconSet, source clone.Endpoint, targets []CloneTarget, readOnly []string, items bool) CloneForm {
	f := CloneForm{
		frame:      frame{title: cloneContainerTitle, focused: true},
		icons:      icons,
		source:     source,
		targets:    targets,
		readOnly:   readOnly,
		contents:   []clone.Content{clone.DefinitionOnly},
		capacities: []clone.Capacity{clone.Minimum, clone.None},
	}
	if items {
		f.contents = []clone.Content{clone.DefinitionAndItems, clone.DefinitionOnly}
	}
	if !source.Container() {
		f.frame.title = cloneDatabaseTitle
	}
	f.fields = f.newFields()
	return f.setFocus(0)
}

func (f CloneForm) newFields() []formField {
	fields := []formField{f.targetField(f.source.Account)}
	if f.source.Container() {
		database := textField(FieldDatabase, "Database", "sales", errNoTargetDatabase)
		database.input.SetValue(f.source.Path[0])
		fields = append(fields, database)
	}
	fields = append(fields,
		textField(FieldName, "Name", "orders"+copySuffix, errNoCopyName),
		choiceField(FieldContent, "Copy", f.contents),
		choiceField(FieldFidelity, "Definition", fidelities),
		choiceField(FieldCapacity, "Throughput", f.capacities),
	)
	return fields
}

// targetField offers every target with its state, on the one called chosen
// when it is among them.
func (f CloneForm) targetField(chosen string) formField {
	target := formField{name: FieldTarget, label: "Target account"}
	if len(f.targets) == 0 {
		target.options = []string{"none"}
	}
	for i, t := range f.targets {
		target.options = append(target.options, t.Name+" · "+accountStateText(t.State))
		if t.Name == chosen {
			target.choice = i
		}
	}
	return target
}

// SetTargets lists the targets as the accounts stand now, staying on the one
// chosen.
func (f CloneForm) SetTargets(targets []CloneTarget, readOnly []string) CloneForm {
	chosen := f.Target()
	f.targets, f.readOnly = targets, readOnly
	f.fields = slices.Clone(f.fields)
	i := slices.IndexFunc(f.fields, func(field formField) bool { return field.name == FieldTarget })
	f.fields[i] = f.targetField(chosen)
	return f
}

func choiceField[T fmt.Stringer](name FormField, label string, values []T) formField {
	field := formField{name: name, label: label}
	for _, value := range values {
		field.options = append(field.options, value.String())
	}
	return field
}

func accountStateText(state AccountState) string {
	switch state {
	case AccountConnected:
		return "connected"
	case AccountConnecting:
		return "connecting…"
	case AccountFailed:
		return "failed"
	}
	return "not connected"
}

// SetSurvey fills in the source block, and offers the source's own
// throughput as a choice once it is known.
func (f CloneForm) SetSurvey(survey clone.Survey) CloneForm {
	f.survey, f.surveyed = survey, true
	f.capacities = []clone.Capacity{clone.Minimum, clone.None}
	if survey.ThroughputKnown {
		f.capacities = []clone.Capacity{clone.Minimum, clone.SameAsSource, clone.None}
	}
	i := slices.IndexFunc(f.fields, func(field formField) bool { return field.name == FieldCapacity })
	chosen := f.capacity()
	f.fields[i] = choiceField(FieldCapacity, "Throughput", f.capacities)
	f.fields[i].choice = max(slices.Index(f.capacities, chosen), 0)
	return f
}

func (f CloneForm) Surveyed() bool { return f.surveyed }

func (f CloneForm) Survey() clone.Survey { return f.survey }

func (f CloneForm) ApplyDefaults(d CloneDefaults) CloneForm {
	for i := range f.fields {
		field := &f.fields[i]
		switch field.name {
		case FieldName:
			if field.input.Value() == field.defaulted {
				field.input.SetValue(d.Name)
				field.defaulted = d.Name
			}
		case FieldFidelity:
			if !field.touched {
				field.choice = max(slices.Index(fidelities, d.Fidelity), 0)
			}
		case FieldCapacity:
			if !field.touched {
				field.choice = max(slices.Index(f.capacities, d.Capacity), 0)
			}
		}
	}
	return f
}

// CopyName is the name a copy takes by default: the source's own on
// another account, and one marked as a copy beside it.
func CopyName(source clone.Endpoint, target string) string {
	name := source.Path[len(source.Path)-1]
	if target == source.Account {
		return name + copySuffix
	}
	return name
}

func (f CloneForm) SetSize(width, height int) CloneForm {
	f.frame = f.frame.size(width, height)
	width, _ = f.frame.inner()
	for i := range f.fields {
		f.fields[i].input.Width = max(width-formLabelWidth-1, 1)
	}
	return f
}

// Update types into the focused field, or cycles it. Any edit drops the
// last failure.
func (f CloneForm) Update(msg tea.KeyMsg) (CloneForm, tea.Cmd) {
	f.failure = ""
	field := &f.fields[f.focus]
	if field.choosing() {
		if field.cycle(msg) {
			field.touched = true
		}
		return f, nil
	}
	var cmd tea.Cmd
	field.input, cmd = field.input.Update(msg)
	return f, cmd
}

func (f CloneForm) NextField() CloneForm { return f.setFocus((f.focus + 1) % len(f.fields)) }

func (f CloneForm) PrevField() CloneForm {
	return f.setFocus((f.focus + len(f.fields) - 1) % len(f.fields))
}

func (f CloneForm) setFocus(i int) CloneForm {
	for j := range f.fields {
		f.fields[j].input.Blur()
	}
	f.focus = i
	if !f.fields[i].choosing() {
		f.fields[i].input.Focus()
	}
	return f
}

func (f CloneForm) Target() string {
	field, _ := f.fieldNamed(FieldTarget)
	if len(f.targets) == 0 {
		return ""
	}
	return f.targets[field.choice].Name
}

func (f CloneForm) Choice() CloneChoice {
	path := []string{f.text(FieldName)}
	if f.source.Container() {
		path = []string{f.text(FieldDatabase), f.text(FieldName)}
	}
	content, _ := f.fieldNamed(FieldContent)
	fidelity, _ := f.fieldNamed(FieldFidelity)
	return CloneChoice{
		Target:   clone.Endpoint{Account: f.Target(), Path: path},
		Content:  f.contents[content.choice],
		Fidelity: fidelities[fidelity.choice],
		Capacity: f.capacity(),
	}
}

func (f CloneForm) capacity() clone.Capacity {
	field, _ := f.fieldNamed(FieldCapacity)
	return f.capacities[min(field.choice, len(f.capacities)-1)]
}

func (f CloneForm) Validate() error {
	if len(f.targets) == 0 {
		return errNoWritableAccount
	}
	if !f.surveyed {
		return errStillReading
	}
	for _, field := range f.fields {
		if field.missing != nil && f.text(field.name) == "" {
			return field.missing
		}
	}
	return nil
}

func (f CloneForm) SetStatus(status string) CloneForm {
	f.status = status
	f.failure = ""
	return f
}

func (f CloneForm) Fail(err error) CloneForm {
	f.failure = err.Error()
	f.status = ""
	return f
}

// Waiting reports whether the form is waiting on a connection or a check,
// which a second enter must not start again.
func (f CloneForm) Waiting() bool { return f.status != "" }

func (f CloneForm) text(name FormField) string {
	field, ok := f.fieldNamed(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(field.input.Value())
}

func (f CloneForm) fieldNamed(name FormField) (formField, bool) {
	for _, field := range f.fields {
		if field.name == name {
			return field, true
		}
	}
	return formField{}, false
}

func (f CloneForm) View() string {
	width, _ := f.frame.inner()
	lines := f.sourceLines()
	lines = append(lines, "")
	for i, field := range f.fields {
		lines = append(lines, row(field.label, f.fieldView(field), i == f.focus))
	}
	lines = append(lines, "")
	for _, note := range f.notes() {
		lines = append(lines, styleAll(theme.TextStyle(), wrapText(note, width))...)
	}
	if f.status != "" {
		lines = append(lines, styleAll(theme.HintStyle(), wrapText(f.status, width))...)
	}
	if f.failure != "" {
		lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(f.icons.Failure+" "+f.failure, width))...)
	}
	return f.frame.renderWithHint(lines, theme.HintStyle().Render(cloneFormHint))
}

func (f CloneForm) fieldView(field formField) string {
	switch {
	case field.name == FieldTarget && len(f.targets) == 0:
		return theme.HintStyle().Render("none")
	case field.choosing():
		return choiceView(f.icons, field)
	}
	return inputView(field.input)
}

func (f CloneForm) sourceLines() []string {
	head := row("Source", theme.TextStyle().Render(f.source.Account+" / "+strings.Join(f.source.Path, ".")), false)
	if !f.surveyed {
		return []string{head, row("", theme.HintStyle().Render("reading the source…"), false)}
	}
	lines := []string{head, row("", theme.TextStyle().Render(SizeText(f.survey.Size())), false)}
	if f.source.Container() && len(f.survey.Containers) == 1 {
		keys := strings.Join(f.survey.Containers[0].Definition.PartitionKeys, ", ")
		return append(lines, row("", theme.TextStyle().Render("partition key "+keys), false))
	}
	return append(lines, row("", theme.TextStyle().Render(containerCount(len(f.survey.Containers))), false))
}

// notes say why accounts are missing from the targets, and what the clone
// will cost where.
func (f CloneForm) notes() []string {
	var notes []string
	for _, name := range f.readOnly {
		notes = append(notes, fmt.Sprintf("%s is read-only; set read_only = false on the profile to write to it", name))
	}
	target := f.Target()
	if target == "" {
		return notes
	}
	if target != f.source.Account {
		notes = append(notes, fmt.Sprintf("%s → %s: items leave %s.", f.source.Account, target, f.source.Account))
	}
	return append(notes, fmt.Sprintf("Reading spends RU on %s, writing spends RU on %s. %s",
		f.source.Account, target, f.costNote()))
}

// costNote promises a projection only when there is a size to project to.
func (f CloneForm) costNote() string {
	if !f.survey.Size().Known {
		return "The cost cannot be known up front, and with no size to go on it is counted as it is spent."
	}
	return "The cost cannot be known up front; it is projected once the first page has been copied."
}

func SizeText(size adapter.SizeEstimate) string {
	switch {
	case !size.Known:
		return "size unknown: the account did not say"
	case size.Items == 0:
		return "no items, as the account reports it"
	}
	return fmt.Sprintf("about %s items · %s", FormatCount(size.Items), FormatBytes(size.Bytes))
}

func containerCount(n int) string {
	if n == 1 {
		return "1 container"
	}
	return fmt.Sprintf("%d containers", n)
}
