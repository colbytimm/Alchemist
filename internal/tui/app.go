// Package tui contains the terminal user interface. The root model owns
// layout, focus, and message routing; panes live in internal/tui/panes and
// every adapter call is converted into a tea.Cmd here.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Compile-time contract check.
var _ tea.Model = Model{}

// Pane sizing.
const (
	statusBarHeight       = 1
	preferredCatalogWidth = 28
	minPaneWidth          = 20
	minPaneHeight         = 3
	editorHeightDivisor   = 3
)

// Refusals the TUI raises before it reaches an adapter. Their text is
// rendered in the results pane, so it reads as a message to the person at the
// keyboard rather than as a wrapped error chain.
var (
	errNoQuery = errors.New("nothing to run: type a query in the editor")
	errNoScope = errors.New("no container in scope: select one in the catalog, or write FROM db.container")
)

// accountQualifiedParts is the length of a source path whose first part may
// name an account: account.database.container.
const accountQualifiedParts = 3

// focus names the pane receiving keys.
type focus int

const (
	focusCatalog focus = iota
	focusEditor
	focusResults
	focusCount
)

func (f focus) next() focus { return (f + 1) % focusCount }

func (f focus) prev() focus { return (f + focusCount - 1) % focusCount }

// overlay names the full-screen view covering the layout, if any.
type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayDetail
	overlayHistory
	overlayExport
	overlayForm
	overlayConfirm
	overlayInfo
	overlayAccounts
	overlayConnect
)

// runState is how far the current query has got.
type runState int

const (
	runIdle     runState = iota // nothing has been run
	runRunning                  // the first page of a run is in flight
	runLoaded                   // a page is on screen
	runFetching                 // a further page of that result set is in flight
	runFailed                   // the run produced no page at all
)

func (s runState) running() bool { return s == runRunning || s == runFetching }

func (s runState) loaded() bool { return s == runLoaded || s == runFetching }

// Options configures a TUI session; Icons are required, the rest may be zero.
type Options struct {
	Icons    theme.IconSet
	Accounts []Account // every profile
	// ListAccounts reads the profiles afresh each time the switcher opens,
	// so one added outside the session is listed too. Nil lists Accounts.
	ListAccounts func() ([]Account, error)
	Launch       string // empty opens the connect form, as a first run does
	Open         Opener
	Connect      Connector
	Manage       Manager
	Form         panes.ConnectForm // first run only: no account exists yet
	Logger       *log.Logger
	History      history.Store
}

// Management is what a session may do with the catalog beyond browsing it:
// change it, and read the metadata behind one node. A nil field is a backend
// that cannot do that, and its bindings are removed.
type Management struct {
	Admin      adapter.CatalogAdmin
	Throughput adapter.ThroughputEditor
	Inspector  adapter.Inspector
}

// Manager reports what a connection allows. cmd/ supplies it: every type
// assertion to a backend's optional interfaces belongs there, never here.
type Manager func(adapter.Connection) Management

type Model struct {
	keys         KeyMap
	icons        theme.IconSet
	accounts     accountSet
	listAccounts func() ([]Account, error)
	open         Opener
	connect      Connector
	manage       Manager
	logger       *log.Logger
	history      history.Store

	connectPane   panes.Connect
	accountsPane  panes.Accounts
	noAccountPane panes.Catalog
	editor        panes.Editor
	results       panes.Results
	detail        panes.Detail
	historyPane   panes.History
	exportPrompt  panes.ExportPrompt
	form          panes.Form
	confirm       panes.Confirm
	statusBar     panes.StatusBar
	help          panes.Help

	// lastAttempt numbers the newest attempt to connect an account, by the
	// Opener or by a connect form. formAttempts are the accounts connect
	// forms are connecting, with the number of each attempt, and
	// shownFormAttempt the one the form on screen made, zero before it is
	// submitted.
	lastAttempt      int
	formAttempts     map[string]int
	shownFormAttempt int

	// runAccount is the account the current run went to.
	runAccount string
	state      runState
	run        runID
	pageCursor adapter.Cursor
	cancel     context.CancelFunc
	stats      adapter.Stats
	// simulated marks a run merged client-side from several containers.
	simulated bool
	// historyEntry is the record of the current run, appended to the log
	// once the run has settled one way or the other.
	historyEntry history.Entry

	// managing is the operation the open dialog will run, target the node it
	// runs against, and dialog which opening of it this is.
	managing string
	target   []string
	dialog   dialogID

	focus         focus
	previousFocus focus
	overlay       overlay
	width         int
	height        int
}

func New(opts Options) Model {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard)
	}
	store := opts.History
	if store == nil {
		store = history.Discard{}
	}
	open := opts.Open
	if open == nil {
		open = func(context.Context, string) (adapter.Connection, error) { return nil, ErrCredentialsNeeded }
	}
	keys := DefaultKeyMap()
	m := Model{
		keys:          keys,
		icons:         opts.Icons,
		open:          open,
		listAccounts:  opts.ListAccounts,
		connect:       opts.Connect,
		manage:        opts.Manage,
		logger:        logger,
		history:       store,
		connectPane:   panes.NewConnect(opts.Icons, opts.Form),
		accountsPane:  panes.NewAccounts(opts.Icons, append([]key.Binding{keys.Filter}, keys.AccountsKeys()...)),
		noAccountPane: panes.NewCatalog(opts.Icons).SetError(nil, errNoAccount, 0),
		editor:        panes.NewEditor(),
		results:       panes.NewResults(),
		detail:        panes.NewDetail(),
		historyPane:   panes.NewHistory(opts.Icons, keys.HistoryKeys()),
		exportPrompt:  panes.NewExportPrompt(append(keys.ExportKeys(), keys.Close)),
		statusBar:     panes.NewStatusBar(opts.Icons, ""),
		help:          panes.NewHelp(keys.HelpSections()),
		formAttempts:  map[string]int{},
	}
	m.accounts = newAccountSet(opts.Accounts, m.blankEntry)
	if opts.Launch == "" {
		m.overlay = overlayConnect
		return m.withManagement(Management{}).setFocus(focusCatalog)
	}
	return m.startLaunch(opts.Launch).setFocus(focusCatalog)
}

// Init connects the account the session starts on. A first run needs nothing
// until its form is submitted.
func (m Model) Init() tea.Cmd {
	entry, ok := m.accounts.get(m.accounts.active)
	if !ok {
		return nil
	}
	return tea.Batch(entry.pane.SpinnerTick(), m.launchAccount(entry))
}

// withManagement rebuilds the bindings from what the active account allows,
// so a session offers nothing its backend cannot carry out.
func (m Model) withManagement(management Management) Model {
	m.keys = DefaultKeyMap().forManagement(management)
	m.help = panes.NewHelp(m.keys.HelpSections()).SetSize(m.width, m.height)
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case CatalogLoadedMsg:
		return m.applyCatalog(msg)
	case CatalogChangedMsg:
		return m.applyChange(msg)
	case ThroughputReadMsg:
		if msg.Account != m.accounts.active {
			return m, nil
		}
		return m.openThroughputForm(msg), nil
	case DetailsLoadedMsg:
		return m.fileDetails(msg), nil
	case ScopeChangedMsg:
		return m.setScope(msg.Account, msg.Scope), nil
	case PageLoadedMsg:
		return m.loadPage(msg)
	case PageAppendedMsg:
		return m.appendPage(msg)
	case QueryFailedMsg:
		return m.failRun(msg)
	case PageFailedMsg:
		return m.failPage(msg)
	case ErrMsg:
		return m.handleErr(msg)
	case AccountConnectedMsg:
		if m.formAttempts[msg.Account] == msg.attempt {
			return m.acceptFormConnection(msg)
		}
		return m.acceptConnection(msg)
	case ConnectFailedMsg:
		return m.failFormConnection(msg)
	case AccountsListedMsg:
		return m.mergeAccounts(msg.Accounts)
	case HistoryLoadedMsg:
		return m.openHistory(msg), nil
	case ExportedMsg:
		return m.finishExport(msg)
	}
	return m.animate(msg)
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		MaxWidth(m.width).
		MaxHeight(m.height).
		Render(m.layout())
}

func (m Model) layout() string {
	switch m.overlay {
	case overlayConnect:
		return m.connectPane.View()
	case overlayAccounts:
		return m.accountsPane.View()
	case overlayHelp:
		return m.help.View()
	case overlayDetail:
		return m.detail.View()
	case overlayHistory:
		return m.historyPane.View()
	case overlayExport:
		return m.exportPrompt.View()
	case overlayForm:
		return m.form.View()
	case overlayConfirm:
		return m.confirm.View()
	case overlayInfo:
		return m.activeInfo().View()
	}
	right := lipgloss.JoinVertical(lipgloss.Left, m.editor.View(), m.results.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.catalogPane().View(), right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar.View())
}

// animate forwards a message no pane owns outright to the ones that run
// timers of their own. Every account's tree gets it, on screen or not: each
// spinner answers only to its own ticks.
func (m Model) animate(msg tea.Msg) (Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 3, 3+2*len(m.accounts.entries))
	m.connectPane, cmds[0] = m.connectPane.Update(msg)
	m.accountsPane, cmds[1] = m.accountsPane.Update(msg)
	m.statusBar, cmds[2] = m.statusBar.Update(msg)
	for name, entry := range m.accounts.entries {
		var paneCmd, infoCmd tea.Cmd
		entry.pane, paneCmd = entry.pane.Update(msg)
		entry.info, infoCmd = entry.info.Update(msg)
		m.accounts.entries[name] = entry
		cmds = append(cmds, paneCmd, infoCmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleErr(msg ErrMsg) (Model, tea.Cmd) {
	switch msg.Op {
	case OpConnect:
		return m.failConnection(msg)
	case OpCatalogRoot, OpCatalogChildren:
		return m.failCatalog(msg), nil
	}
	m.logger.Error("operation failed", "op", msg.Op, "error", msg.Err)
	switch msg.Op {
	case OpListAccounts:
		return m, nil
	case OpHistory:
		return m.failHistory(msg), nil
	case OpExport:
		m.exportPrompt = m.exportPrompt.Fail(msg.Err)
	case OpCreateDatabase, OpCreateContainer, OpSetThroughput, OpDeleteDatabase, OpDeleteContainer:
		return m.failManagement(msg), nil
	case OpReadThroughput:
		return m.failThroughputRead(msg), nil
	case OpInspect:
		return m.failInspect(msg), nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}
	if m.focus == focusEditor && typesIntoBuffer(msg) {
		return m.editorUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Help):
		m.overlay = overlayHelp
		return m, nil
	case key.Matches(msg, m.keys.NextPane):
		return m.setFocus(m.focus.next()), nil
	case key.Matches(msg, m.keys.PrevPane):
		return m.setFocus(m.focus.prev()), nil
	case key.Matches(msg, m.keys.FocusEditor):
		return m.setFocus(focusEditor), nil
	case key.Matches(msg, m.keys.Run):
		return m.startRun()
	case key.Matches(msg, m.keys.History):
		return m.openHistoryOrRefuse()
	case key.Matches(msg, m.keys.Accounts):
		return m.openAccounts()
	}
	return m.handleFocusedKey(msg)
}

// typesIntoBuffer reports whether msg is a character the editor must receive
// as text, so global shortcuts bound to plain keys — q, r, ? — cannot steal
// it once the pane accepts typing.
func typesIntoBuffer(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes
}

func (m Model) handleFocusedKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.focus {
	case focusCatalog:
		return m.handleCatalogKey(msg)
	case focusEditor:
		return m.handleEditorKey(msg)
	case focusResults:
		return m.handleResultsKey(msg)
	}
	return m, nil
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.overlay {
	case overlayConnect:
		return m.handleConnectKey(msg)
	case overlayAccounts:
		return m.handleAccountsKey(msg)
	case overlayHistory:
		return m.handleHistoryKey(msg)
	case overlayExport:
		return m.handleExportKey(msg)
	case overlayForm:
		return m.handleFormKey(msg)
	case overlayConfirm:
		return m.handleConfirmKey(msg)
	case overlayInfo:
		return m.handleInfoKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = overlayNone
	case m.overlay == overlayHelp && key.Matches(msg, m.keys.Help):
		m.overlay = overlayNone
	case m.overlay == overlayDetail && key.Matches(msg, m.keys.Up):
		m.detail = m.detail.ScrollUp()
	case m.overlay == overlayDetail && key.Matches(msg, m.keys.Down):
		m.detail = m.detail.ScrollDown()
	}
	return m, nil
}

// quit ends the run in progress and closes every connection the session
// holds.
func (m Model) quit() (Model, tea.Cmd) {
	m = m.endRun()
	m.CloseConnections()
	return m, tea.Quit
}

func (m Model) handleCatalogKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		return m.setCatalogPane(m.catalogPane().CursorUp()), nil
	case key.Matches(msg, m.keys.Down):
		return m.setCatalogPane(m.catalogPane().CursorDown()), nil
	case key.Matches(msg, m.keys.Select):
		return m.selectNode(m.accounts.active)
	case key.Matches(msg, m.keys.Refresh):
		return m.refreshNode(m.accounts.active)
	case key.Matches(msg, m.keys.NewDatabase):
		return m.openDatabaseForm(), nil
	case key.Matches(msg, m.keys.NewContainer):
		return m.openContainerForm(), nil
	case key.Matches(msg, m.keys.Delete):
		return m.openDelete(), nil
	case key.Matches(msg, m.keys.Throughput):
		return m, m.openThroughput()
	case key.Matches(msg, m.keys.Info):
		return m.openInfo()
	}
	return m, nil
}

func (m Model) handleEditorKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Close) {
		return m.setFocus(m.previousFocus), nil
	}
	return m.editorUpdate(msg)
}

func (m Model) handleResultsKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.results = m.results.CursorUp()
	case key.Matches(msg, m.keys.Down):
		return m.moveDown()
	case key.Matches(msg, m.keys.ScrollLeft):
		m.results = m.results.ScrollLeft()
	case key.Matches(msg, m.keys.ScrollRight):
		m.results = m.results.ScrollRight()
	case key.Matches(msg, m.keys.FetchMore):
		return m.fetchMore()
	case key.Matches(msg, m.keys.Detail):
		return m.openDetail(), nil
	case key.Matches(msg, m.keys.Export):
		return m.openExport(), nil
	}
	return m, nil
}

func (m Model) editorUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m Model) moveDown() (Model, tea.Cmd) {
	if m.results.AtLastRow() {
		return m.fetchMore()
	}
	m.results = m.results.CursorDown()
	return m, nil
}

func (m Model) openDetail() Model {
	document, ok := m.results.SelectedDocument()
	if !ok {
		return m
	}
	m.detail = m.detail.SetDocument(document)
	m.overlay = overlayDetail
	return m
}

// startRun replaces whatever is on screen with a fresh run of the editor
// buffer. A refusal to run is reported the same way a service error is.
func (m Model) startRun() (Model, tea.Cmd) {
	m = m.endRun()
	m.run++
	m.stats = adapter.Stats{}
	m.simulated = false
	m.runAccount = m.accounts.active
	m.results = m.results.Clear().SetSource(m.runAccount)

	account, connected := m.activeConnection()
	if !connected {
		return m.showFailure(m.whyNoConnection(), runFailed)
	}
	plan, err := m.resolvePlan()
	if err != nil {
		return m.refuseRun(err)
	}
	m.simulated = plan.Simulated()
	m.historyEntry = m.newHistoryEntry(plan.Scope())
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	m.cancel = cancel
	m.state = runRunning
	engine := query.Engine{Connection: account.connection, MaxJoinRows: account.account.MaxJoinRows}
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.runPlan(ctx, engine, plan))
}

// refuseRun reports a run that never reached the adapter. It is recorded like
// any other failure, so a query worth fixing can be recalled; an empty buffer
// leaves nothing to recall.
func (m Model) refuseRun(err error) (Model, tea.Cmd) {
	model, cmd := m.showFailure(err, runFailed)
	if errors.Is(err, errNoQuery) {
		return model, cmd
	}
	model.historyEntry = model.newHistoryEntry(model.activeScope())
	return model, tea.Batch(cmd, model.recordFailure(err))
}

// fetchMore asks the open cursor for another page, handing it to the command
// so nothing else can close it while that read is in flight.
func (m Model) fetchMore() (Model, tea.Cmd) {
	if m.state != runLoaded || !m.hasMore() {
		return m, nil
	}
	cursor := m.pageCursor
	m.pageCursor = nil
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	m.cancel = cancel
	m.state = runFetching
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.fetchPage(ctx, cursor))
}

// resolvePlan decides what to run and where: a db.container source written
// in the query wins over the container selected in the catalog.
func (m Model) resolvePlan() (query.Plan, error) {
	text := m.editor.Value()
	if strings.TrimSpace(text) == "" {
		return query.Plan{}, errNoQuery
	}
	if err := m.checkNoAccountNamed(text); err != nil {
		return query.Plan{}, err
	}
	plan, err := query.BuildPlan(text)
	if err != nil {
		return query.Plan{}, err
	}
	plan = plan.WithDefaultScope(m.activeScope())
	if len(plan.Scope()) == 0 {
		return query.Plan{}, errNoScope
	}
	return plan, nil
}

func (m Model) loadPage(msg PageLoadedMsg) (Model, tea.Cmd) {
	if msg.run != m.run {
		closeCursor(m.logger, msg.cursor)
		return m, nil
	}
	m.stats = msg.Page.Stats
	m.results = m.results.Load(msg.Page)
	model, cmd := m.acceptPage(msg.cursor)
	return model, tea.Batch(cmd, model.recordSuccess(msg.Page.Stats))
}

func (m Model) appendPage(msg PageAppendedMsg) (Model, tea.Cmd) {
	if msg.run != m.run {
		closeCursor(m.logger, msg.cursor)
		return m, nil
	}
	m.stats = totalStats(m.stats, msg.Page.Stats)
	m.results = m.results.Append(msg.Page)
	return m.acceptPage(msg.cursor)
}

// acceptPage takes the cursor back from the command that read the page.
func (m Model) acceptPage(cursor adapter.Cursor) (Model, tea.Cmd) {
	m = m.cancelInFlight()
	m.pageCursor = cursor
	m.state = runLoaded
	return m.syncStatusBar()
}

func (m Model) failRun(msg QueryFailedMsg) (Model, tea.Cmd) {
	if msg.run != m.run {
		return m, nil
	}
	model, cmd := m.showFailure(msg.Err, runFailed)
	return model, tea.Batch(cmd, model.recordFailure(msg.Err))
}

// failPage keeps the result set loaded: a page that never arrived says
// nothing about the ones that did. Only the cursor goes with it.
func (m Model) failPage(msg PageFailedMsg) (Model, tea.Cmd) {
	if msg.run != m.run {
		return m, nil
	}
	return m.showFailure(msg.Err, runLoaded)
}

// showFailure puts err in the results pane and leaves the run in next.
func (m Model) showFailure(err error, next runState) (Model, tea.Cmd) {
	m.logger.Error("run failed", "error", err)
	m = m.cancelInFlight()
	m.pageCursor = nil
	m.results = m.results.Fail(err)
	m.state = next
	return m.syncStatusBar()
}

// checkNoAccountNamed refuses a source whose first part names an account of
// this session: a query runs on the account the session is on, and nowhere
// else. FROM prod.sales.orders is valid Cosmos SQL, so the refusal answers
// what would otherwise come back as a baffling service error.
func (m Model) checkNoAccountNamed(text string) error {
	for _, path := range query.SourcePaths(text) {
		if len(path) >= accountQualifiedParts && m.accounts.known(path[0]) {
			return fmt.Errorf("FROM %s: %w", strings.Join(path, "."), errAccountInQuery)
		}
	}
	return nil
}

// abandonRun lets go of a run whose account is going away. A page still in
// flight is discarded, and its cursor closed, on arrival; the rows already on
// screen stay there.
func (m Model) abandonRun() (Model, tea.Cmd) {
	m = m.endRun()
	m.run++
	switch m.state {
	case runRunning:
		return m.showFailure(errRunAbandoned, runFailed)
	case runFetching:
		m.state = runLoaded
	}
	return m.syncStatusBar()
}

// endRun stops what the current run left running. Its cursor is closed only
// when the model still holds it: a fetch in flight owns the cursor instead
// and hands it back through a message the run number then discards.
func (m Model) endRun() Model {
	m = m.cancelInFlight()
	closeCursor(m.logger, m.pageCursor)
	m.pageCursor = nil
	return m
}

func (m Model) cancelInFlight() Model {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	return m
}

// hasMore reports whether another page is available. A fetch in flight has
// taken the cursor, and only ever starts when there is a page to fetch.
func (m Model) hasMore() bool {
	if m.state == runFetching {
		return true
	}
	return m.pageCursor != nil && m.pageCursor.HasMore()
}

// syncStatusBar also drops the notice: every caller has just changed the run
// the notice was about.
func (m Model) syncStatusBar() (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.statusBar, _ = m.statusBar.SetNotice("")
	m.statusBar, cmd = m.statusBar.SetProgress(panes.Progress{
		Stats:     m.stats,
		More:      m.hasMore(),
		Running:   m.state.running(),
		Loaded:    m.state.loaded(),
		Simulated: m.simulated,
	})
	return m, cmd
}

// totalStats accumulates one page's statistics into the result set's, so the
// status bar reports the whole of what has been fetched.
func totalStats(total, page adapter.Stats) adapter.Stats {
	return adapter.Stats{
		RequestCharge: total.RequestCharge + page.RequestCharge,
		Elapsed:       total.Elapsed + page.Elapsed,
		RowCount:      total.RowCount + page.RowCount,
		LeafCharges:   totalLeafCharges(total.LeafCharges, page.LeafCharges),
	}
}

func totalLeafCharges(total, page map[string]float64) map[string]float64 {
	if len(page) == 0 {
		return total
	}
	sum := maps.Clone(page)
	for leaf, charge := range total {
		sum[leaf] += charge
	}
	return sum
}

// applyCatalog files a response in the tree of the account it was made for,
// on screen or not. One for an account no longer connected is dropped.
func (m Model) applyCatalog(msg CatalogLoadedMsg) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(msg.Account)
	if !ok || !entry.connected() {
		return m, nil
	}
	entry.pane = entry.pane.SetChildren(msg.Parent, msg.Nodes, msg.Token)
	m.accounts.put(entry)
	if len(msg.Parent) == 0 && entry.pendingDatabase != "" {
		return m.openDefaultDatabase(entry, msg.Nodes)
	}
	return m.prefetch(msg.Account)
}

func (m Model) failCatalog(msg ErrMsg) Model {
	entry, ok := m.accounts.get(msg.Account)
	if !ok || !entry.connected() {
		return m
	}
	m.logger.Error("catalog load failed", "account", msg.Account, "op", msg.Op, "error", msg.Err)
	entry.pane = entry.pane.SetError(msg.Path, msg.Err, msg.Token)
	m.accounts.put(entry)
	return m
}

// selectNode also republishes the scope when the selected node is a
// container, which toggling alone cannot know to do.
func (m Model) selectNode(account string) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(account)
	if !ok || !entry.connected() {
		return m, nil
	}
	node, ok := entry.pane.SelectedNode()
	if !ok {
		return m, nil
	}
	pane, fetch, tick := entry.pane.Toggle()
	entry.pane = pane
	m.accounts.put(entry)

	cmds := []tea.Cmd{tick}
	if node.Kind == adapter.NodeContainer {
		cmds = append(cmds, scopeChanged(account, node.Path))
	}
	if fetch.Needed {
		cmds = append(cmds, m.load(entry, fetch))
	}
	m, chevrons := m.prefetch(account) // the rows this opened onto are new to the screen
	return m, tea.Batch(append(cmds, chevrons)...)
}

// openDefaultDatabase expands the database the profile names, so its
// containers are on screen from the first frame. It runs once: a reload later
// leaves the tree as the user arranged it.
func (m Model) openDefaultDatabase(entry accountEntry, roots []adapter.Node) (Model, tea.Cmd) {
	name := entry.pendingDatabase
	entry.pendingDatabase = ""
	m.accounts.put(entry)
	row := slices.IndexFunc(roots, func(n adapter.Node) bool { return n.Name == name })
	if row < 0 {
		m.logger.Warn("default database is not in the catalog", "account", entry.account.Name, "database", name)
		return m.prefetch(entry.account.Name)
	}
	for range row {
		entry.pane = entry.pane.CursorDown()
	}
	m.accounts.put(entry)
	return m.selectNode(entry.account.Name)
}

// prefetch loads the children of the rows a response just put on screen, so
// their chevrons stop guessing. It runs behind the tree rather than ahead of
// it: the catalog paints as soon as the root arrives, and each answer settles
// one more row.
func (m Model) prefetch(account string) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(account)
	if !ok || !entry.connected() {
		return m, nil
	}
	pane, fetches, tick := entry.pane.Prefetch()
	entry.pane = pane
	m.accounts.put(entry)

	cmds := make([]tea.Cmd, 0, len(fetches)+1)
	if tick != nil {
		cmds = append(cmds, tick)
	}
	for _, fetch := range fetches {
		cmds = append(cmds, m.load(entry, fetch))
	}
	return m, tea.Batch(cmds...)
}

// refreshNode reloads the node under the cursor of account, and no other
// account's.
func (m Model) refreshNode(account string) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(account)
	if !ok || !entry.connected() {
		return m, nil
	}
	pane, fetch, tick := entry.pane.Refresh()
	entry.pane = pane
	m.accounts.put(entry)
	if !fetch.Needed {
		return m, nil
	}
	return m, tea.Batch(tick, m.load(entry, fetch))
}

// load issues fetch against entry's catalog: the children of its node, or the
// top level when that node has no path.
func (m Model) load(entry accountEntry, fetch panes.Fetch) tea.Cmd {
	if len(fetch.Node.Path) == 0 {
		return loadRoot(entry.account.Name, entry.catalog, fetch.Token)
	}
	return loadChildren(entry.account.Name, entry.catalog, fetch.Node, fetch.Token)
}

func (m Model) setFocus(f focus) Model {
	if f != m.focus {
		m.previousFocus = m.focus
	}
	m.focus = f
	m = m.setCatalogPane(m.catalogPane().Blur())
	m.editor = m.editor.Blur()
	m.results = m.results.Blur()
	switch f {
	case focusCatalog:
		m = m.setCatalogPane(m.catalogPane().Focus())
	case focusEditor:
		m.editor = m.editor.Focus()
	case focusResults:
		m.results = m.results.Focus()
	}
	return m
}

// resize sizes every account's tree, not only the one on screen, so a switch
// needs no layout pass.
func (m Model) resize(width, height int) Model {
	m.width, m.height = width, height
	catalogWidth, bodyHeight := m.catalogSize()
	editorHeight := max(bodyHeight/editorHeightDivisor, minPaneHeight)

	m.connectPane = m.connectPane.SetSize(width, height)
	m.accountsPane = m.accountsPane.SetSize(width, height)
	m.noAccountPane = m.noAccountPane.SetSize(catalogWidth, bodyHeight)
	for _, name := range m.accounts.names() {
		entry, _ := m.accounts.get(name)
		entry.pane = entry.pane.SetSize(catalogWidth, bodyHeight)
		entry.info = entry.info.SetSize(width, height)
		m.accounts.put(entry)
	}
	m.editor = m.editor.SetSize(width-catalogWidth, editorHeight)
	m.results = m.results.SetSize(width-catalogWidth, bodyHeight-editorHeight)
	m.statusBar = m.statusBar.SetWidth(width)
	m.help = m.help.SetSize(width, height)
	m.detail = m.detail.SetSize(width, height)
	m.historyPane = m.historyPane.SetSize(width, height)
	m.exportPrompt = m.exportPrompt.SetSize(width, height)
	m.form = m.form.SetSize(width, height)
	m.confirm = m.confirm.SetSize(width, height)
	return m
}

func (m Model) catalogSize() (width, height int) {
	return fitCatalogWidth(m.width), max(m.height-statusBarHeight, minPaneHeight)
}

// fitCatalogWidth keeps the catalog at its preferred width while the rest of
// the row still clears the minimum, and splits the terminal evenly when it
// cannot.
func fitCatalogWidth(total int) int {
	if total < preferredCatalogWidth+minPaneWidth {
		return max(total/2, 1)
	}
	return preferredCatalogWidth
}
