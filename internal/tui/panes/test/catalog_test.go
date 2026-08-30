package panes_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	paneWidth  = 28
	paneHeight = 12
)

var database = adapter.Node{
	Kind:        adapter.NodeDatabase,
	Name:        "sales",
	Path:        []string{"sales"},
	HasChildren: true,
}

var container = adapter.Node{
	Kind:        adapter.NodeContainer,
	Name:        "orders",
	Path:        []string{"sales", "orders"},
	HasChildren: true,
}

func newCatalog() panes.Catalog {
	return panes.NewCatalog(theme.Icons()).
		SetSize(paneWidth, paneHeight).
		SetChildren(nil, []adapter.Node{database})
}

func TestCatalogRendersRootNodes(t *testing.T) {
	view := newCatalog().View()

	assert.Contains(t, view, "sales")
	assert.Contains(t, view, theme.Icons().Collapsed, "an unexpanded node shows a closed chevron")
}

func TestCatalogHidesChildrenUntilExpanded(t *testing.T) {
	c := newCatalog().SetChildren(database.Path, []adapter.Node{container})
	assert.NotContains(t, c.View(), "orders")

	assert.Contains(t, c.Expand(database).View(), "orders")
}

func TestCatalogCollapseHidesChildrenAgain(t *testing.T) {
	c := newCatalog().
		SetChildren(database.Path, []adapter.Node{container}).
		Expand(database)

	assert.NotContains(t, c.Collapse(database).View(), "orders")
}

func TestCatalogTracksWhatIsLoaded(t *testing.T) {
	c := newCatalog()
	require.False(t, c.IsLoaded(database))

	assert.True(t, c.SetChildren(database.Path, nil).IsLoaded(database),
		"a node with no children is still loaded")
}

func TestCatalogInvalidateForgetsChildren(t *testing.T) {
	c := newCatalog().SetChildren(database.Path, []adapter.Node{container})

	assert.False(t, c.Invalidate(database).IsLoaded(database))
}

func TestCatalogCursorWalksTheExpandedTree(t *testing.T) {
	c := newCatalog().
		SetChildren(database.Path, []adapter.Node{container}).
		Expand(database)

	node, ok := c.SelectedNode()
	require.True(t, ok)
	require.Equal(t, database.Name, node.Name)

	node, ok = c.CursorDown().SelectedNode()
	require.True(t, ok)
	assert.Equal(t, container.Name, node.Name)
}

func TestCatalogCursorSurvivesACollapse(t *testing.T) {
	c := newCatalog().
		SetChildren(database.Path, []adapter.Node{container}).
		Expand(database).
		CursorDown()

	node, ok := c.Collapse(database).SelectedNode()

	require.True(t, ok)
	assert.Equal(t, database.Name, node.Name, "the cursor falls back onto the parent")
}

func TestCatalogHasNoSelectionWhenEmpty(t *testing.T) {
	c := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight)

	_, ok := c.SelectedNode()

	assert.False(t, ok)
}

func TestCatalogRendersFailuresUnderTheirNode(t *testing.T) {
	c := newCatalog().SetError(database.Path, errors.New("permission denied"))
	lines := strings.Split(c.View(), "\n")

	node := indexOfLineContaining(t, lines, database.Name)
	failure := indexOfLineContaining(t, lines, "permission denied")
	assert.Equal(t, node+1, failure, "the failure belongs directly under its node")
}

func TestCatalogWrapsLongFailures(t *testing.T) {
	message := "the catalog request was rejected because the account is unreachable"
	c := newCatalog().SetError(database.Path, errors.New(message))

	view := c.View()

	for _, word := range strings.Fields(message) {
		assert.Contains(t, view, word, "wrapping must not drop words")
	}
}

func TestCatalogClearsAFailureOnReload(t *testing.T) {
	c := newCatalog().
		SetError(database.Path, errors.New("permission denied")).
		SetChildren(database.Path, []adapter.Node{container})

	assert.NotContains(t, c.View(), "permission denied")
}

func TestCatalogFillsItsFrameExactly(t *testing.T) {
	view := newCatalog().View()

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
	var many []adapter.Node
	for i := 1; i <= 3*paneHeight; i++ {
		name := fmt.Sprintf("node-%02d", i)
		many = append(many, adapter.Node{Kind: adapter.NodeDatabase, Name: name, Path: []string{name}})
	}
	c := panes.NewCatalog(theme.Icons()).SetSize(paneWidth, paneHeight).SetChildren(nil, many)
	for range many {
		c = c.CursorDown()
	}

	view := c.View()

	assert.Contains(t, view, "node-36", "the last node must scroll into view")
	assert.NotContains(t, view, "node-01", "the first node must scroll out of view")
	assert.Equal(t, paneHeight, lipgloss.Height(view))
}

func TestCatalogRendersNothingWhenTooSmall(t *testing.T) {
	assert.Empty(t, newCatalog().SetSize(4, 2).View())
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
