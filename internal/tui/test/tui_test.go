package tui_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// plain drops the styling, so an assertion can match text a renderer split
// into separately styled runs — a filter prompt and the text typed after it,
// say.
func plain(view string) string {
	return ansi.Strip(view)
}

// recordingConnection wraps a mock connection, and serves as its own catalog,
// so a test can see everything the model asked of the backend in one place:
// how often each node's children were fetched, which queries ran, how many
// page reads reached it, whether a cursor it walked away from was closed,
// and — by keeping the contexts it was handed, which only a recorder has any
// business doing — whether a replaced run was cancelled. failRoot, failQuery
// and failPage fail that many calls before the fixture answers, which is how
// a test gets a failure the next attempt recovers from.
type recordingConnection struct {
	inner     adapter.Connection
	calls     map[string]int
	queries   []adapter.Query
	contexts  []context.Context
	pageReads int
	failRoot  int
	failQuery int
	failPage  int
	closed    int
}

func newConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	inner, err := mock.New(opts...).Connect(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inner.Close()) })
	return &recordingConnection{inner: inner, calls: map[string]int{}}
}

func (c *recordingConnection) Catalog() adapter.Catalog { return c }

func (c *recordingConnection) Root(ctx context.Context) ([]adapter.Node, error) {
	c.calls[""]++
	if c.failRoot > 0 {
		c.failRoot--
		return nil, errors.New("catalog unreachable")
	}
	return c.inner.Catalog().Root(ctx)
}

func (c *recordingConnection) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	c.calls[n.Name]++
	return c.inner.Catalog().Children(ctx, n)
}

func (c *recordingConnection) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	c.queries = append(c.queries, q)
	c.contexts = append(c.contexts, ctx)
	if c.failQuery > 0 {
		c.failQuery--
		return nil, errors.New("container unreachable")
	}
	cursor, err := c.inner.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return &recordingCursor{Cursor: cursor, connection: c}, nil
}

func (c *recordingConnection) Ping(ctx context.Context) error { return c.inner.Ping(ctx) }

func (c *recordingConnection) Close() error { return c.inner.Close() }

// recordingCursor tallies its close on the connection that opened it, and
// fails the pages that connection was told to fail.
type recordingCursor struct {
	adapter.Cursor
	connection *recordingConnection
}

func (c *recordingCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	c.connection.pageReads++
	if c.connection.failPage > 0 {
		c.connection.failPage--
		return adapter.Page{}, errors.New("page unreachable")
	}
	return c.Cursor.NextPage(ctx)
}

func (c *recordingCursor) Close() error {
	c.connection.closed++
	return c.Cursor.Close()
}

// newModel builds a model sized to the minimum supported terminal.
func newModel(t *testing.T, connection adapter.Connection) tea.Model {
	t.Helper()
	return newModelWith(t, tui.Options{Connection: connection})
}

// newModelWith builds a model from opts, with the icons and profile every
// test shares filled in, sized to the minimum supported terminal.
func newModelWith(t *testing.T, opts tui.Options) tea.Model {
	t.Helper()
	opts.Icons = theme.Icons()
	opts.Profile = mock.Name
	m := tui.New(opts)
	model, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return model
}

// newLoadedModel returns a model whose catalog root has already arrived and
// whose prefetches have settled.
func newLoadedModel(t *testing.T, connection adapter.Connection) tea.Model {
	t.Helper()
	m := newModel(t, connection)
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

// keyText types a whole string in one message, the way a paste arrives.
func keyText(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// keySpace is the space bar as bubbletea reports it: its own key type, with
// the rune still attached.
func keySpace() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
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
