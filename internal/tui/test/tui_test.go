package tui_test

import (
	"context"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The smallest terminal the layout supports.
const (
	testWidth  = 80
	testHeight = 24
)

// TestMain pins a color profile. Without a TTY lipgloss renders everything
// unstyled, which erases the focus and selection differences these tests exist
// to observe.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

// countingCatalog reports how often each node's children were fetched, so a
// test can tell a cache hit from a second round trip.
type countingCatalog struct {
	inner adapter.Catalog
	calls map[string]int
}

func (c *countingCatalog) Root(ctx context.Context) ([]adapter.Node, error) {
	c.calls[""]++
	return c.inner.Root(ctx)
}

func (c *countingCatalog) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	c.calls[n.Name]++
	return c.inner.Children(ctx, n)
}

// newCatalog opens a mock connection and wraps its catalog in a call counter.
func newCatalog(t *testing.T, opts ...mock.Option) *countingCatalog {
	t.Helper()
	conn, err := mock.New(opts...).Connect(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return &countingCatalog{inner: conn.Catalog(), calls: map[string]int{}}
}

// newModel builds a model sized to the minimum supported terminal.
func newModel(t *testing.T, catalog adapter.Catalog) tea.Model {
	t.Helper()
	m := tui.New(tui.Options{Icons: theme.Icons(), Catalog: catalog, Profile: mock.Name})
	model, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return model
}

// loaded returns a model whose catalog root has already arrived.
func loaded(t *testing.T, catalog adapter.Catalog) tea.Model {
	t.Helper()
	model := newModel(t, catalog)
	for _, msg := range run(model.Init()) {
		model, _ = model.Update(msg)
	}
	return model
}

// run executes cmd the way the runtime does, flattening batches into the
// messages they produce. Nested commands are not followed.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, run(c)...)
	}
	return msgs
}

// press sends a key and feeds every message its commands produce back into
// the model, returning the settled model together with those messages.
func press(t *testing.T, m tea.Model, key tea.KeyMsg) (tea.Model, []tea.Msg) {
	t.Helper()
	model, cmd := m.Update(key)
	msgs := run(cmd)
	for _, msg := range msgs {
		model, _ = model.Update(msg)
	}
	return model, msgs
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// has reports whether msgs contains a message of type T.
func has[T tea.Msg](msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			return true
		}
	}
	return false
}
