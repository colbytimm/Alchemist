package panes_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const accountsWidth = 80

var accountsHints = []key.Binding{
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "disconnect")),
}

func fixtureRows() []panes.AccountRow {
	return []panes.AccountRow{
		{Name: "emulator", Endpoint: "http://localhost:8081", State: panes.AccountConnected},
		{Name: "eu-prod", Endpoint: "https://eu.documents.azure.com:443/", State: panes.AccountFailed,
			Err: errors.New("account unreachable: dial tcp: lookup eu.documents.azure.com: no such host")},
		{Name: "prod", Endpoint: "https://orders.documents.azure.com:443/", Database: "sales", State: panes.AccountConnected},
		{Name: "staging", Endpoint: "https://staging.documents.azure.com:443/", State: panes.AccountConnecting},
		{Name: "zeta", Endpoint: "https://zeta.documents.azure.com:443/"},
	}
}

func newAccounts(t *testing.T, height int) panes.Accounts {
	t.Helper()
	pane, _ := panes.NewAccounts(theme.Icons(), accountsHints).
		SetSize(accountsWidth, height).
		SetRows(fixtureRows(), "prod")
	return pane.Open()
}

func typeAccountsFilter(pane panes.Accounts, text string) panes.Accounts {
	pane = pane.StartFilter()
	for _, r := range text {
		pane, _ = pane.Update(typed(string(r)))
	}
	return pane
}

func line(t *testing.T, view, name string) string {
	t.Helper()
	for _, l := range strings.Split(plain(view), "\n") {
		if strings.Contains(l, " "+name+" ") {
			return l
		}
	}
	require.Fail(t, "no row for "+name)
	return ""
}

func TestAccountsDrawsEveryRowWithItsHostDatabaseAndState(t *testing.T) {
	view := newAccounts(t, paneHeight+8).View()

	tests := []struct {
		name string
		want []string
	}{
		{name: "emulator", want: []string{"localhost:8081", "connected"}},
		{name: "eu-prod", want: []string{theme.Icons().Failure, "eu.documents.azure.com", "failed"}},
		{name: "prod", want: []string{theme.Icons().Success, "orders.documents.azure.com", "sales", "current"}},
		{name: "staging", want: []string{"connecting…"}},
		{name: "zeta", want: []string{"not connected"}},
	}
	for _, tt := range tests {
		row := line(t, view, tt.name)
		for _, want := range tt.want {
			assert.Contains(t, row, want, "row %s", tt.name)
		}
	}
	assert.NotContains(t, plain(view), ":443", "the port HTTPS implies is left off")
}

func TestAccountsListsRowsInTheOrderGiven(t *testing.T) {
	view := plain(newAccounts(t, paneHeight+8).View())

	previous := -1
	for _, row := range fixtureRows() {
		at := strings.Index(view, " "+row.Name+" ")
		assert.Greater(t, at, previous, "%s is out of order", row.Name)
		previous = at
	}
}

func TestAccountsOpensWithTheCursorOnTheCurrentAccount(t *testing.T) {
	selected, ok := newAccounts(t, paneHeight).Selected()

	require.True(t, ok)
	assert.Equal(t, "prod", selected.Name)
}

func TestAccountsWrapsAFailureUnderItsRow(t *testing.T) {
	view := plain(newAccounts(t, paneHeight+8).View())

	assert.Contains(t, view, "no such host", "the reason is wrapped, not cut off")
	lines := strings.Split(view, "\n")
	for i, l := range lines {
		if strings.Contains(l, " eu-prod ") {
			assert.Contains(t, lines[i+1], "account unreachable")
			return
		}
	}
	require.Fail(t, "no eu-prod row")
}

func TestAccountsFilterNarrowsByNameAndByEndpoint(t *testing.T) {
	byName := plain(typeAccountsFilter(newAccounts(t, paneHeight), "stag").View())
	assert.Contains(t, byName, "staging")
	assert.NotContains(t, byName, "emulator")

	byEndpoint := plain(typeAccountsFilter(newAccounts(t, paneHeight), "localhost").View())
	assert.Contains(t, byEndpoint, "emulator")
	assert.NotContains(t, byEndpoint, "staging")
}

func TestAccountsFilterTakesEveryLetterAsText(t *testing.T) {
	pane := typeAccountsFilter(newAccounts(t, paneHeight), "ax")

	assert.True(t, pane.Filtering())
	assert.Contains(t, plain(pane.View()), "/ ax")
	assert.Contains(t, plain(pane.View()), "nothing matches")
}

func TestAccountsClearFilterRestoresTheList(t *testing.T) {
	pane := typeAccountsFilter(newAccounts(t, paneHeight), "stag").ClearFilter()

	assert.False(t, pane.Filtering())
	assert.Contains(t, plain(pane.View()), "emulator")
}

func TestAccountsSetRowsKeepsTheCursorOnTheSameName(t *testing.T) {
	pane := newAccounts(t, paneHeight).CursorDown()
	selected, _ := pane.Selected()
	require.Equal(t, "staging", selected.Name)

	rows := fixtureRows()
	rows[3].State = panes.AccountConnected
	pane, _ = pane.SetRows(append([]panes.AccountRow{{Name: "alpha"}}, rows...), "prod")

	selected, _ = pane.Selected()
	assert.Equal(t, "staging", selected.Name, "a state change never moves the cursor")
}

func TestAccountsStartsItsSpinnerOnceARowIsConnecting(t *testing.T) {
	idle := fixtureRows()[:3]
	pane, tick := panes.NewAccounts(theme.Icons(), accountsHints).SetRows(idle, "prod")
	assert.Nil(t, tick)

	_, tick = pane.SetRows(fixtureRows(), "prod")
	assert.NotNil(t, tick)
}

func TestAccountsKeepsTheCursorRowAndHintOnAShortPane(t *testing.T) {
	var rows []panes.AccountRow
	for _, name := range strings.Fields("a b c d e f g h i j k l m n o p q r s t") {
		rows = append(rows, panes.AccountRow{Name: "account-" + name, Endpoint: "https://" + name + ".example"})
	}
	pane, _ := panes.NewAccounts(theme.Icons(), accountsHints).SetSize(accountsWidth, paneHeight).SetRows(rows, "account-s")
	pane = pane.Open()

	view := plain(pane.View())
	assert.Contains(t, view, "account-s")
	assert.Contains(t, view, "x disconnect", "the hint line stays at the bottom")
	assert.Equal(t, paneHeight, lipgloss.Height(pane.View()))
}

func TestAccountsSaysWhenThereAreNone(t *testing.T) {
	pane, _ := panes.NewAccounts(theme.Icons(), accountsHints).SetSize(accountsWidth, paneHeight).SetRows(nil, "")

	_, ok := pane.Selected()
	assert.False(t, ok)
	assert.Contains(t, plain(pane.View()), "no accounts yet")
}
