package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// emptySize is the storage of a container created during the session.
const emptySize = "0 B"

// Section titles, in the order a container reads.
const (
	sectionIdentity     = "Identity"
	sectionPartitionKey = "Partition key"
	sectionThroughput   = "Throughput"
	sectionStorage      = "Storage"
	sectionIndexing     = "Indexing"
	sectionContainers   = "Containers"
)

// Inspect serves the fixture's metadata for a database or a container.
func (c *conn) Inspect(ctx context.Context, n adapter.Node) (adapter.Details, error) {
	if err := c.a.stall(ctx, OpInspect); err != nil {
		return adapter.Details{}, err
	}
	switch n.Kind {
	case adapter.NodeDatabase:
		return c.a.inspectDatabase(n.Path)
	case adapter.NodeContainer:
		return c.a.inspectContainer(n.Path)
	}
	return adapter.Details{}, fmt.Errorf("mock: inspect %s: %s node has no details: %w", pathText(n.Path), n.Kind, adapter.ErrUnsupported)
}

func (a *Adapter) inspectDatabase(path []string) (adapter.Details, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(path) != 1 {
		return adapter.Details{}, fmt.Errorf("mock: inspect %s: path must name a database", pathText(path))
	}
	i := a.indexOf(path[0])
	if i < 0 {
		return adapter.Details{}, fmt.Errorf("mock: inspect %s: no such database", pathText(path))
	}
	db := a.databases[i]
	raw, err := json.Marshal(map[string]any{"id": db.name})
	if err != nil {
		return adapter.Details{}, fmt.Errorf("mock: inspect %s: %w", pathText(path), err)
	}
	return adapter.Details{
		Sections: []adapter.Section{
			{Title: sectionIdentity, Properties: []adapter.Property{{Name: "Database", Value: db.name}}},
			databaseThroughput(db.throughput),
			{Title: sectionContainers, Properties: []adapter.Property{{Name: "Count", Value: strconv.Itoa(len(db.containers))}}},
		},
		Raw: raw,
	}, nil
}

func (a *Adapter) inspectContainer(path []string) (adapter.Details, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, i, err := a.locateContainer("inspect", path)
	if err != nil {
		return adapter.Details{}, err
	}
	c := a.databases[db].containers[i]
	raw, err := json.Marshal(map[string]any{
		"id":           c.name,
		"partitionKey": map[string]any{"paths": c.partitionKeys},
	})
	if err != nil {
		return adapter.Details{}, fmt.Errorf("mock: inspect %s: %w", pathText(path), err)
	}
	return adapter.Details{
		Sections: []adapter.Section{
			{Title: sectionIdentity, Properties: []adapter.Property{
				{Name: "Database", Value: path[0]},
				{Name: "Container", Value: c.name},
			}},
			{Title: sectionPartitionKey, Properties: []adapter.Property{
				{Name: "Paths", Value: strings.Join(c.partitionKeys, ", ")},
			}},
			containerThroughput(c.throughput, path[0]),
			containerStorage(c.storage),
			{Title: sectionIndexing, Properties: []adapter.Property{
				{Name: "Mode", Value: "consistent, automatic"},
				{Name: "Included", Value: "/*"},
				{Name: "Excluded", Value: `/"_etag"/?`},
			}},
		},
		Raw: raw,
	}, nil
}

func databaseThroughput(t adapter.Throughput) adapter.Section {
	if !t.Provisioned() {
		return adapter.Section{
			Title: sectionThroughput,
			Note:  "Nothing is provisioned on this database; each container carries its own.",
		}
	}
	return provisionedThroughput(t)
}

func containerThroughput(t adapter.Throughput, database string) adapter.Section {
	switch t.Mode {
	case adapter.ThroughputShared:
		return adapter.Section{
			Title: sectionThroughput,
			Note:  fmt.Sprintf("Inherited from database %s; this container has no throughput of its own.", database),
		}
	case adapter.ThroughputNone:
		return adapter.Section{Title: sectionThroughput, Note: "Nothing is provisioned on this container."}
	}
	return provisionedThroughput(t)
}

func provisionedThroughput(t adapter.Throughput) adapter.Section {
	rate := "Rate"
	if t.Mode == adapter.ThroughputAutoscale {
		rate = "Maximum"
	}
	return adapter.Section{Title: sectionThroughput, Properties: []adapter.Property{
		{Name: "Mode", Value: t.Mode.String()},
		{Name: rate, Value: fmt.Sprintf("%d RU/s", t.RUs)},
	}}
}

func containerStorage(s storage) adapter.Section {
	if s.size == "" {
		return adapter.Section{
			Title: sectionStorage,
			Note:  "The account served storage figures this adapter could not read.",
		}
	}
	return adapter.Section{Title: sectionStorage, Properties: []adapter.Property{
		{Name: "Documents", Value: strconv.Itoa(s.documents)},
		{Name: "Documents size", Value: s.size},
	}}
}
