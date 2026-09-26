package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/query"
)

// containers is a connection serving canned pages per db.container, which
// the mock adapter cannot: it answers every scope with the same rows. It
// tallies the cursors it opened, the ones closed since, and the most ever
// open at once.
type containers struct {
	adapter.Connection
	pages    map[string][]adapter.Page
	queries  []adapter.Query
	opened   int
	closed   int
	mostOpen int
	// onQuery, when set, runs as each leaf is opened, with its db.container.
	onQuery func(label string)
}

func newContainers(pages map[string][]adapter.Page) *containers {
	return &containers{pages: pages}
}

func (c *containers) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.queries = append(c.queries, q)
	c.opened++
	c.mostOpen = max(c.mostOpen, c.opened-c.closed)
	label := strings.Join(q.Scope, ".")
	if c.onQuery != nil {
		c.onQuery(label)
	}
	return &cannedCursor{owner: c, pages: c.pages[label]}, nil
}

type cannedCursor struct {
	owner *containers
	pages []adapter.Page
}

func (c *cannedCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	if err := ctx.Err(); err != nil {
		return adapter.Page{}, err
	}
	if len(c.pages) == 0 {
		return adapter.Page{}, errors.New("no more pages")
	}
	page := c.pages[0]
	c.pages = c.pages[1:]
	return page, nil
}

func (c *cannedCursor) HasMore() bool { return len(c.pages) > 0 }

func (c *cannedCursor) Close() error {
	c.owner.closed++
	return nil
}

// page builds a page costing charge RU out of flat JSON objects, rendering
// cells the way an adapter would for strings and numbers.
func page(t *testing.T, charge float64, items ...string) adapter.Page {
	t.Helper()
	p := adapter.Page{Stats: adapter.Stats{RequestCharge: charge, RowCount: len(items)}}
	seen := map[string]int{}
	for _, item := range items {
		var fields map[string]any
		decoder := json.NewDecoder(strings.NewReader(item))
		decoder.UseNumber()
		require.NoError(t, decoder.Decode(&fields), "item %s", item)
		for _, name := range sortedKeys(fields) {
			if _, ok := seen[name]; !ok {
				seen[name] = len(p.Columns)
				p.Columns = append(p.Columns, name)
			}
		}
		row := make([]string, len(p.Columns))
		for name, value := range fields {
			row[seen[name]] = cell(value)
		}
		p.Rows = append(p.Rows, row)
		p.Raw = append(p.Raw, json.RawMessage(item))
	}
	return p
}

func sortedKeys(fields map[string]any) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func cell(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case nil:
		return ""
	}
	rendered, _ := json.Marshal(value)
	return string(rendered)
}

func plan(t *testing.T, text string) query.Plan {
	t.Helper()
	p, err := query.BuildPlan(text)
	require.NoError(t, err)
	return p
}

func execute(t *testing.T, conn adapter.Connection, text string) adapter.Cursor {
	t.Helper()
	cursor, err := query.Engine{Connection: conn}.Execute(context.Background(), plan(t, text))
	require.NoError(t, err)
	return cursor
}

// drain reads cursor to its end and returns every page it served.
func drain(t *testing.T, cursor adapter.Cursor) []adapter.Page {
	t.Helper()
	var pages []adapter.Page
	for first := true; first || cursor.HasMore(); first = false {
		p, err := cursor.NextPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, p)
	}
	return pages
}

func rows(pages []adapter.Page) [][]string {
	var all [][]string
	for _, p := range pages {
		all = append(all, p.Rows...)
	}
	return all
}

func TestAPassThroughPlanIsServedByTheAdapterUntouched(t *testing.T) {
	conn, err := mock.New(mock.WithPages(1)).Connect(context.Background(), nil)
	require.NoError(t, err)
	direct, err := conn.Query(context.Background(), adapter.Query{Text: "SELECT * FROM c", Scope: []string{"sales", "orders"}})
	require.NoError(t, err)
	want, err := direct.NextPage(context.Background())
	require.NoError(t, err)

	pages := drain(t, execute(t, conn, "SELECT * FROM sales.orders"))

	require.Len(t, pages, 1)
	assert.Equal(t, want, pages[0])
}

const unionQuery = "SELECT * FROM sales.orders, sales.archive"

func TestUnionAllTagsEveryRowWithItsContainer(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1, `{"id":"o1"}`)},
		"sales.archive": {page(t, 1, `{"id":"a1"}`)},
	})

	pages := drain(t, execute(t, conn, unionQuery))

	assert.Equal(t, []string{"_container", "id"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"sales.orders", "o1"}, {"sales.archive", "a1"}}, rows(pages))
}

func TestUnionAllTagsTheRawItemsToo(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1, `{"id":"o1"}`)},
		"sales.archive": {page(t, 1, `{}`)},
	})

	pages := drain(t, execute(t, conn, unionQuery))

	assert.JSONEq(t, `{"_container":"sales.orders","id":"o1"}`, string(pages[0].Raw[0]))
	assert.JSONEq(t, `{"_container":"sales.archive"}`, string(pages[1].Raw[0]))
}

func TestUnionAllPagesAcrossLeafBoundaries(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1, `{"id":"o1"}`), page(t, 1, `{"id":"o2"}`)},
		"sales.archive": {page(t, 1, `{"id":"a1"}`)},
	})
	cursor := execute(t, conn, unionQuery)

	var containerOfPage []string
	for _, p := range drain(t, cursor) {
		containerOfPage = append(containerOfPage, p.Rows[0][0])
	}

	assert.Equal(t, []string{"sales.orders", "sales.orders", "sales.archive"}, containerOfPage)
	assert.False(t, cursor.HasMore())
	assert.Equal(t, conn.opened, conn.closed, "every exhausted leaf is closed")
}

func TestUnionAllAddsALaterContainersColumnsAtTheEnd(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1, `{"id":"o1","total":"5"}`)},
		"sales.archive": {page(t, 1, `{"archivedBy":"kim","id":"a1"}`)},
	})

	pages := drain(t, execute(t, conn, unionQuery))

	assert.Equal(t, []string{"_container", "id", "total", "archivedBy"}, pages[1].Columns)
	assert.Equal(t, []string{"sales.archive", "a1", "", "kim"}, pages[1].Rows[0])
}

func TestUnionAllChargesTheSumOfItsLeaves(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 2.5, `{"id":"o1"}`), page(t, 1.25, `{"id":"o2"}`)},
		"sales.archive": {page(t, 4, `{"id":"a1"}`)},
	})

	var total float64
	leaves := map[string]float64{}
	for _, p := range drain(t, execute(t, conn, unionQuery)) {
		total += p.Stats.RequestCharge
		for leaf, charge := range p.Stats.LeafCharges {
			leaves[leaf] += charge
		}
	}

	assert.InDelta(t, 7.75, total, 1e-9)
	assert.Equal(t, map[string]float64{"sales.orders": 3.75, "sales.archive": 4}, leaves)
}

func TestUnionAllRunsTheSameBodyAgainstEveryContainer(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1)},
		"sales.archive": {page(t, 1)},
	})

	drain(t, execute(t, conn, `SELECT * FROM sales.orders, sales.archive AS c WHERE c.status = "open"`))

	assert.Equal(t, []adapter.Query{
		{Text: `SELECT * FROM c WHERE c.status = "open"`, Scope: []string{"sales", "orders"}},
		{Text: `SELECT * FROM c WHERE c.status = "open"`, Scope: []string{"sales", "archive"}},
	}, conn.queries)
}

func TestCancellingAUnionClosesItsOpenLeaf(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":  {page(t, 1, `{"id":"o1"}`), page(t, 1, `{"id":"o2"}`)},
		"sales.archive": {page(t, 1, `{"id":"a1"}`)},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, unionQuery))
	require.NoError(t, err)
	_, err = cursor.NextPage(ctx)
	require.NoError(t, err)

	cancel()
	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, conn.opened)
	assert.Equal(t, 1, conn.closed)
}

func TestAPlanThatDoesNotFitItsMergeStepIsRefused(t *testing.T) {
	_, err := query.Engine{Connection: newContainers(nil)}.Execute(context.Background(), query.Plan{Root: &query.Join{}})

	require.Error(t, err)
}
