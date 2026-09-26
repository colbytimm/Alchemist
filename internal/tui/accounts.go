package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Account is one profile the session can be on. Its identity is Name.
type Account struct {
	Name        string
	Endpoint    string // shown in the switcher; seeds the connect form
	SkipVerify  bool
	Database    string // expanded when the account's root first arrives
	MaxJoinRows int    // query.DefaultMaxJoinRows when zero
	Writers     int    // writes a clone into this account keeps in flight; writers.DefaultSize when zero
	// SampleFields lets completion read a few items of this account's
	// containers, when the session allows it too.
	SampleFields bool
	// ReadOnly refuses every write the session could make on the account.
	// A snapshot writes nothing to the account, so it is taken either way.
	ReadOnly bool
	// SnapshotMaxItems refuses a snapshot of a larger container;
	// snapshot.DefaultMaxItems when zero.
	SnapshotMaxItems int64
	// MaxMutationItems refuses an update that selects more items;
	// mutate.DefaultMaxTargets when zero.
	MaxMutationItems int
}

// Opener connects the saved account called name.
type Opener func(ctx context.Context, name string) (adapter.Connection, error)

// ErrCredentialsNeeded from an Opener sends the session to the connect form.
var ErrCredentialsNeeded = errors.New("no key in the keychain or the environment")

var (
	errNoAccount       = errors.New("no account connected: ctrl+g to choose one")
	errAccountInQuery  = errors.New("a query runs on the account you are on. Remove the account name, and switch accounts with ctrl+g")
	errAccountBusy     = errors.New("that account is connected, or connecting: pick another name, or switch to it with ctrl+g")
	errRunAbandoned    = errors.New("run stopped: its account was disconnected")
	errStillConnecting = errors.New("still connecting: run the query once the catalog loads")
)

// accountEntry is what the session keeps of one account: its connection and
// the tree and scope it was left with, for as long as it stays connected.
type accountEntry struct {
	account    Account
	state      panes.AccountState
	err        error
	connection adapter.Connection
	catalog    adapter.Catalog
	management Management
	pane       panes.Catalog
	info       panes.Info
	scope      []string
	// index is what completion offers for this account, and samples how far
	// each of its containers' field samples has got.
	index   *complete.Index
	samples map[string]sampleState
	// pendingDatabase is the database still to expand once the root arrives.
	pendingDatabase string
	// attempt is the number of the attempt connecting the account, which a
	// result must carry to count.
	attempt int
}

// permitted is what the session may do with the account: everything its
// connection allows, less every write when the account is read-only. A
// batch that only reads stays possible, so Batcher is kept; Model.batcher
// refuses the rest.
func (e accountEntry) permitted() Management {
	management := e.management
	if e.account.ReadOnly {
		management.Admin, management.Throughput, management.Drafter, management.Writer = nil, nil, nil, nil
		management.Editor = nil
	}
	return management
}

func (e accountEntry) connected() bool {
	return e.state == panes.AccountConnected
}

// readOn reports whether e is still on the connection attempt made.
func (e accountEntry) readOn(attempt int) bool {
	return e.connected() && e.attempt == attempt
}

// awaits reports whether the result of attempt is the one e is waiting for.
func (e accountEntry) awaits(attempt int) bool {
	return e.state == panes.AccountConnecting && e.attempt == attempt
}

// accountSet is every account of the session. Like the catalog panes it
// holds, its value receiver shares the entry map, so a caller must keep every
// accountSet it is handed.
type accountSet struct {
	entries map[string]accountEntry
	// active is the account the session is on; empty when none.
	active string
	// waiting is the account the switcher waits on to connect.
	waiting string
	// recent lists connected accounts, most recently used first.
	recent []string
}

func newAccountSet(accounts []Account, blank func() accountEntry) accountSet {
	s := accountSet{entries: map[string]accountEntry{}}
	for _, account := range accounts {
		s = s.add(account, blank)
	}
	return s
}

// known reports whether name is an account of this session, connected or not.
func (s accountSet) known(name string) bool {
	_, ok := s.entries[name]
	return ok
}

func (s accountSet) get(name string) (accountEntry, bool) {
	entry, ok := s.entries[name]
	return entry, ok
}

func (s accountSet) put(entry accountEntry) {
	s.entries[entry.account.Name] = entry
}

// add lists account, or refreshes what the switcher shows of one already
// listed without touching its state.
func (s accountSet) add(account Account, blank func() accountEntry) accountSet {
	entry, ok := s.entries[account.Name]
	if !ok {
		entry = blank()
	}
	entry.account = account
	s.put(entry)
	return s
}

func (s accountSet) empty() bool {
	return len(s.entries) == 0
}

// busy reports whether name is connected or an Opener is connecting it.
func (s accountSet) busy(name string) bool {
	entry, ok := s.entries[name]
	return ok && (entry.connected() || entry.state == panes.AccountConnecting)
}

// used moves name to the front of the recently used order.
func (s accountSet) used(name string) accountSet {
	s = s.forget(name)
	s.recent = append([]string{name}, s.recent...)
	return s
}

func (s accountSet) forget(name string) accountSet {
	s.recent = slices.DeleteFunc(slices.Clone(s.recent), func(n string) bool { return n == name })
	return s
}

// fallback is the most recently used account still connected, empty when
// there is none.
func (s accountSet) fallback() string {
	for _, name := range s.recent {
		if entry, ok := s.entries[name]; ok && entry.connected() {
			return name
		}
	}
	return ""
}

func (s accountSet) names() []string {
	return slices.Sorted(maps.Keys(s.entries))
}

func (s accountSet) rows() []panes.AccountRow {
	names := s.names()
	rows := make([]panes.AccountRow, 0, len(names))
	for _, name := range names {
		entry := s.entries[name]
		rows = append(rows, panes.AccountRow{
			Name:     name,
			Endpoint: entry.account.Endpoint,
			Database: entry.account.Database,
			State:    entry.state,
			Err:      entry.err,
			ReadOnly: entry.account.ReadOnly,
		})
	}
	return rows
}

// blankEntry is an account the session has not connected yet.
func (m Model) blankEntry() accountEntry {
	return accountEntry{
		pane:    m.newCatalogPane(),
		info:    m.newInfoPane(),
		index:   complete.NewIndex(),
		samples: map[string]sampleState{},
	}
}

func (m Model) newInfoPane() panes.Info {
	return panes.NewInfo(m.icons, m.keys.InfoKeys()).SetSize(m.width, m.height)
}

// newCatalogPane builds a tree sized for the layout, so an account connected
// mid-session needs no layout pass before it is shown.
func (m Model) newCatalogPane() panes.Catalog {
	width, height := m.catalogSize()
	return panes.NewCatalog(m.icons).SetSize(width, height)
}

// catalogPane is the tree on screen: the active account's, or the notice that
// there is none.
func (m Model) catalogPane() panes.Catalog {
	if entry, ok := m.accounts.get(m.accounts.active); ok {
		return entry.pane
	}
	return m.noAccountPane
}

func (m Model) setCatalogPane(pane panes.Catalog) Model {
	entry, ok := m.accounts.get(m.accounts.active)
	if !ok {
		m.noAccountPane = pane
		return m
	}
	entry.pane = pane
	m.accounts.put(entry)
	return m
}

// activeManagement is what the active account permits, so no dialog can
// write to a read-only one even if a binding were left enabled.
func (m Model) activeManagement() Management {
	entry, _ := m.accounts.get(m.accounts.active)
	return entry.permitted()
}

// activeConnection is the account a run goes to, when it is connected.
func (m Model) activeConnection() (accountEntry, bool) {
	entry, ok := m.accounts.get(m.accounts.active)
	return entry, ok && entry.connected()
}

// setActive moves the session onto account, empty for none. It is the one
// place the active account changes, so a feature that shows something per
// account hooks in here, and reloads through the command. The editor, the
// results and the run are left alone.
func (m Model) setActive(account string) (Model, tea.Cmd) {
	changed := account != m.accounts.active
	m.accounts.active = account
	if account != "" {
		m.accounts = m.accounts.used(account)
	}
	entry, _ := m.accounts.get(account)
	m.statusBar = m.statusBar.SetAccount(account).SetScope(entry.scope)
	m = m.closeSuggestions().refreshAccess().setFocus(m.focus)
	if !changed {
		return m, nil
	}
	m.pendingBatch = pendingBatch{} // a batch belongs to the account it was started on
	return m.followActiveAccount(account)
}

func (m Model) setScope(account string, scope []string) Model {
	if entry, ok := m.accounts.get(account); ok {
		entry.scope = scope
		entry.index.SetScope(scope)
		m.accounts.put(entry)
	}
	if account == m.accounts.active {
		m.statusBar = m.statusBar.SetScope(scope)
	}
	return m
}

func (m Model) activeScope() []string {
	entry, _ := m.accounts.get(m.accounts.active)
	return entry.scope
}

// startLaunch moves the session onto the account it was started for, which
// connects in Init. Its tree shows it loading from the first frame; the
// request itself waits for the connection, and attach makes it anew.
func (m Model) startLaunch(launch string) Model {
	if !m.accounts.known(launch) {
		m.accounts = m.accounts.add(m.withSessionAccess(Account{Name: launch}), m.blankEntry)
	}
	m = m.startConnecting(launch)
	entry, _ := m.accounts.get(launch)
	entry.pane, _, _ = entry.pane.Reload()
	m.accounts.put(entry)
	m, _ = m.setActive(launch) // nothing is open yet for the switch to reload
	return m
}

// startConnecting numbers a new attempt to connect name, which supersedes any
// earlier one still in flight.
func (m Model) startConnecting(name string) Model {
	m.lastAttempt++
	entry, _ := m.accounts.get(name)
	entry.state, entry.err, entry.attempt = panes.AccountConnecting, nil, m.lastAttempt
	m.accounts.put(entry)
	return m
}

// whyNoConnection explains a run refused for want of a connected account.
func (m Model) whyNoConnection() error {
	if entry, ok := m.accounts.get(m.accounts.active); ok && entry.state == panes.AccountConnecting {
		return fmt.Errorf("%s: %w", m.accounts.active, errStillConnecting)
	}
	return errNoAccount
}

// openAccounts shows the switcher, with the cursor on the account the session
// is on, and asks for the profiles afresh.
func (m Model) openAccounts() (Model, tea.Cmd) {
	m.refusedDisconnect = ""
	m, cmd := m.syncAccountRows()
	m.accountsPane = m.accountsPane.Open()
	m.overlay = overlayAccounts
	return m, tea.Batch(cmd, m.listAccountsCmd())
}

func (m Model) listAccountsCmd() tea.Cmd {
	list := m.listAccounts
	if list == nil {
		return nil
	}
	return func() tea.Msg {
		accounts, err := list()
		if err != nil {
			return ErrMsg{Op: OpListAccounts, Err: err}
		}
		return AccountsListedMsg{Accounts: accounts}
	}
}

// mergeAccounts lists every account read, and refreshes what the switcher
// shows of those already listed. None is dropped: a profile removed from the
// file stays reachable, and connected, for the rest of the session.
func (m Model) mergeAccounts(accounts []Account) (Model, tea.Cmd) {
	for _, account := range accounts {
		m.accounts = m.accounts.add(m.withSessionAccess(account), m.blankEntry)
	}
	return m.refreshAccess().syncAccountRows()
}

// refreshAccess offers the bindings, and shows the badge, of what the active
// account permits now.
func (m Model) refreshAccess() Model {
	entry, _ := m.accounts.get(m.accounts.active)
	m.statusBar = m.statusBar.SetReadOnly(entry.account.ReadOnly)
	return m.withManagement(entry.permitted())
}

// withSessionAccess applies the session's own read-only switch, which only
// ever tightens what a profile allows.
func (m Model) withSessionAccess(account Account) Account {
	account.ReadOnly = account.ReadOnly || m.readOnly
	return account
}

// syncAccountRows redraws the switcher, showing an account a connect form is
// connecting as connecting too, and the clone form's list of targets.
func (m Model) syncAccountRows() (Model, tea.Cmd) {
	rows := m.accounts.rows()
	for i, row := range rows {
		if m.formAttempts[row.Name] != 0 && row.State != panes.AccountConnected {
			rows[i].State, rows[i].Err = panes.AccountConnecting, nil
		}
		if row.Name == m.refusedDisconnect && m.job.uses(row.Name) {
			rows[i].Notice = m.job.usingText(row.Name)
		}
	}
	var cmd tea.Cmd
	m.accountsPane, cmd = m.accountsPane.SetRows(rows, m.accounts.active)
	if m.clonePrompt.source.Account != "" {
		m.cloneForm = m.cloneForm.SetTargets(m.cloneTargets())
	}
	return m, cmd
}

// handleAccountsKey drives the switcher. While the filter line has the
// keyboard, typed characters narrow the list — which is why q, a and x mean
// nothing there, and only the arrow keys move the cursor.
func (m Model) handleAccountsKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.accountsPane.Filtering() && typesIntoBuffer(msg) {
		return m.accountsFilterUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Accounts):
		return m.closeAccounts(), nil
	case key.Matches(msg, m.keys.Close):
		return m.escapeAccounts(), nil
	case key.Matches(msg, m.keys.Up):
		m.accountsPane = m.accountsPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.accountsPane = m.accountsPane.CursorDown()
	case key.Matches(msg, m.keys.Filter):
		m.accountsPane = m.accountsPane.StartFilter()
	case key.Matches(msg, m.keys.Switch):
		return m.switchToSelected()
	case key.Matches(msg, m.keys.AddAccount):
		return m.openAddAccount(), nil
	case key.Matches(msg, m.keys.Disconnect):
		return m.disconnectSelected()
	case m.accountsPane.Filtering():
		return m.accountsFilterUpdate(msg)
	}
	return m, nil
}

func (m Model) accountsFilterUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.accountsPane, cmd = m.accountsPane.Update(msg)
	return m, cmd
}

func (m Model) escapeAccounts() Model {
	if m.accountsPane.Filtering() {
		m.accountsPane = m.accountsPane.ClearFilter()
		return m
	}
	return m.closeAccounts()
}

// closeAccounts drops the wait: an attempt still in flight carries on, and
// leaves its account connected in the background.
func (m Model) closeAccounts() Model {
	m.accounts.waiting = ""
	m.overlay = overlayNone
	return m
}

// switchToSelected moves the session to the account under the cursor,
// connecting it first when it is not connected yet.
func (m Model) switchToSelected() (Model, tea.Cmd) {
	row, ok := m.accountsPane.Selected()
	if !ok {
		return m, nil
	}
	entry, _ := m.accounts.get(row.Name)
	switch {
	case entry.connected():
		return m.closeAccounts().setActive(row.Name)
	case entry.state == panes.AccountConnecting, m.formAttempts[row.Name] != 0:
		m.accounts.waiting = row.Name
		return m, nil
	}
	m = m.startConnecting(row.Name)
	m.accounts.waiting = row.Name
	m, tick := m.syncAccountRows()
	entry, _ = m.accounts.get(row.Name)
	return m, tea.Batch(tick, m.connectAccount(entry))
}

// disconnectSelected closes the account under the cursor and forgets its
// tree and scope. Nothing is destroyed: the profile and its key stay where
// they are.
func (m Model) disconnectSelected() (Model, tea.Cmd) {
	row, ok := m.accountsPane.Selected()
	if !ok {
		return m, nil
	}
	if m.formAttempts[row.Name] != 0 {
		return m.abandonFormAttempt(row.Name)
	}
	if m.job.uses(row.Name) {
		m.refusedDisconnect = row.Name
		return m.syncAccountRows()
	}
	entry, _ := m.accounts.get(row.Name)
	if entry.state == panes.AccountDisconnected {
		return m, nil
	}
	if m.accounts.waiting == row.Name {
		m.accounts.waiting = ""
	}
	var abandon, follow tea.Cmd
	if m.runAccount == row.Name && entry.connected() {
		m, abandon = m.abandonRun()
	}
	blank := m.blankEntry()
	blank.account, blank.pane = entry.account, entry.pane.Forget()
	m.accounts.put(blank)
	m.accounts = m.accounts.forget(row.Name)
	if m.accounts.active == row.Name {
		m, follow = m.setActive(m.accounts.fallback())
	}
	m, sync := m.syncAccountRows()
	return m, tea.Batch(abandon, follow, sync, m.closeInBackground(entry.connection))
}

// acceptConnection takes the connection an Opener delivered. One nothing is
// waiting for any more — its account was disconnected meanwhile — is closed,
// the way a superseded run's cursor is.
func (m Model) acceptConnection(msg AccountConnectedMsg) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(msg.Account)
	if !ok || !entry.awaits(msg.attempt) {
		return m, m.closeInBackground(msg.Connection)
	}
	m.logger.Info("connected", "account", msg.Account)
	m, load := m.attach(entry, msg.Connection)
	var follow tea.Cmd
	switch {
	case m.takeWait(msg.Account):
		m, follow = m.closeAccounts().setActive(msg.Account)
	case m.accounts.active == "", m.accounts.active == msg.Account:
		m, follow = m.setActive(msg.Account)
	}
	m, sync := m.syncAccountRows()
	return m, tea.Batch(load, follow, sync)
}

// attach records conn as entry's connection, and starts loading its tree. The
// tree keeps its request counters from any earlier connection, so a response
// still on its way from that one is dropped as stale.
func (m Model) attach(entry accountEntry, conn adapter.Connection) (Model, tea.Cmd) {
	entry.state, entry.err = panes.AccountConnected, nil
	entry.connection = conn
	entry.catalog = conn.Catalog()
	if m.manage != nil {
		entry.management = m.manage(conn)
	}
	entry.pendingDatabase = entry.account.Database
	entry.index, entry.samples = complete.NewIndex(), map[string]sampleState{}
	pane, fetch, tick := entry.pane.Forget().Reload()
	entry.pane = pane
	m.accounts.put(entry)
	return m, tea.Batch(tick, m.load(entry, fetch))
}

// failConnection reports an attempt that did not connect under its row, and
// the session stays where it was. The account it started on leaves it
// nowhere: unless the person has moved on to something else, the switcher —
// or the form, for a missing key — opens in its place.
func (m Model) failConnection(msg ErrMsg) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(msg.Account)
	if !ok || !entry.awaits(msg.attempt) {
		return m, nil
	}
	waited := m.takeWait(msg.Account)
	launching := m.accounts.active == msg.Account
	if launching {
		m, _ = m.setActive("") // no account has nothing to reload
	}
	idle := waited || launching && m.overlay == overlayNone
	if idle && errors.Is(msg.Err, ErrCredentialsNeeded) {
		entry.state = panes.AccountDisconnected
		m.accounts.put(entry)
		return m.openCredentialsForm(entry.account), nil
	}
	m.logger.Error("connect failed", "account", msg.Account, "error", msg.Err)
	entry.state, entry.err = panes.AccountFailed, msg.Err
	m.accounts.put(entry)
	if launching && m.overlay == overlayNone {
		return m.openAccounts()
	}
	return m.syncAccountRows()
}

// takeWait reports whether the switcher is on screen waiting for account. The
// wait ends either way: an answer that finds another overlay open leaves the
// account connected, or failed, in the background rather than closing it.
func (m *Model) takeWait(account string) bool {
	if m.accounts.waiting != account {
		return false
	}
	m.accounts.waiting = ""
	return m.overlay == overlayAccounts
}

func (m Model) connectAccount(entry accountEntry) tea.Cmd {
	return openAccount(m.open, entry, ping, m.logger)
}

// launchAccount does not ping, as the switcher does: there is nowhere else to
// stay, so an unreachable account shows in its catalog with the key that
// retries.
func (m Model) launchAccount(entry accountEntry) tea.Cmd {
	return openAccount(m.open, entry, trust, m.logger)
}

// connectionCheck decides whether a freshly opened connection is worth
// switching to.
type connectionCheck func(ctx context.Context, conn adapter.Connection) error

func ping(ctx context.Context, conn adapter.Connection) error {
	return conn.Ping(ctx)
}

func trust(context.Context, adapter.Connection) error {
	return nil
}

func openAccount(open Opener, entry accountEntry, check connectionCheck, logger *log.Logger) tea.Cmd {
	name, attempt := entry.account.Name, entry.attempt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		conn, err := open(ctx, name)
		if err != nil {
			return ErrMsg{Op: OpConnect, Account: name, Err: err, attempt: attempt}
		}
		if err := check(ctx, conn); err != nil {
			closeConnection(logger, conn)
			return ErrMsg{Op: OpConnect, Account: name, Err: err, attempt: attempt}
		}
		return AccountConnectedMsg{Account: name, Connection: conn, attempt: attempt}
	}
}

// closeInBackground releases a connection nothing uses any more without
// holding up Update.
func (m Model) closeInBackground(conn adapter.Connection) tea.Cmd {
	if conn == nil {
		return nil
	}
	logger := m.logger
	return func() tea.Msg {
		closeConnection(logger, conn)
		return nil
	}
}

// closeConnection releases conn. A failure is worth a log line and nothing
// more: whatever used the connection is over.
func closeConnection(logger *log.Logger, conn adapter.Connection) {
	if err := conn.Close(); err != nil {
		logger.Error("close connection", "error", err)
	}
}

// CloseConnections closes every connection the session holds; an attempt
// still connecting holds none yet. A second call closes nothing.
func (m Model) CloseConnections() {
	for _, name := range m.accounts.names() {
		entry, _ := m.accounts.get(name)
		if !entry.connected() {
			continue
		}
		closeConnection(m.logger, entry.connection)
		entry.state, entry.connection, entry.catalog = panes.AccountDisconnected, nil, nil
		m.accounts.put(entry)
	}
}
