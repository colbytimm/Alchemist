// Package tui contains the terminal user interface. The root model owns
// layout, focus, and message routing; panes live in internal/tui/panes and
// every adapter call is converted into a tea.Cmd here.
package tui

import (
	"io"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Compile-time contract check.
var _ tea.Model = Model{}

// Pane sizing. The catalog keeps a fixed width where the terminal allows it and
// the editor takes a third of what is left over the results.
const (
	statusBarHeight       = 1
	preferredCatalogWidth = 28
	minPaneWidth          = 20
	minPaneHeight         = 3
	editorHeightDivisor   = 3
)

// focus names the pane receiving keys. The zero value is the catalog, which is
// where a session starts.
type focus int

const (
	focusCatalog focus = iota
	focusEditor
	focusResults
	focusCount
)

func (f focus) next() focus { return (f + 1) % focusCount }

func (f focus) prev() focus { return (f + focusCount - 1) % focusCount }

// Options configures a TUI session. Catalog is required; a nil Logger
// discards output.
type Options struct {
	Icons   theme.IconSet
	Catalog adapter.Catalog
	Logger  *log.Logger
	Profile string
}

type Model struct {
	keys    KeyMap
	catalog adapter.Catalog
	logger  *log.Logger

	catalogPane panes.Catalog
	editor      panes.Editor
	results     panes.Results
	statusBar   panes.StatusBar
	help        panes.Help

	focus    focus
	showHelp bool
	width    int
	height   int
}

func New(opts Options) Model {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard)
	}
	keys := DefaultKeyMap()
	m := Model{
		keys:        keys,
		catalog:     opts.Catalog,
		logger:      logger,
		catalogPane: panes.NewCatalog(opts.Icons),
		editor:      panes.NewEditor(),
		results:     panes.NewResults(),
		statusBar:   panes.NewStatusBar(opts.Icons, opts.Profile),
		help:        panes.NewHelp(keys),
	}
	return m.setFocus(focusCatalog)
}

func (m Model) Init() tea.Cmd {
	return m.loadRoot()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case CatalogLoadedMsg:
		m.catalogPane = m.catalogPane.SetChildren(msg.Parent, msg.Nodes)
		return m, nil
	case ScopeChangedMsg:
		m.statusBar = m.statusBar.SetScope(msg.Scope)
		return m, nil
	case ErrMsg:
		return m.handleErr(msg), nil
	}
	var cmd tea.Cmd
	m.catalogPane, cmd = m.catalogPane.Update(msg)
	return m, cmd
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
	if m.showHelp {
		return m.help.View()
	}
	right := lipgloss.JoinVertical(lipgloss.Left, m.editor.View(), m.results.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.catalogPane.View(), right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar.View())
}

func (m Model) handleErr(msg ErrMsg) Model {
	m.logger.Error("operation failed", "op", msg.Op, "error", msg.Err)
	switch msg.Op {
	case OpCatalogRoot, OpCatalogChildren:
		m.catalogPane = m.catalogPane.SetError(msg.Path, msg.Err)
	}
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		return m.handleOverlayKey(msg)
	}
	// Plain characters belong to the editor's buffer, so global letter
	// shortcuts such as q and ? must not fire while it has focus.
	if m.focus == focusEditor && msg.Type == tea.KeyRunes {
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.NextPane):
		return m.setFocus(m.focus.next()), nil
	case key.Matches(msg, m.keys.PrevPane):
		return m.setFocus(m.focus.prev()), nil
	case key.Matches(msg, m.keys.FocusEditor):
		return m.setFocus(focusEditor), nil
	}
	if m.focus != focusCatalog {
		return m, nil
	}
	return m.handleCatalogKey(msg)
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help), key.Matches(msg, m.keys.Close):
		m.showHelp = false
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

// selectNode expands or collapses the node under the cursor and, for a
// container, republishes the scope queries default to.
func (m Model) selectNode() (Model, tea.Cmd) {
	node, ok := m.catalogPane.SelectedNode()
	if !ok {
		return m, nil
	}
	var cmds []tea.Cmd
	if node.Kind == adapter.NodeContainer {
		cmds = append(cmds, scopeChanged(node.Path))
	}
	if node.HasChildren {
		var cmd tea.Cmd
		m, cmd = m.toggle(node)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// toggle expands or collapses node, fetching its children the first time it
// opens and reusing the cache afterwards.
func (m Model) toggle(node adapter.Node) (Model, tea.Cmd) {
	if m.catalogPane.IsExpanded(node) {
		m.catalogPane = m.catalogPane.Collapse(node)
		return m, nil
	}
	m.catalogPane = m.catalogPane.Expand(node)
	if m.catalogPane.IsLoaded(node) {
		return m, nil
	}
	pane, tick := m.catalogPane.MarkLoading(node)
	m.catalogPane = pane
	return m, tea.Batch(tick, m.loadChildren(node))
}

// refreshNode drops the cached children of the node under the cursor and
// fetches them again.
func (m Model) refreshNode() (Model, tea.Cmd) {
	node, ok := m.catalogPane.SelectedNode()
	if !ok || !node.HasChildren {
		return m, nil
	}
	pane := m.catalogPane.Invalidate(node).Expand(node)
	pane, tick := pane.MarkLoading(node)
	m.catalogPane = pane
	return m, tea.Batch(tick, m.loadChildren(node))
}

func (m Model) setFocus(f focus) Model {
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
