package mock_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

func inspector(t *testing.T, conn adapter.Connection) adapter.Inspector {
	t.Helper()
	i, ok := conn.(adapter.Inspector)
	require.True(t, ok, "a mock connection inspects its catalog")
	return i
}

func inspect(t *testing.T, conn adapter.Connection, kind adapter.NodeKind, path ...string) adapter.Details {
	t.Helper()
	details, err := inspector(t, conn).Inspect(context.Background(), adapter.Node{
		Kind: kind,
		Name: path[len(path)-1],
		Path: path,
	})
	require.NoError(t, err)
	return details
}

func titles(details adapter.Details) []string {
	listed := make([]string, 0, len(details.Sections))
	for _, section := range details.Sections {
		listed = append(listed, section.Title)
	}
	return listed
}

func section(t *testing.T, details adapter.Details, title string) adapter.Section {
	t.Helper()
	for _, section := range details.Sections {
		if section.Title == title {
			return section
		}
	}
	require.Failf(t, "section missing", "no section titled %q among %v", title, titles(details))
	return adapter.Section{}
}

func TestContainerDetailsReadInAStableOrder(t *testing.T) {
	details := inspect(t, connect(t), adapter.NodeContainer, "sales", "orders")

	assert.Equal(t, []string{"Identity", "Partition key", "Throughput", "Storage", "Indexing"}, titles(details))
	assert.Equal(t, []adapter.Property{
		{Name: "Database", Value: "sales"},
		{Name: "Container", Value: "orders"},
	}, section(t, details, "Identity").Properties)
	assert.Equal(t, []adapter.Property{{Name: "Paths", Value: "/customerId"}},
		section(t, details, "Partition key").Properties)
	assert.Equal(t, []adapter.Property{
		{Name: "Mode", Value: "manual"},
		{Name: "Rate", Value: "400 RU/s"},
	}, section(t, details, "Throughput").Properties)
	assert.Equal(t, []adapter.Property{
		{Name: "Documents", Value: "1284"},
		{Name: "Documents size", Value: "4.2 MB"},
	}, section(t, details, "Storage").Properties)
	assert.True(t, json.Valid(details.Raw))
}

func TestDatabaseDetailsReadInAStableOrder(t *testing.T) {
	details := inspect(t, connect(t), adapter.NodeDatabase, "telemetry")

	assert.Equal(t, []string{"Identity", "Throughput", "Containers"}, titles(details))
	assert.Equal(t, []adapter.Property{
		{Name: "Mode", Value: "manual"},
		{Name: "Rate", Value: "1000 RU/s"},
	}, section(t, details, "Throughput").Properties)
	assert.Equal(t, []adapter.Property{{Name: "Count", Value: "3"}}, section(t, details, "Containers").Properties)
}

func TestASharedContainerExplainsItsThroughputInANote(t *testing.T) {
	details := inspect(t, connect(t), adapter.NodeContainer, "telemetry", "events")

	throughput := section(t, details, "Throughput")
	assert.Empty(t, throughput.Properties)
	assert.Equal(t, "Inherited from database telemetry; this container has no throughput of its own.", throughput.Note)
}

func TestADatabaseWithNothingProvisionedSaysSo(t *testing.T) {
	details := inspect(t, connect(t), adapter.NodeDatabase, "sales")

	throughput := section(t, details, "Throughput")
	assert.Empty(t, throughput.Properties)
	assert.Contains(t, throughput.Note, "Nothing is provisioned")
}

func TestUnreadableStorageBecomesANoteNotANumber(t *testing.T) {
	details := inspect(t, connect(t), adapter.NodeContainer, "telemetry", "devices")

	storage := section(t, details, "Storage")
	assert.Empty(t, storage.Properties)
	assert.Contains(t, storage.Note, "could not read")
}

func TestACreatedContainerReportsEmptyStorage(t *testing.T) {
	conn := connect(t)
	require.NoError(t, admin(t, conn).CreateContainer(context.Background(), adapter.ContainerSpec{
		Database: "sales", Name: "shipments", PartitionKeys: []string{"/tenantId"},
	}))

	details := inspect(t, conn, adapter.NodeContainer, "sales", "shipments")

	assert.Equal(t, []adapter.Property{
		{Name: "Documents", Value: "0"},
		{Name: "Documents size", Value: "0 B"},
	}, section(t, details, "Storage").Properties)
}

func TestInspectRefusals(t *testing.T) {
	tests := []struct {
		name string
		node adapter.Node
		want string
	}{
		{
			name: "unknown database",
			node: adapter.Node{Kind: adapter.NodeDatabase, Name: "nowhere", Path: []string{"nowhere"}},
			want: "mock: inspect nowhere: no such database",
		},
		{
			name: "unknown container",
			node: adapter.Node{Kind: adapter.NodeContainer, Name: "nowhere", Path: []string{"sales", "nowhere"}},
			want: "mock: inspect sales.nowhere: no such container",
		},
		{
			name: "field node",
			node: adapter.Node{Kind: adapter.NodeField, Name: "/pk", Path: []string{"sales", "orders", "partitionKey", "/pk"}},
			want: "mock: inspect sales.orders.partitionKey./pk: field node has no details: unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := inspector(t, connect(t)).Inspect(context.Background(), tt.node)

			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

func TestInjectedInspectError(t *testing.T) {
	_, err := inspector(t, connect(t, mock.WithError(mock.OpInspect))).Inspect(context.Background(),
		adapter.Node{Kind: adapter.NodeContainer, Name: "orders", Path: []string{"sales", "orders"}})

	requireInjected(t, err, mock.OpInspect)
}
