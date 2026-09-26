// Package mutate runs a statement that writes many items, client-side:
// select the items its WHERE matches with a scan the backend filters, then
// write to each one, the write re-asserting what selected it. Its unit is
// one step — a page of the selection, a chunk of the writes — so a caller
// can run a job as a chain of short calls and stay responsive between
// them. It knows the adapter interfaces and nothing of the backends.
package mutate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	SelectionPageSize = 1000
	// DefaultMaxTargets is how many items a statement may select when its
	// account sets no limit of its own.
	DefaultMaxTargets = 10000
	previewSize       = 3
	// maxRemovals is how many UNSET paths Target.Absent has a bit for.
	maxRemovals = 16
)

// ErrTooManyTargets is a selection that found more items than its limit
// and stopped: nothing is written, and nothing is truncated either.
var ErrTooManyTargets = errors.New("too many items match")

// Target is one selected item: what addresses it, the version it had, and
// which of the statement's UNSET paths it lacked.
type Target struct {
	ID string
	// Key is nil for an item with no value at a key path, which cannot be
	// addressed and is never written.
	Key     adapter.PartitionKey
	Version string
	// Absent has bit i set when the item lacks UNSET path i.
	Absent uint16
}

func (t Target) lacks(removal int) bool {
	return t.Absent&(1<<removal) != 0
}

// Targets is what a selection found: the items to write, in the order the
// backend served them, and what finding them cost.
type Targets struct {
	Items         []Target
	SelectedAt    time.Time
	RequestCharge float64
	// Unaffected counts the items that matched but lack every path the
	// statement would unset, so no operation is left for them.
	Unaffected int
	// WholeItems marks a selection that read whole items, as one with an
	// UNSET must, to see which paths each item has.
	WholeItems bool
	// Preview holds a few matched items whole, for a before and after.
	Preview []json.RawMessage
}

// Keyless counts the targets that cannot be addressed.
func (t Targets) Keyless() int {
	n := 0
	for _, target := range t.Items {
		if target.Key == nil {
			n++
		}
	}
	return n
}

// SelectionProgress is how far a selection has got.
type SelectionProgress struct {
	Matched       int
	RequestCharge float64
	Done          bool
}

// Selection reads the targets of a statement, a page at a time, then one
// more short page for the preview. Whoever calls Next owns it until the
// call returns: it is not safe for concurrent use.
type Selection struct {
	scanner  adapter.ItemScanner
	mutation query.Mutation
	keyPaths []string
	limit    int
	scan     adapter.ItemScan
	targets  Targets
	selected map[string]bool
	scanned  bool
	done     bool
}

// Select opens the scan of m's targets over a container keyed on keyPaths.
// A limit of zero is DefaultMaxTargets.
func Select(ctx context.Context, scanner adapter.ItemScanner, m query.Mutation, keyPaths []string, limit int) (*Selection, error) {
	if len(m.Removals) > maxRemovals {
		return nil, fmt.Errorf("mutate: %d UNSET paths; at most %d are read", len(m.Removals), maxRemovals)
	}
	if limit <= 0 {
		limit = DefaultMaxTargets
	}
	s := &Selection{
		scanner: scanner, mutation: m, keyPaths: keyPaths, limit: limit,
		targets:  Targets{WholeItems: len(m.Removals) > 0},
		selected: map[string]bool{},
	}
	projection := adapter.ScanIdentity
	if s.targets.WholeItems {
		projection = adapter.ScanWholeItems
	}
	scan, err := scanner.ScanItems(ctx, s.request(projection, SelectionPageSize))
	if err != nil {
		return nil, fmt.Errorf("mutate: select from %s: %w", s.container(), err)
	}
	s.scan = scan
	return s, nil
}

func (s *Selection) request(projection adapter.ScanProjection, pageSize int32) adapter.ScanRequest {
	return adapter.ScanRequest{
		Container:  s.mutation.Target,
		PageSize:   pageSize,
		Projection: projection,
		Filter:     adapter.ScanFilter{Alias: s.mutation.Alias, Predicate: s.mutation.Where},
	}
}

func (s *Selection) container() string {
	return strings.Join(s.mutation.Target, ".")
}

// Next reads one page of the selection, or once every page is in, the
// preview. Past the limit it stops with ErrTooManyTargets.
func (s *Selection) Next(ctx context.Context) (SelectionProgress, error) {
	if !s.scanned {
		if err := s.readPage(ctx); err != nil {
			return s.progress(), err
		}
		return s.progress(), nil
	}
	if !s.done {
		if err := s.readPreview(ctx); err != nil {
			return s.progress(), err
		}
		s.done = true
	}
	return s.progress(), nil
}

func (s *Selection) Done() bool { return s.done }

func (s *Selection) Targets() Targets { return s.targets }

func (s *Selection) Close() error {
	if s.scan == nil {
		return nil
	}
	return s.scan.Close()
}

func (s *Selection) progress() SelectionProgress {
	return SelectionProgress{Matched: len(s.targets.Items), RequestCharge: s.targets.RequestCharge, Done: s.done}
}

func (s *Selection) readPage(ctx context.Context) error {
	page, err := s.scan.NextPage(ctx)
	s.targets.RequestCharge += page.RequestCharge
	if err != nil {
		return fmt.Errorf("mutate: select from %s: %w", s.container(), err)
	}
	for _, item := range page.Items {
		s.keep(item)
	}
	if len(s.targets.Items) > s.limit {
		s.targets.Items = nil
		return fmt.Errorf("mutate: more than %s items match: %w", formatCount(s.limit), ErrTooManyTargets)
	}
	if page.Next == "" || !s.scan.HasMore() {
		s.scanned = true
		s.targets.SelectedAt = time.Now()
		s.done = len(s.targets.Items) == 0 || len(s.targets.Preview) > 0
	}
	return nil
}

// keep reduces one matched item to its target, or counts it as needing no
// operation at all.
func (s *Selection) keep(item json.RawMessage) {
	target := Target{ID: itemID(item)}
	for i, path := range s.mutation.Removals {
		if _, ok := valueAt(item, path.Steps); !ok {
			target.Absent |= 1 << i
		}
	}
	if len(s.mutation.Assignments) == 0 && int(target.Absent) == 1<<len(s.mutation.Removals)-1 {
		s.targets.Unaffected++
		return
	}
	if key, err := adapter.PartitionKeyValues(item, s.keyPaths); err == nil {
		target.Key = key
	}
	if meta, err := adapter.ReadItemMeta(item); err == nil {
		target.Version = meta.Version
	}
	s.targets.Items = append(s.targets.Items, target)
	s.selected[identity(target.ID, target.Key)] = true
	if s.targets.WholeItems && len(s.targets.Preview) < previewSize {
		s.targets.Preview = append(s.targets.Preview, item)
	}
}

// readPreview reads a few matched items whole. One that is not a target
// matched only after the selection, and is not shown.
func (s *Selection) readPreview(ctx context.Context) error {
	scan, err := s.scanner.ScanItems(ctx, s.request(adapter.ScanWholeItems, previewSize))
	if err != nil {
		return fmt.Errorf("mutate: preview %s: %w", s.container(), err)
	}
	defer closeQuietly(scan)
	page, err := scan.NextPage(ctx)
	s.targets.RequestCharge += page.RequestCharge
	if err != nil {
		return fmt.Errorf("mutate: preview %s: %w", s.container(), err)
	}
	for _, item := range page.Items {
		key, err := adapter.PartitionKeyValues(item, s.keyPaths)
		if err != nil {
			key = nil
		}
		if s.selected[identity(itemID(item), key)] && len(s.targets.Preview) < previewSize {
			s.targets.Preview = append(s.targets.Preview, item)
		}
	}
	return nil
}

// closeQuietly closes a scan read to its one page; a scan holds nothing
// worth reporting a failure to release.
func closeQuietly(scan adapter.ItemScan) {
	_ = scan.Close()
}

func identity(id string, key adapter.PartitionKey) string {
	return id + "\x00" + query.PartitionText(key)
}

func itemID(item json.RawMessage) string {
	var head struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(item, &head) != nil {
		return ""
	}
	return head.ID
}
