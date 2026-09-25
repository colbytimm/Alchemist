package clone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/writers"
)

// readRetryAfter is the pause before a throttled read is tried again, when
// the backend named no delay.
const readRetryAfter = time.Second

// writeTimeout bounds one upsert, retries included.
const writeTimeout = 30 * time.Second

// Copy moves the items of one container. Whoever calls CopyPage owns it
// until the call returns: it is not safe for concurrent use.
type Copy struct {
	container ContainerPlan
	scanner   adapter.ItemScanner
	scan      adapter.ItemScan
	sink      adapter.ItemSink
	pool      *writers.Pool
	clock     writers.Clock
	position  adapter.ScanPosition
	done      bool
	skipped   int
}

// Progress is what one CopyPage did. A page that failed reports what it
// spent and nothing else: its items are written again when the copy
// resumes, and counted then.
type Progress struct {
	Read, Written, Skipped  int
	ReadCharge, WriteCharge float64
	Duration                time.Duration
	Writers                 int
	Throttles               int
	Skips                   []Skip
	Done                    bool
}

type Skip struct {
	ID     string
	Reason error
}

// Open starts the copy of the plan's container i from position from, the
// zero value being the beginning.
func (p Plan) Open(ctx context.Context, i int, from adapter.ScanPosition) (*Copy, error) {
	container := p.Containers[i]
	sink, err := p.target.Items.OpenItemSink(ctx, container.Target())
	if err != nil {
		return nil, fmt.Errorf("clone: open %s for writing: %w", strings.Join(container.Target(), "."), err)
	}
	c := &Copy{
		container: container,
		scanner:   p.source.Items,
		sink:      sink,
		pool:      writers.NewPool(p.Job.Writers, p.Job.clock()),
		clock:     p.Job.clock(),
		position:  from,
	}
	if err := c.openScan(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Copy) openScan(ctx context.Context) error {
	scan, err := c.scanner.ScanItems(ctx, adapter.ScanRequest{Container: c.container.Source, From: c.position})
	if err != nil {
		return fmt.Errorf("clone: read %s: %w", strings.Join(c.container.Source, "."), err)
	}
	c.scan = scan
	return nil
}

// Position is where a copy resumes: after the last page that was written in
// full. Resuming from it writes at most one page again, which an upsert
// makes harmless.
func (c *Copy) Position() adapter.ScanPosition { return c.position }

func (c *Copy) Done() bool { return c.done }

func (c *Copy) Close() error {
	if c.scan == nil {
		return nil
	}
	return c.scan.Close()
}

// CopyPage reads one page, strips the system fields of its items, and
// writes them through at most the job's number of writers. It returns once
// every item is written or skipped, or on the first failure; the position
// moves only in the first case.
func (c *Copy) CopyPage(ctx context.Context) (Progress, error) {
	start := time.Now()
	page, progress, err := c.readPage(ctx)
	if err != nil {
		return progress, err
	}
	bodies, skips := stripAll(page.Items)
	writes, refused := c.writes(bodies)
	outcomes := c.pool.Run(ctx, writes)
	progress.Read = len(page.Items)
	progress.Writers = c.pool.Size()
	written, err := fold(&progress, outcomes, refused, bodies)
	if err != nil {
		return spent(progress), c.writeError(ctx, err)
	}
	progress.Written = written
	progress.Skips = append(skips, progress.Skips...)
	progress.Skipped = len(progress.Skips)
	progress.Duration = time.Since(start)
	c.position = page.Next
	c.done = page.Next == "" || !c.scan.HasMore()
	progress.Done = c.done
	c.skipped += progress.Skipped
	if c.skipped > MaxSkipped {
		return progress, fmt.Errorf("clone: %s: %w", strings.Join(c.container.Target(), "."), ErrTooManySkipped)
	}
	return progress, nil
}

// spent is what a failed page cost, and nothing it did: the page is written
// again when the copy resumes.
func spent(p Progress) Progress {
	return Progress{ReadCharge: p.ReadCharge, WriteCharge: p.WriteCharge, Throttles: p.Throttles, Writers: p.Writers}
}

// writeError names a cancelled context as the reason a step ended, rather
// than the writes it kept from starting.
func (c *Copy) writeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("clone: write into %s: %w", strings.Join(c.container.Target(), "."), err)
}

// readPage reads the next page, waiting out a throttle and reading again
// from the last position: the source is never read in parallel.
func (c *Copy) readPage(ctx context.Context) (adapter.ItemPage, Progress, error) {
	var progress Progress
	for {
		page, err := c.scan.NextPage(ctx)
		progress.ReadCharge += page.RequestCharge
		var throttled *adapter.ThrottledError
		if !errors.As(err, &throttled) {
			if err != nil {
				return page, progress, fmt.Errorf("clone: read %s: %w", strings.Join(c.container.Source, "."), err)
			}
			return page, progress, nil
		}
		progress.Throttles++
		if progress.Throttles >= writers.MaxThrottles {
			return page, progress, fmt.Errorf("clone: read %s: %w", strings.Join(c.container.Source, "."), err)
		}
		if err := c.waitOut(ctx, throttled); err != nil {
			return page, progress, err
		}
		if err := c.reopen(ctx); err != nil {
			return page, progress, err
		}
	}
}

func (c *Copy) waitOut(ctx context.Context, throttled *adapter.ThrottledError) error {
	wait := throttled.RetryAfter
	if wait <= 0 {
		wait = readRetryAfter
	}
	select {
	case <-c.clock.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Copy) reopen(ctx context.Context) error {
	if err := c.scan.Close(); err != nil {
		return fmt.Errorf("clone: read %s: %w", strings.Join(c.container.Source, "."), err)
	}
	return c.openScan(ctx)
}

// stripAll removes every item's system fields. An item that is not an
// object is skipped: no target could take it.
func stripAll(items []json.RawMessage) ([]json.RawMessage, []Skip) {
	var bodies []json.RawMessage
	var skips []Skip
	for _, item := range items {
		body, err := StripSystemFields(item)
		if err != nil {
			skips = append(skips, Skip{ID: itemID(item), Reason: err})
			continue
		}
		bodies = append(bodies, body)
	}
	return bodies, skips
}

// writes builds one upsert per body. A body the target refuses for its own
// sake is recorded in refused, at its index, and counts as written: it must
// not end the step for every other item of the page. An upsert runs on a
// deadline of its own, not the step's: a stopped step starts no new write,
// and lets the ones in flight finish.
func (c *Copy) writes(bodies []json.RawMessage) ([]writers.Write, []error) {
	refused := make([]error, len(bodies))
	writes := make([]writers.Write, len(bodies))
	for i, body := range bodies {
		writes[i] = func(ctx context.Context) (float64, error) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
			defer cancel()
			charge, err := c.sink.Upsert(ctx, body)
			if errors.Is(err, adapter.ErrItemRefused) {
				refused[i] = err
				return charge, nil
			}
			return charge, err
		}
	}
	return writes, refused
}

func fold(progress *Progress, outcomes []writers.Outcome, refused []error, bodies []json.RawMessage) (int, error) {
	var failure error
	written := 0
	for i, outcome := range outcomes {
		progress.WriteCharge += outcome.RequestCharge
		progress.Throttles += outcome.Throttles
		switch {
		case outcome.Err != nil:
			if failure == nil || errors.Is(failure, writers.ErrNotStarted) {
				failure = outcome.Err
			}
		case refused[i] != nil:
			progress.Skips = append(progress.Skips, Skip{ID: itemID(bodies[i]), Reason: refused[i]})
		default:
			written++
		}
	}
	return written, failure
}

// StripSystemFields is item without the fields the backend writes on it,
// which the target assigns afresh.
func StripSystemFields(item json.RawMessage) (json.RawMessage, error) {
	body, _, err := adapter.SplitSystemFields(item)
	return body, err
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
