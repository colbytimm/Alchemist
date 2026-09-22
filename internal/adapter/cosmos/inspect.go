package cosmos

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Compile-time contract check.
var _ adapter.Inspector = (*connection)(nil)

// resourceUsageHeader carries a container's storage figures when a read asks
// for quota info. The SDK leaves it as the raw header.
const resourceUsageHeader = "x-ms-resource-usage"

const lastModifiedLayout = "2006-01-02 15:04:05 UTC"

// Section titles, in the order a container reads: what it is, what it costs,
// what it holds, then the policies, which run longest.
const (
	sectionIdentity     = "Identity"
	sectionPartitionKey = "Partition key"
	sectionThroughput   = "Throughput"
	sectionStorage      = "Storage"
	sectionPartitions   = "Physical partitions"
	sectionTimeToLive   = "Time to live"
	sectionIndexing     = "Indexing"
	sectionPolicies     = "Policies"
)

// Inspect reads everything the account serves about a database or a
// container: two requests for the first, three for the second, one after
// another. A request that fails after the first leaves a note in its section
// rather than failing the whole read.
func (c *connection) Inspect(ctx context.Context, n adapter.Node) (adapter.Details, error) {
	op := "inspect " + pathText(n.Path)
	switch n.Kind {
	case adapter.NodeDatabase:
		return c.inspectDatabase(ctx, op, n.Path)
	case adapter.NodeContainer:
		return c.inspectContainer(ctx, op, n.Path)
	}
	return adapter.Details{}, fmt.Errorf("cosmos: %s: %s node has no details: %w", op, n.Kind, adapter.ErrUnsupported)
}

func (c *connection) inspectDatabase(ctx context.Context, op string, path []string) (adapter.Details, error) {
	if len(path) != 1 {
		return adapter.Details{}, fmt.Errorf("cosmos: %s: path must name a database", op)
	}
	db, err := c.database(path[0])
	if err != nil {
		return adapter.Details{}, err
	}
	resp, err := db.Read(ctx, nil)
	if err != nil {
		return adapter.Details{}, wrap(op, err)
	}
	props := resp.DatabaseProperties
	raw, err := json.Marshal(props)
	if err != nil {
		return adapter.Details{}, fmt.Errorf("cosmos: %s: %w", op, err)
	}
	identity := []adapter.Property{{Name: "Database", Value: props.ID}}
	identity = append(identity, resourceIdentity(props.ResourceID, props.ETag, props.LastModified)...)
	return adapter.Details{
		Sections: []adapter.Section{
			{Title: sectionIdentity, Properties: identity},
			c.throughputSection(ctx, op, db, path),
		},
		Raw: raw,
	}, nil
}

func (c *connection) inspectContainer(ctx context.Context, op string, path []string) (adapter.Details, error) {
	container, err := c.containerAt(op, path)
	if err != nil {
		return adapter.Details{}, err
	}
	resp, err := container.Read(ctx, &azcosmos.ReadContainerOptions{PopulateQuotaInfo: true})
	if err != nil {
		return adapter.Details{}, wrap(op, err)
	}
	props := resp.ContainerProperties
	raw, err := json.Marshal(props)
	if err != nil {
		return adapter.Details{}, fmt.Errorf("cosmos: %s: %w", op, err)
	}
	identity := []adapter.Property{
		{Name: "Database", Value: path[0]},
		{Name: "Container", Value: props.ID},
	}
	identity = append(identity, resourceIdentity(props.ResourceID, props.ETag, props.LastModified)...)
	var usage string
	if resp.RawResponse != nil {
		usage = resp.RawResponse.Header.Get(resourceUsageHeader)
	}
	return adapter.Details{
		Sections: []adapter.Section{
			{Title: sectionIdentity, Properties: identity},
			partitionKeySection(props.PartitionKeyDefinition),
			c.throughputSection(ctx, op, container, path),
			storageSection(usage),
			c.partitionsSection(ctx, op, container),
			timeToLiveSection(props),
			indexingSection(props.IndexingPolicy),
			policiesSection(props),
		},
		Raw: raw,
	}, nil
}

func resourceIdentity(resourceID string, etag *azcore.ETag, lastModified time.Time) []adapter.Property {
	properties := []adapter.Property{{Name: "Resource ID", Value: resourceID}}
	if etag != nil {
		properties = append(properties, adapter.Property{Name: "ETag", Value: string(*etag)})
	}
	if !lastModified.IsZero() {
		properties = append(properties, adapter.Property{
			Name:  "Last modified",
			Value: lastModified.UTC().Format(lastModifiedLayout),
		})
	}
	return properties
}

func partitionKeySection(def azcosmos.PartitionKeyDefinition) adapter.Section {
	kind := string(def.Kind)
	if kind == "" {
		kind = string(azcosmos.PartitionKeyKindHash)
		if len(def.Paths) > 1 {
			kind = string(azcosmos.PartitionKeyKindMultiHash)
		}
	}
	if def.Version > 0 {
		kind = fmt.Sprintf("%s (version %d)", kind, def.Version)
	}
	return adapter.Section{Title: sectionPartitionKey, Properties: []adapter.Property{
		{Name: "Kind", Value: kind},
		{Name: "Paths", Value: strings.Join(def.Paths, ", ")},
	}}
}

// throughputSection reads the offer on path. A resource with no offer of its
// own answers 404, which is a fact about the resource and not a failure; any
// other refusal is reported in the section, since the rest of the read still
// has something to say.
func (c *connection) throughputSection(ctx context.Context, op string, holder offerHolder, path []string) adapter.Section {
	resp, err := holder.ReadThroughput(ctx, nil)
	switch {
	case err == nil:
		return offerSection(resp)
	case !notFound(err):
		return adapter.Section{Title: sectionThroughput, Note: wrap(op, err).Error()}
	case len(path) == 2:
		return adapter.Section{
			Title: sectionThroughput,
			Note:  fmt.Sprintf("Inherited from database %s; this container has no throughput of its own.", path[0]),
		}
	}
	return adapter.Section{
		Title: sectionThroughput,
		Note:  "Nothing is provisioned on this database: its containers carry their own, or the account is serverless.",
	}
}

func offerSection(resp azcosmos.ThroughputResponse) adapter.Section {
	props := resp.ThroughputProperties
	if props == nil {
		return adapter.Section{Title: sectionThroughput, Note: "The account served an offer this adapter could not read."}
	}
	properties := offerProperties(props)
	if resp.MinThroughput != nil {
		properties = append(properties, adapter.Property{Name: "Minimum", Value: rate(*resp.MinThroughput)})
	}
	scaling := "complete"
	if resp.IsReplacePending {
		scaling = "in progress"
	}
	properties = append(properties, adapter.Property{Name: "Scaling", Value: scaling})
	return adapter.Section{Title: sectionThroughput, Properties: properties}
}

// offerProperties reads autoscale first: an autoscale offer carries no
// manual rate.
func offerProperties(props *azcosmos.ThroughputProperties) []adapter.Property {
	if maximum, ok := props.AutoscaleMaxThroughput(); ok {
		properties := []adapter.Property{
			{Name: "Mode", Value: adapter.ThroughputAutoscale.String()},
			{Name: "Maximum", Value: rate(maximum)},
		}
		if increment, ok := props.AutoscaleIncrement(); ok {
			properties = append(properties, adapter.Property{Name: "Increment", Value: fmt.Sprintf("%d%%", increment)})
		}
		return properties
	}
	if rus, ok := props.ManualThroughput(); ok {
		return []adapter.Property{
			{Name: "Mode", Value: adapter.ThroughputManual.String()},
			{Name: "Rate", Value: rate(rus)},
		}
	}
	return nil
}

func rate(rus int32) string { return fmt.Sprintf("%d RU/s", rus) }

func storageSection(header string) adapter.Section {
	usage, ok := ParseResourceUsage(header)
	if !ok {
		return adapter.Section{
			Title: sectionStorage,
			Note:  "The account served no storage figures this adapter could read.",
		}
	}
	return adapter.Section{Title: sectionStorage, Properties: []adapter.Property{
		{Name: "Documents", Value: strconv.FormatInt(usage.Documents, 10)},
		{Name: "Documents size", Value: formatKilobytes(usage.DocumentsKB)},
		{Name: "Collection size", Value: formatKilobytes(usage.CollectionKB)},
	}}
}

// ResourceUsage is what a container holds, as the x-ms-resource-usage header
// reports it. Sizes are in kilobytes, the unit the service uses.
type ResourceUsage struct {
	Documents    int64
	DocumentsKB  int64
	CollectionKB int64
}

// ParseResourceUsage reads the header: key=value pairs separated by
// semicolons, in no fixed order, among keys this adapter has no use for. It
// reports false when any of the three figures is missing or not a number, so
// a view never shows a guess in place of one.
func ParseResourceUsage(header string) (ResourceUsage, bool) {
	figures := map[string]int64{}
	for _, pair := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		figures[strings.TrimSpace(key)] = n
	}
	documents, hasDocuments := figures["documentsCount"]
	documentsKB, hasDocumentsSize := figures["documentsSize"]
	collectionKB, hasCollectionSize := figures["collectionSize"]
	if !hasDocuments || !hasDocumentsSize || !hasCollectionSize {
		return ResourceUsage{}, false
	}
	return ResourceUsage{Documents: documents, DocumentsKB: documentsKB, CollectionKB: collectionKB}, true
}

const kilobytesPerUnit = 1024

func formatKilobytes(kb int64) string {
	switch {
	case kb < kilobytesPerUnit:
		return fmt.Sprintf("%d KB", kb)
	case kb < kilobytesPerUnit*kilobytesPerUnit:
		return fmt.Sprintf("%.1f MB", float64(kb)/kilobytesPerUnit)
	}
	return fmt.Sprintf("%.1f GB", float64(kb)/(kilobytesPerUnit*kilobytesPerUnit))
}

func (c *connection) partitionsSection(ctx context.Context, op string, container *azcosmos.ContainerClient) adapter.Section {
	ranges, err := container.ReadFeedRanges(ctx, nil)
	if err != nil {
		return adapter.Section{Title: sectionPartitions, Note: wrap(op, err).Error()}
	}
	properties := []adapter.Property{{Name: "Count", Value: strconv.Itoa(len(ranges))}}
	for i, r := range ranges {
		properties = append(properties, adapter.Property{Name: fmt.Sprintf("Range %d", i+1), Value: rangeText(r)})
	}
	return adapter.Section{Title: sectionPartitions, Properties: properties}
}

// rangeText writes a range the way the service does, with the open ends of
// the key space spelled out as the hex bounds they stand for.
func rangeText(r azcosmos.FeedRange) string {
	low, high := r.MinInclusive, r.MaxExclusive
	if low == "" {
		low = "00"
	}
	if high == "" {
		high = "FF"
	}
	return fmt.Sprintf("[%s, %s)", low, high)
}

func timeToLiveSection(props *azcosmos.ContainerProperties) adapter.Section {
	return adapter.Section{Title: sectionTimeToLive, Properties: []adapter.Property{
		{Name: "Default", Value: timeToLive(props.DefaultTimeToLive)},
		{Name: "Analytical store", Value: timeToLive(props.AnalyticalStoreTimeToLiveInSeconds)},
	}}
}

// timeToLive reads a TTL the way the service defines it: absent is off, -1
// is on with no expiry unless an item sets its own, and anything else is a
// lifetime in seconds.
func timeToLive(seconds *int32) string {
	switch {
	case seconds == nil:
		return "off"
	case *seconds < 0:
		return "on, no default expiry"
	}
	return fmt.Sprintf("%d seconds", *seconds)
}

func indexingSection(policy *azcosmos.IndexingPolicy) adapter.Section {
	if policy == nil {
		return adapter.Section{Title: sectionIndexing, Note: "The account served no indexing policy."}
	}
	properties := []adapter.Property{
		{Name: "Mode", Value: indexingMode(policy)},
		{Name: "Included", Value: joinPaths(includedPaths(policy.IncludedPaths))},
		{Name: "Excluded", Value: joinPaths(excludedPaths(policy.ExcludedPaths))},
	}
	for _, composite := range policy.CompositeIndexes {
		properties = append(properties, adapter.Property{Name: "Composite", Value: compositeText(composite)})
	}
	for _, spatial := range policy.SpatialIndexes {
		properties = append(properties, adapter.Property{Name: "Spatial", Value: spatialText(spatial)})
	}
	for _, vector := range policy.VectorIndexes {
		properties = append(properties, adapter.Property{Name: "Vector", Value: vector.Path + " (" + string(vector.Type) + ")"})
	}
	for _, fullText := range policy.FullTextIndexes {
		properties = append(properties, adapter.Property{Name: "Full text", Value: fullText.Path})
	}
	return adapter.Section{Title: sectionIndexing, Properties: properties}
}

func indexingMode(policy *azcosmos.IndexingPolicy) string {
	mode := strings.ToLower(string(policy.IndexingMode))
	if mode == "" {
		mode = strings.ToLower(string(azcosmos.IndexingModeConsistent))
	}
	if policy.Automatic {
		return mode + ", automatic"
	}
	return mode + ", manual"
}

func includedPaths(paths []azcosmos.IncludedPath) []string {
	texts := make([]string, 0, len(paths))
	for _, p := range paths {
		texts = append(texts, p.Path)
	}
	return texts
}

func excludedPaths(paths []azcosmos.ExcludedPath) []string {
	texts := make([]string, 0, len(paths))
	for _, p := range paths {
		texts = append(texts, p.Path)
	}
	return texts
}

func joinPaths(paths []string) string {
	if len(paths) == 0 {
		return "none"
	}
	return strings.Join(paths, ", ")
}

func compositeText(index []azcosmos.CompositeIndex) string {
	parts := make([]string, 0, len(index))
	for _, part := range index {
		parts = append(parts, part.Path+" "+string(part.Order))
	}
	return strings.Join(parts, ", ")
}

func spatialText(index azcosmos.SpatialIndex) string {
	types := make([]string, 0, len(index.SpatialTypes))
	for _, t := range index.SpatialTypes {
		types = append(types, string(t))
	}
	return index.Path + " (" + strings.Join(types, ", ") + ")"
}

func policiesSection(props *azcosmos.ContainerProperties) adapter.Section {
	var properties []adapter.Property
	if props.UniqueKeyPolicy != nil {
		for _, key := range props.UniqueKeyPolicy.UniqueKeys {
			properties = append(properties, adapter.Property{Name: "Unique key", Value: strings.Join(key.Paths, ", ")})
		}
	}
	if props.ConflictResolutionPolicy != nil {
		properties = append(properties, adapter.Property{
			Name:  "Conflict resolution",
			Value: conflictResolutionText(*props.ConflictResolutionPolicy),
		})
	}
	if props.VectorEmbeddingPolicy != nil {
		for _, embedding := range props.VectorEmbeddingPolicy.VectorEmbeddings {
			properties = append(properties, adapter.Property{Name: "Vector embedding", Value: embeddingText(embedding)})
		}
	}
	if props.FullTextPolicy != nil {
		properties = append(properties, fullTextProperties(*props.FullTextPolicy)...)
	}
	if len(properties) == 0 {
		return adapter.Section{Title: sectionPolicies, Note: "None configured."}
	}
	return adapter.Section{Title: sectionPolicies, Properties: properties}
}

func conflictResolutionText(policy azcosmos.ConflictResolutionPolicy) string {
	text := string(policy.Mode)
	switch {
	case policy.ResolutionPath != "":
		return text + " on " + policy.ResolutionPath
	case policy.ResolutionProcedure != "":
		return text + " via " + policy.ResolutionProcedure
	}
	return text
}

func embeddingText(embedding azcosmos.VectorEmbedding) string {
	return fmt.Sprintf("%s (%s, %s, %d dimensions)",
		embedding.Path, embedding.DataType, embedding.DistanceFunction, embedding.Dimensions)
}

func fullTextProperties(policy azcosmos.FullTextPolicy) []adapter.Property {
	properties := []adapter.Property{{Name: "Full text language", Value: policy.DefaultLanguage}}
	for _, path := range policy.FullTextPaths {
		properties = append(properties, adapter.Property{
			Name:  "Full text path",
			Value: path.Path + " (" + path.Language + ")",
		})
	}
	return properties
}
