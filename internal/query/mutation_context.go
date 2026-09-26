package query

import "slices"

// Token counts of an update's head, up to the cursor.
const (
	afterUpdate       = 1 // UPDATE
	afterTargetDot    = 3 // UPDATE db .
	afterTarget       = 4 // UPDATE db . container
	afterAliasKeyword = 5 // UPDATE db . container AS
)

// valueKeywords are the literals an assignment takes that are words.
var valueKeywords = []string{"TRUE", "FALSE", "NULL"}

// mutationCompletion completes an update: its target, a database then a
// container, the clause words where the grammar takes them, the target's
// fields after SET, UNSET and in the WHERE, and only literals after =.
func mutationCompletion(before []token, typing bool) Completion {
	n := len(before)
	switch {
	case n == afterUpdate:
		return Completion{Kind: CompleteDatabase}
	case n == afterTargetDot && before[2].kind == tokDot:
		return Completion{Kind: CompleteContainer, Database: before[1].text}
	case n < afterTarget || before[2].kind != tokDot:
		return Completion{}
	}
	u := updateCompleter{toks: before, target: []string{before[1].text, before[3].text}, typing: typing}
	return u.complete()
}

type updateCompleter struct {
	toks   []token
	target []string
	typing bool
}

func (u updateCompleter) complete() Completion {
	clause, start := u.clause()
	since := u.toks[start:]
	if last := u.toks[len(u.toks)-1]; last.kind == tokDot && clause != "" {
		if clause == "SET" && slices.ContainsFunc(since, func(tok token) bool { return isSymbol(tok, "=") }) {
			return Completion{}
		}
		return u.afterDot(clause)
	}
	switch clause {
	case "SET":
		return u.inSet(since)
	case "UNSET":
		return u.inUnset(since)
	case "WHERE":
		return u.inWhere(since)
	}
	return u.afterHead()
}

// clause is the last of SET, UNSET and WHERE before the cursor, and where
// the part of it the cursor is in starts: after the keyword, or after the
// last comma between two changes.
func (u updateCompleter) clause() (string, int) {
	clause, start := "", len(u.toks)
	for i := afterTarget; i < len(u.toks); i++ {
		tok := u.toks[i]
		switch {
		case tok.kind == tokIdent && mutationClause(tok.upper) && !followsDot(u.toks, i):
			clause, start = tok.upper, i+1
		case tok.kind == tokComma && clause != "WHERE":
			start = i + 1
		}
	}
	return clause, start
}

func mutationClause(word string) bool {
	return word == "SET" || word == "UNSET" || word == "WHERE"
}

// afterHead completes what follows the target: an alias, which is the
// person's to invent, then the clause that starts the changes.
func (u updateCompleter) afterHead() Completion {
	switch n := len(u.toks); {
	case n == afterTarget && u.typing:
		return Completion{}
	case n == afterTarget:
		return keywordsOnly([]string{"SET", "UNSET", "AS"})
	case keywordAt(u.toks, afterTarget, "AS") && n == afterAliasKeyword:
		return Completion{}
	}
	return keywordsOnly([]string{"SET", "UNSET"})
}

func (u updateCompleter) alias() Alias {
	name := defaultAlias
	switch {
	case keywordAt(u.toks, afterTarget, "AS") && len(u.toks) > afterAliasKeyword:
		name = u.toks[afterAliasKeyword].text
	case len(u.toks) > afterTarget && u.toks[afterTarget].kind == tokIdent && !mutationClause(u.toks[afterTarget].upper):
		name = u.toks[afterTarget].text
	}
	return Alias{Name: name, Scopes: [][]string{u.target}}
}

// inSet completes one assignment: a field to write, then after = only a
// literal, then the clause that may follow.
func (u updateCompleter) inSet(since []token) Completion {
	eq := -1
	for i, tok := range since {
		if isSymbol(tok, "=") {
			eq = i
		}
	}
	switch {
	case len(since) == 0:
		return Completion{Kind: CompleteReference, Aliases: []Alias{u.alias()}, Writable: true}
	case eq == len(since)-1:
		return keywordsOnly(valueKeywords)
	case eq >= 0 && !u.typing:
		return keywordsOnly([]string{"UNSET", "WHERE"})
	}
	return Completion{}
}

func (u updateCompleter) inUnset(since []token) Completion {
	switch {
	case len(since) == 0:
		return Completion{Kind: CompleteReference, Aliases: []Alias{u.alias()}, Writable: true}
	case !u.typing:
		return keywordsOnly([]string{"WHERE"})
	}
	return Completion{}
}

func (u updateCompleter) inWhere(since []token) Completion {
	last, ok := lastOf(since)
	if ok && endsValue(last) {
		return keywordsOnly(operatorKeywords)
	}
	return Completion{Kind: CompleteExpression, Aliases: []Alias{u.alias()}, Keywords: literalKeywords}
}

// afterDot completes the fields under the target's alias; a field an
// update writes is never the id, a key path or one the service owns.
func (u updateCompleter) afterDot(clause string) Completion {
	i := len(u.toks) - 1
	var chain []string
	for i > 0 && u.toks[i].kind == tokDot && u.toks[i-1].kind == tokIdent {
		chain = append([]string{u.toks[i-1].text}, chain...)
		i -= 2
	}
	alias := u.alias()
	if len(chain) == 0 || chain[0] != alias.Name {
		return Completion{}
	}
	completion := Completion{Kind: CompleteField, Aliases: []Alias{alias}, Writable: clause != "WHERE"}
	if len(chain) > 1 {
		completion.Path = chain[1:]
	}
	return completion
}
