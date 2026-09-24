package mock_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

func admin(t *testing.T, conn adapter.Connection) adapter.CatalogAdmin {
	t.Helper()
	a, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok, "a mock connection manages its catalog")
	return a
}

func editor(t *testing.T, conn adapter.Connection) adapter.ThroughputEditor {
	t.Helper()
	e, ok := conn.(adapter.ThroughputEditor)
	require.True(t, ok, "a mock connection edits throughput")
	return e
}

func names(nodes []adapter.Node) []string {
	listed := make([]string, 0, len(nodes))
	for _, node := range nodes {
		listed = append(listed, node.Name)
	}
	return listed
}

func databaseNames(t *testing.T, conn adapter.Connection) []string {
	t.Helper()
	roots, err := conn.Catalog().Root(context.Background())
	require.NoError(t, err)
	return names(roots)
}

func containers(t *testing.T, conn adapter.Connection, database string) []adapter.Node {
	t.Helper()
	nodes, err := conn.Catalog().Children(context.Background(), adapter.Node{
		Kind: adapter.NodeDatabase,
		Name: database,
		Path: []string{database},
	})
	require.NoError(t, err)
	return nodes
}

func TestCreatedDatabaseIsServedByTheNextRoot(t *testing.T) {
	conn := connect(t)

	require.NoError(t, admin(t, conn).CreateDatabase(context.Background(), adapter.DatabaseSpec{Name: "hr"}))

	assert.Equal(t, []string{"sales", "telemetry", "hr"}, databaseNames(t, conn))
}

func TestDeletedDatabaseIsGoneFromTheNextRoot(t *testing.T) {
	conn := connect(t)

	require.NoError(t, admin(t, conn).DeleteDatabase(context.Background(), "sales"))

	assert.Equal(t, []string{"telemetry"}, databaseNames(t, conn))
}

func TestCreatedContainerIsServedByTheNextChildren(t *testing.T) {
	conn := connect(t)
	spec := adapter.ContainerSpec{
		Database:      "sales",
		Name:          "shipments",
		PartitionKeys: []string{"/tenantId", "/customerId"},
	}

	require.NoError(t, admin(t, conn).CreateContainer(context.Background(), spec))

	nodes := containers(t, conn, "sales")
	require.Equal(t, []string{"orders", "customers", "shipments"}, names(nodes))
	assert.Equal(t, "/tenantId,/customerId", nodes[2].Meta[adapter.MetaPartitionKey],
		"a hierarchical key keeps every path")
}

func TestDeletedContainerIsGoneFromTheNextChildren(t *testing.T) {
	conn := connect(t)

	require.NoError(t, admin(t, conn).DeleteContainer(context.Background(), []string{"sales", "orders"}))

	assert.Equal(t, []string{"customers"}, names(containers(t, conn, "sales")))
}

func TestAdaptersDoNotSeeEachOthersMutations(t *testing.T) {
	changed, untouched := connect(t), connect(t)

	require.NoError(t, admin(t, changed).DeleteDatabase(context.Background(), "sales"))

	assert.Equal(t, []string{"sales", "telemetry"}, databaseNames(t, untouched))
}

func TestManagementRefusals(t *testing.T) {
	tests := []struct {
		name string
		run  func(adapter.CatalogAdmin) error
		want string
	}{
		{
			name: "duplicate database",
			run: func(a adapter.CatalogAdmin) error {
				return a.CreateDatabase(context.Background(), adapter.DatabaseSpec{Name: "sales"})
			},
			want: `mock: create database "sales": already exists`,
		},
		{
			name: "unknown database to delete",
			run: func(a adapter.CatalogAdmin) error {
				return a.DeleteDatabase(context.Background(), "nowhere")
			},
			want: `mock: delete database "nowhere": no such database`,
		},
		{
			name: "container in an unknown database",
			run: func(a adapter.CatalogAdmin) error {
				return a.CreateContainer(context.Background(), adapter.ContainerSpec{Database: "nowhere", Name: "x"})
			},
			want: "mock: create container nowhere.x: no such database",
		},
		{
			name: "duplicate container",
			run: func(a adapter.CatalogAdmin) error {
				return a.CreateContainer(context.Background(), adapter.ContainerSpec{Database: "sales", Name: "orders"})
			},
			want: "mock: create container sales.orders: already exists",
		},
		{
			name: "unknown container to delete",
			run: func(a adapter.CatalogAdmin) error {
				return a.DeleteContainer(context.Background(), []string{"sales", "nowhere"})
			},
			want: "mock: delete container sales.nowhere: no such container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(admin(t, connect(t)))

			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

func TestThroughputRoundTrips(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want adapter.Throughput
	}{
		{
			name: "manual on a container",
			path: []string{"sales", "orders"},
			want: adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 1000},
		},
		{
			name: "autoscale on a container",
			path: []string{"sales", "orders"},
			want: adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 4000},
		},
		{
			name: "autoscale on a database",
			path: []string{"sales"},
			want: adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 8000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			throughput := editor(t, connect(t))
			require.NoError(t, throughput.SetThroughput(context.Background(), tt.path, tt.want))

			got, err := throughput.Throughput(context.Background(), tt.path)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSeededContainersProvisionManualThroughput(t *testing.T) {
	got, err := editor(t, connect(t)).Throughput(context.Background(), []string{"sales", "orders"})

	require.NoError(t, err)
	assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400}, got)
}

// A mode nothing can provision is refused before the call, which the injected
// failure on that very operation proves: it never runs.
func TestSetThroughputRefusesAModeNothingProvisions(t *testing.T) {
	tests := []struct {
		name string
		mode adapter.ThroughputMode
		want string
	}{
		{
			name: "shared",
			mode: adapter.ThroughputShared,
			want: "mock: set throughput sales.orders: shared capacity is provisioned on the database: unsupported",
		},
		{
			name: "nothing to provision",
			mode: adapter.ThroughputNone,
			want: "mock: set throughput sales.orders: no capacity to provision: unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			throughput := editor(t, connect(t, mock.WithError(mock.OpSetThroughput)))

			err := throughput.SetThroughput(context.Background(), []string{"sales", "orders"},
				adapter.Throughput{Mode: tt.mode, RUs: 400})

			require.ErrorIs(t, err, adapter.ErrUnsupported)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

func TestInjectedManagementErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		op   string
		run  func(adapter.Connection) error
	}{
		{
			name: "create database",
			op:   mock.OpCreateDatabase,
			run: func(c adapter.Connection) error {
				return admin(t, c).CreateDatabase(ctx, adapter.DatabaseSpec{Name: "hr"})
			},
		},
		{
			name: "delete database",
			op:   mock.OpDeleteDatabase,
			run:  func(c adapter.Connection) error { return admin(t, c).DeleteDatabase(ctx, "sales") },
		},
		{
			name: "create container",
			op:   mock.OpCreateContainer,
			run: func(c adapter.Connection) error {
				return admin(t, c).CreateContainer(ctx, adapter.ContainerSpec{Database: "sales", Name: "shipments"})
			},
		},
		{
			name: "delete container",
			op:   mock.OpDeleteContainer,
			run: func(c adapter.Connection) error {
				return admin(t, c).DeleteContainer(ctx, []string{"sales", "orders"})
			},
		},
		{
			name: "read throughput",
			op:   mock.OpThroughput,
			run: func(c adapter.Connection) error {
				_, err := editor(t, c).Throughput(ctx, []string{"sales", "orders"})
				return err
			},
		},
		{
			name: "set throughput",
			op:   mock.OpSetThroughput,
			run: func(c adapter.Connection) error {
				return editor(t, c).SetThroughput(ctx, []string{"sales", "orders"},
					adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireInjected(t, tt.run(connect(t, mock.WithError(tt.op))), tt.op)
		})
	}
}
