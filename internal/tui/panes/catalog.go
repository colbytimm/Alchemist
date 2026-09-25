package panes

import (
	"errors"
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
	retryHint    = "r to retry"
)

const (
	// separator joins path elements into a map key. NUL cannot appear in a
	// backend identifier, so no path can forge another's key.
	separator = "\x00"
	// rootKey is the key of the top level of the tree, which has no node.
	rootKey = ""
)

// Token orders the requests made for one node. A response carrying anything
// but that node's latest token is stale and dropped, which is what lets a
// reload issued after a mutation overtake a read that started before it.
type Token int

// Fetch is the load an interaction produced. Node and Token are meaningful
// only when Needed is true, and a Node with no Path means the top level of
// the tree.
type Fetch struct {
	Node   adapter.Node
	Token  Token
	Needed bool
}

// Catalog renders the lazy database tree. It holds no adapter: it reports the
// fetches it needs and the root model feeds the results back through
// SetChildren and SetError.
//
// Following the bubbles models it composes, its methods take a value receiver
// but share the underlying maps, so a caller must keep every Catalog it is
// handed. Dropping one still mutates the original — and dropping a Fetch
// leaves its node marked in flight with no request behind it, which only a
// refresh can clear.
type Catalog struct {
	frame    frame
	icons    theme.IconSet
	spinner  spinner.Model
	roots    []adapter.Node
	children map[string][]adapter.Node
	expanded map[string]bool
	loading  map[string]bool
	failures map[string]error
	tokens   map[string]Token
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
		failures: map[string]error{},
		tokens:   map[string]Token{},
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

// Refresh reloads the node under the cursor. With nothing selected it
// reloads the top level, which is the only way back from a failed startup
// fetch.
func (c Catalog) Refresh() (Catalog, Fetch, tea.Cmd) {
	node, ok := c.SelectedNode()
	if !ok {
		return c.Reload()
	}
	if !node.HasChildren {
		return c, Fetch{}, nil
	}
	return c.RefreshPath(node.Path)
}

// RefreshPath drops what the node at path holds, and everything below it,
// then asks for it again whatever is already in flight: a reload after a
// mutation has to overtake a read that started before it, or the change never
// reaches the screen. An unknown path asks for nothing.
func (c Catalog) RefreshPath(path []string) (Catalog, Fetch, tea.Cmd) {
	node, ok := c.nodeAt(path)
	if !ok {
		return c, Fetch{}, nil
	}
	if len(path) > 0 {
		c = c.invalidate(node)
		c.expanded[pathKey(path)] = true
	}
	return c.reload(node)
}

// Reload asks for the top level of the tree.
func (c Catalog) Reload() (Catalog, Fetch, tea.Cmd) {
	return c.RefreshPath(nil)
}

// LoadPath asks for the children of the node at path unless the tree holds
// them, has asked already, or has a failure on record for them, so a caller
// outside the tree gets them into the tree's own cache with no second request
// and no retry loop. An unknown path asks for nothing.
func (c Catalog) LoadPath(path []string) (Catalog, Fetch, tea.Cmd) {
	node, ok := c.nodeAt(path)
	if !ok || len(path) == 0 || c.failures[pathKey(path)] != nil {
		return c, Fetch{}, nil
	}
	return c.fetch(node)
}

// Loading reports whether a request for the children of path is in flight.
func (c Catalog) Loading(path []string) bool {
	return c.loading[pathKey(path)]
}

// Expects reports whether token is the request for parent the pane is
// waiting on, which is what SetChildren would accept.
func (c Catalog) Expects(parent []string, token Token) bool {
	return token == c.tokens[pathKey(parent)]
}

// Select puts the cursor on path, which need not be on screen yet: a node a
// mutation created appears only once the reload lands, and until then the
// cursor rests on its nearest visible ancestor.
func (c Catalog) Select(path []string) Catalog {
	c.cursor = path
	return c
}

// nodeAt finds the node at path among the ones already fetched. The empty
// path is the top level, which has no node of its own.
func (c Catalog) nodeAt(path []string) (adapter.Node, bool) {
	if len(path) == 0 {
		return adapter.Node{}, true
	}
	siblings := c.roots
	if len(path) > 1 {
		siblings = c.children[pathKey(path[:len(path)-1])]
	}
	for _, node := range siblings {
		if node.Name == path[len(path)-1] {
			return node, true
		}
	}
	return adapter.Node{}, false
}

// SpinnerTick starts the loading animation. The root model runs it from Init,
// which cannot record on the model that the animation began.
func (c Catalog) SpinnerTick() tea.Cmd {
	return c.spinner.Tick
}

// Prefetch reports the loads that settle the chevron of every row on screen.
// An adapter that cannot answer HasChildren without listing them — Cosmos
// cannot, for a database — has to claim it, and only the answer distinguishes
// a node worth opening from one with nothing inside.
func (c Catalog) Prefetch() (Catalog, []Fetch, tea.Cmd) {
	var fetches []Fetch
	var tick tea.Cmd
	for _, node := range c.visibleRows() {
		// A node that already failed is left alone. Retrying it on every
		// unrelated response would reopen a wound the user has to close with
		// a refresh anyway.
		if !node.HasChildren || c.failures[pathKey(node.Path)] != nil {
			continue
		}
		var (
			fetch Fetch
			start tea.Cmd
		)
		c, fetch, start = c.fetch(node)
		if fetch.Needed {
			fetches = append(fetches, fetch)
		}
		if start != nil {
			tick = start
		}
	}
	return c, fetches, tick
}

// fetch asks for node's children unless they are already cached or a request
// for them is still in flight. Dropping the second request is what keeps a
// prefetch cheap; only a caller with a reason to supersede reloads outright.
func (c Catalog) fetch(node adapter.Node) (Catalog, Fetch, tea.Cmd) {
	key := pathKey(node.Path)
	if _, cached := c.children[key]; cached || c.loading[key] {
		return c, Fetch{}, nil
	}
	return c.reload(node)
}

// reload issues a request for node's children whatever else is in flight, and
// hands back the token that makes every earlier response for it stale.
func (c Catalog) reload(node adapter.Node) (Catalog, Fetch, tea.Cmd) {
	key := pathKey(node.Path)
	c.tokens[key]++
	c.loading[key] = true
	fetch := Fetch{Node: node, Token: c.tokens[key], Needed: true}
	if c.spinning {
		return c, fetch, nil
	}
	c.spinning = true
	return c, fetch, c.spinner.Tick
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

// SetChildren records the nodes fetched for parent under token, clearing any
// earlier failure there. An empty parent sets the top level of the tree, and
// a superseded token is ignored outright: the request it answers is no longer
// the one the pane is waiting for.
func (c Catalog) SetChildren(parent []string, nodes []adapter.Node, token Token) Catalog {
	key := pathKey(parent)
	if token != c.tokens[key] {
		return c
	}
	delete(c.loading, key)
	delete(c.failures, key)
	if key == rootKey {
		c.roots = nodes
	} else {
		c.children[key] = nodes
	}
	return c.reanchor()
}

// SetError records a failed fetch so it renders under the node it belongs to,
// ignoring a superseded token the way SetChildren does.
func (c Catalog) SetError(path []string, err error, token Token) Catalog {
	key := pathKey(path)
	if token != c.tokens[key] {
		return c
	}
	delete(c.loading, key)
	c.failures[key] = err
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
	if err := c.failures[rootKey]; err != nil {
		lines = append(lines, c.errorLines(0, err)...)
	}
	cursorLine := 0
	for i, row := range rows {
		if i == selected {
			cursorLine = len(lines)
		}
		lines = append(lines, c.nodeLine(row, i == selected))
		if err := c.failures[pathKey(row.Path)]; err != nil {
			lines = append(lines, c.errorLines(depth(row)+1, err)...)
		}
	}
	return lines, cursorLine
}

func (c Catalog) nodeLine(node adapter.Node, selected bool) string {
	text := strings.Repeat(indent, depth(node)) + c.chevron(node) + c.icon(node) + node.Name
	// Only a load the user asked for gets a spinner; a prefetch settling a
	// chevron would otherwise light up every row on screen at once.
	if key := pathKey(node.Path); c.expanded[key] && c.loading[key] {
		text += " " + c.spinner.View()
	}
	text = c.clip(text)
	if selected {
		return theme.SelectedStyle().Render(text)
	}
	return theme.TextStyle().Render(text)
}

// errorLines renders a failure under its node. An account that could not
// be reached — the emulator not started, the network down — is laid out as
// what happened, why, and the key that tries again; a refusal gets no such
// key, since the same request would only earn it again, and is wrapped
// rather than truncated so the part that explains it is not cut off.
func (c Catalog) errorLines(depth int, err error) []string {
	marker := strings.Repeat(indent, depth) + c.icons.Failure + " "
	width, _ := c.frame.inner()
	textWidth := width - lipgloss.Width(marker)

	var unreachable *adapter.UnreachableError
	if !errors.As(err, &unreachable) {
		return c.hang(marker, styleAll(theme.ErrorStyle(), wrapText(err.Error(), textWidth)))
	}
	lines := []string{theme.ErrorStyle().Bold(true).Render(adapter.UnreachableTitle)}
	lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(unreachable.Reason, textWidth))...)
	lines = append(lines, theme.HintStyle().Render(retryHint))
	return c.hang(marker, lines)
}

// hang puts marker before the first line and indents the rest under it.
func (c Catalog) hang(marker string, lines []string) []string {
	hanging := strings.Repeat(" ", lipgloss.Width(marker))
	hung := make([]string, 0, len(lines))
	for i, line := range lines {
		prefix := hanging
		if i == 0 {
			prefix = theme.ErrorStyle().Render(marker)
		}
		hung = append(hung, c.clip(prefix+line))
	}
	return hung
}

func (c Catalog) chevron(node adapter.Node) string {
	switch {
	case !node.HasChildren || c.childless(node):
		return indent
	case c.expanded[pathKey(node.Path)]:
		return c.icons.Expanded + " "
	default:
		return c.icons.Collapsed + " "
	}
}

// childless reports whether node's children came back empty. An adapter that
// cannot answer HasChildren without fetching them — Cosmos cannot, for a
// database — claims it optimistically, so the chevron offering an expansion
// has to be withdrawn once the fetch settles it.
func (c Catalog) childless(node adapter.Node) bool {
	children, loaded := c.children[pathKey(node.Path)]
	return loaded && len(children) == 0
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

// window returns the at most height lines that keep the cursor line on
// screen.
func window(lines []string, cursorLine, height int) []string {
	start, end := windowBounds(len(lines), cursorLine, height)
	return lines[start:end]
}

// windowBounds is window over a count rather than the lines themselves, so a
// caller with rows still to render can skip the ones that would not show.
func windowBounds(count, cursorLine, height int) (start, end int) {
	if height <= 0 {
		return 0, 0
	}
	if count <= height {
		return 0, count
	}
	start = min(max(cursorLine-height/2, 0), count-height)
	return start, start + height
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
