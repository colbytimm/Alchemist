package mock_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

func connect(t *testing.T, opts ...mock.Option) adapter.Connection {
	t.Helper()
	conn, err := mock.New(opts...).Connect(context.Background(), nil)
	require.NoError(t, err)
	return conn
}

func TestCatalogShape(t *testing.T) {
	catalog := connect(t).Catalog()
	ctx := context.Background()

	roots, err := catalog.Root(ctx)
	require.NoError(t, err)
	require.Len(t, roots, 2)
	assert.Equal(t, "sales", roots[0].Name)
	assert.Equal(t, "telemetry", roots[1].Name)
	for _, db := range roots {
		assert.Equal(t, adapter.NodeDatabase, db.Kind)
		assert.True(t, db.HasChildren)
	}

	containers, err := catalog.Children(ctx, roots[0])
	require.NoError(t, err)
	require.Len(t, containers, 2)
	assert.Equal(t, "orders", containers[0].Name)
	assert.Equal(t, "/customerId", containers[0].Meta[adapter.MetaPartitionKey])
	assert.Equal(t, "customers", containers[1].Name)
	assert.Equal(t, "/region", containers[1].Meta[adapter.MetaPartitionKey])
	assert.Equal(t, []string{"sales", "orders"}, containers[0].Path)

	leaves, err := catalog.Children(ctx, containers[0])
	require.NoError(t, err)
	require.Len(t, leaves, 2)
	assert.Equal(t, adapter.NodeField, leaves[0].Kind)
	assert.Equal(t, adapter.MetaPartitionKey, leaves[0].Name)
	assert.Equal(t, "/customerId", leaves[1].Name)
	assert.Equal(t, []string{"sales", "orders", "partitionKey", "/customerId"}, leaves[1].Path)

	none, err := catalog.Children(ctx, leaves[0])
	require.NoError(t, err)
	assert.Empty(t, none)

	_, err = catalog.Children(ctx, adapter.Node{Kind: adapter.NodeDatabase, Name: "nope"})
	require.Error(t, err)
}

func TestCursorPaging(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()

	cursor, err := conn.Query(ctx, adapter.Query{Text: "SELECT * FROM c", Scope: []string{"sales", "orders"}})
	require.NoError(t, err)

	for page := 1; page <= 3; page++ {
		require.True(t, cursor.HasMore(), "page %d should be available", page)
		p, err := cursor.NextPage(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"id", "pk", "amount", "note"}, p.Columns)
		assert.Len(t, p.Rows, 10)
		assert.Len(t, p.Raw, 10)
		assert.InDelta(t, 2.5, p.Stats.RequestCharge, 0.001)
		assert.Equal(t, 10, p.Stats.RowCount)
		assert.Equal(t, page < 3, cursor.HasMore(), "page %d", page)
		for _, raw := range p.Raw {
			assert.True(t, json.Valid(raw))
		}
	}
	assert.False(t, cursor.HasMore())
	_, err = cursor.NextPage(ctx)
	require.Error(t, err)
	require.NoError(t, cursor.Close())
}

func TestWithPages(t *testing.T) {
	conn := connect(t, mock.WithPages(1))
	cursor, err := conn.Query(context.Background(), adapter.Query{Text: "SELECT * FROM c"})
	require.NoError(t, err)
	_, err = cursor.NextPage(context.Background())
	require.NoError(t, err)
	assert.False(t, cursor.HasMore())
}

func requireInjected(t *testing.T, err error, op string) {
	t.Helper()
	var injected *mock.InjectedError
	require.ErrorAs(t, err, &injected)
	assert.Equal(t, op, injected.Op)
}

func TestInjectedErrors(t *testing.T) {
	ctx := context.Background()

	_, err := mock.New(mock.WithError(mock.OpConnect)).Connect(ctx, nil)
	requireInjected(t, err, mock.OpConnect)

	requireInjected(t, connect(t, mock.WithError(mock.OpPing)).Ping(ctx), mock.OpPing)

	_, err = connect(t, mock.WithError(mock.OpQuery)).Query(ctx, adapter.Query{})
	requireInjected(t, err, mock.OpQuery)

	cursor, err := connect(t, mock.WithError(mock.OpNextPage)).Query(ctx, adapter.Query{})
	require.NoError(t, err)
	_, err = cursor.NextPage(ctx)
	requireInjected(t, err, mock.OpNextPage)

	catalog := connect(t, mock.WithError(mock.OpRoot), mock.WithError(mock.OpChildren)).Catalog()
	_, err = catalog.Root(ctx)
	requireInjected(t, err, mock.OpRoot)
	_, err = catalog.Children(ctx, adapter.Node{Kind: adapter.NodeDatabase, Name: "sales"})
	requireInjected(t, err, mock.OpChildren)
}

func TestLatencyRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mock.New(mock.WithLatency(time.Minute)).Connect(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPingAndCloseSucceed(t *testing.T) {
	conn := connect(t)
	require.NoError(t, conn.Ping(context.Background()))
	require.NoError(t, conn.Close())
	assert.Equal(t, mock.Name, mock.New().Name())
}
