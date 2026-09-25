package cosmos_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// ordersDefinition is sales.orders as the service describes it, with the
// identity fields it assigns and the account-bound policies a portable read
// drops.
const ordersDefinition = `{"id":"orders","_rid":"abc=","_self":"dbs/abc=/colls/def=/","_etag":"\"0000\"","_ts":1700000000,` +
	`"partitionKey":{"kind":"MultiHash","paths":["/tenantId","/userId"],"version":2},` +
	`"indexingPolicy":{"automatic":true,"indexingMode":"consistent","includedPaths":[{"path":"/*"}],` +
	`"vectorIndexes":[{"path":"/embedding","type":"flat"}]},` +
	`"defaultTtl":3600,"analyticalStorageTtl":-1,` +
	`"vectorEmbeddingPolicy":{"vectorEmbeddings":[{"path":"/embedding","dataType":"float32","dimensions":3,"distanceFunction":"cosine"}]}}`

// cloneAccount stands in for an account holding sales.orders: it answers
// the container read, pages a query by continuation token, and keeps every
// write it is sent.
type cloneAccount struct {
	usage string
	// writeStatus answers every upsert and create; zero is success.
	writeStatus int
	retryAfter  string

	mu       sync.Mutex
	writes   []capturedWrite
	resumeAt []string
}

type capturedWrite struct {
	path         string
	body         []byte
	partitionKey string
}

func (a *cloneAccount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		_, _ = io.WriteString(w, `{}`)
	case r.Method == http.MethodGet && r.URL.Path == "/dbs/sales/colls/orders":
		w.Header().Set("x-ms-resource-usage", a.usage)
		_, _ = io.WriteString(w, ordersDefinition)
	case r.Header.Get("Content-Type") == "application/query+json":
		a.page(w, r)
	case r.Method == http.MethodPost:
		a.write(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (a *cloneAccount) page(w http.ResponseWriter, r *http.Request) {
	continuation := r.Header.Get("x-ms-continuation")
	a.mu.Lock()
	a.resumeAt = append(a.resumeAt, continuation)
	a.mu.Unlock()
	w.Header().Set("x-ms-request-charge", "2.5")
	if continuation == "" {
		w.Header().Set("x-ms-continuation", "page-2")
		_, _ = io.WriteString(w, `{"Documents":[{"id":"a","_etag":"\"1\""},{"id":"b"}]}`)
		return
	}
	_, _ = io.WriteString(w, `{"Documents":[{"id":"c","n":12345678901234567890}]}`)
}

func (a *cloneAccount) write(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.writes = append(a.writes, capturedWrite{path: r.URL.Path, body: body, partitionKey: r.Header.Get("x-ms-documentdb-partitionkey")})
	a.mu.Unlock()
	w.Header().Set("x-ms-request-charge", "7.25")
	if a.retryAfter != "" {
		w.Header().Set("x-ms-retry-after-ms", a.retryAfter)
	}
	if a.writeStatus != 0 {
		w.WriteHeader(a.writeStatus)
		_, _ = io.WriteString(w, `{"message":"refused"}`)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, `{"id":"x"}`)
}

func (a *cloneAccount) sent() []capturedWrite {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]capturedWrite(nil), a.writes...)
}

func (a *cloneAccount) continuations() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.resumeAt...)
}

func definitionReader(t *testing.T, conn adapter.Connection) adapter.DefinitionReader {
	t.Helper()
	d, ok := conn.(adapter.DefinitionReader)
	require.True(t, ok, "a cosmos connection reads definitions")
	return d
}

func admin(t *testing.T, conn adapter.Connection) adapter.CatalogAdmin {
	t.Helper()
	a, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok, "a cosmos connection manages its catalog")
	return a
}

func TestADefinitionKeepsWhatAContainerIsMadeWith(t *testing.T) {
	conn, _ := account(t, &cloneAccount{usage: "documentsCount=30112;documentsSize=42803;collectionSize=50000"})

	def, err := definitionReader(t, conn).ContainerDefinition(context.Background(), []string{"sales", "orders"}, adapter.DefinitionFull)

	require.NoError(t, err)
	assert.Equal(t, []string{"/tenantId", "/userId"}, def.PartitionKeys)
	assert.Equal(t, adapter.SizeEstimate{Items: 30112, Bytes: 42803 * 1024, Known: true}, def.Size)
	assert.Equal(t, cosmos.Name, def.Policies.Backend)
	raw := string(def.Policies.Raw)
	assert.Contains(t, raw, `"kind":"MultiHash"`)
	assert.Contains(t, raw, `"defaultTtl":3600`)
	assert.Contains(t, raw, "vectorEmbeddingPolicy")
	for _, identity := range []string{"abc=", `"_self"`, `"_etag"`, `"_ts"`, `"orders"`} {
		assert.NotContains(t, raw, identity)
	}
}

func TestAPortableDefinitionDropsAccountBoundPolicies(t *testing.T) {
	conn, _ := account(t, &cloneAccount{})

	def, err := definitionReader(t, conn).ContainerDefinition(context.Background(), []string{"sales", "orders"}, adapter.DefinitionPortable)

	require.NoError(t, err)
	raw := string(def.Policies.Raw)
	for _, dropped := range []string{"vectorEmbeddingPolicy", "vectorIndexes", "analyticalStorageTtl"} {
		assert.NotContains(t, raw, dropped)
	}
	assert.Contains(t, raw, `"defaultTtl":3600`)
	assert.Contains(t, raw, "indexingPolicy")
	assert.False(t, def.Size.Known, "no usage header is no size")
}

func TestCreateStartsFromThePolicies(t *testing.T) {
	acct := &cloneAccount{}
	conn, _ := account(t, acct)
	def, err := definitionReader(t, conn).ContainerDefinition(context.Background(), []string{"sales", "orders"}, adapter.DefinitionFull)
	require.NoError(t, err)

	err = admin(t, conn).CreateContainer(context.Background(), adapter.ContainerSpec{
		Database: "sales", Name: "orders-copy", PartitionKeys: def.PartitionKeys, Policies: def.Policies,
	})

	require.NoError(t, err)
	writes := acct.sent()
	require.Len(t, writes, 1)
	var created map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(writes[0].body, &created))
	assert.JSONEq(t, `"orders-copy"`, string(created["id"]))
	assert.JSONEq(t, `{"kind":"MultiHash","paths":["/tenantId","/userId"],"version":2}`, string(created["partitionKey"]))
	assert.JSONEq(t, `3600`, string(created["defaultTtl"]))
	assert.NotContains(t, created, "_rid")
}

func TestCreateRefusesAnotherBackendsPolicies(t *testing.T) {
	acct := &cloneAccount{}
	conn, _ := account(t, acct)

	err := admin(t, conn).CreateContainer(context.Background(), adapter.ContainerSpec{
		Database: "sales", Name: "copy", PartitionKeys: []string{"/pk"},
		Policies: adapter.PolicyDocument{Backend: "mock", Raw: json.RawMessage(`{}`)},
	})

	require.ErrorIs(t, err, adapter.ErrUnsupported)
	assert.Empty(t, acct.sent())
}

func TestAConflictIsAlreadyExists(t *testing.T) {
	conn, _ := account(t, &cloneAccount{writeStatus: http.StatusConflict})

	err := admin(t, conn).CreateContainer(context.Background(), adapter.ContainerSpec{Database: "sales", Name: "orders", PartitionKeys: []string{"/pk"}})

	require.ErrorIs(t, err, adapter.ErrAlreadyExists)
	assert.Equal(t, `cosmos: create container sales.orders: 409 Conflict: refused`, err.Error())
}

func TestAScanPagesByContinuationToken(t *testing.T) {
	acct := &cloneAccount{}
	conn, _ := account(t, acct)
	scanner, ok := conn.(adapter.ItemScanner)
	require.True(t, ok)

	scan, err := scanner.ScanItems(context.Background(), adapter.ScanRequest{Container: []string{"sales", "orders"}})
	require.NoError(t, err)
	first, err := scan.NextPage(context.Background())
	require.NoError(t, err)
	resumed, err := scanner.ScanItems(context.Background(), adapter.ScanRequest{Container: []string{"sales", "orders"}, From: first.Next})
	require.NoError(t, err)
	second, err := resumed.NextPage(context.Background())
	require.NoError(t, err)

	assert.Equal(t, adapter.ScanPosition("page-2"), first.Next)
	assert.Len(t, first.Items, 2)
	assert.Contains(t, string(first.Items[0]), `"_etag"`, "items arrive whole")
	assert.InDelta(t, 2.5, first.RequestCharge, 0)
	assert.Empty(t, second.Next)
	assert.False(t, resumed.HasMore())
	assert.Contains(t, string(second.Items[0]), "12345678901234567890", "a number keeps its digits")
	assert.Equal(t, []string{"", "page-2"}, acct.continuations())
}

func itemSink(t *testing.T, conn adapter.Connection) adapter.ItemSink {
	t.Helper()
	writer, ok := conn.(adapter.ItemWriter)
	require.True(t, ok, "a cosmos connection writes items")
	sink, err := writer.OpenItemSink(context.Background(), []string{"sales", "orders"})
	require.NoError(t, err)
	return sink
}

func TestAnUpsertIsSentUnderTheItemsOwnKey(t *testing.T) {
	acct := &cloneAccount{}
	conn, _ := account(t, acct)

	charge, err := itemSink(t, conn).Upsert(context.Background(), json.RawMessage(`{"id":"a","tenantId":"t1","userId":7}`))

	require.NoError(t, err)
	assert.InDelta(t, 7.25, charge, 0)
	writes := acct.sent()
	require.Len(t, writes, 1)
	assert.JSONEq(t, `["t1",7]`, writes[0].partitionKey)
	assert.JSONEq(t, `{"id":"a","tenantId":"t1","userId":7}`, string(writes[0].body))
}

func TestAnItemTheSinkCannotKeyIsRefusedUnsent(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{name: "no value at a key path", item: `{"id":"a","tenantId":"t1"}`},
		{name: "an integer past 2^53", item: `{"id":"a","tenantId":"t1","userId":12345678901234567890}`},
		{name: "an object at a key path", item: `{"id":"a","tenantId":"t1","userId":{"n":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := &cloneAccount{}
			conn, _ := account(t, acct)

			_, err := itemSink(t, conn).Upsert(context.Background(), json.RawMessage(tt.item))

			require.ErrorIs(t, err, adapter.ErrItemRefused)
			assert.Empty(t, acct.sent())
		})
	}
}

func TestAnItemTheServiceRefusesIsRefused(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			conn, _ := account(t, &cloneAccount{writeStatus: status})

			_, err := itemSink(t, conn).Upsert(context.Background(), json.RawMessage(`{"id":"a","tenantId":"t1","userId":7}`))

			require.ErrorIs(t, err, adapter.ErrItemRefused)
		})
	}
}

func TestAThrottleIsAThrottledErrorWithTheServicesDelay(t *testing.T) {
	conn, _ := account(t, &cloneAccount{writeStatus: http.StatusTooManyRequests, retryAfter: "5"})

	_, err := itemSink(t, conn).Upsert(context.Background(), json.RawMessage(`{"id":"a","tenantId":"t1","userId":7}`))

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, err, &throttled)
	assert.Equal(t, 5*time.Millisecond, throttled.RetryAfter)
	assert.False(t, errors.Is(err, adapter.ErrItemRefused), "a throttle says nothing about the item")
	assert.True(t, strings.HasPrefix(err.Error(), "cosmos: upsert into sales.orders: 429"), err.Error())
}
