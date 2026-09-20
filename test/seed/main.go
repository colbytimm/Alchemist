// Command seed fills a local Cosmos DB emulator with sample databases for
// trying Alchemist by hand: containers to union, containers to join, keys that
// match nothing, and one container large enough to page and to hit
// max_join_rows. It replaces the databases it names and refuses any endpoint
// that is not on this machine.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// The emulator's fixed, publicly documented account key. Not a secret.
const wellKnownKey = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw==" // #gitleaks:allow

const seedTimeout = 5 * time.Minute

type item map[string]any

type container struct {
	name         string
	partitionKey string // a top-level field holding a string
	items        []item
}

type database struct {
	name       string
	containers []container
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := envOr("COSMOS_ENDPOINT", "http://localhost:8081")
	if err := requireLocal(endpoint); err != nil {
		return err
	}
	client, err := newClient(endpoint, envOr("COSMOS_KEY", wellKnownKey))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	for _, db := range databases() {
		if err := seedDatabase(ctx, client, db); err != nil {
			return err
		}
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func requireLocal(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	return fmt.Errorf("endpoint %q is not local: seeding replaces whole databases, so it only runs against an emulator", endpoint)
}

func newClient(endpoint, key string) (*azcosmos.Client, error) {
	cred, err := azcosmos.NewKeyCredential(key)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	opts := &azcosmos.ClientOptions{}
	opts.Transport = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- self-signed emulator cert; requireLocal has vetted the host
	}}
	client, err := azcosmos.NewClientWithKey(endpoint, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return client, nil
}

func seedDatabase(ctx context.Context, client *azcosmos.Client, db database) error {
	handle, err := client.NewDatabase(db.name)
	if err != nil {
		return fmt.Errorf("database %s: %w", db.name, err)
	}
	if _, err := handle.Delete(ctx, nil); err != nil && !isNotFound(err) {
		return fmt.Errorf("drop database %s: %w", db.name, err)
	}
	if _, err := client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: db.name}, nil); err != nil {
		return fmt.Errorf("create database %s: %w", db.name, err)
	}
	for _, c := range db.containers {
		if err := seedContainer(ctx, client, handle, db.name, c); err != nil {
			return err
		}
		fmt.Printf("%s.%s: %d items, partitioned on /%s\n", db.name, c.name, len(c.items), c.partitionKey)
	}
	return nil
}

func seedContainer(ctx context.Context, client *azcosmos.Client, db *azcosmos.DatabaseClient, dbName string, c container) error {
	_, err := db.CreateContainer(ctx, azcosmos.ContainerProperties{
		ID:                     c.name,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/" + c.partitionKey}},
	}, nil)
	if err != nil {
		return fmt.Errorf("create container %s.%s: %w", dbName, c.name, err)
	}
	handle, err := client.NewContainer(dbName, c.name)
	if err != nil {
		return fmt.Errorf("container %s.%s: %w", dbName, c.name, err)
	}
	for _, it := range c.items {
		if err := createItem(ctx, handle, c.partitionKey, it); err != nil {
			return fmt.Errorf("%s.%s: item %v: %w", dbName, c.name, it["id"], err)
		}
	}
	return nil
}

func createItem(ctx context.Context, handle *azcosmos.ContainerClient, partitionKey string, it item) error {
	key, ok := it[partitionKey].(string)
	if !ok {
		return fmt.Errorf("partition key %q is not a string", partitionKey)
	}
	body, err := json.Marshal(it)
	if err != nil {
		return err
	}
	_, err = handle.CreateItem(ctx, azcosmos.NewPartitionKeyString(key), body, nil)
	return err
}

func isNotFound(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound
}
