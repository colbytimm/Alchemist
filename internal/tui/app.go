// Package tui contains the terminal user interface. The root model owns
// layout, focus, and message routing; panes live in internal/tui/panes and
// every adapter call is converted into a tea.Cmd here.
package tui

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
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

// Options configures a TUI session. Icons and Connection are required; a nil
// Logger discards output.
type Options struct {
	Icons      theme.IconSet
	Connection adapter.Connection
	Logger     *log.Logger
	Profile    string
}

type Model struct {
	keys       KeyMap
	connection adapter.Connection
	catalog    adapter.Catalog
	logger     *log.Logger

	catalogPane panes.Catalog
	editor      panes.Editor
	results     panes.Results
	detail      panes.Detail
	statusBar   panes.StatusBar
	help        panes.Help

	scope      []string
	state      runState
	run        runID
	pageCursor adapter.Cursor
	cancel     context.CancelFunc
	stats      adapter.Stats

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
	keys := DefaultKeyMap()
	m := Model{
		keys:        keys,
		connection:  opts.Connection,
		catalog:     opts.Connection.Catalog(),
		logger:      logger,
		catalogPane: panes.NewCatalog(opts.Icons),
		editor:      panes.NewEditor(),
		results:     panes.NewResults(),
		detail:      panes.NewDetail(),
		statusBar:   panes.NewStatusBar(opts.Icons, opts.Profile),
		help:        panes.NewHelp(keys),
	}
	// The tick is discarded because Init, which bubbletea always calls next,
	// starts the animation; a constructor cannot hand back a command.
	m.catalogPane, _, _ = m.catalogPane.Reload()
	return m.setFocus(focusCatalog)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.catalogPane.SpinnerTick(), m.loadRoot())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case CatalogLoadedMsg:
		m.catalogPane = m.catalogPane.SetChildren(msg.Parent, msg.Nodes)
		return m.prefetch()
	case ScopeChangedMsg:
		m.scope = msg.Scope
		m.statusBar = m.statusBar.SetScope(msg.Scope)
		return m, nil
	case PageLoadedMsg:
		return m.loadPage(msg)
	case PageAppendedMsg:
		return m.appendPage(msg)
	case QueryFailedMsg:
		return m.failRun(msg)
	case PageFailedMsg:
		return m.failPage(msg)
	case ErrMsg:
		return m.handleErr(msg), nil
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
	case overlayHelp:
		return m.help.View()
	case overlayDetail:
		return m.detail.View()
	}
	right := lipgloss.JoinVertical(lipgloss.Left, m.editor.View(), m.results.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.catalogPane.View(), right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar.View())
}

// animate forwards a message no pane owns outright to the two that run
// timers of their own.
func (m Model) animate(msg tea.Msg) (Model, tea.Cmd) {
	var catalogCmd, statusCmd tea.Cmd
	m.catalogPane, catalogCmd = m.catalogPane.Update(msg)
	m.statusBar, statusCmd = m.statusBar.Update(msg)
	return m, tea.Batch(catalogCmd, statusCmd)
}

func (m Model) handleErr(msg ErrMsg) Model {
	m.logger.Error("operation failed", "op", msg.Op, "error", msg.Err)
	switch msg.Op {
	case OpCatalogRoot, OpCatalogChildren:
		m.catalogPane = m.catalogPane.SetError(msg.Path, msg.Err)
	}
	return m
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
		return m.endRun(), tea.Quit
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
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.endRun(), tea.Quit
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

func (m Model) handleCatalogKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.catalogPane = m.catalogPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.catalogPane = m.catalogPane.CursorDown()
	case key.Matches(msg, m.keys.Select):
		return m.selectNode()
	case key.Matches(msg, m.keys.Refresh):
		return m.refreshNode()
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
	m.results = m.results.Clear()

	q, err := m.resolveQuery()
	if err != nil {
		return m.showFailure(err, runFailed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	m.cancel = cancel
	m.state = runRunning
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.runQuery(ctx, q))
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

// resolveQuery decides what to run and where: a db.container source written
// in the query wins over the container selected in the catalog.
func (m Model) resolveQuery() (adapter.Query, error) {
	text := m.editor.Value()
	if strings.TrimSpace(text) == "" {
		return adapter.Query{}, errNoQuery
	}
	parsed, err := query.ParseScope(text)
	if err != nil {
		return adapter.Query{}, err
	}
	scope := parsed.Scope
	if !parsed.Explicit {
		scope = m.scope
	}
	if len(scope) == 0 {
		return adapter.Query{}, errNoScope
	}
	return adapter.Query{Text: parsed.Rewritten, Scope: scope}, nil
}

func (m Model) loadPage(msg PageLoadedMsg) (Model, tea.Cmd) {
	if msg.run != m.run {
		closeCursor(m.logger, msg.cursor)
		return m, nil
	}
	m.stats = msg.Page.Stats
	m.results = m.results.Load(msg.Page)
	return m.acceptPage(msg.cursor)
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
	return m.showFailure(msg.Err, runFailed)
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

func (m Model) syncStatusBar() (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.statusBar, cmd = m.statusBar.SetProgress(panes.Progress{
		Stats:   m.stats,
		More:    m.hasMore(),
		Running: m.state.running(),
		Loaded:  m.state.loaded(),
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
	}
}

// selectNode also republishes the scope when the selected node is a
// container, which toggling alone cannot know to do.
func (m Model) selectNode() (Model, tea.Cmd) {
	node, ok := m.catalogPane.SelectedNode()
	if !ok {
		return m, nil
	}
	pane, fetch, tick := m.catalogPane.Toggle()
	m.catalogPane = pane

	cmds := []tea.Cmd{tick}
	if node.Kind == adapter.NodeContainer {
		cmds = append(cmds, scopeChanged(node.Path))
	}
	if fetch.Needed {
		cmds = append(cmds, m.load(fetch.Node))
	}
	m, chevrons := m.prefetch() // the rows this opened onto are new to the screen
	return m, tea.Batch(append(cmds, chevrons)...)
}

// prefetch loads the children of the rows a response just put on screen, so
// their chevrons stop guessing. It runs behind the tree rather than ahead of
// it: the catalog paints as soon as the root arrives, and each answer settles
// one more row.
func (m Model) prefetch() (Model, tea.Cmd) {
	pane, nodes, tick := m.catalogPane.Prefetch()
	m.catalogPane = pane

	cmds := make([]tea.Cmd, 0, len(nodes)+1)
	if tick != nil {
		cmds = append(cmds, tick)
	}
	for _, node := range nodes {
		cmds = append(cmds, m.loadChildren(node))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) refreshNode() (Model, tea.Cmd) {
	pane, fetch, tick := m.catalogPane.Refresh()
	m.catalogPane = pane
	if !fetch.Needed {
		return m, nil
	}
	return m, tea.Batch(tick, m.load(fetch.Node))
}

// load fetches node's children, or the top level when node has no path.
func (m Model) load(node adapter.Node) tea.Cmd {
	if len(node.Path) == 0 {
		return m.loadRoot()
	}
	return m.loadChildren(node)
}

func (m Model) setFocus(f focus) Model {
	if f != m.focus {
		m.previousFocus = m.focus
	}
	m.focus = f
	m.catalogPane = m.catalogPane.Blur()
	m.editor = m.editor.Blur()
	m.results = m.results.Blur()
	switch f {
	case focusCatalog:
		m.catalogPane = m.catalogPane.Focus()
	case focusEditor:
		m.editor = m.editor.Focus()
	case focusResults:
		m.results = m.results.Focus()
	}
	return m
}

func (m Model) resize(width, height int) Model {
	m.width, m.height = width, height
	catalogWidth := fitCatalogWidth(width)
	bodyHeight := max(height-statusBarHeight, minPaneHeight)
	editorHeight := max(bodyHeight/editorHeightDivisor, minPaneHeight)

	m.catalogPane = m.catalogPane.SetSize(catalogWidth, bodyHeight)
	m.editor = m.editor.SetSize(width-catalogWidth, editorHeight)
	m.results = m.results.SetSize(width-catalogWidth, bodyHeight-editorHeight)
	m.statusBar = m.statusBar.SetWidth(width)
	m.help = m.help.SetSize(width, height)
	m.detail = m.detail.SetSize(width, height)
	return m
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
