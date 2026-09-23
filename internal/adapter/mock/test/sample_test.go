package mock_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

func sampler(t *testing.T, conn adapter.Connection) adapter.FieldSampler {
	t.Helper()
	s, ok := conn.(adapter.FieldSampler)
	require.True(t, ok, "a mock connection samples its containers")
	return s
}

func orders() adapter.Node {
	return adapter.Node{Kind: adapter.NodeContainer, Name: "orders", Path: []string{"sales", "orders"}}
}

func TestSampleFieldsFindsTheFieldsAQueryWouldShow(t *testing.T) {
	sample, err := sampler(t, connect(t)).SampleFields(context.Background(), orders())
	require.NoError(t, err)

	assert.Equal(t, []adapter.Field{
		{Path: "id", Kind: "string"}, {Path: "pk", Kind: "string"},
		{Path: "amount", Kind: "number"}, {Path: "note", Kind: "string"},
	}, sample.Fields)
	assert.Equal(t, 2.5, sample.Stats.RequestCharge)
}

func TestSampleFieldsRefusesWhatIsNotAContainer(t *testing.T) {
	conn := connect(t)

	_, err := sampler(t, conn).SampleFields(context.Background(), adapter.Node{Kind: adapter.NodeDatabase, Name: "sales", Path: []string{"sales"}})
	require.ErrorIs(t, err, adapter.ErrUnsupported)

	_, err = sampler(t, conn).SampleFields(context.Background(), adapter.Node{Kind: adapter.NodeContainer, Name: "x", Path: []string{"sales", "x"}})
	require.ErrorContains(t, err, "no such container")
}

func TestSampleFieldsCanBeMadeToFail(t *testing.T) {
	_, err := sampler(t, connect(t, mock.WithError(mock.OpSampleFields))).SampleFields(context.Background(), orders())

	var injected *mock.InjectedError
	require.ErrorAs(t, err, &injected)
	assert.Equal(t, mock.OpSampleFields, injected.Op)
}
