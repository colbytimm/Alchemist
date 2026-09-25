package tui_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

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
// page reads reached it, whether a cursor it walked away from was closed
// (closed), how often it was pinged and closed itself (closes),
// and — by keeping the contexts it was handed, which only a recorder has any
// business doing — whether a replaced run was cancelled. failRoot, failQuery,
// failPage and failPing fail that many calls before the fixture answers,
// which is how a test gets a failure the next attempt recovers from;
// failChildren does the same for every child listing. Management calls pass
// straight through to the mock, keeping the specs so a test can see what the
// dialogs assembled, and inspections and field samples keep the path they
// were asked about. Batches pass through to the mock's item store, which
// store is, and are kept in the order they arrived; so do a clone's
// definition reads, scans and upserts, and deletes keep what they deleted.
type recordingConnection struct {
	inner        adapter.Connection
	store        *mock.Adapter
	batcher      adapter.Batcher
	drafter      adapter.ItemDrafter
	batches      []adapter.Batch
	admin        adapter.CatalogAdmin
	editor       adapter.ThroughputEditor
	inspector    adapter.Inspector
	sampler      adapter.FieldSampler
	definitions  adapter.DefinitionReader
	scanner      adapter.ItemScanner
	writer       adapter.ItemWriter
	deleted      [][]string
	calls        map[string]int
	queries      []adapter.Query
	contexts     []context.Context
	containers   []adapter.ContainerSpec
	provisions   []adapter.Throughput
	inspected    [][]string
	sampled      [][]string
	pageReads    int
	failRoot     int
	failChildren int
	failQuery    int
	failPage     int
	failPing     int
	pings        int
	closed       int
	closes       int
}

func newConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	store := mock.New(opts...)
	inner, err := store.Connect(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inner.Close()) })
	conn := &recordingConnection{inner: inner, store: store, calls: map[string]int{}}
	conn.batcher, _ = inner.(adapter.Batcher)
	conn.drafter, _ = inner.(adapter.ItemDrafter)
	conn.admin, _ = inner.(adapter.CatalogAdmin)
	conn.editor, _ = inner.(adapter.ThroughputEditor)
	conn.inspector, _ = inner.(adapter.Inspector)
	conn.sampler, _ = inner.(adapter.FieldSampler)
	conn.definitions, _ = inner.(adapter.DefinitionReader)
	conn.scanner, _ = inner.(adapter.ItemScanner)
	conn.writer, _ = inner.(adapter.ItemWriter)
	return conn
}

func (c *recordingConnection) Inspect(ctx context.Context, n adapter.Node) (adapter.Details, error) {
	c.inspected = append(c.inspected, n.Path)
	return c.inspector.Inspect(ctx, n)
}

func (c *recordingConnection) SampleFields(ctx context.Context, n adapter.Node) (adapter.FieldSample, error) {
	c.sampled = append(c.sampled, n.Path)
	return c.sampler.SampleFields(ctx, n)
}

func (c *recordingConnection) ExecuteBatch(ctx context.Context, b adapter.Batch) (adapter.BatchResult, error) {
	c.batches = append(c.batches, b)
	return c.batcher.ExecuteBatch(ctx, b)
}

func (c *recordingConnection) DraftReplace(item json.RawMessage) (adapter.Operation, error) {
	return c.drafter.DraftReplace(item)
}

func (c *recordingConnection) CreateDatabase(ctx context.Context, spec adapter.DatabaseSpec) error {
	return c.admin.CreateDatabase(ctx, spec)
}

func (c *recordingConnection) DeleteDatabase(ctx context.Context, name string) error {
	c.deleted = append(c.deleted, []string{name})
	return c.admin.DeleteDatabase(ctx, name)
}

func (c *recordingConnection) CreateContainer(ctx context.Context, spec adapter.ContainerSpec) error {
	c.containers = append(c.containers, spec)
	return c.admin.CreateContainer(ctx, spec)
}

func (c *recordingConnection) DeleteContainer(ctx context.Context, path []string) error {
	c.deleted = append(c.deleted, path)
	return c.admin.DeleteContainer(ctx, path)
}

func (c *recordingConnection) ContainerDefinition(ctx context.Context, path []string, fidelity adapter.DefinitionFidelity) (adapter.ContainerDefinition, error) {
	return c.definitions.ContainerDefinition(ctx, path, fidelity)
}

func (c *recordingConnection) ScanItems(ctx context.Context, request adapter.ScanRequest) (adapter.ItemScan, error) {
	return c.scanner.ScanItems(ctx, request)
}

func (c *recordingConnection) OpenItemSink(ctx context.Context, path []string) (adapter.ItemSink, error) {
	return c.writer.OpenItemSink(ctx, path)
}

func (c *recordingConnection) Throughput(ctx context.Context, path []string) (adapter.Throughput, error) {
	return c.editor.Throughput(ctx, path)
}

func (c *recordingConnection) SetThroughput(ctx context.Context, path []string, t adapter.Throughput) error {
	c.provisions = append(c.provisions, t)
	return c.editor.SetThroughput(ctx, path, t)
}

func (c *recordingConnection) Catalog() adapter.Catalog { return c }

func (c *recordingConnection) Root(ctx context.Context) ([]adapter.Node, error) {
	c.calls[""]++
	if c.failRoot > 0 {
		c.failRoot--
		return nil, &adapter.UnreachableError{Reason: "connection refused"}
	}
	return c.inner.Catalog().Root(ctx)
}

func (c *recordingConnection) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	c.calls[n.Name]++
	if c.failChildren > 0 {
		c.failChildren--
		return nil, errors.New("container list unreachable")
	}
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

func (c *recordingConnection) Ping(ctx context.Context) error {
	c.pings++
	if c.failPing > 0 {
		c.failPing--
		return errors.New("ping: 503 Service Unavailable")
	}
	return c.inner.Ping(ctx)
}

func (c *recordingConnection) Close() error {
	c.closes++
	return c.inner.Close()
}

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

// managed reports what a connection allows, the way cmd/ does.
func managed(conn adapter.Connection) tui.Management {
	admin, _ := conn.(adapter.CatalogAdmin)
	throughput, _ := conn.(adapter.ThroughputEditor)
	inspector, _ := conn.(adapter.Inspector)
	sampler, _ := conn.(adapter.FieldSampler)
	batcher, _ := conn.(adapter.Batcher)
	drafter, _ := conn.(adapter.ItemDrafter)
	definitions, _ := conn.(adapter.DefinitionReader)
	scanner, _ := conn.(adapter.ItemScanner)
	writer, _ := conn.(adapter.ItemWriter)
	return tui.Management{
		Admin:       admin,
		Throughput:  throughput,
		Inspector:   inspector,
		Sampler:     sampler,
		Batcher:     batcher,
		Drafter:     drafter,
		Definitions: definitions,
		Scanner:     scanner,
		Writer:      writer,
	}
}

// newModel builds a model sized to the minimum supported terminal, managing
// whatever its connection allows and sampling fields, as a profile does by
// default.
func newModel(t *testing.T, connection adapter.Connection) tea.Model {
	t.Helper()
	return newModelWith(t, connection, tui.Options{Manage: managed, SampleFields: true})
}

// newModelWith builds a model from opts, sized to the minimum supported
// terminal, whose session starts on one account served by connection: the
// first of opts.Accounts, or one called mock when opts lists none.
func newModelWith(t *testing.T, connection adapter.Connection, opts tui.Options) tea.Model {
	t.Helper()
	opts.Icons = theme.Icons()
	if len(opts.Accounts) == 0 {
		opts.Accounts = []tui.Account{{Name: mock.Name, SampleFields: true}}
	}
	opts.Launch = opts.Accounts[0].Name
	opts.Open = func(context.Context, string) (adapter.Connection, error) { return connection, nil }
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

// timerGrace is how long messages waits on a command before taking it for a
// timer. Every command a test does follow answers from memory, in microseconds.
const timerGrace = 50 * time.Millisecond

// messages executes cmd the way the runtime does, flattening batches into what
// they produce. Nested commands are not followed, and neither is one still
// counting down: a notice retiring itself seconds from now is not part of what
// the key press produced, and waiting for it would stall the whole suite.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	answered := make(chan tea.Msg, 1)
	go func() { answered <- cmd() }()
	select {
	case msg := <-answered:
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return []tea.Msg{msg}
		}
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, messages(c)...)
		}
		return msgs
	case <-time.After(timerGrace):
		return nil
	}
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
