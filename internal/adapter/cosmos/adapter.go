// Package cosmos implements the adapter contract for Azure Cosmos DB
// (NoSQL API) on the azcosmos v1.5.0 SDK. Cross-partition queries are the
// default; see docs/plan/03-cosmos-adapter.md.
package cosmos

import (
	"context"
	"errors"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Name is the registry name of this adapter.
const Name = "cosmos"

// Compile-time contract check.
var _ adapter.Adapter = Adapter{}

// Adapter implements adapter.Adapter for Azure Cosmos DB.
type Adapter struct{}

// Name returns the adapter's registry name.
func (Adapter) Name() string { return Name }

// Connect validates settings and opens an authenticated session against one
// Cosmos account. Accepted settings: "connection_string", or "endpoint" plus
// "key"; optional "insecure_skip_verify" (emulator only) and "page_size".
// Client construction lands with the catalog and cursor implementations.
func (Adapter) Connect(_ context.Context, settings map[string]string) (adapter.Connection, error) {
	if settings["connection_string"] == "" && (settings["endpoint"] == "" || settings["key"] == "") {
		return nil, errors.New("cosmos: connect requires connection_string, or endpoint and key")
	}
	return nil, errors.New("cosmos: client construction not implemented yet")
}
