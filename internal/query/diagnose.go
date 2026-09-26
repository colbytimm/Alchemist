package query

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Diagnostic is a byte range of a statement Alchemist can tell is wrong
// from the text alone, and why.
type Diagnostic struct {
	Start, End int
	Message    string
}

// strayCharacters are the ones Cosmos SQL has no use for outside a string or
// a comment.
const strayCharacters = "#`\\"

// clauseWords are the words a misspelled clause is matched against, in the
// order a tie is broken.
var clauseKeywords = []string{"SELECT", "FROM", "WHERE", "GROUP", "ORDER", "OFFSET", "LIMIT", "JOIN"}

// updateClauseKeywords are an UPDATE's: its own, then a query's.
var updateClauseKeywords = append([]string{"SET", "UNSET"}, clauseKeywords...)

// maxSuggestionDistance is how far a word may be from a known one for
// "did you mean" to offer it.
const maxSuggestionDistance = 2

// Diagnose lists what is wrong with the analyzed statement, in the order it
// appears. Where rules flag overlapping bytes, the rule listed first wins.
// The service stays the judge: a statement with no diagnostic can still
// fail there.
func Diagnose(a Analysis) []Diagnostic {
	var found []Diagnostic
	for _, rule := range [][]Diagnostic{
		a.unterminatedStrings(),
		a.strayCharacters(),
		a.unknownFunctions(),
		a.undeclaredAliases(),
		a.misspelledClauses(),
		a.statementStart(),
		a.refusedJoins(),
		a.unbalancedBrackets(),
		a.batchSyntax(),
		a.mutationSyntax(),
	} {
		for _, d := range rule {
			if !slices.ContainsFunc(found, d.overlaps) {
				found = append(found, d)
			}
		}
	}
	slices.SortFunc(found, func(x, y Diagnostic) int { return x.Start - y.Start })
	return found
}

// LexicalDiagnostics are the diagnostics a lex alone decides, which cost
// nothing beyond the spans and so need not wait for typing to pause.
func (a Analysis) LexicalDiagnostics() []Diagnostic {
	return slices.Concat(a.unterminatedStrings(), a.strayCharacters())
}

func (d Diagnostic) overlaps(other Diagnostic) bool {
	return d.Start < other.End && other.Start < d.End
}

func (a Analysis) unterminatedStrings() []Diagnostic {
	var found []Diagnostic
	for _, tok := range a.tokens {
		if tok.kind == tokString && tok.open {
			found = append(found, Diagnostic{Start: tok.start, End: tok.end,
				Message: "unterminated string: close it with " + a.text[tok.start:tok.start+1]})
		}
	}
	return found
}

func (a Analysis) strayCharacters() []Diagnostic {
	var found []Diagnostic
	for _, tok := range a.code {
		if tok.kind == tokOther && strings.Contains(strayCharacters, tok.text) {
			found = append(found, Diagnostic{Start: tok.start, End: tok.end,
				Message: `"` + tok.text + `" is not part of Cosmos SQL`})
		}
	}
	return found
}

func (a Analysis) unknownFunctions() []Diagnostic {
	if a.batch {
		return nil
	}
	var found []Diagnostic
	for i, tok := range a.code {
		if tok.kind != tokIdent || followsDot(a.code, i) || !a.isCall(i) || isKnownWord(tok.upper) {
			continue
		}
		if builtinFunctions[tok.upper] != "" {
			continue
		}
		message := "unknown function " + tok.text
		if name, ok := closestFunction(tok.upper); ok {
			message += ": did you mean " + name + "?"
		}
		found = append(found, Diagnostic{Start: tok.start, End: tok.end, Message: message})
	}
	return found
}

func isKnownWord(upper string) bool {
	return keywords[upper] || joinModifiers[upper]
}

func closestFunction(upper string) (string, bool) {
	names := make([]string, 0, len(functions))
	for _, f := range functions {
		names = append(names, strings.ToUpper(f.Name))
	}
	closest, ok := closestWord(upper, names)
	return builtinFunctions[closest], ok
}

// undeclaredAliases waits for a FROM clause, where a query declares its
// aliases, so a query typed from the top is not flagged on its way there.
// An UPDATE declares its alias up front.
func (a Analysis) undeclaredAliases() []Diagnostic {
	if a.batch || a.parser.clauses == 0 && !a.mutation {
		return nil
	}
	names := a.aliasNames()
	var found []Diagnostic
	for i, tok := range a.code {
		if tok.kind != tokIdent || !a.readsThrough(i) || a.isRoot(i) || isKnownWord(tok.upper) || slices.Contains(names, tok.text) {
			continue
		}
		found = append(found, Diagnostic{Start: tok.start, End: tok.end,
			Message: tok.text + " is not declared: the query reads " + strings.Join(names, ", ")})
	}
	return found
}

// misspelledClauses flags a word near a clause keyword where a clause could
// start: after a complete value. FROM c WERE declares WERE a bare alias, so
// an alias is only let off once something reads through it.
func (a Analysis) misspelledClauses() []Diagnostic {
	if a.batch {
		return nil
	}
	var found []Diagnostic
	for i, tok := range a.code {
		if !a.inClausePosition(i) {
			continue
		}
		clause, ok := closestWord(tok.upper, a.clauseKeywords())
		if !ok || a.readsThroughName(tok.text) {
			continue
		}
		found = append(found, Diagnostic{Start: tok.start, End: tok.end,
			Message: fmt.Sprintf("%s is not a clause: did you mean %s?", tok.text, clause)})
	}
	return found
}

func (a Analysis) clauseKeywords() []string {
	if a.mutation {
		return updateClauseKeywords
	}
	return clauseKeywords
}

// inClausePosition reports whether the identifier at i follows a complete
// value and is followed by nothing that makes it a name.
func (a Analysis) inClausePosition(i int) bool {
	tok := a.code[i]
	if tok.kind != tokIdent || len(tok.text) < 3 || isKnownWord(tok.upper) || i == 0 || a.isRoot(i) {
		return false
	}
	if previous := a.code[i-1]; !endsValue(previous) || a.mutation && (mutationKeywords[tok.upper] || mutationKeywords[previous.upper]) {
		return false
	}
	if i+1 == len(a.code) {
		return true
	}
	next := a.code[i+1]
	return next.kind != tokDot && !isSymbol(next, "(") && !isSymbol(next, "[")
}

func (a Analysis) readsThroughName(name string) bool {
	for i, tok := range a.code {
		if tok.kind == tokIdent && tok.text == name && a.readsThrough(i) {
			return true
		}
	}
	return false
}

// statementStart leaves a lone BEGIN alone: it is how a batch is typed.
func (a Analysis) statementStart() []Diagnostic {
	if a.batch || a.mutation || len(a.code) == 0 || keywordAt(a.code, 0, "SELECT") || keywordAt(a.code, 0, "WITH") {
		return nil
	}
	first := a.code[0]
	if len(a.code) == 1 && first.upper == "BEGIN" {
		return nil
	}
	start, end := a.wholeRunes(first)
	return []Diagnostic{{Start: start, End: end, Message: "a statement starts with SELECT, WITH, UPDATE, DELETE or BEGIN BATCH"}}
}

// refusedJoins flags the join and CTE syntax the planner refuses by name
// whatever surrounds it: a NATURAL JOIN, which names no condition, and a
// recursive CTE.
func (a Analysis) refusedJoins() []Diagnostic {
	if a.batch {
		return nil
	}
	var found []Diagnostic
	for i, tok := range a.code {
		switch {
		case followsDot(a.code, i):
		case tok.upper == "NATURAL" && keywordAt(a.code, i+1, "JOIN"):
			found = append(found, Diagnostic{Start: tok.start, End: tok.end,
				Message: "NATURAL JOIN is not supported: join ON an equality"})
		case tok.upper == "RECURSIVE" && i == 1 && keywordAt(a.code, 0, "WITH"):
			found = append(found, Diagnostic{Start: tok.start, End: tok.end,
				Message: "a recursive CTE is not supported"})
		}
	}
	return found
}

var closingBrackets = map[string]string{")": "(", "]": "[", "}": "{"}

func (a Analysis) unbalancedBrackets() []Diagnostic {
	var found []Diagnostic
	var open []token
	for _, tok := range a.code {
		switch {
		case isSymbol(tok, "("), isSymbol(tok, "["), isSymbol(tok, "{"):
			open = append(open, tok)
		case tok.kind == tokOther && closingBrackets[tok.text] != "":
			opening := closingBrackets[tok.text]
			if len(open) > 0 && open[len(open)-1].text == opening {
				open = open[:len(open)-1]
				continue
			}
			found = append(found, Diagnostic{Start: tok.start, End: tok.end,
				Message: tok.text + " has no opening " + opening})
		}
	}
	for _, tok := range open {
		found = append(found, Diagnostic{Start: tok.start, End: tok.end, Message: tok.text + " is never closed"})
	}
	return found
}

func (a Analysis) batchSyntax() []Diagnostic {
	if !a.batch {
		return nil
	}
	p := &batchParser{statementReader: a.statementReader()}
	_, err := p.parse()
	return a.statementSyntax(err)
}

func (a Analysis) mutationSyntax() []Diagnostic {
	if !a.mutation {
		return nil
	}
	p := &mutationParser{statementReader: a.statementReader()}
	_, err := p.parse()
	return a.statementSyntax(err)
}

// statementReader reads the analyzed statement for its own parser, which
// then reports where it stopped as a stopError.
func (a Analysis) statementReader() statementReader {
	return statementReader{text: a.text, toks: a.code, syntaxError: func(line, column int, message string) error {
		return &stopError{line: line, column: column, message: message}
	}}
}

// stopError is where a statement's parser stopped, and why.
type stopError struct {
	line, column int
	message      string
}

func (s *stopError) Error() string {
	return s.message
}

// statementSyntax places a parser's error on the token it stopped at, or on
// the last one when the statement ran out first.
func (a Analysis) statementSyntax(err error) []Diagnostic {
	var stopped *stopError
	if !errors.As(err, &stopped) {
		return nil
	}
	offset := offsetOf(a.text, stopped.line, stopped.column)
	at := slices.IndexFunc(a.code, func(tok token) bool { return tok.start == offset })
	if at < 0 {
		at = len(a.code) - 1
	}
	start, end := a.wholeRunes(a.code[at])
	return []Diagnostic{{Start: start, End: end, Message: stopped.message}}
}

// wholeRunes widens tok to the runes it lies in: the lexer takes a byte it
// does not know for a token of its own, even one inside a multibyte rune.
func (a Analysis) wholeRunes(tok token) (start, end int) {
	start, end = tok.start, tok.end
	for start > 0 && !utf8.RuneStart(a.text[start]) {
		start--
	}
	for end < len(a.text) && !utf8.RuneStart(a.text[end]) {
		end++
	}
	return start, end
}

// offsetOf is position's inverse: the byte offset of a line and column,
// both from one.
func offsetOf(text string, line, column int) int {
	offset := 0
	for range line - 1 {
		offset += strings.IndexByte(text[offset:], '\n') + 1
	}
	for range column - 1 {
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}
	return offset
}

// closestWord is the candidate nearest word within maxSuggestionDistance,
// the first of equals winning. word itself is no suggestion.
func closestWord(word string, candidates []string) (string, bool) {
	best, bestDistance := "", maxSuggestionDistance+1
	for _, candidate := range candidates {
		d := editDistance(word, candidate)
		if d > 0 && d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best, best != ""
}

// editDistance counts the single-byte insertions, deletions and
// substitutions that turn a into b.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
