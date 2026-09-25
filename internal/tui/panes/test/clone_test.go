package panes_test

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

var ordersSource = clone.Endpoint{Account: "prod", Path: []string{"sales", "orders"}}

func cloneForm(targets ...panes.CloneTarget) panes.CloneForm {
	return panes.NewCloneForm(theme.Icons(), ordersSource, targets, nil, true).SetSize(dialogWidth, dialogHeight)
}

func surveyed(known bool) clone.Survey {
	return clone.Survey{
		Source:          ordersSource,
		ThroughputKnown: known,
		Containers: []clone.SourceContainer{{
			Path:       ordersSource.Path,
			Definition: adapter.ContainerDefinition{PartitionKeys: []string{"/customerId"}, Size: adapter.SizeEstimate{Items: 30112, Bytes: 43_830_067, Known: true}},
		}},
	}
}

func pressed(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestTheCloneFormDescribesItsSourceOnceRead(t *testing.T) {
	form := cloneForm(panes.CloneTarget{Name: "prod", State: panes.AccountConnected})
	assert.Contains(t, ansi.Strip(form.View()), "reading the source…")

	view := ansi.Strip(form.SetSurvey(surveyed(true)).View())

	assert.Contains(t, view, "prod / sales.orders")
	assert.Contains(t, view, "about 30,112 items · 41.8 MB")
	assert.Contains(t, view, "partition key /customerId")
}

func TestDefaultsFollowTheTargetUntilAFieldIsChanged(t *testing.T) {
	form := cloneForm(
		panes.CloneTarget{Name: "emulator", State: panes.AccountDisconnected},
		panes.CloneTarget{Name: "prod", State: panes.AccountConnected},
	).SetSurvey(surveyed(true))
	form = form.ApplyDefaults(panes.CloneDefaults{Name: "orders-copy", Fidelity: adapter.DefinitionFull})
	require.Equal(t, "orders-copy", form.Choice().Target.Path[1])

	form = form.ApplyDefaults(panes.CloneDefaults{Name: "orders", Fidelity: adapter.DefinitionPortable})
	assert.Equal(t, "orders", form.Choice().Target.Path[1], "an untouched name follows")
	assert.Equal(t, adapter.DefinitionPortable, form.Choice().Fidelity)

	form = form.NextField().NextField()
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-old")})
	form = form.NextField().NextField()
	form, _ = form.Update(pressed(tea.KeyRight))
	form = form.ApplyDefaults(panes.CloneDefaults{Name: "orders-copy", Fidelity: adapter.DefinitionPortable})

	assert.Equal(t, "orders-old", form.Choice().Target.Path[1], "an edited name is never rewritten")
	assert.Equal(t, adapter.DefinitionFull, form.Choice().Fidelity, "nor is a choice the user made")
}

func TestSameAsSourceIsOfferedOnlyWhenTheSourcesThroughputIsKnown(t *testing.T) {
	tests := []struct {
		name  string
		known bool
		want  []string
	}{
		{name: "known", known: true, want: []string{"minimum", "same as source", "none", "minimum"}},
		{name: "not known", known: false, want: []string{"minimum", "none", "minimum", "none"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := cloneForm(panes.CloneTarget{Name: "prod"}).SetSurvey(surveyed(tt.known)).PrevField()
			var seen []string
			for range tt.want {
				seen = append(seen, form.Choice().Capacity.String())
				form, _ = form.Update(pressed(tea.KeyRight))
			}
			for i, want := range tt.want {
				assert.Contains(t, seen[i], want)
			}
		})
	}
}

func TestACloneFormWithNoTargetCannotBeSubmitted(t *testing.T) {
	form := panes.NewCloneForm(theme.Icons(), ordersSource, nil, []string{"prod"}, true).
		SetSize(dialogWidth, dialogHeight).SetSurvey(surveyed(true))

	view := ansi.Strip(form.View())

	assert.Contains(t, view, "Target account  none")
	assert.Contains(t, view, "prod is read-only; set read_only = false on the profile to write to it")
	assert.Error(t, form.Validate())
	assert.Empty(t, form.Target())
}

func TestASourceWithNoScannerOffersTheDefinitionOnly(t *testing.T) {
	form := panes.NewCloneForm(theme.Icons(), ordersSource, []panes.CloneTarget{{Name: "prod"}}, nil, false)

	assert.Equal(t, clone.DefinitionOnly, form.Choice().Content)
}

func progressView(status panes.CloneStatus) string {
	keys := panes.CloneKeys{}
	return ansi.Strip(panes.NewCloneProgress(theme.Icons(), keys).SetSize(dialogWidth, dialogHeight).SetStatus(status).View())
}

func runningStatus(estimate adapter.SizeEstimate) panes.CloneStatus {
	return panes.CloneStatus{
		Source: ordersSource, Target: clone.Endpoint{Account: "emulator", Path: []string{"sales", "orders"}},
		Phase: "Copying items", Items: true, Written: 12400, Estimate: estimate, Rate: 212,
		ReadCharge: 1912.4, WriteCharge: 78204.11, Projected: 194000, Writers: 3, MaxWriters: 4, Throttles: 2,
	}
}

func TestTheProgressViewCountsAgainstTheEstimate(t *testing.T) {
	view := progressView(runningStatus(adapter.SizeEstimate{Items: 30112, Known: true}))

	assert.Contains(t, view, "Clone · prod/sales.orders → emulator/sales.orders")
	assert.Contains(t, view, "41%")
	assert.Contains(t, view, "12,400 of about 30,112")
	assert.Contains(t, view, "212 items/s")
	assert.Contains(t, view, "about 1m 24s")
	assert.Contains(t, view, "1,912.40 on prod")
	assert.Contains(t, view, "78,204.11 on emulator")
	assert.Contains(t, view, "about 194,000 RU in total")
	assert.Contains(t, view, "3 of 4")
	assert.Contains(t, view, "throttled twice")
	assert.Contains(t, view, "This is a copy, not a snapshot.")
}

func TestWithNoEstimateTheViewShowsNoPercentageAndNoTimeLeft(t *testing.T) {
	view := progressView(runningStatus(adapter.SizeEstimate{}))

	assert.NotContains(t, view, "%")
	assert.NotContains(t, view, "about 1m")
	assert.Contains(t, view, "Items      12,400 ")
}

func TestAnEndedCloneSaysWhatItLeftBehind(t *testing.T) {
	status := runningStatus(adapter.SizeEstimate{Items: 30112, Known: true})
	status.End, status.Err = panes.CloneFailed, errors.New("429 Too Many Requests")
	status.LeftBehind = "emulator/sales.orders holds 12,400 of about 30,112 items and is incomplete."

	view := progressView(status)

	assert.Contains(t, view, "429 Too Many Requests")
	assert.Contains(t, view, "and is incomplete.")
}

func TestNumbersAreWrittenForPeople(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "a small count", got: panes.FormatCount(12), want: "12"},
		{name: "a grouped count", got: panes.FormatCount(1234567), want: "1,234,567"},
		{name: "a negative count", got: panes.FormatCount(-30112), want: "-30,112"},
		{name: "a charge", got: panes.FormatCharge(194310.524), want: "194,310.52"},
		{name: "bytes", got: panes.FormatBytes(512), want: "512 B"},
		{name: "megabytes", got: panes.FormatBytes(43_830_067), want: "41.8 MB"},
		{name: "gigabytes", got: panes.FormatBytes(3 << 30), want: "3.0 GB"},
		{name: "an unknown size", got: panes.SizeText(adapter.SizeEstimate{}), want: "size unknown: the account did not say"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestTheStatusBarCarriesTheJob(t *testing.T) {
	bar := panes.NewStatusBar(theme.Icons(), "emulator").SetWidth(120).SetJob("clone prod/sales.orders → emulator 41% (y)")

	assert.Contains(t, ansi.Strip(bar.View()), "clone prod/sales.orders → emulator 41% (y)")
}
