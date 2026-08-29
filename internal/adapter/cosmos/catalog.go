package cosmos

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Compile-time contract check.
var _ adapter.Catalog = (*catalog)(nil)

// catalog is the lazy database/container tree of one account.
type catalog struct {
	client *azcosmos.Client
}

// Root lists the account's databases.
func (t *catalog) Root(ctx context.Context) ([]adapter.Node, error) {
	pager := t.client.NewQueryDatabasesPager("select * from dbs d", nil)
	var nodes []adapter.Node
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("cosmos: list databases: %w", err)
		}
		for _, db := range page.Databases {
			nodes = append(nodes, adapter.Node{
				Kind:        "database",
				Name:        db.ID,
				Path:        []string{db.ID},
				HasChildren: true,
			})
		}
	}
	return nodes, nil
}

// Children expands a database into its containers, and a container into its
// metadata leaves. Field nodes have no children.
func (t *catalog) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	switch n.Kind {
	case "database":
		return t.containers(ctx, n)
	case "container":
		return []adapter.Node{{
			Kind: "field",
			Name: "partitionKey " + n.Meta["partitionKey"],
			Path: append(append([]string{}, n.Path...), "partitionKey"),
		}}, nil
	default:
		return nil, nil
	}
}

// containers lists one database's containers with partition key metadata.
func (t *catalog) containers(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	db, err := t.client.NewDatabase(n.Name)
	if err != nil {
		return nil, fmt.Errorf("cosmos: open database %q: %w", n.Name, err)
	}
	pager := db.NewQueryContainersPager("select * from containers c", nil)
	var nodes []adapter.Node
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("cosmos: list containers of %q: %w", n.Name, err)
		}
		for _, props := range page.Containers {
			nodes = append(nodes, adapter.Node{
				Kind:        "container",
				Name:        props.ID,
				Path:        []string{n.Name, props.ID},
				Meta:        map[string]string{"partitionKey": strings.Join(props.PartitionKeyDefinition.Paths, ",")},
				HasChildren: true,
			})
		}
	}
	return nodes, nil
}
