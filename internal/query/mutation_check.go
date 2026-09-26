package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// MaxPatchOperations is the service's limit on the operations of one patch.
const MaxPatchOperations = 10

// MutationCheck is what validation found. Any Problem refuses the
// statement; Warnings are for the review to show.
type MutationCheck struct {
	Problems []string
	Warnings []string
}

// CheckMutation validates m against its container's partition key paths,
// and lists every problem found, not only the first.
func CheckMutation(m Mutation, keyPaths []string) MutationCheck {
	c := mutationChecker{mutation: m, keyPaths: keyPaths}
	c.checkCount()
	for _, change := range c.changes() {
		c.checkPath(change)
	}
	c.checkOverlaps()
	c.warnScope()
	c.warnNested()
	return c.check
}

type mutationChecker struct {
	mutation Mutation
	keyPaths []string
	check    MutationCheck
}

// change is one path the statement writes, with the verb that writes it.
type change struct {
	verb string
	path FieldPath
}

func (c *mutationChecker) changes() []change {
	changes := make([]change, 0, c.mutation.Operations())
	for _, a := range c.mutation.Assignments {
		changes = append(changes, change{verb: "set", path: a.Path})
	}
	for _, path := range c.mutation.Removals {
		changes = append(changes, change{verb: "unset", path: path})
	}
	return changes
}

func (c *mutationChecker) problem(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if !slices.Contains(c.check.Problems, message) {
		c.check.Problems = append(c.check.Problems, message)
	}
}

// checkCount refuses more operations than one patch takes. A statement is
// never split into two patches: they would be two writes that can succeed
// and fail apart, per item.
func (c *mutationChecker) checkCount() {
	if count := c.mutation.Operations(); count > MaxPatchOperations {
		c.problem("a patch takes at most %d operations; this statement has %d. Two statements are two writes per item",
			MaxPatchOperations, count)
	}
}

func (c *mutationChecker) checkPath(ch change) {
	pointer := ch.path.Pointer()
	if pointer == "/id" {
		c.problem("cannot change id: that is a delete and a create")
	}
	for _, key := range c.keyPaths {
		if overlaps(pointer, key) {
			c.problem("cannot change the partition key %s: that is a delete and a create", key)
		}
	}
	if first := ch.path.Steps[0]; !first.IsIndex && adapter.IsSystemField(first.Name) {
		c.problem("cannot %s %s: the service owns it", ch.verb, first.Name)
	}
}

func (c *mutationChecker) checkOverlaps() {
	changes := c.changes()
	for i, a := range changes {
		for _, b := range changes[i+1:] {
			switch {
			case a.path.Pointer() == b.path.Pointer():
				c.problem("%s appears twice", a.path)
			case overlaps(a.path.Pointer(), b.path.Pointer()):
				c.problem("%s and %s overlap", a.path, b.path)
			}
		}
	}
}

// overlaps reports whether two pointers name one place, or one lies within
// the other.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// warnScope says when the selection reads every item, or every partition.
func (c *mutationChecker) warnScope() {
	target := strings.Join(c.mutation.Target, ".")
	switch {
	case c.mutation.EveryItem && c.mutation.Kind == MutationDelete:
		c.check.Warnings = append(c.check.Warnings, fmt.Sprintf("Every item in %s. Deleting and recreating the container "+
			"(d, then c in the catalog) spends no RU per item, but it is a new container and its settings must be given again.", target))
	case c.mutation.EveryItem:
		c.check.Warnings = append(c.check.Warnings, fmt.Sprintf("WHERE true: every item in %s is a target.", target))
	case len(c.keyPaths) > 0 && !pinsKey(c.mutation, c.keyPaths[0]):
		c.check.Warnings = append(c.check.Warnings,
			fmt.Sprintf("The WHERE does not pin %s: the selection reads every partition.", c.keyPaths[0]))
	}
}

// warnNested says what a SET below the top level needs: a patch's set
// creates the last step of its path, never a parent.
func (c *mutationChecker) warnNested() {
	for _, a := range c.mutation.Assignments {
		if last := len(a.Path.Steps) - 1; last > 0 {
			parent := FieldPath{Alias: a.Path.Alias, Steps: a.Path.Steps[:last]}
			c.check.Warnings = append(c.check.Warnings,
				fmt.Sprintf("SET %s needs %s on each item: a patch creates only the last step of a path, so an item without it is skipped, not written.", a.Path, parent))
		}
	}
}

// pinsKey reports whether m's condition holds, at its top level, an
// equality between the key path and a literal. It is the one thing read out
// of a condition, and only to warn.
func pinsKey(m Mutation, keyPath string) bool {
	conjuncts, err := splitConjuncts(code(lex(m.Where)))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(conjuncts, func(conjunct []token) bool {
		pointer, ok := equalityPath(m.Where, conjunct, m.Alias)
		return ok && pointer == keyPath
	})
}

// equalityPath reads `<path> = <literal>`, either way round, and returns
// the path's pointer.
func equalityPath(text string, conjunct []token, alias string) (string, bool) {
	eq := slices.IndexFunc(conjunct, func(tok token) bool { return isSymbol(tok, "=") })
	if eq <= 0 || isComparison(conjunct[eq-1]) {
		return "", false
	}
	left, right := conjunct[:eq], conjunct[eq+1:]
	if pointer, ok := pathOf(text, left, alias); ok && isLiteral(right) {
		return pointer, true
	}
	if pointer, ok := pathOf(text, right, alias); ok && isLiteral(left) {
		return pointer, true
	}
	return "", false
}

// isComparison marks the first half of <=, >= and !=, which the lexer
// splits.
func isComparison(tok token) bool {
	return isSymbol(tok, "<") || isSymbol(tok, ">") || isSymbol(tok, "!")
}

func pathOf(text string, toks []token, alias string) (string, bool) {
	p := &mutationParser{statementReader: statementReader{text: text, toks: toks, syntaxError: mutationSyntaxError}}
	path, err := p.parseFieldPath()
	if err != nil || !p.done() || path.Alias != alias || len(path.Steps) == 0 {
		return "", false
	}
	return path.Pointer(), true
}

func isLiteral(toks []token) bool {
	if len(toks) == 2 && isSymbol(toks[0], "-") {
		toks = toks[1:]
	}
	if len(toks) != 1 {
		return false
	}
	tok := toks[0]
	return tok.kind == tokString || tok.kind == tokNumber ||
		tok.kind == tokIdent && (tok.upper == "TRUE" || tok.upper == "FALSE" || tok.upper == "NULL")
}
