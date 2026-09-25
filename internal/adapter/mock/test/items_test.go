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

// steppedClock tells the time a test set.
type steppedClock struct {
	now time.Time
}

func (c *steppedClock) Now() time.Time { return c.now }

func TestPutItemWritesANewVersionStampedFromTheClock(t *testing.T) {
	clock := &steppedClock{now: time.Unix(1_800_000_000, 0)}
	a := mock.New(mock.WithClock(clock.Now), mock.WithItems(ordersPath, order("o1", "c01")))
	_, before, err := adapter.SplitSystemFields(a.Items(ordersPath)[0])
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Minute)

	require.NoError(t, a.PutItem(ordersPath, order("o1", "c01")))

	items := a.Items(ordersPath)
	require.Len(t, items, 1)
	_, after, err := adapter.SplitSystemFields(items[0])
	require.NoError(t, err)
	assert.NotEqual(t, before.Version, after.Version)
	assert.Equal(t, time.Unix(1_800_000_060, 0).UTC(), after.Modified)
}

func TestDeleteItemRemovesTheItemUnderItsKey(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01"), order("o1", "c02")))

	require.NoError(t, a.DeleteItem(ordersPath, "o1", adapter.PartitionKey{json.RawMessage(`"c01"`)}))

	assert.Len(t, a.Items(ordersPath), 1)
	assert.Error(t, a.DeleteItem(ordersPath, "o1", adapter.PartitionKey{json.RawMessage(`"c01"`)}))
}

func TestASinceScanKeepsWhatWasWrittenFromThatSecondOn(t *testing.T) {
	clock := &steppedClock{now: time.Unix(1_800_000_000, 0)}
	a := mock.New(mock.WithClock(clock.Now), mock.WithItems(ordersPath, order("o1", "c01"), order("o2", "c01")))
	clock.now = clock.now.Add(time.Second)
	require.NoError(t, a.PutItem(ordersPath, order("o3", "c01")))
	clock.now = clock.now.Add(time.Second)
	require.NoError(t, a.PutItem(ordersPath, order("o1", "c01")))

	_, got := scanAll(t, connectTo(t, a), adapter.ScanRequest{Container: ordersPath, PageSize: 1, Since: time.Unix(1_800_000_001, 500)})

	assert.Equal(t, []string{"o1", "o3"}, got)
}

func TestAnIdentityScanKeepsTheIdKeyAndSystemFields(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath,
		json.RawMessage(`{"id":"o1","customerId":"c01","total":5,"lines":[1,2]}`),
		json.RawMessage(`{"id":"o2","total":6}`)))

	pages, _ := scanAll(t, connectTo(t, a), adapter.ScanRequest{Container: ordersPath, Projection: adapter.ScanIdentity})

	require.Len(t, pages[0].Items, 2)
	body, meta, err := adapter.SplitSystemFields(pages[0].Items[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"o1","customerId":"c01"}`, string(body))
	assert.NotEmpty(t, meta.Version)
	assert.False(t, meta.Modified.IsZero())
	body, _, err = adapter.SplitSystemFields(pages[0].Items[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"o2"}`, string(body), "a key value the item lacks is absent")
}

func TestAnIdentityScanKeepsANestedKeyNested(t *testing.T) {
	a := mock.New()
	path := []string{"sales", "shipments"}
	require.NoError(t, admin(t, connectTo(t, a)).CreateContainer(context.Background(),
		adapter.ContainerSpec{Database: "sales", Name: "shipments", PartitionKeys: []string{"/shipTo/region"}}))
	require.NoError(t, a.PutItem(path, json.RawMessage(`{"id":"s1","shipTo":{"region":"east","city":"Oslo"},"weight":3}`)))

	pages, _ := scanAll(t, connectTo(t, a), adapter.ScanRequest{Container: path, Projection: adapter.ScanIdentity})

	body, _, err := adapter.SplitSystemFields(pages[0].Items[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"s1","shipTo":{"region":"east"}}`, string(body))
}
