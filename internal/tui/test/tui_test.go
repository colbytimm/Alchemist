package tui_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
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
// test can tell a cache hit from a second round trip. failRoot fails that
// many opening root fetches before serving the fixture.
type countingCatalog struct {
	inner    adapter.Catalog
	calls    map[string]int
	failRoot int
}

func (c *countingCatalog) Root(ctx context.Context) ([]adapter.Node, error) {
	c.calls[""]++
	if c.failRoot > 0 {
		c.failRoot--
		return nil, errors.New("catalog unreachable")
	}
	return c.inner.Root(ctx)
}

func (c *countingCatalog) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	c.calls[n.Name]++
	return c.inner.Children(ctx, n)
}

func newCountingCatalog(t *testing.T, opts ...mock.Option) *countingCatalog {
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

// newLoadedModel returns a model whose catalog root has already arrived and
// whose prefetches have settled.
func newLoadedModel(t *testing.T, catalog adapter.Catalog) tea.Model {
	t.Helper()
	m := newModel(t, catalog)
	model, _ := settle(m, m.Init())
	return model
}

// settle runs cmd and keeps feeding the model whatever the results produce,
// until nothing new comes back. A response can start further work — a load
// settling one chevron asks for the next — so stopping after one round would
// leave the tree half built. Animation ticks are delivered but not followed:
// the spinner reschedules itself forever.
func settle(m tea.Model, cmd tea.Cmd) (tea.Model, []tea.Msg) {
	pending := messages(cmd)
	delivered := append([]tea.Msg(nil), pending...)
	for len(pending) > 0 {
		var next []tea.Msg
		for _, msg := range pending {
			model, cmd := m.Update(msg)
			m = model
			if _, animating := msg.(spinner.TickMsg); animating {
				continue
			}
			next = append(next, messages(cmd)...)
		}
		delivered = append(delivered, next...)
		pending = next
	}
	return m, delivered
}

// messages executes cmd the way the runtime does, flattening batches into what
// they produce. Nested commands are not followed.
func messages(cmd tea.Cmd) []tea.Msg {
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
		msgs = append(msgs, messages(c)...)
	}
	return msgs
}

// press sends a key and drives the model to rest, returning it together with
// every message the key produced.
func press(t *testing.T, m tea.Model, key tea.KeyMsg) (tea.Model, []tea.Msg) {
	t.Helper()
	model, cmd := m.Update(key)
	return settle(model, cmd)
}

func pressAll(t *testing.T, m tea.Model, keys ...tea.KeyMsg) tea.Model {
	t.Helper()
	for _, key := range keys {
		m, _ = press(t, m, key)
	}
	return m
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyMsg(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// hasMsg reports whether msgs contains a message of type T.
func hasMsg[T tea.Msg](msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			return true
		}
	}
	return false
}
