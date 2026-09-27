// Package sample holds the sample databases for trying Alchemist on an
// emulator: containers to union, containers to join, keys that match
// nothing, and one container large enough to page and to hit max_join_rows.
// It writes through the adapter interfaces, so any backend can hold it.
package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Target is the connection a seed writes through.
type Target struct {
	Catalog adapter.Catalog
	Admin   adapter.CatalogAdmin
	Writer  adapter.ItemWriter
}

// Seeded reports one container the seed filled.
type Seeded struct {
	Database, Container, PartitionKey string
	Items                             int
}

// ExistsError is a seed refused because sample databases are already there.
type ExistsError struct {
	Databases []string
}

func (e *ExistsError) Error() string {
	verb := "exist"
	if len(e.Databases) == 1 {
		verb = "exists"
	}
	return fmt.Sprintf("sample: %s %s", listing(e.Databases), verb)
}

type item map[string]any

type container struct {
	name         string
	partitionKey string // a top-level field
	items        []item
}

type database struct {
	name       string
	containers []container
}

// Names lists the databases the sample creates.
func Names() []string {
	var names []string
	for _, db := range databases() {
		names = append(names, db.name)
	}
	return names
}

// Seed creates the sample databases, calling report after each container.
// It refuses, with ExistsError, an account holding any of them.
func Seed(ctx context.Context, t Target, report func(Seeded)) error {
	existing, err := existingSamples(ctx, t.Catalog)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return &ExistsError{Databases: existing}
	}
	return create(ctx, t, report)
}

// Replace drops whichever sample databases exist, then seeds them afresh.
// It leaves every other database alone.
func Replace(ctx context.Context, t Target, report func(Seeded)) error {
	existing, err := existingSamples(ctx, t.Catalog)
	if err != nil {
		return err
	}
	for _, name := range existing {
		if err := t.Admin.DeleteDatabase(ctx, name); err != nil {
			return err
		}
	}
	return create(ctx, t, report)
}

func existingSamples(ctx context.Context, catalog adapter.Catalog) ([]string, error) {
	nodes, err := catalog.Root(ctx)
	if err != nil {
		return nil, err
	}
	var existing []string
	for _, name := range Names() {
		if slices.ContainsFunc(nodes, func(n adapter.Node) bool { return n.Name == name }) {
			existing = append(existing, name)
		}
	}
	return existing, nil
}

func create(ctx context.Context, t Target, report func(Seeded)) error {
	for _, db := range databases() {
		if err := t.Admin.CreateDatabase(ctx, adapter.DatabaseSpec{Name: db.name}); err != nil {
			return err
		}
		for _, c := range db.containers {
			if err := createContainer(ctx, t, db.name, c); err != nil {
				return err
			}
			report(Seeded{Database: db.name, Container: c.name, PartitionKey: c.keyPath(), Items: len(c.items)})
		}
	}
	return nil
}

func createContainer(ctx context.Context, t Target, database string, c container) error {
	spec := adapter.ContainerSpec{Database: database, Name: c.name, PartitionKeys: []string{c.keyPath()}}
	if err := t.Admin.CreateContainer(ctx, spec); err != nil {
		return err
	}
	sink, err := t.Writer.OpenItemSink(ctx, []string{database, c.name})
	if err != nil {
		return err
	}
	for _, it := range c.items {
		if err := write(ctx, sink, it); err != nil {
			return fmt.Errorf("sample: %s.%s: item %v: %w", database, c.name, it["id"], err)
		}
	}
	return nil
}

func write(ctx context.Context, sink adapter.ItemSink, it item) error {
	body, err := json.Marshal(it)
	if err != nil {
		return err
	}
	_, err = sink.Upsert(ctx, body)
	return err
}

func (c container) keyPath() string { return "/" + c.partitionKey }

// listing joins names as a sentence does: "a", "a and b", "a, b and c".
func listing(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
