package panes

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	databaseFormTitle   = "New database"
	containerFormTitle  = "New container"
	throughputFormTitle = "Throughput"
	formLabelWidth      = 16
	createHint          = "enter create · tab next field · ←/→ change · esc cancel"
	saveHint            = "enter save · tab next field · ←/→ change · esc cancel"
	closeHint           = "esc close"
)

// What a form refuses to submit without, worded the way its fields are
// labeled, since the message sits right under them.
var (
	errNoDatabaseName  = errors.New("name the database")
	errNoContainerName = errors.New("name the container")
	errNoPartitionKey  = errors.New("enter the partition key, such as /customerId")
	errNoRUs           = errors.New("enter the RU/s")
)

// FormField names one editable row. Values are read by constant, so an
// operation needing another input adds a field rather than a pane.
type FormField int

const (
	FieldName FormField = iota
	FieldPartitionKey
	FieldMode
	FieldRUs
	FieldTarget
	FieldDatabase
	FieldContent
	FieldFidelity
	FieldCapacity
)

// The modes each form offers, in cycling order. The first is the default: a
// database or container created without capacity of its own.
var (
	databaseModes  = []adapter.ThroughputMode{adapter.ThroughputNone, adapter.ThroughputManual, adapter.ThroughputAutoscale}
	containerModes = []adapter.ThroughputMode{adapter.ThroughputShared, adapter.ThroughputManual, adapter.ThroughputAutoscale}
	offerModes     = []adapter.ThroughputMode{adapter.ThroughputManual, adapter.ThroughputAutoscale}
)

// formField is one editable row: a text input, or a choice cycling through
// options when options is set. A throughput mode choice keeps the modes its
// options name.
type formField struct {
	name    FormField
	label   string
	input   textinput.Model
	missing error
	options []string
	modes   []adapter.ThroughputMode
	choice  int
	// defaulted is the text a default last filled in, and touched marks a
	// choice the user moved: what is theirs, a new default leaves alone.
	defaulted string
	touched   bool
}

func (f formField) choosing() bool { return len(f.options) > 0 }

// cycle moves a choice field by the step msg asks for, and reports whether
// it moved.
func (f *formField) cycle(msg tea.KeyMsg) bool {
	step := choiceStep(msg)
	f.choice = (f.choice + step + len(f.options)) % len(f.options)
	return step != 0
}

// formNote is a row stating something about what the form acts on, which
// nothing may edit.
type formNote struct {
	label string
	value string
}

// Form collects the fields one management operation needs. Like the inputs it
// wraps, its value receiver hides shared state, so a caller must keep every
// Form it is handed.
type Form struct {
	frame  frame
	icons  theme.IconSet
	hint   string
	notes  []formNote
	fields []formField
	focus  int
	// refusal is why this form can never be submitted, whatever is typed
	// into it. It is a note about the target, not something that went wrong,
	// and no edit clears it.
	refusal    error
	failure    string
	submitting bool
}

// NewDatabaseForm collects a database to create, with the shared throughput
// its containers would draw on.
func NewDatabaseForm(icons theme.IconSet) Form {
	return newForm(icons, databaseFormTitle, createHint, nil, []formField{
		textField(FieldName, "Name", "sales", errNoDatabaseName),
		modeField(databaseModes, adapter.ThroughputNone),
		rusField(0),
	})
}

// NewContainerForm collects a container to create in database.
func NewContainerForm(icons theme.IconSet, database string) Form {
	return newForm(icons, containerFormTitle, createHint, []formNote{{label: "Database", value: database}}, []formField{
		textField(FieldName, "Name", "shipments", errNoContainerName),
		textField(FieldPartitionKey, "Partition key", "/customerId", errNoPartitionKey),
		modeField(containerModes, adapter.ThroughputShared),
		rusField(0),
	})
}

// NewThroughputForm edits the capacity provisioned for path, seeded with what
// it holds now. Capacity that cannot be replaced where it was read has
// nothing to edit: that dialog reads what is there back and says where it is
// changed instead.
func NewThroughputForm(icons theme.IconSet, path []string, current adapter.Throughput) Form {
	target := formNote{label: "Target", value: strings.Join(path, ".")}
	if current.Provisioned() {
		return newForm(icons, throughputFormTitle, saveHint, []formNote{target}, []formField{
			modeField(offerModes, current.Mode),
			rusField(current.RUs),
		})
	}
	notes := []formNote{target, {label: "Throughput", value: current.Mode.String()}}
	form := newForm(icons, throughputFormTitle, closeHint, notes, nil)
	form.refusal = refusalFor(path, current)
	return form
}

// refusalFor says where the capacity at path is changed, since it is not
// here. Cosmos moves no container between shared and dedicated throughput,
// adds no shared offer to a database created without one, and provisions
// nothing at all on a serverless account, so the dialog explains instead of
// sending a request the service would only refuse.
func refusalFor(path []string, current adapter.Throughput) error {
	switch {
	case current.Mode == adapter.ThroughputShared:
		return fmt.Errorf("draws on the throughput provisioned on %s; change it there", path[0])
	case len(path) > 1:
		return errors.New("no throughput is provisioned here")
	}
	return errors.New("no throughput is provisioned here: the account is serverless, or each container carries its own")
}

func newForm(icons theme.IconSet, title, hint string, notes []formNote, fields []formField) Form {
	form := Form{
		frame:  frame{title: title, focused: true},
		icons:  icons,
		hint:   hint,
		notes:  notes,
		fields: fields,
	}
	return form.setFocus(0)
}

func textField(name FormField, label, placeholder string, missing error) formField {
	return formField{name: name, label: label, input: newInput(placeholder, ""), missing: missing}
}

// modeField cycles modes, opening on current when it is one of them.
func modeField(modes []adapter.ThroughputMode, current adapter.ThroughputMode) formField {
	field := formField{name: FieldMode, label: "Throughput", modes: modes}
	for i, mode := range modes {
		field.options = append(field.options, mode.String())
		if mode == current {
			field.choice = i
		}
	}
	return field
}

// rusField opens on rus, or empty when nothing is provisioned yet.
func rusField(rus int32) formField {
	value := ""
	if rus > 0 {
		value = strconv.Itoa(int(rus))
	}
	return formField{name: FieldRUs, label: "RU/s", input: newInput("400", value)}
}

func (f Form) SetSize(width, height int) Form {
	f.frame = f.frame.size(width, height)
	width, _ = f.frame.inner()
	for i := range f.fields {
		f.fields[i].input.Width = max(width-formLabelWidth-1, 1)
	}
	return f
}

// Update types into the focused field. A choice field takes no text: it
// answers to the keys that move through its values instead, which is why h
// and l reach it as keys rather than as the letters a name may contain. Any
// edit drops the last failure, which was about the fields as they stood
// before it; a refusal outlives them all.
func (f Form) Update(msg tea.KeyMsg) (Form, tea.Cmd) {
	f.failure = ""
	if len(f.fields) == 0 {
		return f, nil
	}
	field := &f.fields[f.focus]
	if field.choosing() {
		field.cycle(msg)
		return f, nil
	}
	var cmd tea.Cmd
	field.input, cmd = field.input.Update(msg)
	return f, cmd
}

// choiceStep is how far msg moves through a choice field's values.
func choiceStep(msg tea.KeyMsg) int {
	switch msg.String() {
	case " ", "right", "l":
		return 1
	case "left", "h":
		return -1
	}
	return 0
}

func (f Form) NextField() Form {
	return f.step(1)
}

func (f Form) PrevField() Form {
	return f.step(-1)
}

// step moves the focus by delta, wrapping at either end. A dialog that only
// reports has nothing to step through.
func (f Form) step(delta int) Form {
	if len(f.fields) == 0 {
		return f
	}
	return f.setFocus((f.focus + delta + len(f.fields)) % len(f.fields))
}

// setFocus moves the keyboard to the field at i. The inputs' own focus
// command only drives a blinking cursor, and these are static.
func (f Form) setFocus(i int) Form {
	if len(f.fields) == 0 {
		return f
	}
	for j := range f.fields {
		f.fields[j].input.Blur()
	}
	f.focus = i
	if !f.fields[i].choosing() {
		f.fields[i].input.Focus()
	}
	return f
}

// Value is what was typed into the named field, trimmed.
func (f Form) Value(name FormField) string {
	field, ok := f.field(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(field.input.Value())
}

// Throughput is the capacity the mode and RU/s fields describe. The RU/s text
// is known to parse: Validate refuses the form otherwise.
func (f Form) Throughput() adapter.Throughput {
	rus, _ := strconv.ParseInt(f.Value(FieldRUs), 10, 32)
	return adapter.Throughput{Mode: f.mode(), RUs: int32(rus)}
}

// Validate reports what stands between the form and the operation it
// describes: a refusal that no edit can clear, then the first field still to
// fill in, and nothing once it is ready to submit.
func (f Form) Validate() error {
	if f.refusal != nil {
		return f.refusal
	}
	for _, field := range f.fields {
		if field.missing != nil && f.Value(field.name) == "" {
			return field.missing
		}
	}
	if !f.Throughput().Provisioned() {
		return nil
	}
	rus := f.Value(FieldRUs)
	if rus == "" {
		return errNoRUs
	}
	if _, err := strconv.ParseInt(rus, 10, 32); err != nil {
		return fmt.Errorf("RU/s must be a whole number, not %q", rus)
	}
	return nil
}

// Submitting reports whether the operation this form asked for is in flight.
func (f Form) Submitting() bool {
	return f.submitting
}

// StartSubmitting marks the operation as sent, so a second enter cannot send
// it again while the first is still out.
func (f Form) StartSubmitting() Form {
	f.submitting = true
	f.failure = ""
	return f
}

// Fail shows why the operation was refused and keeps the fields, so a
// rejected name is one edit away from a retry.
func (f Form) Fail(err error) Form {
	f.failure = err.Error()
	f.submitting = false
	return f
}

func (f Form) View() string {
	width, _ := f.frame.inner()
	var lines []string
	for _, note := range f.notes {
		lines = append(lines, row(note.label, theme.TextStyle().Render(note.value), false))
	}
	for i, field := range f.fields {
		lines = append(lines, row(field.label, f.fieldView(field), i == f.focus))
	}
	lines = append(lines, "")
	lines = append(lines, f.messageLines(width)...)
	return f.frame.renderWithHint(lines, theme.HintStyle().Render(f.hint))
}

// messageLines is what sits under the rows: a refusal, which is a note about
// the target rather than something that went wrong, or the service's answer
// to what was submitted, which is.
func (f Form) messageLines(width int) []string {
	if f.refusal != nil {
		return styleAll(theme.TextStyle(), wrapText(f.refusal.Error(), width))
	}
	return styleAll(theme.ErrorStyle(), failureLines(f.failureText(), width))
}

func (f Form) failureText() string {
	if f.failure == "" {
		return ""
	}
	return f.icons.Failure + " " + f.failure
}

// fieldView draws a choice between the arrows that move it, so a value that
// can be changed does not read as one that cannot.
func (f Form) fieldView(field formField) string {
	if !field.choosing() {
		return inputView(field.input)
	}
	return choiceView(f.icons, field)
}

func choiceView(icons theme.IconSet, field formField) string {
	arrows := theme.HintStyle()
	return arrows.Render(icons.Left+" ") +
		theme.TextStyle().Render(field.options[field.choice]) +
		arrows.Render(" "+icons.Right)
}

func (f Form) field(name FormField) (formField, bool) {
	for _, field := range f.fields {
		if field.name == name {
			return field, true
		}
	}
	return formField{}, false
}

func (f Form) mode() adapter.ThroughputMode {
	field, ok := f.field(FieldMode)
	if !ok {
		return adapter.ThroughputNone
	}
	return field.modes[field.choice]
}

func row(label, value string, focused bool) string {
	style := theme.TextStyle()
	if focused {
		style = theme.SelectedStyle()
	}
	return style.Width(formLabelWidth).Render(label) + value
}
