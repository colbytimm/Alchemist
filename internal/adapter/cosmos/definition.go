package cosmos

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.DefinitionReader = (*connection)(nil)

const bytesPerKilobyte = 1024

// ContainerDefinition reads the container and its usage in one request. The
// definition travels as the JSON the SDK writes for ContainerProperties,
// with the identity fields the service assigns left out: only what
// UnmarshalJSON reads back can be carried, which leaves computed
// properties, the change feed policy, the client encryption policy and the
// spatial configuration behind.
func (c *connection) ContainerDefinition(ctx context.Context, path []string, fidelity adapter.DefinitionFidelity) (adapter.ContainerDefinition, error) {
	op := "definition " + pathText(path)
	container, err := c.containerAt(op, path)
	if err != nil {
		return adapter.ContainerDefinition{}, err
	}
	resp, err := container.Read(ctx, &azcosmos.ReadContainerOptions{PopulateQuotaInfo: true})
	if err != nil {
		return adapter.ContainerDefinition{}, wrap(op, err)
	}
	props := withoutIdentity(*resp.ContainerProperties)
	if fidelity == adapter.DefinitionPortable {
		props = portable(props)
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return adapter.ContainerDefinition{}, fmt.Errorf("cosmos: %s: %w", op, err)
	}
	var usage string
	if resp.RawResponse != nil {
		usage = resp.RawResponse.Header.Get(resourceUsageHeader)
	}
	return adapter.ContainerDefinition{
		PartitionKeys: slices.Clone(props.PartitionKeyDefinition.Paths),
		Policies:      adapter.PolicyDocument{Backend: Name, Raw: raw},
		Size:          sizeEstimate(usage),
	}, nil
}

// withoutIdentity clears what the service assigns a container, which a
// create must not send.
func withoutIdentity(props azcosmos.ContainerProperties) azcosmos.ContainerProperties {
	props.ID = ""
	props.ETag = nil
	props.SelfLink = ""
	props.ResourceID = ""
	props.LastModified = time.Time{}
	return props
}

// portable keeps what decides how data is laid out and queried, and drops
// what depends on a feature of the account the definition was read from:
// the analytical store, a conflict policy naming a stored procedure that is
// not copied, vector and full-text search.
func portable(props azcosmos.ContainerProperties) azcosmos.ContainerProperties {
	props.AnalyticalStoreTimeToLiveInSeconds = nil
	props.ConflictResolutionPolicy = nil
	props.VectorEmbeddingPolicy = nil
	props.FullTextPolicy = nil
	if props.IndexingPolicy != nil {
		indexing := *props.IndexingPolicy
		indexing.VectorIndexes = nil
		indexing.FullTextIndexes = nil
		props.IndexingPolicy = &indexing
	}
	return props
}

// sizeEstimate knows a size only when the header counts some documents.
// The emulator reports zeros for every container, full or not, so a zero is
// no evidence of an empty container, and saying "unknown" of one that is
// empty costs nothing: its copy is over at once.
func sizeEstimate(usageHeader string) adapter.SizeEstimate {
	usage, ok := ParseResourceUsage(usageHeader)
	if !ok || usage.Documents <= 0 {
		return adapter.SizeEstimate{}
	}
	return adapter.SizeEstimate{Items: usage.Documents, Bytes: usage.DocumentsKB * bytesPerKilobyte, Known: true}
}

// containerProperties is what a create sends for spec: its policies when it
// carries some, with the name and key paths the spec gives.
func containerProperties(spec adapter.ContainerSpec) (azcosmos.ContainerProperties, error) {
	props := azcosmos.ContainerProperties{}
	switch spec.Policies.Backend {
	case "":
	case Name:
		if err := json.Unmarshal(spec.Policies.Raw, &props); err != nil {
			return props, fmt.Errorf("read the policies: %w", err)
		}
		props = withoutIdentity(props)
	default:
		return props, fmt.Errorf("policies written by %s: %w", spec.Policies.Backend, adapter.ErrUnsupported)
	}
	props.ID = spec.Name
	if !slices.Equal(props.PartitionKeyDefinition.Paths, spec.PartitionKeys) {
		props.PartitionKeyDefinition = azcosmos.PartitionKeyDefinition{Paths: spec.PartitionKeys}
	}
	return props, nil
}
