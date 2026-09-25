package tui_test

import (
	"errors"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

var archivedOrders = []string{"archive", firstContainer}

// newCloneAccountsModel is a session on prod, whose sales.orders holds 25
// items, beside emulator and staging; readOnly names the accounts whose
// profiles refuse writes.
func newCloneAccountsModel(t *testing.T, o *opener, readOnly ...string) tea.Model {
	t.Helper()
	o.options["prod"] = []mock.Option{mock.WithItemCount(ordersPath, cloneItemCount)}
	accounts := fixtureAccounts()
	for i := range accounts {
		accounts[i].ReadOnly = slices.Contains(readOnly, accounts[i].Name)
	}
	m, _ := tui.New(tui.Options{
		Icons:    theme.Icons(),
		Accounts: accounts,
		Launch:   "prod",
		Open:     o.open,
		Manage:   managed,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	return m
}

// cloneFormOnProd opens the prompt on prod's sales.orders, which the
// session starts with in view.
func cloneFormOnProd(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter), keyRune('y'))
}

// toStagingArchive cycles the target to staging, and puts the copy in a
// database staging does not have.
func toStagingArchive(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyRight), keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText(archivedOrders[0]))
}

// cloneToStaging runs the review for a clone of prod's sales.orders into
// staging's archive database, and confirms it.
func cloneToStaging(t *testing.T, m tea.Model) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, toStagingArchive(t, cloneFormOnProd(t, m)), keyMsg(tea.KeyEnter))
	require.Contains(t, plain(m.View()), "prod / sales.orders  →  staging / archive.orders")
	return confirmClone(t, m, "staging")
}

func TestTheTargetFieldOffersEveryWritableAccountWithItsState(t *testing.T) {
	m := cloneFormOnProd(t, newCloneAccountsModel(t, newOpener(t), "staging"))

	view := plain(m.View())
	assert.Contains(t, view, "prod · connected", "the source's own account comes first when it is writable")
	assert.Contains(t, view, "staging is read-only; set read_only = false on the profile to write to it")
	m = pressAll(t, m, keyMsg(tea.KeyRight))
	assert.Contains(t, plain(m.View()), "emulator · not connected")
	m = pressAll(t, m, keyMsg(tea.KeyRight))
	assert.Contains(t, plain(m.View()), "prod · connected", "and a read-only account is never offered")
}

func TestWithNoWritableAccountThereIsNowhereToClone(t *testing.T) {
	m := cloneFormOnProd(t, newCloneAccountsModel(t, newOpener(t), "emulator", "prod", "staging"))

	m, msgs := press(t, m, keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, "Target account  none")
	for _, name := range accountNames() {
		assert.Contains(t, view, name+" is read-only")
	}
	assert.False(t, hasMsg[tui.ClonePlannedMsg](msgs))
	assert.Contains(t, view, cloneContainerTitle)
}

func TestAReadOnlySourceClonesIntoAWritableTarget(t *testing.T) {
	o := newOpener(t)
	m := cloneFormOnProd(t, newCloneAccountsModel(t, o, "prod"))
	require.Contains(t, plain(m.View()), "emulator · not connected", "the first writable account by name")

	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyCtrlU), keyText(archivedOrders[0]), keyMsg(tea.KeyEnter))
	m, cmd := confirmClone(t, m, "emulator")
	runSteps(m, cmd)

	assert.Len(t, storedIn(t, o.last("emulator"), archivedOrders), cloneItemCount)
	prod := o.last("prod")
	assert.Zero(t, prod.store.Upserts())
	assert.Empty(t, prod.containers)
	assert.Empty(t, prod.deleted)
}

func TestTheFormConnectsATargetWithoutMovingTheSession(t *testing.T) {
	o := newOpener(t)
	m := toStagingArchive(t, cloneFormOnProd(t, newCloneAccountsModel(t, o)))

	connecting, cmd := m.Update(keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(connecting.View()), "connecting staging…")
	m, _ = settle(connecting, cmd)

	require.Len(t, o.opened["staging"], 1)
	assert.Equal(t, 1, o.last("staging").pings)
	assert.Contains(t, plain(m.View()), "prod / sales.orders  →  staging / archive.orders", "the review opens")
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyEscape))
	onAccount(t, m, "prod")
	assert.Contains(t, statusBar(m), "sales.orders", "with its scope")
	assert.Contains(t, switcherRow(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "staging"), "connected")
}

func TestAFailedTargetConnectionShowsInTheFormAndRetries(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *opener)
		want  string
	}{
		{
			name:  "open refused",
			setup: func(o *opener) { o.failOpen["staging"] = errors.New("dial tcp: lookup staging: no such host") },
			want:  "staging did not connect: dial tcp: lookup staging: no such host",
		},
		{
			name:  "ping refused",
			setup: func(o *opener) { o.failPing["staging"] = 1 },
			want:  "staging did not connect: ping: 503 Service Unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOpener(t)
			tt.setup(o)
			m := toStagingArchive(t, cloneFormOnProd(t, newCloneAccountsModel(t, o)))

			m = pressAll(t, m, keyMsg(tea.KeyEnter))

			assert.Contains(t, plain(m.View()), tt.want)
			for _, conn := range o.opened["staging"] {
				assert.Equal(t, 1, conn.closes, "what the failed attempt opened is closed")
			}
			m = pressAll(t, m, keyMsg(tea.KeyEnter))
			assert.Contains(t, plain(m.View()), "staging / archive.orders", "enter tries again")
		})
	}
}

func TestATargetWithNoKeyIsSentToTheSwitcher(t *testing.T) {
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	m := toStagingArchive(t, cloneFormOnProd(t, newCloneAccountsModel(t, o)))

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, "staging has no key: add it from the account switcher (ctrl+g), then clone")
	assert.Contains(t, view, cloneContainerTitle, "no connect form opens over the prompt")
}

func TestACrossAccountCloneWritesOnlyToTheTarget(t *testing.T) {
	o := newOpener(t)
	m, cmd := cloneToStaging(t, newCloneAccountsModel(t, o))

	m = runSteps(m, cmd)

	assert.Contains(t, plain(m.View()), "Done.")
	staging, prod := o.last("staging"), o.last("prod")
	assert.Len(t, storedIn(t, staging, archivedOrders), cloneItemCount)
	assert.Zero(t, prod.store.Upserts())
	assert.Empty(t, prod.containers)
	assert.Empty(t, prod.batches)
}

func TestTheTargetsTreeHoldsTheCopyBeforeTheSessionGetsThere(t *testing.T) {
	o := newOpener(t)
	m, cmd := cloneToStaging(t, newCloneAccountsModel(t, o))
	m = pressAll(t, runSteps(m, cmd), keyMsg(tea.KeyEscape))
	staging := o.last("staging")
	rootLoads := staging.calls[""]

	m = switchTo(t, m, "staging")

	assert.Contains(t, plain(m.View()), "archive")
	assert.Equal(t, rootLoads, staging.calls[""], "no request at switch time")
	m = switchTo(t, m, "prod")
	assert.Contains(t, plain(pressAll(t, m, keyRune('d')).View()), "Deleting sales.orders removes",
		"the source tree's cursor never moved")
}

func TestACloneSurvivesAccountSwitches(t *testing.T) {
	o := newOpener(t)
	m, cmd := cloneToStaging(t, newCloneAccountsModel(t, o))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = switchTo(t, m, "emulator")
	m = runSteps(m, cmd)

	assert.Len(t, storedIn(t, o.last("staging"), archivedOrders), cloneItemCount, "every page arrived")
	assert.Contains(t, statusBar(m), "clone done (y)")
	onAccount(t, m, "emulator")
	m = pressAll(t, m, keyRune('y'))
	assert.Contains(t, plain(m.View()), cloneProgressTitle, "y on another account's row reopens the clone")
	assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyCtrlG)).View()), cloneProgressTitle,
		"ctrl+g does nothing over the view")
}

func TestTheStatusBarFieldFollowsTheSessionEverywhere(t *testing.T) {
	m, _ := cloneToStaging(t, newCloneAccountsModel(t, newOpener(t)))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	before := statusBar(m)

	m = switchTo(t, m, "emulator")

	assert.Contains(t, before, "clone prod/sales.orders → staging 0% (y)")
	assert.Contains(t, statusBar(m), "clone prod/sales.orders → staging 0% (y)")
}

func TestTheSwitcherKeepsTheClonesAccountsConnected(t *testing.T) {
	o := newOpener(t)
	m, cmd := cloneToStaging(t, newCloneAccountsModel(t, o))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	m = switchTo(t, m, "emulator")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))

	for _, name := range []string{"prod", "staging"} {
		m = pressAll(t, highlight(t, m, name), keyRune('x'))
		assert.Contains(t, switcherRow(t, m, name), "connected")
		assert.Contains(t, plain(m.View()), "a clone is using "+name+": stop it first (y in the catalog)")
		assert.Zero(t, o.last(name).closes)
	}
	m = pressAll(t, highlight(t, m, "emulator"), keyRune('x'))
	assert.Equal(t, 1, o.last("emulator").closes, "any other account disconnects as usual")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	m = runSteps(m, cmd)
	m = pressAll(t, m, keyRune('y'), keyMsg(tea.KeyEscape), keyMsg(tea.KeyCtrlG))
	pressAll(t, highlight(t, m, "staging"), keyRune('x'))
	assert.Equal(t, 1, o.last("staging").closes, "once the clone is closed, x works")
}

func TestQuittingMidCloneClosesBothAccountsOnce(t *testing.T) {
	o := newOpener(t)
	m, _ := cloneToStaging(t, newCloneAccountsModel(t, o))
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m, _ = m.Update(keyMsg(tea.KeyCtrlC))
	_, cmd := m.Update(keyMsg(tea.KeyCtrlC))

	assert.Contains(t, messages(cmd), tea.QuitMsg{})
	assert.Equal(t, 1, o.last("prod").closes)
	assert.Equal(t, 1, o.last("staging").closes)
}
