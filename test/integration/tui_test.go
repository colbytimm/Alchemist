//go:build integration

// Package integration walks the run-query flow end to end against the Cosmos
// DB emulator. It lives outside internal/tui because only cmd/ and this
// package may name a concrete adapter.
package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The emulator's fixed, publicly documented account key. Not a secret.
const wellKnownKey = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw==" // #gitleaks:allow

const (
	fixtureDatabase  = "alchemist_tui_it"
	fixtureContainer = "orders"
	// seedCount exceeds the adapter's default page size, so the result set
	// only completes if the pane fetches a second page.
	seedCount = 120
	// The terminal the model is driven at.
	testWidth  = 80
	testHeight = 24
)

var chargePattern = regexp.MustCompile(`([0-9]+\.[0-9]{2}) RU`)

var rowsPattern = regexp.MustCompile(`([0-9]+) rows`)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func settings() map[string]string {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8081"
	}
	key := os.Getenv("COSMOS_KEY")
	if key == "" {
		key = wellKnownKey
	}
	return map[string]string{
		"endpoint":             endpoint,
		"key":                  key,
		"insecure_skip_verify": "true", // classic emulator image serves a self-signed cert
	}
}

func connectWithRetry(t *testing.T) adapter.Connection {
	t.Helper()
	conn, err := cosmos.Adapter{}.Connect(context.Background(), settings())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	waitForEmulator(t, conn)
	return conn
}

// waitForEmulator pings until the emulator answers: the container reports
// healthy before it serves requests.
func waitForEmulator(t *testing.T, conn adapter.Connection) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := conn.Ping(ctx)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("emulator not reachable: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

// freshFixture drops whatever an earlier run left behind and seeds a
// container with more rows than one page holds.
func freshFixture(t *testing.T) {
	t.Helper()
	client := seedClient(t)
	db := freshDatabase(t, client, fixtureDatabase)

	ctx := context.Background()
	_, err := db.CreateContainer(ctx, azcosmos.ContainerProperties{
		ID:                     fixtureContainer,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/pk"}},
	}, nil)
	require.NoError(t, err)
	container, err := client.NewContainer(fixtureDatabase, fixtureContainer)
	require.NoError(t, err)
	for i := range seedCount {
		pk := fmt.Sprintf("pk-%d", i%3)
		item, err := json.Marshal(map[string]any{
			"id":     fmt.Sprintf("item-%03d", i),
			"pk":     pk,
			"amount": i,
		})
		require.NoError(t, err)
		_, err = container.CreateItem(ctx, azcosmos.NewPartitionKeyString(pk), item, nil)
		require.NoError(t, err)
	}
}

// seedClient talks to the emulator directly, for the writes the adapter has
// no business offering.
func seedClient(t *testing.T) *azcosmos.Client {
	t.Helper()
	parsed, err := cosmos.ParseSettings(settings())
	require.NoError(t, err)
	cred, err := azcosmos.NewKeyCredential(parsed.Key)
	require.NoError(t, err)
	opts := &azcosmos.ClientOptions{}
	opts.Transport = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // self-signed emulator cert only
	}}
	client, err := azcosmos.NewClientWithKey(parsed.Endpoint, cred, opts)
	require.NoError(t, err)
	return client
}

// freshDatabase drops whatever an earlier run left behind under name, and
// drops it again when the test ends.
func freshDatabase(t *testing.T, client *azcosmos.Client, name string) *azcosmos.DatabaseClient {
	t.Helper()
	ctx := context.Background()
	db, err := client.NewDatabase(name)
	require.NoError(t, err)
	_, _ = db.Delete(ctx, nil)
	_, err = client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: name}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Delete(context.Background(), nil) })
	return db
}

func newModel(t *testing.T, conn adapter.Connection) tea.Model {
	t.Helper()
	return newSession(t, conn, tui.Options{})
}

// newSession builds a model from opts on one account, served by conn, with
// the icons every test shares filled in, and lets its catalog load.
func newSession(t *testing.T, conn adapter.Connection, opts tui.Options) tea.Model {
	t.Helper()
	opts.Icons = theme.Icons()
	opts.Accounts = []tui.Account{{Name: cosmos.Name, SampleFields: true}}
	opts.Launch = cosmos.Name
	opts.Open = func(context.Context, string) (adapter.Connection, error) { return conn, nil }
	m := tui.New(opts)
	model, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return settle(model, model.Init())
}

// settle runs cmd and keeps feeding the model whatever the results produce,
// until nothing new comes back: a catalog load prefetches the children of the
// rows it put on screen, and a selection depends on those having landed.
// Animation ticks are delivered but not followed, since the spinner
// reschedules itself forever.
func settle(m tea.Model, cmd tea.Cmd) tea.Model {
	pending := messages(cmd)
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
		pending = next
	}
	return m
}

// messages executes cmd the way the runtime does, flattening batches into what
// they produce.
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

func press(t *testing.T, m tea.Model, keys ...tea.KeyMsg) tea.Model {
	t.Helper()
	for _, key := range keys {
		model, cmd := m.Update(key)
		m = settle(model, cmd)
	}
	return m
}

func keyMsg(kind tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: kind}
}

func keyText(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func view(m tea.Model) string {
	return ansi.Strip(m.View())
}

// selectFixtureContainer walks the catalog to the seeded container the way a
// person would, which is what publishes a default scope.
func selectFixtureContainer(t *testing.T, m tea.Model, conn adapter.Connection) tea.Model {
	t.Helper()
	roots, err := conn.Catalog().Root(context.Background())
	require.NoError(t, err)
	row := -1
	for i, node := range roots {
		if node.Name == fixtureDatabase {
			row = i
		}
	}
	require.GreaterOrEqual(t, row, 0, "the seeded database should be in the catalog")

	for range row {
		m = press(t, m, keyMsg(tea.KeyDown))
	}
	return press(t, m, keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
}

func runQuery(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return press(t, m, keyRune('e'), keyText(text), keyMsg(tea.KeyCtrlR))
}

// runAnother replaces the one-line query a run left in the editor, where the
// focus still is, and runs text instead.
func runAnother(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return press(t, m, keyMsg(tea.KeyCtrlU), keyText(text), keyMsg(tea.KeyCtrlR))
}

func number(t *testing.T, pattern *regexp.Regexp, rendered string) float64 {
	t.Helper()
	match := pattern.FindStringSubmatch(rendered)
	require.Len(t, match, 2, "%s should appear in the status bar", pattern)
	value, err := strconv.ParseFloat(match[1], 64)
	require.NoError(t, err)
	return value
}

func TestIntegrationRunQuery(t *testing.T) {
	conn := connectWithRetry(t)
	freshFixture(t)

	t.Run("catalog selection scopes a bare query", func(t *testing.T) {
		m := selectFixtureContainer(t, newModel(t, conn), conn)

		rendered := view(runQuery(t, m, "SELECT * FROM c"))

		assert.Contains(t, rendered, "item-0")
		assert.Contains(t, rendered, fixtureDatabase+"."+fixtureContainer)
		assert.Positive(t, number(t, chargePattern, rendered), "the request charge is reported")
		assert.NotContains(t, rendered, "— elapsed", "and so is the elapsed time")
	})

	t.Run("a query naming its own container needs no selection", func(t *testing.T) {
		m := newModel(t, conn)

		rendered := view(runQuery(t, m,
			fmt.Sprintf(`SELECT * FROM %s.%s AS c WHERE c.pk = "pk-1"`, fixtureDatabase, fixtureContainer)))

		assert.Equal(t, float64(seedCount/3), number(t, rowsPattern, rendered),
			"every seeded item of partition pk-1, and nothing else")
		assert.Contains(t, rendered, "pk-1")
		assert.NotContains(t, rendered, "pk-0")
	})

	t.Run("scrolling past the last loaded row fetches the next page", func(t *testing.T) {
		m := runQuery(t, selectFixtureContainer(t, newModel(t, conn), conn), "SELECT * FROM c")
		first := number(t, rowsPattern, view(m))
		require.Less(t, first, float64(seedCount), "the first page cannot hold the whole fixture")

		m = press(t, m, keyMsg(tea.KeyTab))
		for range seedCount {
			m = press(t, m, keyMsg(tea.KeyDown))
		}

		assert.Equal(t, float64(seedCount), number(t, rowsPattern, view(m)),
			"scrolling to the end pulls every remaining page")
	})

	t.Run("a syntax error is shown inline and the session survives it", func(t *testing.T) {
		m := selectFixtureContainer(t, newModel(t, conn), conn)

		failed := runQuery(t, m, "SELEC * FRM c")

		assert.Contains(t, view(failed), "cosmos:", "the service message reaches the pane")
		assert.Contains(t, view(failed), "Syntax error", "with what the service said")
		assert.NotContains(t, view(failed), "localhost", "and not the request URL")
		assert.Contains(t, view(failed), "Catalog", "and the layout is still standing")

		recovered := press(t, failed, keyMsg(tea.KeyCtrlU), keyText("SELECT * FROM c"), keyMsg(tea.KeyCtrlR))
		assert.Contains(t, view(recovered), "item-0", "a valid query runs straight after")
	})

	t.Run("enter opens the raw document and esc closes it", func(t *testing.T) {
		m := runQuery(t, selectFixtureContainer(t, newModel(t, conn), conn), "SELECT * FROM c")

		opened := press(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyEnter))
		assert.Contains(t, view(opened), "Document")
		assert.Contains(t, view(opened), `"id"`)
		assert.NotContains(t, view(opened), "Catalog")

		assert.Contains(t, view(press(t, opened, keyMsg(tea.KeyEscape))), "Catalog")
	})
}

func TestIntegrationQueryHistory(t *testing.T) {
	conn := connectWithRetry(t)
	freshFixture(t)
	dir := t.TempDir()
	store, err := history.Open(dir)
	require.NoError(t, err)
	m := selectFixtureContainer(t, newSession(t, conn, tui.Options{History: store}), conn)

	m = runQuery(t, m, "SELECT * FROM c")
	m = runAnother(t, m, "SELEC * FRM c")
	m = runAnother(t, m, `SELECT * FROM c WHERE c.pk = "pk-1"`)

	opened := press(t, m, keyMsg(tea.KeyCtrlO))
	rendered := view(opened)
	assert.Contains(t, rendered, "History", "ctrl+o opens the overlay")
	assert.Less(t, strings.Index(rendered, "WHERE c.pk"), strings.Index(rendered, "SELEC * FRM c"), "newest first")
	assert.Less(t, strings.Index(rendered, "SELEC * FRM c"), strings.LastIndex(rendered, "SELECT * FROM c"),
		"the bare query, run first, is the last row")
	assert.Contains(t, rendered, theme.Icons().Failure, "the syntax error is listed as a failure")

	filtered := view(press(t, opened, keyRune('/'), keyText(fixtureContainer)))
	assert.Contains(t, filtered, "SELEC * FRM c", "every run targeted the fixture container")
	narrowed := view(press(t, opened, keyRune('/'), keyText("pk-1")))
	assert.Contains(t, narrowed, "WHERE c.pk")
	assert.NotContains(t, narrowed, "SELEC * FRM c", "the filter narrows the list")

	recalled := press(t, opened, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
	assert.Contains(t, view(recalled), "Catalog", "enter closes the overlay")
	assert.Contains(t, view(recalled), "SELEC * FRM c", "with the failed query back in the editor")
	assert.Contains(t, view(recalled), fixtureDatabase+"."+fixtureContainer, "and its scope restored")

	rerun := press(t, recalled, keyMsg(tea.KeyCtrlO), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyCtrlR))
	assert.Contains(t, view(rerun), "item-0", "ctrl+r runs the oldest query again")

	contents, err := os.ReadFile(filepath.Join(dir, history.FileName))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	assert.Len(t, lines, 4, "three runs and the re-run")
	for i, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line %d is not JSON: %s", i+1, line)
		assert.NotContains(t, line, wellKnownKey, "the log holds no credential")
		assert.NotContains(t, line, "localhost", "nor the endpoint, even in a recorded error")
	}
}
