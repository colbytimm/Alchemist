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
)

// rootKey is the map key of the top level of the tree, which has no node.
const rootKey = ""

// Fetch is the children request an interaction produced. Node is meaningful
// only when Needed is true.
type Fetch struct {
	Node   adapter.Node
	Needed bool
}

// Catalog renders the lazy database tree. It holds no adapter: it reports the
// fetches it needs and the root model feeds the results back through
// SetChildren and SetError.
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

// Refresh drops the cached children of the node under the cursor and asks for
// them again.
func (c Catalog) Refresh() (Catalog, Fetch, tea.Cmd) {
	node, ok := c.SelectedNode()
	if !ok || !node.HasChildren {
		return c, Fetch{}, nil
	}
	key := pathKey(node.Path)
	delete(c.children, key)
	delete(c.failures, key)
	c.expanded[key] = true
	return c.fetch(node)
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

// SetChildren records the nodes fetched for parent, clearing any earlier
// failure there. An empty parent sets the top level of the tree.
func (c Catalog) SetChildren(parent []string, nodes []adapter.Node) Catalog {
	key := pathKey(parent)
	delete(c.loading, key)
	delete(c.failures, key)
	if key == rootKey {
		c.roots = nodes
		return c
	}
	c.children[key] = nodes
	return c
}

// SetError records a failed fetch so it renders under the node it belongs to.
func (c Catalog) SetError(path []string, err error) Catalog {
	key := pathKey(path)
	delete(c.loading, key)
	c.failures[key] = err.Error()
	return c
}

func (c Catalog) SelectedNode() (adapter.Node, bool) {
	rows := c.visible()
	i := c.cursorRow(rows)
	if i < 0 {
		return adapter.Node{}, false
	}
	return rows[i].node, true
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
	rows := c.visible()
	i := c.cursorRow(rows)
	if i < 0 {
		return c
	}
	c.cursor = rows[min(max(i+delta, 0), len(rows)-1)].node.Path
	return c
}

// cursorRow locates the selected node, falling back to its nearest visible
// ancestor when it has been collapsed away and to the first row when the
// selection is gone entirely. Tracking the node rather than a row index keeps
// the selection put when a slow load inserts rows above it.
func (c Catalog) cursorRow(rows []treeRow) int {
	found, depth := -1, -1
	for i, row := range rows {
		if prefixes(row.node.Path, c.cursor) && len(row.node.Path) > depth {
			found, depth = i, len(row.node.Path)
		}
	}
	if found < 0 && len(rows) > 0 {
		return 0
	}
	return found
}

type treeRow struct {
	node  adapter.Node
	depth int
}

// visible flattens the expanded tree into the selectable rows, in draw order.
func (c Catalog) visible() []treeRow {
	return c.flatten(c.roots, 0)
}

func (c Catalog) flatten(nodes []adapter.Node, depth int) []treeRow {
	var rows []treeRow
	for _, node := range nodes {
		rows = append(rows, treeRow{node: node, depth: depth})
		if key := pathKey(node.Path); c.expanded[key] {
			rows = append(rows, c.flatten(c.children[key], depth+1)...)
		}
	}
	return rows
}

// lines renders every row, interleaving inline failures, and reports which
// line the cursor sits on so the view can scroll to it.
func (c Catalog) lines() ([]string, int) {
	rows := c.visible()
	selected := c.cursorRow(rows)

	var lines []string
	if message := c.failures[rootKey]; message != "" {
		lines = append(lines, c.errorLines(0, message)...)
	}
	cursorLine := 0
	for i, row := range rows {
		if i == selected {
			cursorLine = len(lines)
		}
		lines = append(lines, c.nodeLine(row, i == selected))
		if message := c.failures[pathKey(row.node.Path)]; message != "" {
			lines = append(lines, c.errorLines(row.depth+1, message)...)
		}
	}
	return lines, cursorLine
}

func (c Catalog) nodeLine(row treeRow, selected bool) string {
	text := strings.Repeat(indent, row.depth) + c.chevron(row.node) + c.icon(row.node) + row.node.Name
	if c.loading[pathKey(row.node.Path)] {
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

// pathKey identifies a node by its catalog path. NUL cannot appear in a
// backend identifier, so no path can forge another's key.
func pathKey(path []string) string {
	return strings.Join(path, "\x00")
}
