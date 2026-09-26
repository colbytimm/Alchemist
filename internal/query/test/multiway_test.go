package query_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	starQuery = `SELECT o.id, cu.name, p.name AS product
FROM sales.orders o
JOIN sales.customers cu ON o.customerId = cu.id
JOIN sales.products p ON o.sku = p.id`
	chainQuery = `SELECT a.message, e.kind, d.site
FROM telemetry.alerts a
JOIN telemetry.events e ON a.deviceId = e.deviceId
JOIN telemetry.devices d ON e.deviceRef = d.id`
)

func starContainers(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"sales.orders": {
			page(t, 2,
				`{"customerId":"c1","id":"o1","sku":"s1"}`,
				`{"customerId":"c9","id":"o2","sku":"s1"}`,
				`{"customerId":"c2","id":"o3","sku":"s9"}`),
			page(t, 3, `{"customerId":"c1","id":"o4","sku":"s2"}`),
		},
		"sales.customers": {page(t, 1.5, `{"id":"c1","name":"Ada"}`, `{"id":"c2","name":"Grace"}`)},
		"sales.products":  {page(t, 0.5, `{"id":"s1","name":"Lamp"}`, `{"id":"s2","name":"Desk"}`)},
	})
}

func chainContainers(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"telemetry.alerts": {page(t, 1,
			`{"deviceId":"d1","message":"hot"}`,
			`{"deviceId":"d2","message":"cold"}`)},
		"telemetry.events": {page(t, 1,
			`{"deviceId":"d1","deviceRef":"r1","kind":"pressure"}`,
			`{"deviceId":"d1","kind":"no device"}`,
			`{"deviceRef":"r1","kind":"no alert"}`,
			`{"deviceId":"d2","deviceRef":"r2","kind":"orphan"}`)},
		"telemetry.devices": {page(t, 1, `{"id":"r1","site":"north"}`)},
	})
}

func TestAThreeWayStarCombinesARowPerOrderUnderEveryAlias(t *testing.T) {
	pages := drain(t, execute(t, starContainers(t), starQuery))

	assert.Equal(t, []string{"o.id", "cu.name", "product"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"o1", "Ada", "Lamp"}, {"o4", "Ada", "Desk"}}, rows(pages),
		"o2 has no customer and o3 no product")
}

func TestAChainJoinsThroughItsMiddleSide(t *testing.T) {
	pages := drain(t, execute(t, chainContainers(t), chainQuery))

	assert.Equal(t, [][]string{{"hot", "pressure", "north"}}, rows(pages),
		"an event missing the key of either hop it takes part in matches nothing")
}

func TestTheStreamedSideIsTheFirstUnfilteredLeafAndIsReadLast(t *testing.T) {
	tests := []struct {
		name  string
		where string
		want  []string
	}{
		{name: "no filter", want: []string{"sales.customers", "sales.products", "sales.orders"}},
		{name: "filter on the first", where: `o.id = "o1"`, want: []string{"sales.orders", "sales.products", "sales.customers"}},
		{name: "filter on the middle", where: `cu.name = "Ada"`, want: []string{"sales.customers", "sales.products", "sales.orders"}},
		{
			name:  "filters on all",
			where: `o.id = "o1" AND cu.name = "Ada" AND p.name = "Lamp"`,
			want:  []string{"sales.customers", "sales.products", "sales.orders"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := starContainers(t)
			text := starQuery
			if tt.where != "" {
				text += " WHERE " + tt.where
			}

			drain(t, execute(t, conn, text))

			assert.Equal(t, tt.want, queriedLabels(conn))
		})
	}
}

func queriedLabels(conn *containers) []string {
	var labels []string
	for _, q := range conn.queries {
		labels = append(labels, q.Scope[0]+"."+q.Scope[1])
	}
	return labels
}

func TestRootingTheJoinAtAMiddleSideYieldsTheSameRows(t *testing.T) {
	fromOrders := rows(drain(t, execute(t, starContainers(t), starQuery)))
	fromCustomers := rows(drain(t, execute(t, starContainers(t), starQuery+` WHERE o.id != "none"`)))

	assert.ElementsMatch(t, fromOrders, fromCustomers)
}

func TestSelectStarListsColumnsInWrittenOrderWhicheverSideWasReadFirst(t *testing.T) {
	conn := chainContainers(t)
	text := "SELECT * FROM telemetry.alerts a JOIN telemetry.events e ON a.deviceId = e.deviceId " +
		`JOIN telemetry.devices d ON e.deviceRef = d.id WHERE a.message = "hot" AND e.kind = "pressure"`

	pages := drain(t, execute(t, conn, text))

	require.Equal(t, []string{"telemetry.events", "telemetry.alerts", "telemetry.devices"}, queriedLabels(conn))
	assert.Equal(t, []string{
		"a.deviceId", "a.message",
		"e.deviceId", "e.deviceRef", "e.kind",
		"d.id", "d.site",
	}, pages[0].Columns)
	assert.Equal(t, `{"a":{"deviceId":"d1","message":"hot"},`+
		`"e":{"deviceId":"d1","deviceRef":"r1","kind":"pressure"},`+
		`"d":{"id":"r1","site":"north"}}`, string(pages[0].Raw[0]))
}

func TestAMultiWayRawItemHoldsTheProjectedFieldsOfEachSide(t *testing.T) {
	pages := drain(t, execute(t, starContainers(t), starQuery))

	assert.Equal(t, `{"o":{"id":"o1"},"cu":{"name":"Ada"},"p":{"product":"Lamp"}}`, string(pages[0].Raw[0]))
}

func TestDuplicateKeysOnTwoHeldSidesYieldTheProductOfTheirMatches(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`, `{"id":"c1","name":"Ada L."}`)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`, `{"id":"s1","name":"Lamp II"}`)},
	})

	pages := drain(t, execute(t, conn, starQuery))

	assert.Equal(t, [][]string{
		{"o1", "Ada", "Lamp"}, {"o1", "Ada", "Lamp II"},
		{"o1", "Ada L.", "Lamp"}, {"o1", "Ada L.", "Lamp II"},
	}, rows(pages))
}

func TestAnEmptyHeldSideEndsTheRunBeforeTheLaterSidesAreRead(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.customers": {page(t, 1)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`)},
	})
	cursor := execute(t, conn, starQuery)

	pages := drain(t, cursor)

	assert.Empty(t, rows(pages))
	assert.False(t, cursor.HasMore())
	assert.Equal(t, []string{"sales.customers"}, queriedLabels(conn))
}

func TestTheRowCapCountsEveryHeldSide(t *testing.T) {
	tests := []struct {
		name      string
		customers int
		products  int
		wantErr   bool
	}{
		{name: "two sides that each fit but not together", customers: 6, products: 6, wantErr: true},
		{name: "exactly the cap in total", customers: 6, products: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newContainers(map[string][]adapter.Page{
				"sales.orders":    {page(t, 1, `{"customerId":"c0","id":"o1","sku":"s0"}`)},
				"sales.customers": {page(t, 1, items("c", tt.customers)...)},
				"sales.products":  {page(t, 1, items("s", tt.products)...)},
			})
			cursor, err := query.Engine{Connection: conn, MaxJoinRows: 10}.Execute(context.Background(), plan(t, starQuery))
			require.NoError(t, err)

			_, err = cursor.NextPage(context.Background())

			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, query.ErrJoinTooLarge)
			assert.ErrorContains(t, err, "sales.products takes the rows held in memory past 10 (sales.customers 6)")
			assert.Equal(t, conn.opened, conn.closed)
		})
	}
}

func TestEachHeldSideAloneFitsTheCap(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c0","id":"o1","sku":"s0"}`)},
		"sales.customers": {page(t, 1, items("c", 6)...)},
	})
	engine := query.Engine{Connection: conn, MaxJoinRows: 10}

	cursor, err := engine.Execute(context.Background(), plan(t, joinQuery))
	require.NoError(t, err)
	_, err = cursor.NextPage(context.Background())

	require.NoError(t, err)
}

// items are n objects whose ids run prefix0, prefix1, ...
func items(prefix string, n int) []string {
	var objects []string
	for i := range n {
		objects = append(objects, fmt.Sprintf(`{"id":"%s%d","name":"%s %d"}`, prefix, i, prefix, i))
	}
	return objects
}

func TestAMultiWayJoinChargesTheSumOfEveryLeaf(t *testing.T) {
	total, leaves := charges(drain(t, execute(t, starContainers(t), starQuery)))

	assert.InDelta(t, 7, total, 1e-9)
	assert.Equal(t, map[string]float64{"sales.orders": 5, "sales.customers": 1.5, "sales.products": 0.5}, leaves)
}

func TestAContainerJoinedToItselfReportsBothLeavesUnderOneKey(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"hr.employees": {page(t, 2, `{"id":"e1","managerId":"e2","name":"Kim"}`, `{"id":"e2","name":"Lee"}`)},
	})

	pages := drain(t, execute(t, conn,
		"SELECT e.name, m.name AS manager FROM hr.employees e JOIN hr.employees m ON e.managerId = m.id"))

	_, leaves := charges(pages)
	assert.Equal(t, [][]string{{"Kim", "Lee"}}, rows(pages))
	assert.Equal(t, map[string]float64{"hr.employees": 4}, leaves)
}

func charges(pages []adapter.Page) (float64, map[string]float64) {
	var total float64
	leaves := map[string]float64{}
	for _, p := range pages {
		total += p.Stats.RequestCharge
		for leaf, charge := range p.Stats.LeafCharges {
			leaves[leaf] += charge
		}
	}
	return total, leaves
}

func TestAFannedOutPageIsServedInCappedPages(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c","id":"o1","total":1}`)},
		"sales.customers": {page(t, 1, fannedCustomers(2500)...)},
	})
	cursor := execute(t, conn, joinQuery)

	pages := drain(t, cursor)

	var sizes []int
	for _, p := range pages {
		sizes = append(sizes, len(p.Rows))
	}
	assert.Equal(t, []int{1000, 1000, 500}, sizes)
	assert.InDelta(t, 2, pages[0].Stats.RequestCharge, 1e-9)
	for _, continuation := range pages[1:] {
		assert.Zero(t, continuation.Stats.RequestCharge)
		assert.Empty(t, continuation.Stats.LeafCharges)
	}
	assert.Len(t, uniqueNames(rows(pages)), 2500, "every row is served exactly once")
	assert.Equal(t, 2, conn.opened, "the continuation pages read nothing")
}

func fannedCustomers(n int) []string {
	var customers []string
	for i := range n {
		customers = append(customers, fmt.Sprintf(`{"id":"c","name":"n%d"}`, i))
	}
	return customers
}

func uniqueNames(rows [][]string) map[string]bool {
	names := map[string]bool{}
	for _, row := range rows {
		names[row[2]] = true
	}
	return names
}

func TestUnmatchedStreamedPagesNeverSurfaceAsAnEmptyPage(t *testing.T) {
	conn := starContainers(t)
	conn.pages["sales.orders"] = []adapter.Page{
		page(t, 1, `{"customerId":"c9","id":"o1","sku":"s1"}`),
		page(t, 1, `{"customerId":"c1","id":"o2","sku":"s9"}`),
		page(t, 1, `{"customerId":"c1","id":"o3","sku":"s1"}`),
		page(t, 1, `{"customerId":"c9","id":"o4","sku":"s1"}`),
	}

	pages := drain(t, execute(t, conn, starQuery))

	for _, p := range pages[:len(pages)-1] {
		assert.NotEmpty(t, p.Rows)
	}
	assert.Equal(t, [][]string{{"o3", "Ada", "Lamp"}}, rows(pages))
}

func TestAtMostOneLeafCursorIsOpenAtATime(t *testing.T) {
	conn := starContainers(t)

	drain(t, execute(t, conn, starQuery))

	assert.Equal(t, 1, conn.mostOpen)
	assert.Equal(t, conn.opened, conn.closed)
}

func TestCancellingDuringTheSecondBuildClosesEveryLeafCursor(t *testing.T) {
	conn := starContainers(t)
	ctx, cancel := context.WithCancel(context.Background())
	conn.onQuery = func(label string) {
		if label == "sales.products" {
			cancel()
		}
	}
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, starQuery))
	require.NoError(t, err)

	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 2, conn.opened)
	assert.Equal(t, 2, conn.closed)
}

func TestCancellingAMultiWayJoinMidStreamClosesEveryLeafCursor(t *testing.T) {
	conn := starContainers(t)
	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, starQuery))
	require.NoError(t, err)
	_, err = cursor.NextPage(ctx)
	require.NoError(t, err)

	cancel()
	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 3, conn.opened)
	assert.Equal(t, 3, conn.closed)
	require.NoError(t, cursor.Close())
	assert.Equal(t, 3, conn.closed, "closing again closes nothing twice")
}

func TestAJoinClosedBeforeItsFirstPageOpensNothing(t *testing.T) {
	conn := starContainers(t)
	cursor := execute(t, conn, starQuery)

	require.NoError(t, cursor.Close())

	assert.False(t, cursor.HasMore())
	_, err := cursor.NextPage(context.Background())
	require.Error(t, err)
	assert.Zero(t, conn.opened)
}

func TestAJoinPlanWhoseStepsDoNotFitItsLeavesIsRefused(t *testing.T) {
	star := plan(t, starQuery)
	inputs := joinOf(t, star).Inputs
	step := query.JoinStep{LeftKey: ref("o", "k"), RightKey: ref("cu", "k")}
	forward := step
	forward.Left = 2
	tests := []struct {
		name string
		join query.Join
	}{
		{name: "fewer steps than joined leaves", join: query.Join{Inputs: inputs, Steps: []query.JoinStep{step}}},
		{name: "a step reading a later leaf", join: query.Join{Inputs: inputs, Steps: []query.JoinStep{forward, step}}},
		{
			name: "a column of a leaf that does not exist",
			join: query.Join{Inputs: inputs, Steps: []query.JoinStep{step, step}, Columns: []query.JoinColumn{{Side: 3, Field: "id"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			malformed := query.Plan{Leaves: star.Leaves, Root: &tt.join}

			_, err := query.Engine{Connection: starContainers(t)}.Execute(context.Background(), malformed)

			require.Error(t, err)
		})
	}
}
