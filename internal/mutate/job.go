package mutate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/writers"
)

const (
	ChunkSize = 100
	// MaxFailures is how many items may fail before a job stops: past
	// that, something systematic is wrong.
	MaxFailures = 100
	// MaxUnknown is how many writes in a row may go unanswered before a
	// job stops: the network is gone.
	MaxUnknown = 3
	// PlanningChargePerPatch is the request units a review plans on for a
	// patch of a small item. It is a rough figure, labeled as one.
	PlanningChargePerPatch = 10
	// writeTimeout bounds one write. A write is never cancelled by a stop,
	// only by this: cancelling a sent write is how an unknown outcome is
	// made.
	writeTimeout = 30 * time.Second
)

var (
	// ErrProbeRefused is a job whose first write was refused: whatever the
	// service objects to is likely to refuse every other write too.
	ErrProbeRefused = errors.New("the service refused the first write, so no other was sent. " +
		"A WHERE it takes for a query but not as a patch condition is refused this way; " +
		"a few items can be patched in a BEGIN BATCH with IF MATCH")
	ErrTooManyFailures = fmt.Errorf("more than %d items failed", MaxFailures)
	ErrNoAnswer        = fmt.Errorf("%d writes in a row had no answer: the network may be gone", MaxUnknown)
)

// UnknownAdvice is the status of an item whose write had no answer.
const UnknownAdvice = "no answer: check before assuming. Running the statement again settles it"

type Outcome int

const (
	NotAttempted Outcome = iota
	Applied
	SkippedChanged
	SkippedGone
	SkippedNoKey
	SkippedNoParent
	Failed
	Unknown
)

// Label is how a report names the outcome of a statement of kind.
func (o Outcome) Label(kind query.MutationKind) string {
	switch o {
	case Applied:
		return kind.Applied()
	case SkippedChanged:
		return "skipped: changed"
	case SkippedGone:
		return "skipped: gone"
	case SkippedNoKey:
		return "skipped: no partition key"
	case SkippedNoParent:
		return "skipped: no parent"
	case Failed:
		return "failed"
	case Unknown:
		return "unknown"
	}
	return "not attempted"
}

// Result is what became of one target.
type Result struct {
	Outcome       Outcome
	Status        string
	ETag          string
	RequestCharge float64
	Err           error
}

// Counts are the targets by outcome.
type Counts struct {
	Applied, Changed, Gone, NoKey, NoParent, Failed, Unknown, NotAttempted int
}

// Attempted counts the targets a write was sent for.
func (c Counts) Attempted() int {
	return c.Applied + c.Changed + c.Gone + c.Failed + c.Unknown
}

func (c Counts) Skipped() int {
	return c.Changed + c.Gone + c.NoKey + c.NoParent
}

func (c *Counts) add(o Outcome, n int) {
	switch o {
	case Applied:
		c.Applied += n
	case SkippedChanged:
		c.Changed += n
	case SkippedGone:
		c.Gone += n
	case SkippedNoKey:
		c.NoKey += n
	case SkippedNoParent:
		c.NoParent += n
	case Failed:
		c.Failed += n
	case Unknown:
		c.Unknown += n
	default:
		c.NotAttempted += n
	}
}

// Progress is where a job has got after a step.
type Progress struct {
	Counts      Counts
	Total       int
	WriteCharge float64
	Throttles   int
	Writers     int
	// Items and Duration are the step's own: what it attempted, and how
	// long it took.
	Items    int
	Duration time.Duration
	Done     bool
}

// Job writes the targets of a selection, a chunk at a time, through a pool
// of writers, and keeps one Result per target. Whoever calls ApplyChunk
// owns it until the call returns: it is not safe for concurrent use.
type Job struct {
	mutation query.Mutation
	targets  Targets
	editor   adapter.ItemEditor
	pool     *writers.Pool
	results  []Result
	counts   Counts
	// next is the first target that may still be NotAttempted.
	next int
	// proven marks a job whose writes the service has shown it takes:
	// until one is, the job sends them one at a time.
	proven bool
	// failures and unanswered are judged from the job's start, or its
	// last resume: failures in all, unanswered in a row.
	failures    int
	unanswered  int
	writeCharge float64
	throttles   int
	elapsed     time.Duration
}

// NewJob prepares the writes of targets. A target with no partition key,
// or lacking the parent of a path the statement sets, is skipped from the
// start; nothing is sent until ApplyChunk.
func NewJob(m query.Mutation, targets Targets, editor adapter.ItemEditor, pool *writers.Pool) *Job {
	j := &Job{mutation: m, targets: targets, editor: editor, pool: pool, results: make([]Result, len(targets.Items))}
	for i, target := range targets.Items {
		switch {
		case target.Key == nil:
			j.results[i] = Result{Outcome: SkippedNoKey, Status: "no partition key"}
		case target.NoParent:
			j.results[i] = Result{Outcome: SkippedNoParent, Status: "a path the statement sets has no parent on this item"}
		}
		j.counts.add(j.results[i].Outcome, 1)
	}
	j.advance()
	return j
}

func (j *Job) Done() bool { return j.next >= len(j.results) }

// Resume starts the judgement of failures afresh, for a job the user
// chose to carry on with after it stopped.
func (j *Job) Resume() {
	j.failures, j.unanswered = 0, 0
}

// Pending lists the ids the next step will write, in order.
func (j *Job) Pending() []string {
	var ids []string
	for _, i := range j.pending() {
		ids = append(ids, j.targets.Items[i].ID)
	}
	return ids
}

// ApplyChunk writes the next chunk and returns once each write has an
// outcome. ctx is the stop: cancelling it starts no new write, and lets
// the ones in flight finish, each on a deadline of its own. It returns an
// error when the step ended short, or when the outcomes so far say the job
// should not go on.
func (j *Job) ApplyChunk(ctx context.Context) (Progress, error) {
	start := time.Now()
	indices := j.pending()
	writes := make([]writers.Write, len(indices))
	for k, i := range indices {
		writes[k] = j.write(i)
	}
	outcomes := j.pool.Run(ctx, writes)
	ended := j.fold(indices, outcomes)
	duration := time.Since(start)
	j.elapsed += duration
	progress := j.progress(indices, duration)
	switch {
	case ended != nil:
		return progress, fmt.Errorf("mutate: write into %s: %w", strings.Join(j.mutation.Target, "."), ended)
	case ctx.Err() != nil && !j.Done():
		return progress, ctx.Err()
	}
	return progress, j.judge(indices)
}

// pending is the chunk the next step writes: one target at a time until a
// write has been taken, ChunkSize after.
func (j *Job) pending() []int {
	size := ChunkSize
	if !j.proven {
		size = 1
	}
	var indices []int
	for i := j.next; i < len(j.results) && len(indices) < size; i++ {
		if j.results[i].Outcome == NotAttempted {
			indices = append(indices, i)
		}
	}
	return indices
}

// write edits target i and records what became of it, in the slot only it
// writes. It returns an error only for a throttle, which is the pool's to
// wait out and retry: anything else is the item's outcome, and must not
// stop its neighbors.
func (j *Job) write(i int) writers.Write {
	target := j.targets.Items[i]
	op := operation(j.mutation, target)
	return func(ctx context.Context) (float64, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
		defer cancel()
		result, err := j.editor.EditItem(ctx, j.mutation.Target, target.Key, op)
		var throttled *adapter.ThrottledError
		if errors.As(err, &throttled) {
			return result.RequestCharge, err
		}
		j.results[i] = resultOf(result, err)
		return result.RequestCharge, nil
	}
}

func resultOf(result adapter.OperationResult, err error) Result {
	r := Result{Outcome: outcomeOf(err), Status: result.Status, ETag: result.ETag, Err: err}
	switch {
	case r.Outcome == Unknown:
		r.Status = UnknownAdvice
	case r.Outcome == Failed && err != nil:
		r.Status = err.Error()
	case r.Outcome == SkippedNoKey:
		r.Status = "no partition key"
	}
	return r
}

func outcomeOf(err error) Outcome {
	switch {
	case err == nil:
		return Applied
	case errors.Is(err, adapter.ErrPreconditionFailed):
		return SkippedChanged
	case errors.Is(err, adapter.ErrItemNotFound):
		return SkippedGone
	case errors.Is(err, adapter.ErrNoPartitionKey):
		return SkippedNoKey
	case errors.Is(err, adapter.ErrWriteOutcomeUnknown):
		return Unknown
	}
	return Failed
}

// fold takes in what the pool did. A write it never started, or one that
// met too many throttles, was not applied, and stays to be written when
// the job resumes; the second ends the step.
func (j *Job) fold(indices []int, outcomes []writers.Outcome) error {
	var ended error
	for k, i := range indices {
		outcome := outcomes[k]
		j.writeCharge += outcome.RequestCharge
		j.throttles += outcome.Throttles
		if outcome.Err != nil {
			if ended == nil && !errors.Is(outcome.Err, writers.ErrNotStarted) && !errors.Is(outcome.Err, context.Canceled) {
				ended = outcome.Err
			}
			continue
		}
		j.results[i].RequestCharge = outcome.RequestCharge
		j.counts.add(NotAttempted, -1)
		j.counts.add(j.results[i].Outcome, 1)
	}
	j.advance()
	return ended
}

func (j *Job) advance() {
	for j.next < len(j.results) && j.results[j.next].Outcome != NotAttempted {
		j.next++
	}
}

// judge decides, in target order, whether the outcomes so far let the job
// go on: the service must take the first write it answers, fail fewer than
// MaxFailures, and answer at least one of every MaxUnknown in a row.
func (j *Job) judge(indices []int) error {
	for _, i := range indices {
		result := j.results[i]
		switch result.Outcome {
		case NotAttempted:
			continue
		case Unknown:
			j.unanswered++
		default:
			j.unanswered = 0
		}
		if result.Outcome == Failed {
			j.failures++
		}
		switch {
		case j.unanswered >= MaxUnknown:
			return ErrNoAnswer
		case !j.proven && result.Outcome == Failed:
			return fmt.Errorf("%w: %w", ErrProbeRefused, result.Err)
		case result.Outcome == Applied || result.Outcome == SkippedChanged:
			j.proven = true
		}
	}
	if j.failures > MaxFailures {
		return ErrTooManyFailures
	}
	return nil
}

func (j *Job) progress(indices []int, duration time.Duration) Progress {
	attempted := 0
	for _, i := range indices {
		if j.results[i].Outcome != NotAttempted {
			attempted++
		}
	}
	return Progress{
		Counts:      j.counts,
		Total:       len(j.results),
		WriteCharge: j.writeCharge,
		Throttles:   j.throttles,
		Writers:     j.pool.Size(),
		Items:       attempted,
		Duration:    duration,
		Done:        j.Done(),
	}
}

// Summary is what the job did, whatever it did.
type Summary struct {
	Kind            query.MutationKind
	Container       []string
	Counts          Counts
	Total           int
	SelectionCharge float64
	WriteCharge     float64
	Throttles       int
	Elapsed         time.Duration
}

func (j *Job) Summary() Summary {
	return Summary{
		Kind:            j.mutation.Kind,
		Container:       j.mutation.Target,
		Counts:          j.counts,
		Total:           len(j.results),
		SelectionCharge: j.targets.RequestCharge,
		WriteCharge:     j.writeCharge,
		Throttles:       j.throttles,
		Elapsed:         j.elapsed,
	}
}

// Clean reports a job that wrote every target it could: none failed, none
// went unanswered, none was left unattempted. Skips are expected.
func (s Summary) Clean() bool {
	return s.Counts.Failed == 0 && s.Counts.Unknown == 0 && s.Counts.NotAttempted == 0
}

// Sentence says in words what the job did, as in "Updated 409 of 412
// items in sales.orders. 2 had changed, 1 was gone." for an update.
func (s Summary) Sentence() string {
	c := s.Counts
	verb := s.Kind.Applied()
	text := fmt.Sprintf("%s%s %s of %s in %s.", strings.ToUpper(verb[:1]), verb[1:], formatCount(c.Applied),
		items(s.Total), strings.Join(s.Container, "."))
	var parts []string
	for _, detail := range []struct {
		n    int
		text string
	}{
		{c.Changed, formatCount(c.Changed) + " had changed"},
		{c.Gone, plural(c.Gone, "was gone", "were gone")},
		{c.NoKey, formatCount(c.NoKey) + " had no partition key"},
		{c.NoParent, formatCount(c.NoParent) + " had no parent for a nested SET"},
		{c.Failed, formatCount(c.Failed) + " failed"},
		{c.Unknown, formatCount(c.Unknown) + " had no answer"},
	} {
		if detail.n > 0 {
			parts = append(parts, detail.text)
		}
	}
	if len(parts) > 0 {
		text += " " + strings.Join(parts, ", ") + "."
	}
	if c.NotAttempted > 0 {
		text += " " + plural(c.NotAttempted, "was not attempted and is unchanged.", "were not attempted and are unchanged.")
	}
	return text
}

func items(n int) string {
	if n == 1 {
		return "1 item"
	}
	return formatCount(n) + " items"
}

// plural writes n before the verb phrase that agrees with it: "1 was
// gone", "2 were gone".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return formatCount(n) + " " + many
}
