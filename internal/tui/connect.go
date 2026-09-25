package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Connector connects and saves what the connect form submitted. It is called
// off the main goroutine, once per attempt, never with an incomplete form.
type Connector func(ctx context.Context, form panes.ConnectForm) (adapter.Connection, error)

// openCredentialsForm asks for the key of an account that has none anywhere,
// seeded with everything else about it.
func (m Model) openCredentialsForm(account Account) Model {
	return m.showConnectForm(panes.ConnectForm{
		Profile:    account.Name,
		Endpoint:   account.Endpoint,
		SkipVerify: account.SkipVerify,
		StoreKey:   true,
	})
}

func (m Model) openAddAccount() Model {
	return m.showConnectForm(panes.ConnectForm{StoreKey: true})
}

// showConnectForm drops any wait of the switcher behind it, so an attempt
// settling meanwhile cannot close or replace a form being typed into.
func (m Model) showConnectForm(form panes.ConnectForm) Model {
	m.accounts.waiting = ""
	m.shownFormAttempt = 0
	m.connectPane = panes.NewConnect(m.icons, form).OverSession().SetSize(m.width, m.height)
	m.overlay = overlayConnect
	return m
}

// handleConnectKey drives the form. Typed characters go to the form itself,
// which is why q cannot quit here: a profile may be called anything.
func (m Model) handleConnectKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.connectUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.escapeConnectForm()
	case key.Matches(msg, m.keys.Connect):
		return m.submitConnect()
	case key.Matches(msg, m.keys.NextPane), key.Matches(msg, m.keys.Down):
		m.connectPane = m.connectPane.NextField()
		return m, nil
	case key.Matches(msg, m.keys.PrevPane), key.Matches(msg, m.keys.Up):
		m.connectPane = m.connectPane.PrevField()
		return m, nil
	}
	return m.connectUpdate(msg)
}

// escapeConnectForm goes back to the switcher, or quits a first run, which
// has nothing behind the form to go back to.
func (m Model) escapeConnectForm() (Model, tea.Cmd) {
	if m.accounts.empty() {
		return m.quit()
	}
	return m.openAccounts()
}

func (m Model) connectUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.connectPane, cmd = m.connectPane.Update(msg)
	return m, cmd
}

// submitConnect hands the form to the connector, or shows what it still
// lacks. Enter while an attempt is in flight does nothing. A name connected,
// or on its way there, is refused before the connector sees it: with the key
// never stored, its profile would pass for one to complete.
func (m Model) submitConnect() (Model, tea.Cmd) {
	if m.connectPane.Connecting() {
		return m, nil
	}
	form, err := m.connectPane.Form()
	if err == nil && (m.accounts.busy(form.Profile) || m.formAttempts[form.Profile] != 0) {
		err = errAccountBusy
	}
	if err != nil {
		m.connectPane = m.connectPane.Fail(err)
		return m, nil
	}
	pane, tick := m.connectPane.StartConnecting()
	m.connectPane = pane
	m.lastAttempt++
	m.shownFormAttempt = m.lastAttempt
	m.formAttempts[form.Profile] = m.lastAttempt
	return m, tea.Batch(tick, m.openConnection(form, m.lastAttempt))
}

// acceptFormConnection lands the session on the account the open form
// connected, and closes the form and the switcher. An attempt the person has
// since walked away from stays connected in the background, since its
// profile has been saved either way — unless its account got connected by
// other means meanwhile.
func (m Model) acceptFormConnection(msg AccountConnectedMsg) (Model, tea.Cmd) {
	m = m.settleFormAttempt(msg.Account, msg.attempt)
	if m.accounts.busy(msg.Account) {
		return m, m.closeInBackground(msg.Connection)
	}
	account := msg.submitted
	if entry, ok := m.accounts.get(msg.Account); ok {
		account.Database, account.MaxJoinRows = entry.account.Database, entry.account.MaxJoinRows
	}
	m.accounts = m.accounts.add(account, m.blankEntry)
	entry, _ := m.accounts.get(msg.Account)
	m.logger.Info("connected", "account", msg.Account)
	m, load := m.attach(entry, msg.Connection)
	var follow tea.Cmd
	switch {
	case m.showsFormAttempt(msg.attempt), m.takeWait(msg.Account):
		m, follow = m.closeAccounts().setActive(msg.Account)
	case m.accounts.active == "":
		m, follow = m.setActive(msg.Account)
	}
	m, sync := m.syncAccountRows()
	return m, tea.Batch(load, follow, sync)
}

// failFormConnection shows why the attempt did not connect, on the form that
// made it; one the person has walked away from shows it under the account's
// row instead, when the switcher lists it.
func (m Model) failFormConnection(msg ConnectFailedMsg) (Model, tea.Cmd) {
	if m.formAttempts[msg.account] != msg.attempt {
		return m, nil
	}
	m = m.settleFormAttempt(msg.account, msg.attempt)
	if m.showsFormAttempt(msg.attempt) {
		m.connectPane = m.connectPane.Fail(msg.Err)
		return m, nil
	}
	m.logger.Warn("connect failed", "account", msg.account, "error", msg.Err)
	m.takeWait(msg.account)
	if entry, ok := m.accounts.get(msg.account); ok && !entry.connected() {
		entry.state, entry.err = panes.AccountFailed, msg.Err
		m.accounts.put(entry)
	}
	return m.syncAccountRows()
}

// abandonFormAttempt forgets the connect form's attempt on account: when it
// lands, it finds nothing waiting and its connection is closed.
func (m Model) abandonFormAttempt(account string) (Model, tea.Cmd) {
	delete(m.formAttempts, account)
	if m.accounts.waiting == account {
		m.accounts.waiting = ""
	}
	return m.syncAccountRows()
}

func (m Model) settleFormAttempt(account string, attempt int) Model {
	if m.formAttempts[account] == attempt {
		delete(m.formAttempts, account)
	}
	return m
}

// showsFormAttempt reports whether the form on screen is the one that made
// attempt.
func (m Model) showsFormAttempt(attempt int) bool {
	return m.overlay == overlayConnect && attempt == m.shownFormAttempt
}

func (m Model) openConnection(form panes.ConnectForm, attempt int) tea.Cmd {
	connect := m.connect
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		conn, err := connect(ctx, form)
		if err != nil {
			return ConnectFailedMsg{Err: err, account: form.Profile, attempt: attempt}
		}
		return AccountConnectedMsg{
			Account:    form.Profile,
			Connection: conn,
			attempt:    attempt,
			submitted:  Account{Name: form.Profile, Endpoint: form.Endpoint, SkipVerify: form.SkipVerify},
		}
	}
}
