package panes

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	catalogTitle = "Catalog"
	indent       = "  "
	spinnerFPS   = time.Second / 8
	loadingLabel = "loading"
)

const (
	// separator joins path elements into a map key. NUL cannot appear in a
	// backend identifier, so no path can forge another's key.
	separator = "\x00"
	// rootKey is the key of the top level of the tree, which has no node.
	rootKey = ""
)

// Fetch is the load an interaction produced. Node is meaningful only when
// Needed is true, and a Node with no Path means the top level of the tree.
type Fetch struct {
	Node   adapter.Node
	Needed bool
}

// Catalog renders the lazy database tree. It holds no adapter: it reports the
// fetches it needs and the root model feeds the results back through
// SetChildren and SetError.
//
// Following the bubbles models it composes, its methods take a value receiver
// but share the underlying maps, so a caller must keep every Catalog it is
// handed. Dropping one still mutates the original — and dropping a Fetch
// leaves its node marked in flight with no request behind it, which no later
// interaction can clear.
type Catalog struct {
	frame    frame
	icons    theme.IconSet
	spinner  spinner.Model
	roots    []adapter.Node
	children map[string][]adapter.Node
	expanded map[string]bool
	loading  map[string]bool
	failures map[string]string
	cursor   []string
	spinning bool
}

func NewCatalog(icons theme.IconSet) Catalog {
	return Catalog{
		frame: frame{title: catalogTitle},
		icons: icons,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
			spinner.WithStyle(theme.SpinnerStyle()),
		),
		children: map[string][]adapter.Node{},
		expanded: map[string]bool{},
		loading:  map[string]bool{},
		failures: map[string]string{},
	}
}

// Update advances the loading animation and stops it once every load has
// settled, so an idle catalog wakes the program no further.
func (c Catalog) Update(msg tea.Msg) (Catalog, tea.Cmd) {
	tick, ok := msg.(spinner.TickMsg)
	if !ok {
		return c, nil
	}
	if len(c.loading) == 0 {
		c.spinning = false
		return c, nil
	}
	var cmd tea.Cmd
	c.spinner, cmd = c.spinner.Update(tick)
	return c, cmd
}

func (c Catalog) SetSize(width, height int) Catalog {
	c.frame = c.frame.size(width, height)
	return c
}

func (c Catalog) Focus() Catalog {
	c.frame = c.frame.focus()
	return c
}

func (c Catalog) Blur() Catalog {
	c.frame = c.frame.blur()
	return c
}

// Toggle opens or closes the node under the cursor, reporting the fetch its
// children still need. The returned command starts the loading animation.
func (c Catalog) Toggle() (Catalog, Fetch, tea.Cmd) {
	node, ok := c.SelectedNode()
	if !ok || !node.HasChildren {
		return c, Fetch{}, nil
	}
	key := pathKey(node.Path)
	if c.expanded[key] {
		delete(c.expanded, key)
		return c, Fetch{}, nil
	}
	c.expanded[key] = true
	return c.fetch(node)
}

// Refresh drops the cached children of the node under the cursor, and of
// everything below it, then asks for them again. With nothing selected it
// reloads the top level, which is the only way back from a failed startup
// fetch. A refresh while that node is already loading is dropped rather than
// raced; the animation shows the request it will answer with.
func (c Catalog) Refresh() (Catalog, Fetch, tea.Cmd) {
	node, ok := c.SelectedNode()
	if !ok {
		return c.Reload()
	}
	if !node.HasChildren {
		return c, Fetch{}, nil
	}
	c = c.invalidate(node)
	c.expanded[pathKey(node.Path)] = true
	return c.fetch(node)
}

// Reload asks for the top level of the tree.
func (c Catalog) Reload() (Catalog, Fetch, tea.Cmd) {
	return c.fetch(adapter.Node{})
}

// SpinnerTick starts the loading animation. The root model runs it from Init,
// which cannot record on the model that the animation began.
func (c Catalog) SpinnerTick() tea.Cmd {
	return c.spinner.Tick
}

// fetch asks for node's children unless they are already cached or a request
// for them is still in flight. Letting a second request start would leave two
// responses racing to be the one the tree keeps.
func (c Catalog) fetch(node adapter.Node) (Catalog, Fetch, tea.Cmd) {
	key := pathKey(node.Path)
	if _, cached := c.children[key]; cached || c.loading[key] {
		return c, Fetch{}, nil
	}
	c.loading[key] = true
	if c.spinning {
		return c, Fetch{Node: node, Needed: true}, nil
	}
	c.spinning = true
	return c, Fetch{Node: node, Needed: true}, c.spinner.Tick
}

// invalidate forgets node's children and everything below them, so a refresh
// cannot leave descendants showing what they held before it.
func (c Catalog) invalidate(node adapter.Node) Catalog {
	prefix := pathKey(node.Path)
	for key := range c.children {
		if under(key, prefix) {
			delete(c.children, key)
		}
	}
	for key := range c.expanded {
		if under(key, prefix) {
			delete(c.expanded, key)
		}
	}
	for key := range c.failures {
		if under(key, prefix) {
			delete(c.failures, key)
		}
	}
	return c
}

// under reports whether key names the node at prefix or one of its
// descendants.
func under(key, prefix string) bool {
	return key == prefix || strings.HasPrefix(key, prefix+separator)
}

// SetChildren records the nodes fetched for parent, clearing any earlier
// failure there. An empty parent sets the top level of the tree.
func (c Catalog) SetChildren(parent []string, nodes []adapter.Node) Catalog {
	key := pathKey(parent)
	delete(c.loading, key)
	delete(c.failures, key)
	if key == rootKey {
		c.roots = nodes
	} else {
		c.children[key] = nodes
	}
	return c.reanchor()
}

// SetError records a failed fetch so it renders under the node it belongs to.
func (c Catalog) SetError(path []string, err error) Catalog {
	key := pathKey(path)
	delete(c.loading, key)
	c.failures[key] = err.Error()
	return c
}

func (c Catalog) SelectedNode() (adapter.Node, bool) {
	rows := c.selectableRows()
	i := c.cursorRow(rows)
	if i < 0 {
		return adapter.Node{}, false
	}
	return rows[i], true
}

func (c Catalog) CursorUp() Catalog {
	return c.moveCursor(-1)
}

func (c Catalog) CursorDown() Catalog {
	return c.moveCursor(1)
}

func (c Catalog) View() string {
	_, height := c.frame.inner()
	lines, cursorLine := c.lines()
	return c.frame.render(strings.Join(window(lines, cursorLine, height), "\n"))
}

func (c Catalog) moveCursor(delta int) Catalog {
	rows := c.selectableRows()
	i := c.cursorRow(rows)
	if i < 0 {
		return c
	}
	c.cursor = rows[min(max(i+delta, 0), len(rows)-1)].Path
	return c
}

// cursorRow locates the selected node, falling back to its nearest visible
// ancestor when it has been collapsed away and to the first row when the
// selection is gone entirely. Tracking the node rather than a row index keeps
// the selection put when a slow load inserts rows above it.
func (c Catalog) cursorRow(rows []adapter.Node) int {
	if len(rows) == 0 {
		return -1
	}
	if len(c.cursor) == 0 {
		return 0
	}
	found, longest := 0, -1
	for i, row := range rows {
		if prefixes(row.Path, c.cursor) && len(row.Path) > longest {
			found, longest = i, len(row.Path)
		}
	}
	return found
}

// reanchor commits whatever the cursor resolved to, so a node that later
// reappears at the abandoned path cannot recapture the selection.
func (c Catalog) reanchor() Catalog {
	rows := c.selectableRows()
	if i := c.cursorRow(rows); i >= 0 {
		c.cursor = rows[i].Path
	}
	return c
}

// visibleRows flattens the expanded tree into the rows to draw, in draw order.
func (c Catalog) visibleRows() []adapter.Node {
	return c.flatten(c.roots)
}

// selectableRows are the rows the cursor may land on. A metadata field draws
// but cannot be selected: there is nothing on one to expand or scope a query
// to.
func (c Catalog) selectableRows() []adapter.Node {
	var rows []adapter.Node
	for _, node := range c.visibleRows() {
		if node.Kind != adapter.NodeField {
			rows = append(rows, node)
		}
	}
	return rows
}

func (c Catalog) flatten(nodes []adapter.Node) []adapter.Node {
	var rows []adapter.Node
	for _, node := range nodes {
		rows = append(rows, node)
		if key := pathKey(node.Path); c.expanded[key] {
			rows = append(rows, c.flatten(c.children[key])...)
		}
	}
	return rows
}

// depth is a node's indent level. Taking it from the path rather than from the
// walk lets one batch of children nest among themselves, which is how a
// container serves its partition key label and that key's paths together.
func depth(node adapter.Node) int {
	return max(len(node.Path)-1, 0)
}

// lines renders every row, interleaving inline failures, and reports which
// line the cursor sits on so the view can scroll to it.
func (c Catalog) lines() ([]string, int) {
	rows := c.visibleRows()
	selected := c.cursorRow(rows)

	var lines []string
	if c.loading[rootKey] {
		lines = append(lines, theme.HintStyle().Render(c.clip(c.spinner.View()+" "+loadingLabel)))
	}
	if message := c.failures[rootKey]; message != "" {
		lines = append(lines, c.errorLines(0, message)...)
	}
	cursorLine := 0
	for i, row := range rows {
		if i == selected {
			cursorLine = len(lines)
		}
		lines = append(lines, c.nodeLine(row, i == selected))
		if message := c.failures[pathKey(row.Path)]; message != "" {
			lines = append(lines, c.errorLines(depth(row)+1, message)...)
		}
	}
	return lines, cursorLine
}

func (c Catalog) nodeLine(node adapter.Node, selected bool) string {
	text := strings.Repeat(indent, depth(node)) + c.chevron(node) + c.icon(node) + node.Name
	if c.loading[pathKey(node.Path)] {
		text += " " + c.spinner.View()
	}
	text = c.clip(text)
	if selected {
		return theme.SelectedStyle().Render(text)
	}
	return theme.TextStyle().Render(text)
}

// errorLines wraps message under its node rather than truncating it: a pane
// this narrow would otherwise cut off the part that explains the failure.
func (c Catalog) errorLines(depth int, message string) []string {
	marker := strings.Repeat(indent, depth) + c.icons.Failure + " "
	hanging := strings.Repeat(" ", lipgloss.Width(marker))
	width, _ := c.frame.inner()
	wrapped := lipgloss.NewStyle().Width(max(width-lipgloss.Width(marker), 1)).Render(message)

	var lines []string
	for i, text := range strings.Split(wrapped, "\n") {
		prefix := hanging
		if i == 0 {
			prefix = marker
		}
		lines = append(lines, theme.ErrorStyle().Render(c.clip(prefix+strings.TrimRight(text, " "))))
	}
	return lines
}

func (c Catalog) chevron(node adapter.Node) string {
	switch {
	case !node.HasChildren:
		return indent
	case c.expanded[pathKey(node.Path)]:
		return c.icons.Expanded + " "
	default:
		return c.icons.Collapsed + " "
	}
}

func (c Catalog) icon(node adapter.Node) string {
	switch node.Kind {
	case adapter.NodeDatabase:
		return c.icons.Database + " "
	case adapter.NodeContainer:
		return c.icons.Container + " "
	default:
		return ""
	}
}

func (c Catalog) clip(text string) string {
	width, _ := c.frame.inner()
	return ansi.Truncate(text, width, "…")
}

// window returns the slice of lines of at most height rows that keeps the
// cursor line on screen.
func window(lines []string, cursorLine, height int) []string {
	if height <= 0 || len(lines) <= height {
		return lines
	}
	start := min(max(cursorLine-height/2, 0), len(lines)-height)
	return lines[start : start+height]
}

// prefixes reports whether path is a leading segment of, or equal to, other.
func prefixes(path, other []string) bool {
	if len(path) > len(other) {
		return false
	}
	for i, segment := range path {
		if segment != other[i] {
			return false
		}
	}
	return true
}

func pathKey(path []string) string {
	return strings.Join(path, separator)
}
