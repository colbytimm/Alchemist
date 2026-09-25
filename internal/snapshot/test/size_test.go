package snapshot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

var (
	statuses = []string{"open", "paid", "packed", "shipped", "delivered", "cancelled"}
	cities   = []string{"Calgary", "Lisbon", "Osaka", "Nairobi", "Lima", "Oslo", "Perth", "Quito"}
	products = []string{"Erlenmeyer flask 250 ml", "Beaker 1 l", "Burette 50 ml", "Pipette tips (box of 96)",
		"Nitrile gloves M", "Magnetic stirrer", "pH buffer 7.00", "Petri dish 90 mm"}
	methods = []string{"card", "invoice", "transfer"}
	words   = []string{"leave", "at", "door", "fragile", "call", "before", "delivery", "rear", "entrance", "signature"}
)

// realisticOrder is a document of about 750 bytes shaped like an order in
// a real container: a GUID id, enums, an address, a few lines, free text.
func realisticOrder(random *rand.Rand, id string) json.RawMessage {
	lines := make([]map[string]any, 2+random.IntN(4))
	for i := range lines {
		lines[i] = map[string]any{
			"sku": fmt.Sprintf("SKU-%05d", random.IntN(3000)), "name": products[random.IntN(len(products))],
			"qty": 1 + random.IntN(20), "unitPrice": float64(100+random.IntN(20000)) / 100, "discount": float64(random.IntN(4)) * 0.05,
		}
	}
	note := ""
	for range random.IntN(8) {
		note += words[random.IntN(len(words))] + " "
	}
	document := map[string]any{
		"id":         id,
		"customerId": fmt.Sprintf("cust-%06d", random.IntN(50000)),
		"status":     statuses[random.IntN(len(statuses))],
		"createdAt":  fmt.Sprintf("2026-%02d-%02dT%02d:%02d:%02dZ", 1+random.IntN(9), 1+random.IntN(28), random.IntN(24), random.IntN(60), random.IntN(60)),
		"shipTo": map[string]any{
			"name": fmt.Sprintf("Customer %d", random.IntN(50000)), "street": fmt.Sprintf("%d %s Street", 1+random.IntN(999), cities[random.IntN(len(cities))]),
			"city": cities[random.IntN(len(cities))], "postcode": fmt.Sprintf("%05d", random.IntN(99999)), "country": "CA",
		},
		"lines":   lines,
		"total":   float64(random.IntN(1000000)) / 100,
		"payment": map[string]any{"method": methods[random.IntN(len(methods))], "last4": fmt.Sprintf("%04d", random.IntN(10000)), "authorized": random.IntN(10) > 0},
		"notes":   note,
		"tags":    []string{"web", statuses[random.IntN(len(statuses))]},
	}
	encoded, _ := json.Marshal(document)
	return encoded
}

func guid(random *rand.Rand) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", random.Uint32(), random.Uint32()&0xffff, random.Uint32()&0xffff,
		random.Uint32()&0xffff, random.Uint64()&0xffffffffffff)
}

// churningContainer is a mock container of n realistic orders, rewritten a
// share at a time from a seeded source, so two of them replay the same days.
type churningContainer struct {
	f         *fixture
	random    *rand.Rand
	ids       []string
	customers map[string]string
}

func newChurningContainer(tb testing.TB, root string, n int) *churningContainer {
	tb.Helper()
	random := rand.New(rand.NewPCG(30, 1))
	c := newClock()
	churn := &churningContainer{random: random, customers: map[string]string{}}
	var items []json.RawMessage
	for range n {
		id := guid(random)
		item := realisticOrder(random, id)
		var head struct {
			CustomerID string `json:"customerId"`
		}
		require.NoError(tb, json.Unmarshal(item, &head))
		churn.ids = append(churn.ids, id)
		churn.customers[id] = head.CustomerID
		items = append(items, item)
	}
	a := mock.New(mock.WithClock(c.Now), mock.WithItems(ordersPath, items...))
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(tb, err)
	churn.f = &fixture{t: tb, clock: c, mock: a, conn: conn,
		loc: snapshot.Location{Root: root, Account: "prod", Database: "sales", Container: "orders"}}
	return churn
}

// day rewrites share of the items, each keeping its customerId, which is
// its partition key.
func (c *churningContainer) day(share float64) {
	c.f.clock.advance(24 * time.Hour)
	for range int(share * float64(len(c.ids))) {
		id := c.ids[c.random.IntN(len(c.ids))]
		rewritten := map[string]any{}
		require.NoError(c.f.t, json.Unmarshal(realisticOrder(c.random, id), &rewritten))
		rewritten["customerId"] = c.customers[id]
		encoded, err := json.Marshal(rewritten)
		require.NoError(c.f.t, err)
		require.NoError(c.f.t, c.f.mock.PutItem(ordersPath, encoded))
	}
}

func (c *churningContainer) take() snapshot.Record {
	c.f.t.Helper()
	store := c.f.open()
	return takeFrom(c.f.t, store, c.f.source(), snapshot.CaptureOptions{Clock: c.f.clock.Now, PageSize: 1000})
}

func (c *churningContainer) onDisk() int64 {
	c.f.t.Helper()
	u, err := c.f.open().Usage()
	require.NoError(c.f.t, err)
	return u.OnDisk()
}

// TestThirtySnapshotsCostLittleMoreThanOne is the efficiency claim as a
// test: 20 000 orders, 1% rewritten a day, a snapshot a day for 30 days.
func TestThirtySnapshotsCostLittleMoreThanOne(t *testing.T) {
	if testing.Short() {
		t.Skip("writes thirty snapshots of 20 000 items")
	}
	const items, days, kept = 20000, 30, 7
	churn := newChurningContainer(t, t.TempDir(), items)
	churn.take()
	afterFirst := churn.onDisk()
	for day := 1; day < days; day++ {
		churn.day(0.01)
		churn.take()
	}
	afterThirty := churn.onDisk()
	u, err := churn.f.open().Usage()
	require.NoError(t, err)
	exports := u.LogicalBytes

	unchanged := churn.take()
	afterUnchanged := churn.onDisk()
	_, err = churn.f.open().Prune(snapshot.Policy{KeepLast: kept}, churn.f.clock.Now())
	require.NoError(t, err)
	pruned := churn.onDisk()
	fresh := freshStoreOfTheLast(t, items, days, kept)

	t.Logf("first %d B, thirty %d B (%.2f×), exports %d B (%.1f× smaller), unchanged +%d B, pruned %d B, fresh %d B (%.2f×)",
		afterFirst, afterThirty, ratio(afterThirty, afterFirst), exports, ratio(exports, afterThirty),
		afterUnchanged-afterThirty, pruned, fresh, ratio(pruned, fresh))
	assert.LessOrEqual(t, ratio(afterThirty, afterFirst), 1.5)
	assert.GreaterOrEqual(t, ratio(exports, afterThirty), 20.0)
	assert.Zero(t, unchanged.Changes())
	assert.Less(t, afterUnchanged-afterThirty, int64(4096))
	assert.LessOrEqual(t, ratio(pruned, fresh), 1.2)
}

// freshStoreOfTheLast replays the same thirty days in a store that only
// snapshots the last kept of them, plus the unchanged one after them.
func freshStoreOfTheLast(t *testing.T, items, days, kept int) int64 {
	t.Helper()
	churn := newChurningContainer(t, t.TempDir(), items)
	for day := 1; day < days; day++ {
		churn.day(0.01)
		if day >= days-kept+1 {
			churn.take()
		}
	}
	churn.take()
	return churn.onDisk()
}

func ratio(a, b int64) float64 { return float64(a) / float64(b) }

// BenchmarkSnapshotBytes reports what a snapshot costs on disk per item,
// and what each later one costs per item changed.
func BenchmarkSnapshotBytes(b *testing.B) {
	for b.Loop() {
		churn := newChurningContainer(b, b.TempDir(), 2000)
		churn.take()
		first := churn.onDisk()
		for range 5 {
			churn.day(0.05)
			churn.take()
		}
		b.ReportMetric(float64(first)/2000, "B/item")
		b.ReportMetric(float64(churn.onDisk()-first)/(5*100), "B/changed-item")
	}
}
