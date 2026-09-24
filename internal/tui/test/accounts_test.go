package tui_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// The overlay title as it appears inside the top border.
const accountsTitle = " Accounts "

// fixtureAccounts are the profiles every test in this file starts with, in
// the order the switcher lists them.
func fixtureAccounts() []tui.Account {
	return []tui.Account{
		{Name: "emulator", Endpoint: "http://localhost:8081"},
		{Name: "prod", Endpoint: "https://orders.documents.azure.com:443/", Database: firstDatabase},
		{Name: "staging", Endpoint: "https://staging.documents.azure.com:443/"},
	}
}

func accountNames() []string {
	var names []string
	for _, account := range fixtureAccounts() {
		names = append(names, account.Name)
	}
	return names
}

// opener serves one recording connection per account, and remembers every
// connection it handed out.
type opener struct {
	t      *testing.T
	opened map[string][]*recordingConnection
	// failOpen is what Open answers for an account instead of connecting it.
	failOpen map[string]error
	// failPing is how many pings each account's next connection fails.
	failPing map[string]int
}

func newOpener(t *testing.T) *opener {
	return &opener{
		t:        t,
		opened:   map[string][]*recordingConnection{},
		failOpen: map[string]error{},
		failPing: map[string]int{},
	}
}

func (o *opener) open(_ context.Context, name string) (adapter.Connection, error) {
	if err := o.failOpen[name]; err != nil {
		delete(o.failOpen, name)
		return nil, err
	}
	conn := newConnection(o.t)
	conn.failPing = o.failPing[name]
	delete(o.failPing, name)
	o.opened[name] = append(o.opened[name], conn)
	return conn, nil
}

// last is the connection most recently opened for name.
func (o *opener) last(name string) *recordingConnection {
	o.t.Helper()
	conns := o.opened[name]
	require.NotEmpty(o.t, conns, "%s was never opened", name)
	return conns[len(conns)-1]
}

func newAccountsModel(t *testing.T, o *opener, opts tui.Options) tea.Model {
	t.Helper()
	opts.Icons = theme.Icons()
	opts.Accounts = fixtureAccounts()
	if opts.Launch == "" {
		opts.Launch = "prod"
	}
	opts.Open = o.open
	opts.Manage = managed
	m, _ := tui.New(opts).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	return m
}

// highlight puts the switcher's cursor on name, from wherever it is.
func highlight(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	index := slices.Index(accountNames(), name)
	require.GreaterOrEqual(t, index, 0, "%s is not a fixture account", name)
	for range accountNames() {
		m = pressAll(t, m, keyMsg(tea.KeyUp))
	}
	for range index {
		m = pressAll(t, m, keyMsg(tea.KeyDown))
	}
	return m
}

func switchTo(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	m = highlight(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), name)
	return pressAll(t, m, keyMsg(tea.KeyEnter))
}

// statusBar is the bottom line of the layout, whose first field names the
// account the session is on.
func statusBar(m tea.Model) string {
	lines := strings.Split(plain(m.View()), "\n")
	return lines[len(lines)-1]
}

func onAccount(t *testing.T, m tea.Model, name string) {
	t.Helper()
	require.NotContains(t, plain(m.View()), accountsTitle, "the switcher should be closed")
	assert.True(t, strings.HasPrefix(statusBar(m), name+" "), "status bar %q should name %s", statusBar(m), name)
}

// switcherRow is the switcher's line for name.
func switcherRow(t *testing.T, m tea.Model, name string) string {
	t.Helper()
	for _, line := range strings.Split(plain(m.View()), "\n") {
		if strings.Contains(line, " "+name+" ") {
			return line
		}
	}
	require.Fail(t, "no row for "+name, plain(m.View()))
	return ""
}

func TestTheSessionStartsOnTheLaunchAccountAlone(t *testing.T) {
	o := newOpener(t)

	m := newAccountsModel(t, o, tui.Options{})

	onAccount(t, m, "prod")
	assert.Equal(t, []string{"prod"}, slices.Sorted(func(yield func(string) bool) {
		for name := range o.opened {
			yield(name)
		}
	}))
	require.Len(t, o.opened["prod"], 1)
	assert.Zero(t, o.last("prod").pings, "the launch attempt does not ping")
	view := plain(m.View())
	assert.Contains(t, view, firstContainer, "the default database is expanded")
	assert.NotContains(t, view, "staging", "the tree has no account rows")
}

func TestCtrlGOpensTheSwitcherFromEveryPane(t *testing.T) {
	m := newAccountsModel(t, newOpener(t), tui.Options{})

	for pane, prefix := range map[string][]tea.KeyMsg{
		"catalog": nil,
		"editor":  {keyRune('e')},
		"results": {keyRune('e'), keyMsg(tea.KeyTab)},
	} {
		t.Run(pane, func(t *testing.T) {
			opened := pressAll(t, pressAll(t, m, prefix...), keyMsg(tea.KeyCtrlG))

			view := plain(opened.View())
			require.Contains(t, view, accountsTitle)
			for _, name := range accountNames() {
				assert.Contains(t, view, name)
			}
			closed := pressAll(t, opened, keyMsg(tea.KeyCtrlG))
			assert.NotContains(t, plain(closed.View()), accountsTitle, "ctrl+g closes it again")
		})
	}
}

func TestCtrlGInTheEditorTypesNothing(t *testing.T) {
	m := typeQuery(t, newAccountsModel(t, newOpener(t), tui.Options{}), "SELECT 1")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyMsg(tea.KeyEscape))

	assert.Contains(t, plain(m.View()), "SELECT 1")
	assert.NotContains(t, plain(m.View()), "SELECT 1g")
}

func TestTheSwitcherMarksTheCurrentAccountAndTheOthersState(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{}), keyMsg(tea.KeyCtrlG))

	assert.Contains(t, switcherRow(t, m, "prod"), "current")
	assert.Contains(t, switcherRow(t, m, "prod"), "orders.documents.azure.com")
	assert.Contains(t, switcherRow(t, m, "staging"), "not connected")
}

func TestSwitchingToANewAccountConnectsPingsAndMovesThere(t *testing.T) {
	o := newOpener(t)
	m := highlight(t, pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG)), "staging")

	connecting, cmd := m.Update(keyMsg(tea.KeyEnter))
	assert.Contains(t, switcherRow(t, connecting, "staging"), "connecting…", "the switcher waits for the attempt")
	m, _ = settle(connecting, cmd)

	onAccount(t, m, "staging")
	require.Len(t, o.opened["staging"], 1)
	assert.Equal(t, 1, o.last("staging").pings)
	assert.Contains(t, plain(m.View()), firstDatabase, "the catalog shows the new account's databases")
	assert.Equal(t, 1, o.last("staging").calls[""])
}

func TestAFailedSwitchLeavesTheSessionWhereItWas(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *opener)
		want  string
	}{
		{
			name:  "open refused",
			setup: func(o *opener) { o.failOpen["staging"] = errors.New("dial tcp: lookup staging: no such host") },
			want:  "no such host",
		},
		{
			name:  "ping refused",
			setup: func(o *opener) { o.failPing["staging"] = 1 },
			want:  "503 Service Unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOpener(t)
			tt.setup(o)
			m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
			require.Contains(t, statusBar(m), "sales.orders")

			m = switchTo(t, m, "staging")

			view := plain(m.View())
			assert.Contains(t, view, accountsTitle, "the switcher stays open")
			assert.Contains(t, switcherRow(t, m, "staging"), "failed")
			assert.Contains(t, view, tt.want, "the reason sits under the row")
			for _, conn := range o.opened["staging"] {
				assert.Equal(t, 1, conn.closes, "a connection that failed its ping is closed")
			}

			m = pressAll(t, m, keyMsg(tea.KeyEnter))
			onAccount(t, m, "staging")

			m = switchTo(t, m, "prod")
			assert.Contains(t, statusBar(m), "sales.orders", "prod's scope survived the failure")
			assert.Contains(t, plain(m.View()), firstContainer, "and so did its tree")
		})
	}
}

func TestSwitchingToAConnectedAccountFetchesNothing(t *testing.T) {
	o := newOpener(t)
	m := switchTo(t, newAccountsModel(t, o, tui.Options{}), "staging")
	prod := o.last("prod")
	rootLoads := prod.calls[""]

	m = switchTo(t, m, "prod")

	onAccount(t, m, "prod")
	assert.Len(t, o.opened["prod"], 1, "no second Open")
	assert.Equal(t, rootLoads, prod.calls[""], "no catalog request")
}

func TestSwitchingBackRestoresTheTreeAndScopeOfEachAccount(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	require.Contains(t, statusBar(m), "sales.orders")

	m = switchTo(t, m, "staging")
	assert.Contains(t, statusBar(m), "no scope", "a scope is never carried across accounts")
	assert.NotContains(t, plain(m.View()), firstContainer, "staging's tree starts collapsed")

	m = switchTo(t, m, "prod")
	assert.Contains(t, statusBar(m), "sales.orders")
	assert.Contains(t, plain(m.View()), firstContainer)
	assert.Contains(t, plain(pressAll(t, m, keyRune('e'), keyText("SELECT * FROM c"), keyMsg(tea.KeyCtrlR)).View()),
		"item-1-0", "the restored scope is the one a bare FROM c runs against")
}

func TestTheBufferAndResultsSurviveASwitch(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	m = runQuery(t, m, "SELECT * FROM c")
	require.Contains(t, plain(m.View()), "Results · prod")

	m = switchTo(t, m, "staging")

	view := plain(m.View())
	assert.Contains(t, view, "SELECT * FROM c", "one editor, shared by every account")
	assert.Contains(t, view, "item-1-0")
	assert.Contains(t, view, "Results · prod", "the rows say which account they came from")

	reads := o.last("prod").pageReads
	pressAll(t, m, keyMsg(tea.KeyTab), keyRune('m'))
	assert.Equal(t, reads+1, o.last("prod").pageReads, "m still reads from the connection that ran the query")
}

func TestARunInFlightLandsUnderItsOwnAccountAndTheNextRunGoesToTheNewOne(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	m = typeQuery(t, m, "SELECT * FROM sales.orders")
	running, runCmd := m.Update(keyMsg(tea.KeyCtrlR))

	switched := switchTo(t, running, "staging")
	landed, _ := settle(switched, runCmd)

	assert.Contains(t, plain(landed.View()), "Results · prod")
	assert.Contains(t, plain(landed.View()), "item-1-0")

	pressAll(t, landed, keyMsg(tea.KeyCtrlR))
	assert.Len(t, o.last("prod").queries, 1)
	require.Len(t, o.last("staging").queries, 1, "ctrl+r after the switch reaches the new account")
}

func TestACatalogResponseForABackgroundAccountWaitsForItsReturn(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{}), "staging")
	before := plain(m.View())

	m, _ = settle(m.Update(tui.CatalogLoadedMsg{
		Account: "prod",
		Parent:  []string{"telemetry"},
		Nodes:   []adapter.Node{{Kind: adapter.NodeContainer, Name: "late-arrival", Path: []string{"telemetry", "late-arrival"}}},
		Token:   1,
	}))
	m, _ = settle(m.Update(tui.ErrMsg{
		Account: "prod",
		Op:      tui.OpCatalogChildren,
		Path:    []string{firstDatabase},
		Token:   1,
		Err:     errors.New("background refusal"),
	}))

	assert.Equal(t, before, plain(m.View()), "nothing on screen changes")
	back := plain(switchTo(t, m, "prod").View())
	assert.Contains(t, back, "background refusal", "the failure was filed in prod's tree")
}

func TestAnAccountWithNoKeyOpensTheFormSeededForIt(t *testing.T) {
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	c := &connector{t: t}
	m := switchTo(t, newAccountsModel(t, o, tui.Options{Connect: c.connect}), "staging")

	view := plain(m.View())
	require.Contains(t, view, "Profile staging has no key")
	assert.Contains(t, view, "https://staging.documents.azure.com:443/")

	back := pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(back.View()), accountsTitle, "esc returns to the switcher")

	m = pressAll(t, m, keyText("typed-key"), keyMsg(tea.KeyEnter))
	onAccount(t, m, "staging")
	require.Len(t, c.forms, 1)
	assert.Equal(t, "staging", c.forms[0].Profile)
}

func TestTheLaunchAccountWithNoKeyOpensTheForm(t *testing.T) {
	o := newOpener(t)
	o.failOpen["prod"] = tui.ErrCredentialsNeeded

	m := newAccountsModel(t, o, tui.Options{Connect: (&connector{t: t}).connect})

	assert.Contains(t, plain(m.View()), "Profile prod has no key")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyEscape)).View()), accountsTitle,
		"there are other accounts to go back to, so esc does not quit")
}

func TestALaunchThatCannotConnectOpensTheSwitcherWithTheReason(t *testing.T) {
	o := newOpener(t)
	o.failOpen["prod"] = errors.New("cosmos: endpoint is not a URL")

	m := newAccountsModel(t, o, tui.Options{})

	assert.Contains(t, plain(m.View()), accountsTitle)
	assert.Contains(t, switcherRow(t, m, "prod"), "failed")
	assert.Contains(t, plain(m.View()), "endpoint is not a URL")
}

func TestAddingAnAccountListsItAndMovesThere(t *testing.T) {
	c := &connector{t: t}
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Connect: c.connect}), keyMsg(tea.KeyCtrlG), keyRune('a'))
	require.Contains(t, plain(m.View()), "Add an account")

	m = fillAndSubmit(t, m)

	onAccount(t, m, "quill")
	switcher := plain(pressAll(t, m, keyMsg(tea.KeyCtrlG)).View())
	assert.Contains(t, switcherRow(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "quill"), "current")
	assert.Less(t, strings.Index(switcher, " prod "), strings.Index(switcher, " quill "), "sorted into place")
	assert.Less(t, strings.Index(switcher, " quill "), strings.Index(switcher, " staging "))
}

func TestTheFormRefusesANameConnectedRightNow(t *testing.T) {
	c := &connector{t: t}
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Connect: c.connect}), keyMsg(tea.KeyCtrlG), keyRune('a'))

	m = pressAll(t, m,
		keyText("prod"), keyMsg(tea.KeyTab),
		keyText("https://elsewhere"), keyMsg(tea.KeyTab),
		keyText("typed-key"), keyMsg(tea.KeyEnter))

	assert.Empty(t, c.forms, "the connector is never called")
	assert.Contains(t, plain(m.View()), "connected, or connecting")
}

func TestDisconnectingABackgroundAccountClosesItOnce(t *testing.T) {
	o := newOpener(t)
	m := switchTo(t, newAccountsModel(t, o, tui.Options{}), "staging")
	m = pressAll(t, m, keyMsg(tea.KeyEnter)) // expand staging's first database
	m = switchTo(t, m, "prod")

	m = pressAll(t, highlight(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "staging"), keyRune('x'))

	assert.Equal(t, 1, o.last("staging").closes)
	assert.Contains(t, switcherRow(t, m, "staging"), "not connected")
	again := pressAll(t, m, keyMsg(tea.KeyEnter))
	onAccount(t, again, "staging")
	assert.NotContains(t, plain(again.View()), firstContainer, "its tree starts over, collapsed")
}

func TestDisconnectingTheCurrentAccountFallsBackToTheMostRecent(t *testing.T) {
	o := newOpener(t)
	m := switchTo(t, switchTo(t, newAccountsModel(t, o, tui.Options{}), "emulator"), "staging")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyRune('x'))

	assert.Contains(t, plain(m.View()), accountsTitle, "the switcher stays open")
	assert.True(t, strings.HasPrefix(statusBar(pressAll(t, m, keyMsg(tea.KeyEscape))), "emulator "),
		"the most recently used account still connected takes over")
}

func TestDisconnectingTheLastAccountLeavesNone(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG), keyRune('x'))

	assert.Contains(t, plain(m.View()), accountsTitle, "the switcher stays open")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.True(t, strings.HasPrefix(statusBar(m), "no account"))
	assert.Contains(t, plain(m.View()), "no account connected:", "the catalog says why it is empty")

	m = runQuery(t, m, "SELECT * FROM sales.orders")
	assert.Contains(t, plain(m.View()), "no account connected")
	assert.Equal(t, 1, o.last("prod").closes)
}

func TestDisconnectingTheAccountOfARunDiscardsItsPageInFlight(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	m = focusResults(t, runQuery(t, m, "SELECT * FROM c"))
	fetching, fetchCmd := m.Update(keyRune('m'))

	disconnected := pressAll(t, fetching, keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))
	m, _ = settle(disconnected, fetchCmd)

	prod := o.last("prod")
	assert.Equal(t, 1, prod.closed, "the late page's cursor is closed on arrival")
	assert.Equal(t, 1, prod.closes, "the connection is closed once")
	assert.Contains(t, plain(m.View()), "item-1-0", "loaded rows stay on screen")
	assert.NotContains(t, plain(m.View()), "item-2-0", "the late page is not shown")
	reads := prod.pageReads
	pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('m'))
	assert.Equal(t, reads, prod.pageReads, "m fetches nothing")
}

func TestAConnectionArrivingAfterItsAccountWasDisconnectedIsClosed(t *testing.T) {
	o := newOpener(t)
	m := highlight(t, pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG)), "staging")
	connecting, connectCmd := m.Update(keyMsg(tea.KeyEnter))

	disconnected := pressAll(t, connecting, keyRune('x'))
	m, _ = settle(disconnected, connectCmd)

	assert.Equal(t, 1, o.last("staging").closes)
	assert.Contains(t, switcherRow(t, m, "staging"), "not connected")
}

func TestEnterOnAnotherRowMovesTheWait(t *testing.T) {
	o := newOpener(t)
	m := highlight(t, pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG)), "staging")
	first, stagingCmd := m.Update(keyMsg(tea.KeyEnter))
	second, emulatorCmd := highlight(t, first, "emulator").Update(keyMsg(tea.KeyEnter))

	m, _ = settle(second, emulatorCmd)
	m, _ = settle(m, stagingCmd)

	onAccount(t, m, "emulator")
	assert.Contains(t, switcherRow(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "staging"), "connected",
		"the earlier attempt still connects, in the background")
}

func TestQuittingClosesEveryConnectedAccountOnce(t *testing.T) {
	o := newOpener(t)
	m := switchTo(t, newAccountsModel(t, o, tui.Options{}), "staging")

	_, msgs := press(t, m, keyMsg(tea.KeyCtrlC))

	require.True(t, hasMsg[tea.QuitMsg](msgs))
	assert.Equal(t, 1, o.last("prod").closes)
	assert.Equal(t, 1, o.last("staging").closes)
	assert.Empty(t, o.opened["emulator"])
}

func TestAAndXDoNothingInTheCatalog(t *testing.T) {
	o := newOpener(t)
	m := newAccountsModel(t, o, tui.Options{})

	assert.Equal(t, m.View(), pressAll(t, m, keyRune('a'), keyRune('x')).View())
	assert.Zero(t, o.last("prod").closes)
	help := plain(pressAll(t, m, keyRune('?')).View())
	assert.NotContains(t, help, "add account")
	assert.NotContains(t, help, "disconnect")
}

func TestAQueryNamingAnAccountIsRefusedAndRecorded(t *testing.T) {
	o := newOpener(t)
	store := &recordingStore{}
	m := newAccountsModel(t, o, tui.Options{History: store})

	m = runQuery(t, m, "SELECT * FROM staging.sales.orders c")

	assert.Contains(t, plain(m.View()), "FROM staging.sales.orders: a query runs on the")
	assert.Empty(t, o.last("prod").queries, "the query reaches no connection")
	require.Len(t, store.entries, 1)
	assert.Equal(t, "prod", store.entries[0].Profile)
	assert.Contains(t, store.entries[0].Error, "switch accounts with ctrl+g")
}

func TestAThreePartSourceThatNamesNoAccountPassesThrough(t *testing.T) {
	o := newOpener(t)

	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))

	runQuery(t, m, "SELECT * FROM orders.lines.items c")

	assert.Len(t, o.last("prod").queries, 1)
}

func TestHistoryListsOnlyTheActiveAccountsRuns(t *testing.T) {
	store := &recordingStore{}
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{History: store}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	m = runQuery(t, m, "SELECT 'prod run' FROM c")
	m = runAnother(t, switchTo(t, m, "staging"), "SELECT 'staging run' FROM sales.orders c")
	require.Len(t, store.entries, 2)
	assert.Equal(t, []string{"prod", "staging"}, []string{store.entries[0].Profile, store.entries[1].Profile})

	staging := plain(openHistory(t, m).View())
	assert.Contains(t, staging, "History · staging")
	assert.Contains(t, staging, "staging run")
	assert.NotContains(t, staging, "prod run")

	prod := plain(openHistory(t, switchTo(t, pressAll(t, m, keyMsg(tea.KeyEscape)), "prod")).View())
	assert.Contains(t, prod, "History · prod")
	assert.Contains(t, prod, "prod run")
	assert.NotContains(t, prod, "staging run")
}

func TestHistoryLoadedForAnotherAccountOpensNothing(t *testing.T) {
	m := newAccountsModel(t, newOpener(t), tui.Options{})

	m, _ = settle(m.Update(tui.HistoryLoadedMsg{Account: "staging", Entries: []history.Entry{{Profile: "staging", Query: "SELECT 1"}}}))

	assert.NotContains(t, plain(m.View()), historyTitle)
}

func TestCtrlGInsideAnOverlayDoesNotSwitch(t *testing.T) {
	m := openHistory(t, newAccountsModel(t, newOpener(t), tui.Options{History: &recordingStore{}}))

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))

	assert.NotContains(t, plain(m.View()), accountsTitle)
}

func TestHistoryWithNoAccountSaysWhy(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{}), keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))

	m = openHistory(t, m)

	assert.Contains(t, plain(m.View()), "no account connected: ctrl+g to choose one")
}

func TestTheSwitcherFilterTreatsLettersAsText(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG), keyRune('/'), keyText("rel"), keyRune('a'), keyRune('x'))

	view := plain(m.View())
	assert.Contains(t, view, "relax")
	assert.Contains(t, view, "nothing matches")
	assert.Zero(t, o.last("prod").closes, "x typed into the filter disconnects nothing")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(m.View()), accountsTitle, "the first esc clears the filter")
	assert.NotContains(t, plain(pressAll(t, m, keyMsg(tea.KeyEscape)).View()), accountsTitle, "the second closes")
}

func TestTheSwitcherFilterMatchesTheEndpoint(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{}), keyMsg(tea.KeyCtrlG), keyRune('/'), keyText("localhost"))

	view := plain(m.View())
	assert.Contains(t, view, "emulator")
	assert.NotContains(t, view, "staging")
}

func TestAFirstRunFormSeesNoSwitcherBehindIt(t *testing.T) {
	m := tui.New(tui.Options{Icons: theme.Icons(), Connect: (&connector{t: t}).connect, Form: panes.ConnectForm{}})

	_, msgs := press(t, m, keyMsg(tea.KeyEscape))

	assert.True(t, hasMsg[tea.QuitMsg](msgs), "a first run has nothing behind the form, so esc quits")
}

// submitForm types a whole connect form for name and submits it, handing back
// the attempt unsettled.
func submitForm(t *testing.T, m tea.Model, name string) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, m,
		keyText(name), keyMsg(tea.KeyTab),
		keyText("https://"+name+".documents.azure.com"), keyMsg(tea.KeyTab),
		keyText("typed-key"))
	return m.Update(keyMsg(tea.KeyEnter))
}

func TestAFormLeftWhileConnectingNeverTakesOverTheNextOne(t *testing.T) {
	c := &connector{t: t}
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Connect: c.connect}), keyMsg(tea.KeyCtrlG), keyRune('a'))
	m, alphaCmd := submitForm(t, m, "alpha")
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyRune('a'))
	m, betaCmd := submitForm(t, m, "beta")

	m, _ = settle(m, alphaCmd)
	require.Contains(t, plain(m.View()), "Add an account", "beta's form is still up")
	m, _ = settle(m, betaCmd)

	onAccount(t, m, "beta")
	assert.Contains(t, switcherRow(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "alpha"), "connected",
		"the attempt walked away from connected in the background")
}

func TestAFormNeverSubmitsANameAlreadyOnItsWay(t *testing.T) {
	c := &connector{t: t}
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Connect: c.connect}), keyMsg(tea.KeyCtrlG), keyRune('a'))
	m, _ = submitForm(t, m, "alpha")
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyRune('a'))

	m, _ = submitForm(t, m, "alpha")

	assert.Contains(t, plain(m.View()), "connected, or connecting")
}

func TestAnAttemptSettlingBehindTheAddFormLeavesItAlone(t *testing.T) {
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	m := highlight(t, pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG)), "staging")
	m, connectCmd := m.Update(keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyRune('a'), keyText("half-typed"))

	m, _ = settle(m, connectCmd)

	assert.Contains(t, plain(m.View()), "Add an account")
	assert.Contains(t, plain(m.View()), "half-typed")
}

func TestAResponseFromAnEarlierConnectionIsDroppedAfterAReconnect(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{}), "staging")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEnter))
	onAccount(t, m, "staging")

	m, _ = settle(m.Update(tui.ErrMsg{
		Account: "staging",
		Op:      tui.OpCatalogRoot,
		Token:   1, // the root load of the connection x closed
		Err:     errors.New("client closed"),
	}))

	assert.NotContains(t, plain(m.View()), "client closed")
	assert.Contains(t, plain(m.View()), firstDatabase)
}

func TestAScopeAnnouncedForAnotherAccountStaysWithIt(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{}), "staging")

	m, _ = settle(m.Update(tui.ScopeChangedMsg{Account: "prod", Scope: []string{firstDatabase, firstContainer}}))

	assert.Contains(t, statusBar(m), "no scope")
	assert.Contains(t, statusBar(switchTo(t, m, "prod")), firstDatabase+"."+firstContainer)
}

func TestClosingConnectionsAfterAQuitClosesNothingTwice(t *testing.T) {
	o := newOpener(t)
	m := switchTo(t, newAccountsModel(t, o, tui.Options{}), "staging")

	quit, _ := press(t, m, keyMsg(tea.KeyCtrlC))
	session, ok := quit.(tui.Model)
	require.True(t, ok)
	session.CloseConnections()

	assert.Equal(t, 1, o.last("prod").closes)
	assert.Equal(t, 1, o.last("staging").closes)
}

func TestAnEarlierAttemptNeverSettlesALaterOne(t *testing.T) {
	o := newOpener(t)
	m := highlight(t, pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG)), "staging")
	m, first := m.Update(keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyRune('x'))
	m, second := m.Update(keyMsg(tea.KeyEnter))

	m, _ = settle(m, first)
	require.Contains(t, switcherRow(t, m, "staging"), "connecting…", "the first attempt's answer is not the one awaited")
	m, _ = settle(m, second)

	onAccount(t, m, "staging")
	require.Len(t, o.opened["staging"], 2)
	assert.Equal(t, 1, o.opened["staging"][0].closes, "the superseded connection is closed")
	assert.Zero(t, o.opened["staging"][1].closes)
}

// newLaunchingModel is a session whose launch attempt has not been run yet.
func newLaunchingModel(t *testing.T, o *opener) (tea.Model, tea.Cmd) {
	t.Helper()
	m, _ := tui.New(tui.Options{
		Icons:    theme.Icons(),
		Accounts: fixtureAccounts(),
		Launch:   "prod",
		Open:     o.open,
		Connect:  (&connector{t: t}).connect,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return m, m.Init()
}

func TestTheLaunchAccountShowsItIsLoadingBeforeItConnects(t *testing.T) {
	m, _ := newLaunchingModel(t, newOpener(t))

	assert.Contains(t, plain(m.View()), "loading")
	m = runQuery(t, m, "SELECT * FROM sales.orders")
	assert.Contains(t, plain(m.View()), "prod: still connecting")
}

func TestALaunchFailingBehindAFormLeavesTheFormAlone(t *testing.T) {
	o := newOpener(t)
	o.failOpen["prod"] = errors.New("cosmos: endpoint is not a URL")
	m, launch := newLaunchingModel(t, o)
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyRune('a'), keyText("half-typed"))

	m, _ = settle(m, launch)

	assert.Contains(t, plain(m.View()), "half-typed")
	assert.True(t, strings.HasPrefix(statusBar(pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyEscape))), "no account "))
}

func TestTheSwitcherListsAProfileAddedOutsideTheSession(t *testing.T) {
	listed := append(fixtureAccounts(), tui.Account{Name: "late", Endpoint: "https://late.documents.azure.com"})
	m := newAccountsModel(t, newOpener(t), tui.Options{
		ListAccounts: func() ([]tui.Account, error) { return listed, nil },
	})

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))

	assert.Contains(t, switcherRow(t, m, "late"), "not connected")
}

func TestAConnectingCurrentAccountShowsItIsConnecting(t *testing.T) {
	m, _ := newLaunchingModel(t, newOpener(t))

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))

	assert.Contains(t, switcherRow(t, m, "prod"), "connecting…")
}

func TestAChildLoadFromAClosedConnectionIsDroppedAfterAReconnect(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{}), "staging")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEnter))

	m, _ = settle(m.Update(tui.ErrMsg{
		Account: "staging",
		Op:      tui.OpCatalogChildren,
		Path:    []string{firstDatabase},
		Token:   1, // the prefetch of the connection x closed
		Err:     errors.New("client closed"),
	}))

	assert.NotContains(t, plain(m.View()), "client closed")
}

func TestDisconnectingTheAccountOfARunningQuerySaysWhyItStopped(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{}), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	m = typeQuery(t, m, "SELECT * FROM c")
	running, _ := m.Update(keyMsg(tea.KeyCtrlR))

	m = pressAll(t, running, keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))

	assert.Contains(t, plain(m.View()), "its account was disconnected")
}

func TestAFormAttemptFailingBehindTheSwitcherUpdatesItsRow(t *testing.T) {
	c := &connector{t: t, failFirst: 1}
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	m := switchTo(t, newAccountsModel(t, o, tui.Options{Connect: c.connect}), "staging")
	m, attempt := pressAll(t, m, keyText("typed-key")).Update(keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	require.Contains(t, switcherRow(t, m, "staging"), "connecting…")

	m, _ = settle(m, attempt)

	assert.NotContains(t, switcherRow(t, m, "staging"), "connecting…")
}

func TestHelpStillFillsTheScreenAfterASwitch(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{}), "staging")

	help := plain(pressAll(t, m, keyRune('?')).View())

	assert.Contains(t, help, "Anywhere")
	assert.Contains(t, help, "accounts")
}

func TestASwitchLandingBehindAnotherOverlayLeavesItOpen(t *testing.T) {
	m := highlight(t, pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{History: &recordingStore{}}), keyMsg(tea.KeyCtrlG)), "staging")
	m, connect := m.Update(keyMsg(tea.KeyEnter))
	m = openHistory(t, pressAll(t, m, keyMsg(tea.KeyEscape)))

	m, _ = settle(m, connect)

	assert.Contains(t, plain(m.View()), historyTitle)
	assert.True(t, strings.HasPrefix(statusBar(pressAll(t, m, keyMsg(tea.KeyEscape))), "prod "),
		"the session stays put; staging connected in the background")
}

func TestXCancelsAFormAttemptItsRowShows(t *testing.T) {
	c := &connector{t: t}
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	m := switchTo(t, newAccountsModel(t, o, tui.Options{Connect: c.connect}), "staging")
	m, attempt := pressAll(t, m, keyText("typed-key")).Update(keyMsg(tea.KeyEnter))
	m = highlight(t, pressAll(t, m, keyMsg(tea.KeyEscape)), "staging")

	m = pressAll(t, m, keyRune('x'))
	m, _ = settle(m, attempt)

	assert.Contains(t, switcherRow(t, m, "staging"), "not connected")
}

func TestAFormAttemptFailingBehindTheSwitcherShowsWhy(t *testing.T) {
	c := &connector{t: t, failFirst: 1}
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	m := switchTo(t, newAccountsModel(t, o, tui.Options{Connect: c.connect}), "staging")
	m, attempt := pressAll(t, m, keyText("typed-key")).Update(keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m, _ = settle(m, attempt)

	assert.Contains(t, switcherRow(t, m, "staging"), "failed")
	assert.Contains(t, plain(m.View()), "401 Unauthorized")
}

func TestInfoReadOnOneAccountIsNeverShownForAnother(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyRune('i'))
	require.Len(t, o.last("prod").inspected, 1)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	pressAll(t, switchTo(t, m, "staging"), keyRune('i'))

	assert.Len(t, o.last("staging").inspected, 1, "staging reads its own node, not prod's cached one")
	assert.Len(t, o.last("prod").inspected, 1)
}

func TestInfoReadOnAClosedConnectionIsDroppedAfterAReconnect(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyDown))
	m, inspect := m.Update(keyRune('i'))
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEnter))
	onAccount(t, m, "prod")

	m, _ = settle(m, inspect)
	pressAll(t, pressAll(t, m, keyMsg(tea.KeyDown)), keyRune('i'))

	assert.Len(t, o.last("prod").inspected, 1, "the new connection reads the node itself")
}
