package adapter

import (
	"context"
	"encoding/json"
)

// PolicyDocument is a backend's own description of a container beyond its
// name, partition key and throughput. Only the adapter named by Backend can
// read Raw; the zero value means the backend's defaults.
type PolicyDocument struct {
	Backend string
	Raw     json.RawMessage
}

// DefinitionFidelity is how much of a container's definition a read keeps.
// DefinitionPortable leaves out whatever depends on a feature of the account
// it was read from, so another account is likely to accept it.
type DefinitionFidelity int

const (
	DefinitionFull DefinitionFidelity = iota
	DefinitionPortable
)

func (f DefinitionFidelity) String() string {
	if f == DefinitionPortable {
		return "portable"
	}
	return "full"
}

// DefinitionReader reads what CatalogAdmin.CreateContainer needs to make a
// container like an existing one. Optional: callers find it with a comma-ok
// type assertion.
type DefinitionReader interface {
	ContainerDefinition(ctx context.Context, path []string, fidelity DefinitionFidelity) (ContainerDefinition, error)
}

type ContainerDefinition struct {
	PartitionKeys []string
	Policies      PolicyDocument
	Size          SizeEstimate
}

// SizeEstimate is what the backend believes a container holds. Known is
// false when it would not say; Items and Bytes then mean nothing.
type SizeEstimate struct {
	Items int64
	Bytes int64
	Known bool
}
