package sample_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/sample"
)

// seededContainers is what the sample holds, in the order Seed reports it.
var seededContainers = []sample.Seeded{
	{Database: "sales", Container: "customers", PartitionKey: "/region", Items: 13},
	{Database: "sales", Container: "orders", PartitionKey: "/customerId", Items: 60},
	{Database: "sales", Container: "archive", PartitionKey: "/customerId", Items: 20},
	{Database: "sales", Container: "products", PartitionKey: "/category", Items: 15},
	{Database: "telemetry", Container: "devices", PartitionKey: "/site", Items: 10},
	{Database: "telemetry", Container: "events", PartitionKey: "/deviceId", Items: 300},
	{Database: "telemetry", Container: "alerts", PartitionKey: "/severity", Items: 25},
	{Database: "hr", Container: "departments", PartitionKey: "/site", Items: 4},
	{Database: "hr", Container: "employees", PartitionKey: "/site", Items: 18},
}

type account struct {
	mock   *mock.Adapter
	target sample.Target
}

// newAccount connects a mock adapter and drops the databases its fixture
// starts with, leaving only the ones named in keep.
func newAccount(t *testing.T, keep []string, opts ...mock.Option) account {
	t.Helper()
	a := mock.New(opts...)
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	admin, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok)
	writer, ok := conn.(adapter.ItemWriter)
	require.True(t, ok)
	target := sample.Target{Catalog: conn.Catalog(), Admin: admin, Writer: writer}
	for _, name := range databaseNames(t, target) {
		if !slices.Contains(keep, name) {
			require.NoError(t, admin.DeleteDatabase(context.Background(), name))
		}
	}
	return account{mock: a, target: target}
}

func databaseNames(t *testing.T, target sample.Target) []string {
	t.Helper()
	nodes, err := target.Catalog.Root(context.Background())
	require.NoError(t, err)
	var names []string
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names
}

func collect(reports *[]sample.Seeded) func(sample.Seeded) {
	return func(s sample.Seeded) { *reports = append(*reports, s) }
}

func TestNames(t *testing.T) {
	assert.Equal(t, []string{"sales", "telemetry", "hr"}, sample.Names())
}

func TestSeedFillsAnEmptyAccount(t *testing.T) {
	acct := newAccount(t, nil)
	var reports []sample.Seeded

	require.NoError(t, sample.Seed(context.Background(), acct.target, collect(&reports)))

	assert.Equal(t, seededContainers, reports)
	assert.Equal(t, sample.Names(), databaseNames(t, acct.target))
	for _, s := range seededContainers {
		assert.Len(t, acct.mock.Items([]string{s.Database, s.Container}), s.Items, "%s.%s", s.Database, s.Container)
	}
}

func TestSeedCreatesEachContainerOnItsPartitionKey(t *testing.T) {
	acct := newAccount(t, nil)
	require.NoError(t, sample.Seed(context.Background(), acct.target, func(sample.Seeded) {}))

	for _, s := range seededContainers {
		containers, err := acct.target.Catalog.Children(context.Background(),
			adapter.Node{Kind: adapter.NodeDatabase, Name: s.Database, Path: []string{s.Database}})
		require.NoError(t, err)
		var key string
		for _, c := range containers {
			if c.Name == s.Container {
				key = c.Meta[adapter.MetaPartitionKey]
			}
		}
		assert.Equal(t, s.PartitionKey, key, "%s.%s", s.Database, s.Container)
	}
}

func TestSeedRefusesAnAccountHoldingASampleDatabase(t *testing.T) {
	acct := newAccount(t, []string{"sales"})
	before := len(acct.mock.Items([]string{"sales", "orders"}))
	var reports []sample.Seeded

	err := sample.Seed(context.Background(), acct.target, collect(&reports))

	var exists *sample.ExistsError
	require.ErrorAs(t, err, &exists)
	assert.Equal(t, []string{"sales"}, exists.Databases)
	assert.Contains(t, err.Error(), "sales exists")
	assert.Empty(t, reports)
	assert.Equal(t, []string{"sales"}, databaseNames(t, acct.target), "nothing created or dropped")
	assert.Len(t, acct.mock.Items([]string{"sales", "orders"}), before)
}

func TestExistsErrorNamesEveryDatabase(t *testing.T) {
	err := &sample.ExistsError{Databases: []string{"sales", "telemetry", "hr"}}

	assert.Equal(t, "sample: sales, telemetry and hr exist", err.Error())
}

func TestReplaceDropsTheSampleDatabasesOnly(t *testing.T) {
	acct := newAccount(t, []string{"sales"})
	require.NoError(t, acct.target.Admin.CreateDatabase(context.Background(), adapter.DatabaseSpec{Name: "inventory"}))
	var reports []sample.Seeded

	require.NoError(t, sample.Replace(context.Background(), acct.target, collect(&reports)))

	assert.Equal(t, seededContainers, reports)
	assert.ElementsMatch(t, []string{"inventory", "sales", "telemetry", "hr"}, databaseNames(t, acct.target))
	assert.Len(t, acct.mock.Items([]string{"sales", "orders"}), 60, "the fixture's orders were dropped")
}

func TestSeedNamesTheItemAWriteFailedOn(t *testing.T) {
	acct := newAccount(t, nil, mock.WithWriteError("o007"))

	err := sample.Seed(context.Background(), acct.target, func(sample.Seeded) {})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales.orders")
	assert.Contains(t, err.Error(), "o007")
}
