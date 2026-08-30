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

// Catalog renders the lazy database tree. It holds no adapter: the root model
// fetches children and feeds them back through SetChildren and SetError.
type Catalog struct {
	icons    theme.IconSet
	spinner  spinner.Model
	roots    []adapter.Node
	children map[string][]adapter.Node
	expanded map[string]bool
	loading  map[string]bool
	failures map[string]string
	cursor   int
	width    int
	height   int
	focused  bool
	spinning bool
}

func NewCatalog(icons theme.IconSet) Catalog {
	return Catalog{
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
	c.width, c.height = width, height
	return c
}

func (c Catalog) Focus() Catalog {
	c.focused = true
	return c
}

func (c Catalog) Blur() Catalog {
	c.focused = false
	return c
}

// MarkLoading flags node's children as in flight. The returned command starts
// the animation, and is nil when it is already running.
func (c Catalog) MarkLoading(node adapter.Node) (Catalog, tea.Cmd) {
	c.loading[pathKey(node.Path)] = true
	if c.spinning {
		return c, nil
	}
	c.spinning = true
	return c, c.spinner.Tick
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
	return c.clampCursor()
}

// SetError records a failed load so it renders under the node it belongs to.
func (c Catalog) SetError(path []string, err error) Catalog {
	key := pathKey(path)
	delete(c.loading, key)
	c.failures[key] = err.Error()
	return c
}

func (c Catalog) Expand(node adapter.Node) Catalog {
	c.expanded[pathKey(node.Path)] = true
	return c
}

func (c Catalog) Collapse(node adapter.Node) Catalog {
	delete(c.expanded, pathKey(node.Path))
	return c.clampCursor()
}

// Invalidate drops node's cached children so the next expand fetches them
// again.
func (c Catalog) Invalidate(node adapter.Node) Catalog {
	key := pathKey(node.Path)
	delete(c.children, key)
	delete(c.failures, key)
	return c
}

func (c Catalog) IsExpanded(node adapter.Node) bool {
	return c.expanded[pathKey(node.Path)]
}

// IsLoaded reports whether node's children are cached, including when the
// fetch legitimately returned none.
func (c Catalog) IsLoaded(node adapter.Node) bool {
	_, ok := c.children[pathKey(node.Path)]
	return ok
}

func (c Catalog) SelectedNode() (adapter.Node, bool) {
	rows := c.visible()
	if c.cursor < 0 || c.cursor >= len(rows) {
		return adapter.Node{}, false
	}
	return rows[c.cursor].node, true
}

func (c Catalog) CursorUp() Catalog {
	c.cursor = max(c.cursor-1, 0)
	return c
}

func (c Catalog) CursorDown() Catalog {
	c.cursor = min(c.cursor+1, len(c.visible())-1)
	return c.clampCursor()
}

func (c Catalog) View() string {
	lines, cursorLine := c.lines()
	body := strings.Join(window(lines, cursorLine, c.height-2), "\n")
	return frame{title: catalogTitle, width: c.width, height: c.height, focused: c.focused}.render(body)
}

func (c Catalog) clampCursor() Catalog {
	c.cursor = min(c.cursor, len(c.visible())-1)
	c.cursor = max(c.cursor, 0)
	return c
}

// treeRow is one visible node together with its depth in the tree.
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
	var lines []string
	if message := c.failures[rootKey]; message != "" {
		lines = append(lines, c.errorLines(0, message)...)
	}
	cursorLine := 0
	for i, row := range c.visible() {
		if i == c.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, c.nodeLine(row, i == c.cursor))
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
	wrapped := lipgloss.NewStyle().
		Width(max(c.width-2-lipgloss.Width(marker), 1)).
		Render(message)

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
	case c.IsExpanded(node):
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
	return ansi.Truncate(text, max(c.width-2, 0), "…")
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

// pathKey identifies a node by its catalog path. Cosmos database and container
// names cannot contain "/", so joining on it cannot collide.
func pathKey(path []string) string {
	return strings.Join(path, "/")
}
