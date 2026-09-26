package query

import "slices"

// Analysis is one lex and one parse of a text, for everything that reads
// the same text to share: highlighting, completion and diagnostics.
type Analysis struct {
	text   string
	tokens []token
	code   []token
	parser *parser
	batch  bool
	// mutation marks an UPDATE or a DELETE, and mutationAlias is the name
	// its items go by: the one it declares, or the default.
	mutation      bool
	mutationKind  MutationKind
	mutationAlias string
}

func Analyze(text string) Analysis {
	tokens := lex(text)
	statement := code(tokens)
	a := Analysis{
		text:   text,
		tokens: tokens,
		code:   statement,
		parser: parseTokens(statement),
		batch:  keywordAt(statement, 0, "BEGIN") && keywordAt(statement, 1, "BATCH"),
	}
	kind, mutation := mutationKind(statement)
	if mutation {
		a.mutation, a.mutationKind = true, kind
		a.mutationAlias = a.parser.declareMutationTarget(kind)
	}
	return a
}

// aliasNames are the names the query may read items through: every alias
// it declares, the names BuildPlan gives the containers it rewrites, and a
// source named by one word, which keeps its name once aliased only when the
// alias looks like a misspelled clause, so a typo taken for its alias does
// not leave every reference to it undeclared.
func (a Analysis) aliasNames() []string {
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if a.mutationAlias != "" {
		add(a.mutationAlias)
	}
	for _, alias := range (classifier{parser: a.parser}).aliases() {
		add(alias.Name)
	}
	for _, i := range a.parser.declarations {
		add(a.code[i].text)
	}
	for _, s := range a.parser.sources {
		if len(s.path) == 1 && (s.alias == "" || a.mayBeMisspelledClause(s)) {
			add(s.path[0].text)
		}
	}
	return names
}

// mayBeMisspelledClause reports whether the alias of s is a bare word a
// value follows, as WERE in FROM c WERE c.x: a misspelled clause, so its
// source keeps its own name.
func (a Analysis) mayBeMisspelledClause(s source) bool {
	alias := s.nextTok - 1
	return !keywordAt(a.code, alias-1, "AS") && a.opensClauseBody(s.nextTok)
}

func (a Analysis) isDeclaration(i int) bool {
	return slices.Contains(a.parser.declarations, i)
}

func (a Analysis) isRoot(i int) bool {
	return slices.Contains(a.parser.roots, i)
}

// readsThrough reports whether the identifier at i is followed by a dot or a
// bracket, reading a property of what it names, and is not itself one.
func (a Analysis) readsThrough(i int) bool {
	if followsDot(a.code, i) || i+1 >= len(a.code) {
		return false
	}
	next := a.code[i+1]
	return next.kind == tokDot || isSymbol(next, "[")
}

func (a Analysis) isUserFunction(i int) bool {
	return a.isCall(i) && keywordAt(a.code, i-2, "UDF")
}

func (a Analysis) isCall(i int) bool {
	return isSymbolAt(a.code, i+1, "(")
}
