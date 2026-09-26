package cosmos

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.ItemScanner = (*connection)(nil)

// defaultAlias is what a scan calls an item when no filter names it.
const defaultAlias = "c"

// ScanItems opens a scan resuming at request.From: the query's continuation
// token, as the service issued it. Unordered and across every partition, a
// scan is served by the gateway walking the partition key ranges in a fixed
// order, which is what makes its continuation token a stable place to
// resume.
func (c *connection) ScanItems(ctx context.Context, request adapter.ScanRequest) (adapter.ItemScan, error) {
	op := "scan " + pathText(request.Container)
	container, err := c.containerAt(op, request.Container)
	if err != nil {
		return nil, err
	}
	text, err := c.scanQuery(ctx, container, request)
	if err != nil {
		return nil, wrap(op, err)
	}
	options := &azcosmos.QueryOptions{PageSizeHint: request.PageSize}
	if options.PageSizeHint <= 0 {
		options.PageSizeHint = c.pageSize
	}
	if request.From != "" {
		from := string(request.From)
		options.ContinuationToken = &from
	}
	if !request.Since.IsZero() {
		options.QueryParameters = []azcosmos.QueryParameter{{Name: "@since", Value: request.Since.Unix()}}
	}
	pager := container.NewQueryItemsPager(text, azcosmos.NewPartitionKey(), options)
	return &scan{op: op, pager: pager}, nil
}

// scanQuery is the query a scan runs, reading the container's key paths
// first when the projection needs them.
func (c *connection) scanQuery(ctx context.Context, container *azcosmos.ContainerClient, request adapter.ScanRequest) (string, error) {
	var keyPaths []string
	if request.Projection == adapter.ScanIdentity {
		resp, err := container.Read(ctx, nil)
		if err != nil {
			return "", err
		}
		keyPaths = resp.ContainerProperties.PartitionKeyDefinition.Paths
	}
	return ScanQuery(request, keyPaths), nil
}

// ScanQuery is the text of the query request runs over a container keyed
// on keyPaths. _ts is in seconds, so Since keeps the whole second it falls
// in. A Filter's predicate is the caller's own text, so the query calls an
// item by the filter's alias.
func ScanQuery(request adapter.ScanRequest, keyPaths []string) string {
	alias := defaultAlias
	if request.Filter.Predicate != "" && request.Filter.Alias != "" {
		alias = request.Filter.Alias
	}
	projection := "*"
	if request.Projection == adapter.ScanIdentity {
		projection = "VALUE " + identityProjection(keyPaths, alias)
	}
	var conditions []string
	if !request.Since.IsZero() {
		conditions = append(conditions, alias+"._ts >= @since")
	}
	if request.Filter.Predicate != "" {
		conditions = append(conditions, "("+request.Filter.Predicate+")")
	}
	text := "SELECT " + projection + " FROM " + alias
	if len(conditions) > 0 {
		text += " WHERE " + strings.Join(conditions, " AND ")
	}
	return text
}

// IdentityProjection is an object literal of an item's id, its system
// fields and the values at keyPaths, nested as the item nests them. An
// object literal drops a member whose value is undefined, so a field the
// item lacks is absent rather than null, and its keys are strings, so any
// property name can be written.
func IdentityProjection(keyPaths []string) string {
	return identityProjection(keyPaths, defaultAlias)
}

func identityProjection(keyPaths []string, alias string) string {
	root := &literal{}
	for _, name := range append([]string{"id"}, adapter.SystemFields()...) {
		root.add([]string{name})
	}
	for _, path := range keyPaths {
		root.add(strings.Split(strings.TrimPrefix(path, "/"), "/"))
	}
	return root.render(alias)
}

// literal is one object of the projection, its members in the order they
// were first named.
type literal struct {
	names   []string
	members map[string]*literal
}

func (l *literal) add(path []string) {
	if len(path) == 0 {
		return
	}
	if l.members == nil {
		l.members = map[string]*literal{}
	}
	child, ok := l.members[path[0]]
	if !ok {
		l.names = append(l.names, path[0])
	}
	if len(path) == 1 {
		l.members[path[0]] = nil // a whole value: whatever lies beneath it comes with it
		return
	}
	if ok && child == nil {
		return
	}
	if child == nil {
		child = &literal{}
		l.members[path[0]] = child
	}
	child.add(path[1:])
}

func (l *literal) render(source string) string {
	members := make([]string, 0, len(l.names))
	for _, name := range l.names {
		quoted, _ := json.Marshal(name) // a string always marshals
		value := source + "[" + string(quoted) + "]"
		if child := l.members[name]; child != nil {
			value = child.render(value)
		}
		members = append(members, string(quoted)+": "+value)
	}
	return "{" + strings.Join(members, ", ") + "}"
}

type scan struct {
	op    string
	pager *runtime.Pager[azcosmos.QueryItemsResponse]
}

func (s *scan) NextPage(ctx context.Context) (adapter.ItemPage, error) {
	resp, err := s.pager.NextPage(ctx)
	if err != nil {
		return adapter.ItemPage{}, wrap(s.op, err)
	}
	page := adapter.ItemPage{RequestCharge: float64(resp.RequestCharge)}
	for _, item := range resp.Items {
		page.Items = append(page.Items, json.RawMessage(item))
	}
	if resp.ContinuationToken != nil && strings.TrimSpace(*resp.ContinuationToken) != "" {
		page.Next = adapter.ScanPosition(*resp.ContinuationToken)
	}
	return page, nil
}

func (s *scan) HasMore() bool { return s.pager.More() }

// Close releases nothing: a pager holds no connection between pages.
func (s *scan) Close() error { return nil }
