package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/canonical"
	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// DefaultMaxItems is the largest container a capture takes on unless a
// profile says otherwise: a capture holds about 130 bytes per item.
const DefaultMaxItems = 5_000_000

// MaxThrottles is how many throttled steps in a row a driver waits out
// before it fails the capture. A step that was throttled wrote nothing, so
// waiting and calling Next again is safe.
const MaxThrottles = 5

type Phase string

const (
	PhaseDefinition Phase = "definition"
	PhaseScan       Phase = "scan"
	PhaseSweep      Phase = "sweep"
	PhaseFetch      Phase = "fetch"
	PhasePublish    Phase = "publish"
	PhaseDone       Phase = "done"
)

// Source is the container a capture reads. Definitions and Throughput are
// optional: without them the snapshot has no definition, and PartitionKeys
// names the key paths instead.
type Source struct {
	Container     []string
	PartitionKeys []string
	Items         adapter.ItemScanner
	Definitions   adapter.DefinitionReader
	Throughput    adapter.ThroughputEditor
}

type CaptureOptions struct {
	Note string
	// Full reads every item even when a parent would allow a sweep.
	Full bool
	// MaxItems refuses a larger container before any item is read, and a
	// capture that finds more; zero is DefaultMaxItems.
	MaxItems int64
	// Group is the database snapshot this capture belongs to.
	Group    string
	PageSize int32
	// Clock is what the record's times are read from; nil is time.Now.
	Clock func() time.Time
}

// Progress is how far a capture has got. Record is set once Done.
// PhaseItems counts the items the current phase has read, of about
// Expected when the backend said how many the container holds.
type Progress struct {
	Phase         Phase
	Items         int64
	PhaseItems    int64
	Expected      int64
	RequestCharge float64
	BytesRead     int64
	BytesStored   int64
	Done          bool
	Record        Record
}

// Capture takes one snapshot, a page per call to Next, so a caller can
// cancel between pages and nothing blocks longer than a page read. It is
// not safe for concurrent use.
type Capture struct {
	store   *Store
	source  Source
	options CaptureOptions
	lock    *storeLock
	record  Record

	keyPaths   []string
	definition []byte
	parent     Manifest
	packs      *pack.Set
	writer     *pack.Writer

	scan     adapter.ItemScan
	position adapter.ScanPosition
	request  adapter.ScanRequest

	current  Manifest
	swept    map[Key]Entry
	suspects map[Key]bool

	progress Progress
	ended    bool
}

// Begin takes the store's lock and prepares a capture of source; the first
// Next reads the definition. A capture already running on the store, in
// this process or another, is ErrLocked.
func (s *Store) Begin(source Source, options CaptureOptions) (*Capture, error) {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.MaxItems <= 0 {
		options.MaxItems = DefaultMaxItems
	}
	if err := createStore(s.loc); err != nil {
		return nil, err
	}
	started := options.Clock().UTC()
	lock, err := takeLock(s.loc.Dir(), started)
	if err != nil {
		return nil, err
	}
	records, err := readRecords(s.loc.Dir())
	if err != nil {
		return nil, errors.Join(err, lock.release())
	}
	s.records = records
	record := Record{Started: started, Note: options.Note, Group: options.Group, Mode: ModeFull}
	if head, ok := s.head(); ok {
		record.Parent = head.ID
		record.ID = nextID(started, head.ID)
	} else {
		record.ID = newID(started)
	}
	return &Capture{
		store:    s,
		source:   source,
		options:  options,
		lock:     lock,
		record:   record,
		current:  Manifest{},
		progress: Progress{Phase: PhaseDefinition},
	}, nil
}

func (c *Capture) ID() string { return c.record.ID }

// Next takes the capture one step: the definition, then a page of the scan
// or of the sweep and the fetch, then the publish. A step that fails has
// changed nothing, so the same call may be made again, after the wait a
// *adapter.ThrottledError names; for any other failure, Abort.
func (c *Capture) Next(ctx context.Context) (Progress, error) {
	if c.ended {
		return c.progress, fmt.Errorf("snapshot: capture %s has ended", c.record.ID)
	}
	var err error
	switch c.progress.Phase {
	case PhaseDefinition:
		err = c.prepare(ctx)
	case PhaseScan, PhaseFetch:
		err = c.readPage(ctx, c.takeWhole)
	case PhaseSweep:
		err = c.readPage(ctx, c.takeSwept)
	case PhasePublish:
		err = c.publish()
	}
	if err != nil {
		return c.progress, fmt.Errorf("snapshot: %s: %w", c.store.loc, err)
	}
	return c.progress, nil
}

// Abort releases the lock and drops what was not published. Packs already
// written stay for the next capture to reuse, or for garbage
// collection to reclaim.
func (c *Capture) Abort() error {
	if c.ended {
		return nil
	}
	c.ended = true
	var errs []error
	if c.writer != nil {
		c.writer.Abort()
	}
	if c.scan != nil {
		errs = append(errs, c.scan.Close())
	}
	if c.packs != nil {
		errs = append(errs, c.packs.Close())
	}
	errs = append(errs, c.lock.release())
	return errors.Join(errs...)
}

func (c *Capture) prepare(ctx context.Context) error {
	c.keyPaths = c.source.PartitionKeys
	if err := c.captureDefinition(ctx); err != nil {
		return err
	}
	dir := c.store.loc.Dir()
	packs, err := pack.OpenSet(filepath.Join(dir, packsDir))
	if err != nil {
		return err
	}
	c.packs = packs
	if c.writer, err = pack.NewWriter(filepath.Join(dir, packsDir), c.record.ID); err != nil {
		return err
	}
	if err := c.storeDefinition(); err != nil {
		return err
	}
	if c.parent, err = c.store.headManifest(); err != nil {
		return err
	}
	c.progress.Phase = PhaseScan
	c.request = adapter.ScanRequest{Container: c.source.Container, PageSize: c.options.PageSize}
	if c.record.Parent != "" && !c.options.Full {
		c.progress.Phase = PhaseSweep
		c.record.Mode = ModeIncremental
		c.request.Projection = adapter.ScanIdentity
		c.swept = map[Key]Entry{}
	}
	return nil
}

// definitionDocument is what a definition blob holds. Size is left out: it
// is a reading, and would make every capture look like a change.
type definitionDocument struct {
	PartitionKeys []string          `json:"partitionKeys"`
	Backend       string            `json:"backend"`
	Policies      json.RawMessage   `json:"policies,omitempty"`
	Throughput    *throughputRecord `json:"throughput,omitempty"`
}

type throughputRecord struct {
	Mode string `json:"mode"`
	RUs  int32  `json:"ru,omitempty"`
}

func newThroughputRecord(t adapter.Throughput) *throughputRecord {
	record := &throughputRecord{Mode: t.Mode.String()}
	if t.Provisioned() {
		record.RUs = t.RUs
	}
	return record
}

func (c *Capture) captureDefinition(ctx context.Context) error {
	if c.source.Definitions == nil {
		return nil
	}
	definition, err := c.source.Definitions.ContainerDefinition(ctx, c.source.Container, adapter.DefinitionFull)
	if err != nil {
		return err
	}
	if size := definition.Size; size.Known {
		if size.Items > c.options.MaxItems {
			return tooManyItems(size.Items, c.options.MaxItems)
		}
		c.record.Size = &SizeReading{Items: size.Items, Bytes: size.Bytes}
		c.progress.Expected = size.Items
	}
	c.keyPaths = definition.PartitionKeys
	document := definitionDocument{PartitionKeys: definition.PartitionKeys, Backend: definition.Policies.Backend, Policies: definition.Policies.Raw}
	if c.source.Throughput != nil {
		throughput, err := c.source.Throughput.Throughput(ctx, c.source.Container)
		if err != nil {
			return err
		}
		document.Throughput = newThroughputRecord(throughput)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if c.definition, err = canonical.Marshal(encoded); err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	return nil
}

func tooManyItems(items, limit int64) error {
	return fmt.Errorf("%d items is past snapshot_max_items (%d): raise it in the profile to snapshot this container: %w", items, limit, ErrTooManyItems)
}

func (c *Capture) storeDefinition() error {
	if c.definition == nil {
		return nil
	}
	h := pack.Sum(c.definition)
	c.record.Definition = h.String()
	return c.keep(h, c.definition)
}

// keep adds body to the writer unless it is stored already.
func (c *Capture) keep(h pack.Hash, body []byte) error {
	if c.packs.Has(h) || c.writer.Has(h) {
		return nil
	}
	if err := c.writer.Add(h, body); err != nil {
		return err
	}
	c.progress.BytesStored = c.writer.Stored()
	return nil
}

// readPage reads one page of the scan the phase needs and hands take each
// item. A page that fails is not taken; the scan is reopened at the last
// position the next time, which a throttled pager needs.
func (c *Capture) readPage(ctx context.Context, take func(json.RawMessage) error) error {
	if c.scan == nil {
		request := c.request
		request.From = c.position
		scan, err := c.source.Items.ScanItems(ctx, request)
		if err != nil {
			return err
		}
		c.scan = scan
	}
	page, err := c.scan.NextPage(ctx)
	if err != nil {
		closeErr := c.scan.Close()
		c.scan = nil
		return errors.Join(err, closeErr)
	}
	for _, item := range page.Items {
		if err := take(item); err != nil {
			return err
		}
		c.progress.Items++
		c.progress.PhaseItems++
		c.progress.BytesRead += int64(len(item))
	}
	c.progress.RequestCharge += page.RequestCharge
	c.position = page.Next
	if int64(len(c.current))+int64(len(c.swept)) > c.options.MaxItems {
		return tooManyItems(int64(len(c.current)+len(c.swept)), c.options.MaxItems)
	}
	if page.Next == "" || !c.scan.HasMore() {
		return c.endScan()
	}
	return nil
}

// takeWhole files a whole item: its body stored once, its entry in the
// manifest being built. Whatever a fetch returns wins over the sweep: it
// is a version at least as new.
func (c *Capture) takeWhole(item json.RawMessage) error {
	body, meta, err := adapter.SplitSystemFields(item)
	if err != nil {
		return err
	}
	key, err := ItemKey(body, c.keyPaths)
	if err != nil {
		return err
	}
	encoded, err := canonical.Marshal(body)
	if err != nil {
		return fmt.Errorf("item: %w", err)
	}
	h := pack.Sum(encoded)
	if err := c.keep(h, encoded); err != nil {
		return err
	}
	c.current[key] = Entry{Hash: h, Size: int64(len(encoded)), Version: fingerprint(meta.Version), ModifiedUnix: unixSeconds(meta.Modified)}
	delete(c.suspects, key)
	return nil
}

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// takeSwept files an item's identity: its key, version and modified time.
func (c *Capture) takeSwept(item json.RawMessage) error {
	meta, err := adapter.ReadItemMeta(item)
	if err != nil {
		return err
	}
	key, err := ItemKey(item, c.keyPaths)
	if err != nil {
		return err
	}
	c.swept[key] = Entry{Version: fingerprint(meta.Version), ModifiedUnix: unixSeconds(meta.Modified)}
	return nil
}

func (c *Capture) endScan() error {
	err := c.scan.Close()
	c.scan, c.position = nil, ""
	c.progress.PhaseItems = 0
	switch c.progress.Phase {
	case PhaseSweep:
		c.compare()
	default:
		c.progress.Phase = PhasePublish
	}
	return err
}

// compare carries forward every swept item whose version the parent holds,
// and makes the rest suspects, fetched from the oldest modified time among
// them: every suspect was modified at or after it, by construction. No
// clock but the data's own is consulted.
func (c *Capture) compare() {
	c.suspects = map[Key]bool{}
	var since int64
	for key, swept := range c.swept {
		parent, ok := c.parent[key]
		if ok && parent.Version == swept.Version {
			parent.ModifiedUnix = swept.ModifiedUnix
			c.current[key] = parent
			continue
		}
		if len(c.suspects) == 0 || swept.ModifiedUnix < since {
			since = swept.ModifiedUnix
		}
		c.suspects[key] = true
	}
	c.swept = nil
	if len(c.suspects) == 0 {
		c.progress.Phase = PhasePublish
		return
	}
	c.progress.Phase = PhaseFetch
	c.request.Projection = adapter.ScanWholeItems
	if since > 0 {
		c.request.Since = time.Unix(since, 0).UTC()
	}
}

// publish makes the snapshot exist: packs, then the change set and the
// manifest, then the record, whose rename is the commit. Only then is the
// parent's manifest removed; it is reachable through the change set.
func (c *Capture) publish() error {
	if err := c.writer.Close(); err != nil {
		return err
	}
	dir := c.store.loc.Dir()
	c.tallyContents()
	if c.record.Parent != "" {
		changes := Changes(c.parent, c.current)
		c.record.Added, c.record.Removed, c.record.Modified = tally(changes)
		path := filepath.Join(dir, changesDir, changesName(c.record.Parent, c.record.ID))
		if err := writeChanges(path, changes); err != nil {
			return err
		}
		c.record.StoredBytes += fileSize(path)
	}
	if err := writeManifest(manifestPath(dir, c.record.ID), c.current); err != nil {
		return err
	}
	c.record.Finished = c.options.Clock().UTC()
	if err := writeRecord(dir, c.record); err != nil {
		return err
	}
	c.store.records = append(c.store.records, c.record)
	c.progress.Phase, c.progress.Done, c.progress.Record = PhaseDone, true, c.record
	if c.record.Parent != "" {
		_ = removeAll([]string{manifestPath(dir, c.record.Parent)}) // left behind, it is debris the next Open removes
	}
	return c.Abort()
}

func (c *Capture) tallyContents() {
	c.record.Items = int64(len(c.current))
	c.record.Added = c.record.Items
	c.record.LogicalBytes = c.current.LogicalBytes()
	c.record.StoredBytes = c.writer.Stored()
	c.record.ReadBytes = c.progress.BytesRead
	c.record.RequestCharge = c.progress.RequestCharge
	c.progress.BytesStored = c.writer.Stored()
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
