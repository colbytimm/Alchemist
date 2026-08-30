package adapter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

func containerNode(partitionKey string) adapter.Node {
	return adapter.Node{
		Kind:        adapter.NodeContainer,
		Name:        "orders",
		Path:        []string{"sales", "orders"},
		Meta:        map[string]string{adapter.MetaPartitionKey: partitionKey},
		HasChildren: true,
	}
}

func TestPartitionKeyNodesNestEachPathUnderTheLabel(t *testing.T) {
	nodes := adapter.PartitionKeyNodes(containerNode("/id,/vehicleId"))

	require.Len(t, nodes, 3)
	assert.Equal(t, adapter.MetaPartitionKey, nodes[0].Name)
	assert.Equal(t, []string{"sales", "orders", "partitionKey"}, nodes[0].Path)
	assert.Equal(t, "/id", nodes[1].Name)
	assert.Equal(t, []string{"sales", "orders", "partitionKey", "/id"}, nodes[1].Path)
	assert.Equal(t, "/vehicleId", nodes[2].Name)
	assert.Equal(t, []string{"sales", "orders", "partitionKey", "/vehicleId"}, nodes[2].Path)
}

func TestPartitionKeyNodesAreChildlessFields(t *testing.T) {
	for _, node := range adapter.PartitionKeyNodes(containerNode("/id,/vehicleId")) {
		assert.Equal(t, adapter.NodeField, node.Kind, node.Name)
		assert.False(t, node.HasChildren, node.Name)
	}
}

func TestPartitionKeyNodesGiveASinglePathItsOwnRow(t *testing.T) {
	nodes := adapter.PartitionKeyNodes(containerNode("/customerId"))

	require.Len(t, nodes, 2)
	assert.Equal(t, adapter.MetaPartitionKey, nodes[0].Name)
	assert.Equal(t, "/customerId", nodes[1].Name)
}

func TestPartitionKeyNodesAreEmptyWithoutTheMetadata(t *testing.T) {
	bare := adapter.Node{Kind: adapter.NodeContainer, Name: "orders", Path: []string{"sales", "orders"}}

	assert.Empty(t, adapter.PartitionKeyNodes(bare))
}
