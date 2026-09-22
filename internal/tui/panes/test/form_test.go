package panes_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// A dialog covers the smallest terminal the layout supports.
const (
	dialogWidth  = 80
	dialogHeight = 24
)

// closeHint is the hint line of a dialog that can only be dismissed.
const closeHint = "esc close"

func databaseForm() panes.Form {
	return panes.NewDatabaseForm(theme.Icons()).SetSize(dialogWidth, dialogHeight)
}

// namedDatabaseForm has the one field a database form requires filled in.
func namedDatabaseForm() panes.Form {
	return typeField(databaseForm(), "hr")
}

func containerForm() panes.Form {
	return panes.NewContainerForm(theme.Icons(), "sales").SetSize(dialogWidth, dialogHeight)
}

// refusedThroughputForm opens on a target whose capacity is not changed there.
func refusedThroughputForm(path []string, current adapter.Throughput) panes.Form {
	return panes.NewThroughputForm(theme.Icons(), path, current).SetSize(dialogWidth, dialogHeight)
}

func throughputForm(current adapter.Throughput) panes.Form {
	return panes.NewThroughputForm(theme.Icons(), []string{"sales", "orders"}, current).
		SetSize(dialogWidth, dialogHeight)
}

func typeField(f panes.Form, text string) panes.Form {
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return f
}

func cycleMode(f panes.Form) panes.Form {
	f, _ = f.Update(spaceKey())
	return f
}

func TestContainerFormNamesItsDatabaseAndFields(t *testing.T) {
	view := plain(containerForm().View())

	assert.Contains(t, view, " New container ")
	assert.Contains(t, view, "Database")
	assert.Contains(t, view, "sales")
	assert.Contains(t, view, "Partition key")
	assert.Contains(t, view, "RU/s")
	assert.Contains(t, view, "enter create")
}

func TestFormFillsItsFrameExactly(t *testing.T) {
	tests := []struct {
		name string
		form panes.Form
	}{
		{name: "database", form: databaseForm()},
		{name: "container", form: containerForm()},
		{name: "throughput", form: throughputForm(adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400})},
		{name: "throughput read back", form: refusedThroughputForm([]string{"sales"}, adapter.Throughput{})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.form.View()

			assert.Equal(t, dialogWidth, lipgloss.Width(view))
			assert.Equal(t, dialogHeight, lipgloss.Height(view))
		})
	}
}

func TestFormReadsWhatWasTypedByField(t *testing.T) {
	f := typeField(containerForm(), "shipments").NextField()

	f = typeField(f, "/customerId")

	assert.Equal(t, "shipments", f.Value(panes.FieldName))
	assert.Equal(t, "/customerId", f.Value(panes.FieldPartitionKey))
}

func TestFormValidateNamesTheFirstFieldStillEmpty(t *testing.T) {
	tests := []struct {
		name    string
		form    panes.Form
		wantErr string
	}{
		{
			name:    "no database name",
			form:    databaseForm(),
			wantErr: "name the database",
		},
		{
			name:    "no container name",
			form:    containerForm(),
			wantErr: "name the container",
		},
		{
			name:    "no partition key",
			form:    typeField(containerForm(), "shipments"),
			wantErr: "enter the partition key, such as /customerId",
		},
		{
			name:    "no RU/s once a mode provisions some",
			form:    cycleMode(namedDatabaseForm().NextField()),
			wantErr: "enter the RU/s",
		},
		{
			name:    "RU/s that is not a number",
			form:    typeField(cycleMode(namedDatabaseForm().NextField()).NextField(), "lots"),
			wantErr: `RU/s must be a whole number, not "lots"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.form.Validate()

			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

func TestFormSubmitsOnceEveryRequiredFieldIsFilled(t *testing.T) {
	f := namedDatabaseForm()

	require.NoError(t, f.Validate())
	assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputNone}, f.Throughput(),
		"a database created without shared throughput provisions none")
}

// throughputChoice is the container form with the keyboard on its choice row.
func throughputChoice() panes.Form {
	return containerForm().NextField().NextField()
}

func TestFormCyclesThroughputModesWithSpace(t *testing.T) {
	f := throughputChoice()
	require.Contains(t, plain(f.View()), "shared")

	f = cycleMode(f)
	assert.Contains(t, plain(f.View()), "manual")

	f = cycleMode(f)
	assert.Contains(t, plain(f.View()), "autoscale")

	f = cycleMode(f)
	assert.Contains(t, plain(f.View()), "shared", "the choices wrap round")
}

func TestAChoiceAnswersToEveryKeyThatMovesThroughIt(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
		want string
	}{
		{name: "space", key: spaceKey(), want: "manual"},
		{name: "right", key: tea.KeyMsg{Type: tea.KeyRight}, want: "manual"},
		{name: "l", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}, want: "manual"},
		{name: "left", key: tea.KeyMsg{Type: tea.KeyLeft}, want: "autoscale"},
		{name: "h", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, want: "autoscale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := throughputChoice().Update(tt.key)

			assert.Contains(t, plain(f.View()), tt.want)
		})
	}
}

func TestAChoiceShowsThatItCanBeChanged(t *testing.T) {
	view := plain(throughputChoice().View())

	icons := theme.Icons()
	assert.Contains(t, view, icons.Left+" shared "+icons.Right)
	assert.Contains(t, view, "←/→ change", "and the hint line says which keys do it")
}

func TestFormChoiceFieldTakesNoText(t *testing.T) {
	f := typeField(throughputChoice(), "manual")

	assert.Contains(t, plain(f.View()), "shared", "typing cannot set a choice")
}

func TestALetterThatMovesAChoiceIsStillATextFieldsLetter(t *testing.T) {
	f := typeField(containerForm(), "hl")

	assert.Equal(t, "hl", f.Value(panes.FieldName))
}

func TestFormReadsTheThroughputItsFieldsDescribe(t *testing.T) {
	f := typeField(cycleMode(throughputChoice()).NextField(), "1000")

	assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 1000}, f.Throughput())
}

func TestThroughputFormOpensOnWhatIsProvisionedNow(t *testing.T) {
	current := adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 4000}

	f := throughputForm(current)

	view := plain(f.View())
	assert.Contains(t, view, " Throughput ")
	assert.Contains(t, view, "sales.orders")
	assert.Contains(t, view, "autoscale")
	assert.Contains(t, view, "4000")
	assert.Equal(t, current, f.Throughput())
	assert.Contains(t, view, "enter save")
}

func TestThroughputFormCyclesBetweenTheModesThatProvision(t *testing.T) {
	f := throughputForm(adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400})

	f = cycleMode(f)
	assert.Contains(t, plain(f.View()), "autoscale")

	f = cycleMode(f)
	assert.Contains(t, plain(f.View()), "manual", "the two modes alternate; neither gives capacity away")
}

func TestThroughputFormReadsBackATargetThatIsNotChangedThere(t *testing.T) {
	tests := []struct {
		name    string
		path    []string
		current adapter.Throughput
		want    string
	}{
		{
			name:    "a container drawing on its database",
			path:    []string{"sales", "orders"},
			current: adapter.Throughput{Mode: adapter.ThroughputShared},
			want:    "draws on the throughput provisioned on sales; change it there",
		},
		{
			name:    "a database provisioning nothing",
			path:    []string{"sales"},
			current: adapter.Throughput{},
			want:    "no throughput is provisioned here: the account is serverless, or each container carries its own",
		},
		{
			name:    "a container provisioning nothing",
			path:    []string{"sales", "orders"},
			current: adapter.Throughput{},
			want:    "no throughput is provisioned here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := refusedThroughputForm(tt.path, tt.current)

			require.EqualError(t, f.Validate(), tt.want)

			view := plain(f.View())
			assert.Contains(t, view, tt.current.Mode.String(), "the dialog reads back what is actually there")
			assert.Contains(t, view, closeHint)
			assert.NotContains(t, view, "RU/s", "there is no rate to type")
			assert.NotContains(t, view, theme.Icons().Failure, "nothing here went wrong")
			assert.NotContains(t, view, theme.Icons().Left, "and nothing here is a choice")
			for _, word := range strings.Fields(tt.want) {
				assert.Contains(t, view, word, "wrapping must not drop words")
			}
		})
	}
}

func TestARefusedThroughputTargetTakesNoInput(t *testing.T) {
	f := refusedThroughputForm([]string{"sales", "orders"}, adapter.Throughput{Mode: adapter.ThroughputShared})

	f = typeField(cycleMode(f.NextField().PrevField()), "4000")

	view := plain(f.View())
	assert.Contains(t, view, "shared", "there is nothing to change")
	assert.NotContains(t, view, "4000")
	assert.Contains(t, view, "draws on the throughput", "and the note stays put")
}

func TestFormShowsWhyTheOperationWasRefused(t *testing.T) {
	f := typeField(containerForm(), "orders/2026").Fail(errors.New("cosmos: create container: 400 Bad Request"))

	view := plain(f.View())
	assert.Contains(t, view, "400 Bad Request")
	assert.Contains(t, view, theme.Icons().Failure, "a refused operation is an error, and reads as one")
	assert.Contains(t, view, "orders/2026", "the fields survive so the name can be corrected")
}

func TestFormDropsARefusalWhenTheModeIsCycled(t *testing.T) {
	f := throughputForm(adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 4000}).
		Fail(errors.New("autoscale is not allowed on this account"))
	require.Contains(t, plain(f.View()), "autoscale is not allowed")

	f = cycleMode(f)

	assert.NotContains(t, plain(f.View()), "autoscale is not allowed",
		"the refusal was about the mode that has just been changed")
}

func TestFormFocusWrapsPastTheLastField(t *testing.T) {
	f := databaseForm().PrevField().NextField()

	assert.Equal(t, "hr", typeField(f, "hr").Value(panes.FieldName),
		"stepping back from the first field and forward again returns to it")
}
