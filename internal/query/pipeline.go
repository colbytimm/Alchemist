package query

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// maxMergedPageRows caps a merged page, which one streamed page can fan out
// to many times its own size.
const maxMergedPageRows = 1000

// hop is one step of the pipeline, oriented away from the streamed side: a row
// of from, already in the combination, finds its matches in the table of into.
type hop struct {
	from, into       int // Plan.Leaves indexes
	fromKey, intoKey []string
}

// keyOf is the key path the hop reads on side, if it touches that side.
func (h hop) keyOf(side int) ([]string, bool) {
	switch side {
	case h.from:
		return h.fromKey, true
	case h.into:
		return h.intoKey, true
	}
	return nil, false
}

// streamedSide picks the side not to hold in memory: the one expected to be
// largest. Sizes are unknown before the queries run, so a filter is the only
// evidence of a small side, and among the unfiltered ones the FROM container
// is conventionally the fact table.
func streamedSide(plan Plan) int {
	return max(slices.IndexFunc(plan.Leaves, func(leaf Leaf) bool { return !leaf.Filtered }), 0)
}

// orientSteps roots the join tree at the streamed side: it repeatedly takes
// the first step in written order with exactly one end reached.
func orientSteps(plan Plan, streamed int) []hop {
	steps := plan.Join.Steps
	reached := make([]bool, len(plan.Leaves))
	reached[streamed] = true
	taken := make([]bool, len(steps))
	var hops []hop
	for len(hops) < len(steps) {
		for i, step := range steps {
			joined := i + 1
			if taken[i] || reached[step.Left] == reached[joined] {
				continue
			}
			next := hop{from: step.Left, into: joined, fromKey: step.LeftKey, intoKey: step.RightKey}
			if reached[joined] {
				next = hop{from: joined, into: step.Left, fromKey: step.RightKey, intoKey: step.LeftKey}
			}
			hops = append(hops, next)
			taken[i], reached[next.into] = true, true
			break
		}
	}
	return hops
}

// combination is one candidate merged row: an item per side, indexed as
// Plan.Leaves. A side the pipeline has not reached yet is the zero joinRow.
type combination []joinRow

// pendingRows is what the streamed pages read so far produced beyond the
// pages served: the combinations of the streamed row being expanded, then
// the streamed rows not expanded yet.
type pendingRows struct {
	combinations []combination
	streamed     []joinRow
}

func (p pendingRows) empty() bool {
	return len(p.combinations) == 0 && len(p.streamed) == 0
}

// nextMatches serves unserved rows, reading a streamed page only while it
// has none, so a stretch of unmatched rows never surfaces as an empty page
// with more to come.
func (j *joinCursor) nextMatches(ctx context.Context, meter *meter) (adapter.Page, error) {
	if !j.built {
		if err := j.build(ctx, meter); err != nil {
			return adapter.Page{}, err
		}
	}
	var page adapter.Page
	for {
		j.serve(&page)
		streamed := &j.sides[j.streamed]
		if len(page.Rows) > 0 || streamed.cursor == nil {
			return page, nil
		}
		rows, err := j.readRows(ctx, streamed, meter)
		if err != nil {
			return adapter.Page{}, err
		}
		j.placeInWrittenOrder()
		j.unserved.streamed = rows
	}
}

// serve moves unserved rows onto page until it is full, expanding one
// streamed row at a time.
func (j *joinCursor) serve(page *adapter.Page) {
	for len(page.Rows) < maxMergedPageRows && !j.unserved.empty() {
		if len(j.unserved.combinations) == 0 {
			j.unserved.combinations = j.expand(j.unserved.streamed[0])
			j.unserved.streamed = j.unserved.streamed[1:]
			continue
		}
		j.appendRow(page, j.unserved.combinations[0])
		j.unserved.combinations = j.unserved.combinations[1:]
	}
}

// expand extends a streamed row by every match at every hop; a combination
// with no match at some hop is dropped, as an inner join drops it.
func (j *joinCursor) expand(streamed joinRow) []combination {
	start := make(combination, len(j.sides))
	start[j.streamed] = streamed
	combinations := []combination{start}
	for h, hop := range j.hops {
		var extended []combination
		for _, c := range combinations {
			for _, match := range j.tables[h][c[hop.from].keys[h]] {
				next := slices.Clone(c)
				next[hop.into] = match
				extended = append(extended, next)
			}
		}
		combinations = extended
	}
	return combinations
}

func (j *joinCursor) appendRow(page *adapter.Page, c combination) {
	row := make([]string, len(j.columns.names))
	for _, member := range c {
		member.page.fill(row, member.cells)
	}
	page.Rows = append(page.Rows, row)
	page.Raw = append(page.Raw, j.combinedRaw(c))
}

// combinedRaw nests every item under its alias in written order. Aliases are
// identifiers, and so quote the same in Go as in JSON.
func (j *joinCursor) combinedRaw(c combination) json.RawMessage {
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, member := range c {
		if i > 0 {
			raw.WriteByte(',')
		}
		raw.WriteString(strconv.Quote(j.sides[i].leaf.Alias))
		raw.WriteByte(':')
		raw.Write(member.raw)
	}
	raw.WriteByte('}')
	return raw.Bytes()
}
