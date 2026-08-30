package panes_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

var database = adapter.Node{
	Kind:        adapter.NodeDatabase,
	Name:        "sales",
	Path:        []string{"sales"},
	HasChildren: true,
}

var sibling = adapter.Node{
	Kind:        adapter.NodeDatabase,
	Name:        "telemetry",
	Path:        []string{"telemetry"},
	HasChildren: true,
}

var container = adapter.Node{
	Kind:        adapter.NodeContainer,
	Name:        "orders",
	Path:        []string{"sales", "orders"},
	HasChildren: true,
}

// The metadata leaves a container serves, in one batch: a label and the one
// partition key path beneath it.
var (
	partitionKeyLabel = adapter.Node{
		Kind: adapter.NodeField,
		Name: "partitionKey",
		Path: []string{"sales", "orders", "partitionKey"},
	}
	partitionKeyPath = adapter.Node{
		Kind: adapter.NodeField,
		Name: "/customerId",
		Path: []string{"sales", "orders", "partitionKey", "/customerId"},
	}
)

// newTree is a sized catalog holding two collapsed databases.
func newTree() panes.Catalog {
	return panes.NewCatalog(theme.Icons()).
		SetSize(paneWidth, paneHeight).
		SetChildren(nil, []adapter.Node{database, sibling})
}

// expand opens the node under the cursor and delivers the children it asks
// for, the way the root model does.
func expand(t *testing.T, c panes.Catalog, children ...adapter.Node) panes.Catalog {
	t.Helper()
	c, fetch, _ := c.Toggle()
	require.True(t, fetch.Needed, "expanding an unloaded node should ask for its children")
	return c.SetChildren(fetch.Node.Path, children)
}

func TestCatalogRendersRootNodes(t *testing.T) {
	view := newTree().View()

	assert.Contains(t, view, database.Name)
	assert.Contains(t, view, theme.Icons().Collapsed, "an unexpanded node shows a closed chevron")
}

func TestCatalogHidesChildrenUntilExpanded(t *testing.T) {
	c := newTree()
	require.NotContains(t, c.View(), container.Name)

	assert.Contains(t, expand(t, c, container).View(), container.Name)
}

func TestCatalogNestsOneBatchOfChildrenByTheirPaths(t *testing.T) {
	c := expand(t, expand(t, newTree(), container).CursorDown(), partitionKeyLabel, partitionKeyPath)
	lines := strings.Split(c.View(), "\n")

	label := columnOf(t, lines, partitionKeyLabel.Name)
	path := columnOf(t, lines, partitionKeyPath.Name)

	assert.Greater(t, path, label,
		"a deeper path indents further even though it arrived alongside its parent")
}

func TestCatalogCollapseHidesChildrenAgain(t *testing.T) {
	c := expand(t, newTree(), container)

	c, fetch, _ := c.Toggle()

	assert.False(t, fetch.Needed, "collapsing asks for nothing")
	assert.NotContains(t, c.View(), container.Name)
}

func TestCatalogReusesCachedChildren(t *testing.T) {
	c := expand(t, newTree(), container)
	c, _, _ = c.Toggle() // collapse

	c, fetch, _ := c.Toggle()

	assert.False(t, fetch.Needed, "re-expanding a loaded node must not fetch again")
	assert.Contains(t, c.View(), container.Name)
}

func TestCatalogTreatsNoChildrenAsLoaded(t *testing.T) {
	c := expand(t, newTree())
	c, _, _ = c.Toggle() // collapse

	_, fetch, _ := c.Toggle()

	assert.False(t, fetch.Needed, "a node that legitimately has no children is loaded")
}

func TestCatalogDoesNotFetchWhileAFetchIsInFlight(t *testing.T) {
	c, first, _ := newTree().Toggle()
	require.True(t, first.Needed)

	// Collapse and re-expand, then refresh, before the first response lands.
	c, _, _ = c.Toggle()
	c, second, _ := c.Toggle()
	_, third, _ := c.Refresh()

	assert.False(t, second.Needed, "re-expanding must not race a second request")
	assert.False(t, third.Needed, "refreshing must not race a second request")
}

func TestCatalogRefreshFetchesAgain(t *testing.T) {
	c := expand(t, newTree(), container)

	_, fetch, _ := c.Refresh()

	require.True(t, fetch.Needed)
	assert.Equal(t, database.Path, fetch.Node.Path)
}

func TestCatalogRefreshForgetsTheWholeSubtree(t *testing.T) {
	c := expand(t, expand(t, newTree(), container).CursorDown(), partitionKeyLabel)
	require.Contains(t, c.View(), "partitionKey")

	c, fetch, _ := c.CursorUp().Refresh()
	require.True(t, fetch.Needed)
	c = c.SetChildren(database.Path, []adapter.Node{container})

	assert.NotContains(t, c.View(), "partitionKey",
		"a refreshed node must not keep serving what its descendants held before")
}

func TestCatalogRefreshWithNothingSelectedReloadsTheRoot(t *testing.T) {
	empty := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight)

	_, fetch, _ := empty.Refresh()

	require.True(t, fetch.Needed, "an empty tree has no node to refresh, so ask for the top level")
	assert.Empty(t, fetch.Node.Path)
}

func TestCatalogShowsProgressWhileTheRootLoads(t *testing.T) {
	c, fetch, _ := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight).Reload()

	require.True(t, fetch.Needed)
	assert.Contains(t, c.View(), "loading")
}

func TestCatalogLeafNodesAskForNothing(t *testing.T) {
	leaf := adapter.Node{Kind: adapter.NodeDatabase, Name: "empty", Path: []string{"empty"}}
	c := panes.NewCatalog(theme.Icons()).
		SetSize(paneWidth, paneHeight).
		SetChildren(nil, []adapter.Node{leaf})

	_, fetch, tick := c.Toggle()

	assert.False(t, fetch.Needed)
	assert.Nil(t, tick)
}

func TestCatalogCursorWalksTheExpandedTree(t *testing.T) {
	c := expand(t, newTree(), container)

	node, ok := c.SelectedNode()
	require.True(t, ok)
	require.Equal(t, database.Name, node.Name)

	node, ok = c.CursorDown().SelectedNode()
	require.True(t, ok)
	assert.Equal(t, container.Name, node.Name)
}

func TestCatalogCursorSkipsMetadataFields(t *testing.T) {
	c := expand(t, expand(t, newTree(), container).CursorDown(), partitionKeyLabel, partitionKeyPath)
	require.Equal(t, container.Name, selected(t, c).Name)

	assert.Equal(t, sibling.Name, selected(t, c.CursorDown()).Name,
		"the cursor steps over a container's metadata to the next selectable node")
}

func TestCatalogCursorFallsBackWhenItsNodeDisappears(t *testing.T) {
	c := expand(t, newTree(), container).CursorDown()
	require.Equal(t, container.Name, selected(t, c).Name)

	// A refresh returns a tree the selected container is no longer part of.
	c = c.SetChildren(database.Path, nil)

	assert.Equal(t, database.Name, selected(t, c).Name,
		"the cursor falls back to the nearest surviving ancestor")
}

func TestCatalogCursorStaysWhereItFellBackTo(t *testing.T) {
	c := expand(t, newTree(), container).CursorDown()
	c = c.SetChildren(database.Path, nil)
	require.Equal(t, database.Name, selected(t, c).Name)

	c = c.SetChildren(database.Path, []adapter.Node{container})

	assert.Equal(t, database.Name, selected(t, c).Name,
		"a node reappearing at the abandoned path must not recapture the cursor")
}

func TestCatalogCursorStaysOnItsNodeWhenRowsAppearAbove(t *testing.T) {
	// Expand the first database, move onto the second, then let the children
	// of the first arrive late.
	c, fetch, _ := newTree().Toggle()
	require.True(t, fetch.Needed)
	c = c.CursorDown()
	require.Equal(t, sibling.Name, selected(t, c).Name)

	c = c.SetChildren(fetch.Node.Path, []adapter.Node{container})

	assert.Equal(t, sibling.Name, selected(t, c).Name,
		"a late response must not slide the cursor onto another node")
}

func TestCatalogHasNoSelectionWhenEmpty(t *testing.T) {
	c := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight)

	_, ok := c.SelectedNode()

	assert.False(t, ok)
}

func TestCatalogSpinnerRunsOnlyWhileLoading(t *testing.T) {
	c, fetch, tick := newTree().Toggle()
	require.True(t, fetch.Needed)
	require.NotNil(t, tick, "the first load starts the animation")

	c, again := c.Update(spinner.TickMsg{})
	assert.NotNil(t, again, "the animation keeps ticking while the load is in flight")

	c = c.SetChildren(fetch.Node.Path, []adapter.Node{container})
	_, stopped := c.Update(spinner.TickMsg{})
	assert.Nil(t, stopped, "an idle catalog must stop waking the program")
}

func TestCatalogSpinnerRestartsAfterGoingIdle(t *testing.T) {
	c, fetch, _ := newTree().Toggle()
	c = c.SetChildren(fetch.Node.Path, []adapter.Node{container})
	c, _ = c.Update(spinner.TickMsg{}) // settles, stopping the chain

	_, next, tick := c.CursorDown().Toggle()

	require.True(t, next.Needed)
	assert.NotNil(t, tick, "a later load must start the animation again")
}

func TestCatalogSpinnerDoesNotStartASecondChain(t *testing.T) {
	c, _, first := newTree().Toggle()
	require.NotNil(t, first)

	_, _, second := c.CursorDown().Toggle()

	assert.Nil(t, second, "a concurrent load rides the running animation")
}

func TestCatalogShowsASpinnerOnTheLoadingNode(t *testing.T) {
	c, _, _ := newTree().Toggle()

	assert.Contains(t, c.View(), theme.Icons().SpinnerFrames[0])
}

func TestCatalogRendersFailuresUnderTheirNode(t *testing.T) {
	c := newTree().SetError(database.Path, errors.New("permission denied"))
	lines := strings.Split(c.View(), "\n")

	node := indexOfLineContaining(t, lines, database.Name)
	failure := indexOfLineContaining(t, lines, "permission denied")

	assert.Equal(t, node+1, failure, "the failure belongs directly under its node")
}

func TestCatalogWrapsLongFailures(t *testing.T) {
	message := "the catalog request was rejected because the account is unreachable"
	view := newTree().SetError(database.Path, errors.New(message)).View()

	for _, word := range strings.Fields(message) {
		assert.Contains(t, view, word, "wrapping must not drop words")
	}
}

func TestCatalogClearsAFailureOnReload(t *testing.T) {
	c := newTree().
		SetError(database.Path, errors.New("permission denied")).
		SetChildren(database.Path, []adapter.Node{container})

	assert.NotContains(t, c.View(), "permission denied")
}

func TestCatalogClearsAFailureOnRefresh(t *testing.T) {
	c := newTree().SetError(database.Path, errors.New("permission denied"))

	c, fetch, _ := c.Refresh()

	require.True(t, fetch.Needed)
	assert.NotContains(t, c.View(), "permission denied")
}

func TestCatalogFillsItsFrameExactly(t *testing.T) {
	view := newTree().View()

	assert.Equal(t, paneWidth, lipgloss.Width(view))
	assert.Equal(t, paneHeight, lipgloss.Height(view))
}

func TestCatalogTruncatesNamesTooWideForThePane(t *testing.T) {
	long := adapter.Node{Kind: adapter.NodeContainer, Name: strings.Repeat("x", 100), Path: []string{"long"}}
	c := panes.NewCatalog(theme.Icons()).
		SetSize(paneWidth, paneHeight).
		SetChildren(nil, []adapter.Node{long})

	view := c.View()

	assert.Equal(t, paneWidth, lipgloss.Width(view))
	assert.Contains(t, view, "…")
}

func TestCatalogScrollsToKeepTheCursorVisible(t *testing.T) {
	many := make([]adapter.Node, 0, 3*paneHeight)
	for i := range cap(many) {
		name := fmt.Sprintf("node-%02d", i)
		many = append(many, adapter.Node{Kind: adapter.NodeDatabase, Name: name, Path: []string{name}})
	}
	c := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight).SetChildren(nil, many)
	for range many {
		c = c.CursorDown()
	}

	view := c.View()

	first, last := many[0].Name, many[len(many)-1].Name
	assert.Contains(t, view, last, "the last node must scroll into view")
	assert.NotContains(t, view, first, "the first node must scroll out of view")
	assert.Equal(t, paneHeight, lipgloss.Height(view))
}

func TestCatalogRendersNothingWhenTooSmall(t *testing.T) {
	assert.Empty(t, newTree().SetSize(4, 2).View())
}

func selected(t *testing.T, c panes.Catalog) adapter.Node {
	t.Helper()
	node, ok := c.SelectedNode()
	require.True(t, ok, "expected a selected node")
	return node
}

// columnOf is the cell want starts at on the first line holding it, which is
// how far that row is indented.
func columnOf(t *testing.T, lines []string, want string) int {
	t.Helper()
	return strings.Index(ansi.Strip(lines[indexOfLineContaining(t, lines, want)]), want)
}

func indexOfLineContaining(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	require.Failf(t, "line not found", "no line contains %q", want)
	return -1
}
