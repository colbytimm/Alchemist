package query

import (
	"slices"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// maxMergedPageRows caps a merged page, which one streamed page can fan out
// to many times its own size.
const maxMergedPageRows = 1000

// hop is one step of the pipeline, oriented away from the streamed input: a
// row of from, already in the combination, finds its matches in the table
// of into.
type hop struct {
	from, into       int // Join.Inputs indexes
	fromKey, intoKey FieldRef
	// keepFrom carries a combination nothing matched on, with into absent.
	keepFrom bool
	// keepInto flushes the held rows nothing matched once the stream ends.
	keepInto bool
}

// streamedSide picks the input not to hold in memory. An outer join is not
// associative, so a chain with an outer step streams its first input and
// runs its steps in written order, which is how SQL defines it. Otherwise
// the streamed input is the one expected to be largest: sizes are unknown
// before the queries run, so a filter is the only evidence of a small input,
// and among the unfiltered ones the FROM container is conventionally the
// fact table.
func (r *execution) streamedSide(join *Join) int {
	if len(join.Steps) > 1 && slices.ContainsFunc(join.Steps, func(s JoinStep) bool { return s.Kind != InnerJoin }) {
		return 0
	}
	return max(slices.IndexFunc(join.Inputs, func(s Source) bool { return !r.filtered(s) }), 0)
}

func (r *execution) filtered(source Source) bool {
	scan, ok := source.Rows.(*Scan)
	return ok && r.plan.Leaves[scan.Leaf].Filtered
}

// orientSteps roots the join tree at the streamed input: it repeatedly takes
// the first step in written order with exactly one end reached. Rooted at
// the first input, that is written order.
func orientSteps(join *Join, streamed int) []hop {
	steps := join.Steps
	reached := make([]bool, len(join.Inputs))
	reached[streamed] = true
	taken := make([]bool, len(steps))
	var hops []hop
	for len(hops) < len(steps) {
		for i, step := range steps {
			joined := i + 1
			if taken[i] || reached[step.Left] == reached[joined] {
				continue
			}
			next := forward(step, joined)
			if reached[joined] {
				next = backward(step, joined)
			}
			hops = append(hops, next)
			taken[i], reached[next.into] = true, true
			break
		}
	}
	return hops
}

// forward runs a step from its earlier input into the joined one.
func forward(step JoinStep, joined int) hop {
	return hop{
		from: step.Left, into: joined, fromKey: step.LeftKey, intoKey: step.RightKey,
		keepFrom: step.Kind == LeftOuterJoin || step.Kind == FullOuterJoin,
		keepInto: step.Kind == RightOuterJoin || step.Kind == FullOuterJoin,
	}
}

func backward(step JoinStep, joined int) hop {
	return hop{
		from: joined, into: step.Left, fromKey: step.RightKey, intoKey: step.LeftKey,
		keepFrom: step.Kind == RightOuterJoin || step.Kind == FullOuterJoin,
		keepInto: step.Kind == LeftOuterJoin || step.Kind == FullOuterJoin,
	}
}

// seed is a combination waiting to run through the hops from hop on: a
// streamed row from the first, or a flushed held row from the hop after
// its own.
type seed struct {
	combination combination
	hop         int
}

// pendingRows is what was read or flushed so far beyond the pages served:
// the combinations of the seed being expanded, then the seeds not expanded
// yet.
type pendingRows struct {
	combinations []combination
	seeds        []seed
}

func (p pendingRows) empty() bool {
	return len(p.combinations) == 0 && len(p.seeds) == 0
}

// serve moves unserved rows onto page until it is full, expanding one seed
// at a time. The header is settled first, from every page read so far.
func (j *joinCursor) serve(page *adapter.Page) {
	if !j.unserved.empty() {
		j.rel.placeInWrittenOrder()
	}
	for len(page.Rows) < maxMergedPageRows && !j.unserved.empty() {
		if len(j.unserved.combinations) == 0 {
			j.unserved.combinations = j.expand(j.unserved.seeds[0])
			j.unserved.seeds = j.unserved.seeds[1:]
			continue
		}
		j.rel.appendRow(page, j.unserved.combinations[0])
		j.unserved.combinations = j.unserved.combinations[1:]
	}
}

// expand extends a seed by every match at every hop still to run. A
// combination with no match at a hop goes on with that hop's input absent
// where the hop keeps it, and is dropped, as an inner join drops it,
// everywhere else.
func (j *joinCursor) expand(s seed) []combination {
	combinations := []combination{s.combination}
	for h := s.hop; h < len(j.hops); h++ {
		var extended []combination
		for _, c := range combinations {
			matches := j.matches(h, c)
			if len(matches) == 0 && j.hops[h].keepFrom {
				extended = append(extended, c)
			}
			for _, match := range matches {
				next := slices.Clone(c)
				next[j.hops[h].into] = match
				extended = append(extended, next)
			}
		}
		combinations = extended
	}
	return slices.DeleteFunc(combinations, j.rel.hasPresentAbsent)
}

// matches are the held rows of hop h that c's from row matches, flagged as
// matched.
func (j *joinCursor) matches(h int, c combination) []joinRow {
	from := c[j.hops[h].from]
	if !from.present() || from.keys[h] == "" {
		return nil
	}
	return j.tables[h].match(from.keys[h])
}

// hasPresentAbsent reports whether c holds an alias WHERE NOT IS_DEFINED
// wants absent.
func (rel *relation) hasPresentAbsent(c combination) bool {
	for _, alias := range rel.absent {
		row := c[alias.input]
		if row.present() && row.members[alias.slot].raw != nil {
			return true
		}
	}
	return false
}

// aliasSlot is where an alias's value sits in a combination.
type aliasSlot struct {
	input int
	slot  int
}

// flushNext seeds the held rows nothing matched of the next hop that keeps
// them, and reports whether there was one. Hops flush in written order, so a
// hop's unmatched set is final when its turn comes: rows flushed by an
// earlier hop can still match it.
func (j *joinCursor) flushNext() bool {
	for j.flushed < len(j.hops) {
		h := j.flushed
		j.flushed++
		if !j.hops[h].keepInto || j.tables[h].unmatched == 0 {
			continue
		}
		for _, row := range j.tables[h].unmatchedRows() {
			c := make(combination, len(j.rel.inputs))
			c[j.hops[h].into] = row
			j.unserved.seeds = append(j.unserved.seeds, seed{combination: c, hop: h + 1})
		}
		return true
	}
	return false
}

// flushPending reports whether a hop still to flush has an unmatched row.
func (j *joinCursor) flushPending() bool {
	for h := j.flushed; h < len(j.hops); h++ {
		if j.hops[h].keepInto && j.tables[h].unmatched > 0 {
			return true
		}
	}
	return false
}

// flushFollows reports whether a hop from h on flushes held rows, which an
// empty table at h cannot stop.
func (j *joinCursor) flushFollows(h int) bool {
	return slices.ContainsFunc(j.hops[h:], func(later hop) bool { return later.keepInto })
}
