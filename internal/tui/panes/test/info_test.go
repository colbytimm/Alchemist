package panes_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	infoWidth  = 60
	infoHeight = 20
)

var ordersNode = adapter.Node{
	Kind: adapter.NodeContainer,
	Name: "orders",
	Path: orders,
	Meta: map[string]string{adapter.MetaPartitionKey: "/tenantId,/customerId"},
}

var infoHints = []key.Binding{
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
}

func ordersDetails() adapter.Details {
	return adapter.Details{Sections: []adapter.Section{
		{Title: "Identity", Properties: []adapter.Property{
			{Name: "Database", Value: "sales"},
			{Name: "Container", Value: "orders"},
			{Name: "Resource ID", Value: "hY1TAKjvBQA="},
		}},
		{Title: "Throughput", Note: "Inherited from database sales; this container has no throughput of its own."},
		{Title: "Storage", Properties: []adapter.Property{
			{Name: "Documents", Value: "1284"},
			{Name: "Documents size", Value: "4.2 MB"},
		}},
	}}
}

// tallDetails is more lines than the frame holds.
func tallDetails() adapter.Details {
	var properties []adapter.Property
	for i := range 3 * infoHeight {
		properties = append(properties, adapter.Property{Name: fmt.Sprintf("Path %d", i), Value: fmt.Sprintf("/p%d", i)})
	}
	return adapter.Details{Sections: []adapter.Section{{Title: "Indexing", Properties: properties}}}
}

func newInfo() panes.Info {
	return panes.NewInfo(theme.Icons(), nil).SetSize(infoWidth, infoHeight).Show(ordersNode)
}

func loadedInfo(details adapter.Details) panes.Info {
	info, _ := newInfo().StartLoading()
	return info.SetDetails(ordersNode.Path, details)
}

func lineOf(t *testing.T, view, text string) int {
	t.Helper()
	return indexOfLineContaining(t, strings.Split(view, "\n"), text)
}

func TestInfoNamesTheNodeInItsTitle(t *testing.T) {
	assert.Contains(t, plain(newInfo().View()), "Info ▪ sales.orders")
}

func TestInfoDrawsWhatTheTreeKnowsBeforeAnythingArrives(t *testing.T) {
	info, tick := newInfo().StartLoading()
	require.NotNil(t, tick, "a request in flight drives the animation")

	view := plain(info.View())
	assert.Contains(t, view, "Identity")
	assert.Contains(t, view, "sales")
	assert.Contains(t, view, "orders")
	assert.Contains(t, view, "Partition key")
	assert.Contains(t, view, "/tenantId, /customerId")
	assert.Contains(t, view, "inspecting")
}

func TestInfoRendersEverySectionInTheOrderGiven(t *testing.T) {
	view := plain(loadedInfo(ordersDetails()).View())

	assert.Less(t, lineOf(t, view, "Identity"), lineOf(t, view, "Throughput"))
	assert.Less(t, lineOf(t, view, "Throughput"), lineOf(t, view, "Storage"))
	assert.Contains(t, view, "Resource ID")
	assert.Contains(t, view, "hY1TAKjvBQA=")
	assert.Contains(t, view, "4.2 MB")
	assert.NotContains(t, view, "inspecting", "the spinner leaves with the answer")
}

func TestInfoLinesValuesUpUnderTheirSection(t *testing.T) {
	lines := strings.Split(plain(loadedInfo(ordersDetails()).View()), "\n")

	assert.Equal(t, columnOf(t, lines, "1284"), columnOf(t, lines, "4.2 MB"),
		"values start in the same column however long their names are")
}

func TestInfoRendersANoteUnderItsHeading(t *testing.T) {
	view := plain(loadedInfo(ordersDetails()).View())

	assert.Equal(t, lineOf(t, view, "Throughput")+1, lineOf(t, view, "Inherited from database sales"))
}

func TestInfoScrollsDetailsTallerThanTheFrame(t *testing.T) {
	info := loadedInfo(tallDetails())

	require.NotEqual(t, info.View(), info.ScrollDown().View())
	assert.Equal(t, info.View(), info.ScrollDown().ScrollUp().View())
	assert.Equal(t, info.View(), info.ScrollUp().View(), "the first line is as far up as it goes")

	bottom := info
	for range 4 * infoHeight {
		bottom = bottom.ScrollDown()
	}
	assert.Equal(t, bottom.View(), bottom.ScrollDown().View(), "the last line is as far down as it goes")
	assert.Contains(t, plain(bottom.View()), fmt.Sprintf("Path %d", 3*infoHeight-1), "scrolling reaches the last line")
}

func TestInfoDoesNotScrollDetailsThatFit(t *testing.T) {
	info := loadedInfo(ordersDetails())

	assert.Equal(t, info.View(), info.ScrollDown().View())
}

func TestInfoKeepsTheHintOnTheLastLineWhileScrolling(t *testing.T) {
	info := panes.NewInfo(theme.Icons(), infoHints).SetSize(infoWidth, infoHeight).Show(ordersNode)
	info = info.SetDetails(ordersNode.Path, tallDetails()).ScrollDown().ScrollDown()

	lines := strings.Split(plain(info.View()), "\n")
	assert.Contains(t, lines[len(lines)-2], "close")
	assert.Equal(t, infoHeight, lipgloss.Height(info.View()))
	assert.Equal(t, infoWidth, lipgloss.Width(info.View()))
}

func TestInfoShowsAFailureUnderTheHeader(t *testing.T) {
	info, _ := newInfo().StartLoading()
	info = info.Fail(ordersNode.Path, errors.New("account unreachable"))

	view := plain(info.View())
	assert.Contains(t, view, "Identity", "the header survives")
	assert.Contains(t, view, "orders")
	assert.Contains(t, view, "account unreachable")
	assert.NotContains(t, view, "inspecting")
}

func TestInfoKeepsAnAnswerForANodeNotOnScreen(t *testing.T) {
	info, _ := newInfo().StartLoading()
	other := []string{"sales", "customers"}

	info = info.SetDetails(other, ordersDetails())

	assert.Contains(t, plain(info.View()), "inspecting", "the node on screen is still waiting")
	assert.False(t, info.Loaded())
	assert.True(t, info.Show(adapter.Node{Kind: adapter.NodeContainer, Name: "customers", Path: other}).Loaded(),
		"the answer is on hand when that node opens")
}

func TestInfoDropsAFailureForANodeNotOnScreen(t *testing.T) {
	info, _ := newInfo().StartLoading()

	info = info.Fail([]string{"sales", "customers"}, errors.New("account unreachable"))

	assert.NotContains(t, plain(info.View()), "unreachable")
}

func TestInfoRefreshReplacesTheAnswerWithTheSpinner(t *testing.T) {
	info := loadedInfo(ordersDetails())

	info, _ = info.StartLoading()

	view := plain(info.View())
	assert.Contains(t, view, "inspecting")
	assert.NotContains(t, view, "4.2 MB")
}
