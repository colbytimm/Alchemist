//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	joinDatabase  = "alchemist_join_it"
	orderCount    = 25
	customerCount = 5
	// Orders name one customer more than exists, so an inner join has
	// something to drop.
	customersNamed = customerCount + 1
)

var regions = []string{"east", "north", "west"}

func seedContainer(t *testing.T, client *azcosmos.Client, name string, items []map[string]any) {
	t.Helper()
	ctx := context.Background()
	db, err := client.NewDatabase(joinDatabase)
	require.NoError(t, err)
	_, err = db.CreateContainer(ctx, azcosmos.ContainerProperties{
		ID:                     name,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/region"}},
	}, nil)
	require.NoError(t, err)
	container, err := client.NewContainer(joinDatabase, name)
	require.NoError(t, err)
	for _, item := range items {
		body, err := json.Marshal(item)
		require.NoError(t, err)
		region, ok := item["region"].(string)
		require.True(t, ok)
		_, err = container.CreateItem(ctx, azcosmos.NewPartitionKeyString(region), body, nil)
		require.NoError(t, err)
	}
}

func seedSales(t *testing.T) {
	t.Helper()
	client := seedClient(t)
	freshDatabase(t, client, joinDatabase)

	var orders, customers []map[string]any
	for i := range orderCount {
		orders = append(orders, map[string]any{
			"id":         fmt.Sprintf("o%02d", i),
			"region":     regions[i%len(regions)],
			"customerId": fmt.Sprintf("c%d", i%customersNamed),
			"total":      i * 10,
		})
	}
	for i := range customerCount {
		customers = append(customers, map[string]any{
			"id":     fmt.Sprintf("c%d", i),
			"region": regions[i%len(regions)],
			"name":   fmt.Sprintf("customer %d", i),
		})
	}
	seedContainer(t, client, "orders", orders)
	seedContainer(t, client, "customers", customers)
}

// wantJoined is the join of the seed computed by hand: every order whose
// customer exists, as "id total name".
func wantJoined() []string {
	var want []string
	for i := range orderCount {
		customer := i % customersNamed
		if customer < customerCount {
			want = append(want, fmt.Sprintf("o%02d %d customer %d", i, i*10, customer))
		}
	}
	return want
}

func readAll(t *testing.T, cursor adapter.Cursor) []adapter.Page {
	t.Helper()
	var pages []adapter.Page
	for first := true; first || cursor.HasMore(); first = false {
		page, err := cursor.NextPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, page)
	}
	return pages
}

func TestIntegrationCrossContainerJoin(t *testing.T) {
	conn := connectWithRetry(t)
	seedSales(t)
	plan, err := query.BuildPlan(fmt.Sprintf(
		"SELECT o.id, o.total, cu.name FROM %[1]s.orders AS o JOIN %[1]s.customers AS cu ON o.customerId = cu.id",
		joinDatabase))
	require.NoError(t, err)

	cursor, err := query.Engine{Connection: conn}.Execute(context.Background(), plan)
	require.NoError(t, err)
	pages := readAll(t, cursor)

	var got []string
	var charge, leafCharges float64
	for _, page := range pages {
		for _, row := range page.Rows {
			got = append(got, strings.Join(row, " "))
		}
		charge += page.Stats.RequestCharge
		for _, leaf := range page.Stats.LeafCharges {
			leafCharges += leaf
		}
	}
	slices.Sort(got)
	assert.Equal(t, wantJoined(), got)
	assert.Positive(t, charge)
	assert.InDelta(t, leafCharges, charge, 0.01)
}

func TestIntegrationCrossContainerUnion(t *testing.T) {
	conn := connectWithRetry(t)
	seedSales(t)
	plan, err := query.BuildPlan(fmt.Sprintf("SELECT c.id FROM %[1]s.orders, %[1]s.customers AS c", joinDatabase))
	require.NoError(t, err)

	cursor, err := query.Engine{Connection: conn}.Execute(context.Background(), plan)
	require.NoError(t, err)

	rowsFrom := map[string]int{}
	for _, page := range readAll(t, cursor) {
		for _, row := range page.Rows {
			rowsFrom[row[0]]++
		}
	}
	assert.Equal(t, map[string]int{
		joinDatabase + ".orders":    orderCount,
		joinDatabase + ".customers": customerCount,
	}, rowsFrom)
}
