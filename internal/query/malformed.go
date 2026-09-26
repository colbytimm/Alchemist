package query

import "slices"

// checkPlan refuses a plan whose tree does not fit its leaves, so that
// executing it can index nothing out of range: every leaf exists, every step
// attaches an input to an earlier one, and every alias a step, column or
// test names is bound by the input it reads.
func checkPlan(plan Plan) error {
	if !(planFit{leaves: len(plan.Leaves)}).node(plan.Root, false) {
		return errMalformed
	}
	return nil
}

type planFit struct {
	leaves int
}

// node checks n; flattened marks a join read as a CTE's items, which may
// then have no step and project its one input.
func (f planFit) node(n Node, flattened bool) bool {
	switch n := n.(type) {
	case *Scan:
		return n != nil && f.leaf(n.Leaf)
	case *Union:
		return n != nil && len(n.Leaves) > 0 && !slices.ContainsFunc(n.Leaves, func(i int) bool { return !f.leaf(i) })
	case *Join:
		return n != nil && f.join(n, flattened)
	case *Flatten:
		return n != nil && n.Input != nil && len(n.Input.Columns) > 0 && f.join(n.Input, true)
	case *Materialize:
		return n != nil && f.node(n.Input, false)
	}
	return false
}

func (f planFit) leaf(i int) bool {
	return i >= 0 && i < f.leaves
}

func (f planFit) join(j *Join, flattened bool) bool {
	if len(j.Inputs) == 0 || len(j.Steps) != len(j.Inputs)-1 {
		return false
	}
	if len(j.Steps) == 0 && len(j.Inputs[0].Applies) == 0 && len(j.Columns) == 0 && !flattened {
		return false
	}
	for _, input := range j.Inputs {
		if _, nested := input.Rows.(*Join); nested || !f.node(input.Rows, false) || !appliesFit(input) {
			return false
		}
	}
	for i, step := range j.Steps {
		if !stepFits(j, i, step) {
			return false
		}
	}
	for _, column := range j.Columns {
		if column.Side < 0 || column.Side >= len(j.Inputs) || !binds(j.Inputs[column.Side], column.Alias) {
			return false
		}
	}
	return !slices.ContainsFunc(j.Absent, func(alias string) bool {
		return !slices.ContainsFunc(j.Inputs, func(s Source) bool { return binds(s, alias) })
	})
}

func stepFits(j *Join, i int, step JoinStep) bool {
	joined := i + 1
	switch {
	case step.Kind < InnerJoin || step.Kind > CrossJoin, step.Left < 0, step.Left > i:
		return false
	case step.Kind == CrossJoin:
		return len(j.Inputs) == 2
	}
	return binds(j.Inputs[step.Left], step.LeftKey.Alias) && step.RightKey.Alias == j.Inputs[joined].Alias
}

// appliesFit reports whether each APPLY reads its input's alias or an
// earlier APPLY's.
func appliesFit(s Source) bool {
	bound := []string{s.Alias}
	for _, apply := range s.Applies {
		if !slices.Contains(bound, apply.Array.Alias) {
			return false
		}
		bound = append(bound, apply.Alias)
	}
	return true
}

func binds(s Source, alias string) bool {
	return s.Alias == alias || slices.ContainsFunc(s.Applies, func(a Apply) bool { return a.Alias == alias })
}
