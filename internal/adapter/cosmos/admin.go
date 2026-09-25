package cosmos

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Compile-time contract checks for the optional management interfaces.
var (
	_ adapter.CatalogAdmin     = (*connection)(nil)
	_ adapter.ThroughputEditor = (*connection)(nil)
)

func (c *connection) CreateDatabase(ctx context.Context, spec adapter.DatabaseSpec) error {
	props := azcosmos.DatabaseProperties{ID: spec.Name}
	opts := &azcosmos.CreateDatabaseOptions{ThroughputProperties: createOffer(spec.Throughput)}
	if _, err := c.client.CreateDatabase(ctx, props, opts); err != nil {
		return wrap(fmt.Sprintf("create database %q", spec.Name), err)
	}
	return nil
}

func (c *connection) DeleteDatabase(ctx context.Context, name string) error {
	db, err := c.database(name)
	if err != nil {
		return err
	}
	if _, err := db.Delete(ctx, nil); err != nil {
		return wrap(fmt.Sprintf("delete database %q", name), err)
	}
	return nil
}

func (c *connection) CreateContainer(ctx context.Context, spec adapter.ContainerSpec) error {
	db, err := c.database(spec.Database)
	if err != nil {
		return err
	}
	op := fmt.Sprintf("create container %s.%s", spec.Database, spec.Name)
	props, err := containerProperties(spec)
	if err != nil {
		return fmt.Errorf("cosmos: %s: %w", op, err)
	}
	opts := &azcosmos.CreateContainerOptions{ThroughputProperties: createOffer(spec.Throughput)}
	if _, err := db.CreateContainer(ctx, props, opts); err != nil {
		return wrap(op, err)
	}
	return nil
}

func (c *connection) DeleteContainer(ctx context.Context, path []string) error {
	op := "delete container " + pathText(path)
	container, err := c.containerAt(op, path)
	if err != nil {
		return err
	}
	if _, err := container.Delete(ctx, nil); err != nil {
		return wrap(op, err)
	}
	return nil
}

// Throughput reads the capacity provisioned for path. A resource with no
// offer of its own answers 404 rather than naming that arrangement, so the
// read has to restate it.
func (c *connection) Throughput(ctx context.Context, path []string) (adapter.Throughput, error) {
	op := "throughput " + pathText(path)
	holder, err := c.offerHolderAt(op, path)
	if err != nil {
		return adapter.Throughput{}, err
	}
	resp, err := holder.ReadThroughput(ctx, nil)
	if err == nil {
		return readOffer(resp.ThroughputProperties), nil
	}
	if !notFound(err) {
		return adapter.Throughput{}, wrap(op, err)
	}
	if len(path) == 2 {
		return adapter.Throughput{Mode: adapter.ThroughputShared}, nil
	}
	return adapter.Throughput{}, nil
}

func (c *connection) SetThroughput(ctx context.Context, path []string, t adapter.Throughput) error {
	op := "set throughput " + pathText(path)
	if err := t.Settable(); err != nil {
		return fmt.Errorf("cosmos: %s: %w", op, err)
	}
	holder, err := c.offerHolderAt(op, path)
	if err != nil {
		return err
	}
	if _, err := holder.ReplaceThroughput(ctx, provisionedOffer(t), nil); err != nil {
		return wrap(op, err)
	}
	return nil
}

// offerHolder is the pair of throughput methods DatabaseClient and
// ContainerClient both carry; the SDK declares no interface over them.
type offerHolder interface {
	ReadThroughput(ctx context.Context, o *azcosmos.ThroughputOptions) (azcosmos.ThroughputResponse, error)
	ReplaceThroughput(ctx context.Context, t azcosmos.ThroughputProperties, o *azcosmos.ThroughputOptions) (azcosmos.ThroughputResponse, error)
}

func (c *connection) offerHolderAt(op string, path []string) (offerHolder, error) {
	if len(path) == 1 {
		return c.database(path[0])
	}
	return c.containerAt(op, path)
}

func (c *connection) database(name string) (*azcosmos.DatabaseClient, error) {
	db, err := c.client.NewDatabase(name)
	if err != nil {
		return nil, fmt.Errorf("cosmos: open database %q: %w", name, err)
	}
	return db, nil
}

func (c *connection) containerAt(op string, path []string) (*azcosmos.ContainerClient, error) {
	if len(path) != 2 {
		return nil, fmt.Errorf("cosmos: %s: path must name a database and a container", op)
	}
	container, err := c.client.NewContainer(path[0], path[1])
	if err != nil {
		return nil, fmt.Errorf("cosmos: open container %s: %w", pathText(path), err)
	}
	return container, nil
}

// createOffer is the throughput a create request carries, and nil for a
// resource left to draw on its database or on a serverless account.
func createOffer(t adapter.Throughput) *azcosmos.ThroughputProperties {
	if !t.Provisioned() {
		return nil
	}
	offer := provisionedOffer(t)
	return &offer
}

// provisionedOffer builds the SDK offer for a mode Settable admits. Neither
// minimum RU/s nor autoscale step size is checked here: both vary by account
// type, and the service names the one that was violated.
func provisionedOffer(t adapter.Throughput) azcosmos.ThroughputProperties {
	if t.Mode == adapter.ThroughputAutoscale {
		return azcosmos.NewAutoscaleThroughputProperties(t.RUs)
	}
	return azcosmos.NewManualThroughputProperties(t.RUs)
}

// readOffer restates the SDK's offer. Autoscale is read first: an autoscale
// offer carries no manual rate.
func readOffer(props *azcosmos.ThroughputProperties) adapter.Throughput {
	if props == nil {
		return adapter.Throughput{}
	}
	if rus, ok := props.AutoscaleMaxThroughput(); ok {
		return adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: rus}
	}
	if rus, ok := props.ManualThroughput(); ok {
		return adapter.Throughput{Mode: adapter.ThroughputManual, RUs: rus}
	}
	return adapter.Throughput{}
}

func pathText(path []string) string { return strings.Join(path, ".") }
