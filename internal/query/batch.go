package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// BatchSyntaxError is a batch statement that does not parse, at the line
// and column, both from one, where parsing stopped.
type BatchSyntaxError struct {
	Line    int
	Column  int
	Message string
	offset  int
}

func (e *BatchSyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

const errSecondBatch = "a buffer holds one batch: the service cannot commit two atomically, and Alchemist will not pretend to"

// operationKeywords spell each operation kind in a statement.
var operationKeywords = map[string]adapter.OperationKind{
	"CREATE":  adapter.OperationCreate,
	"UPSERT":  adapter.OperationUpsert,
	"REPLACE": adapter.OperationReplace,
	"DELETE":  adapter.OperationDelete,
	"READ":    adapter.OperationRead,
	"PATCH":   adapter.OperationPatch,
}

// IsBatch reports whether text is a batch statement rather than a query: its
// first two words are BEGIN BATCH. No query starts with BEGIN.
func IsBatch(text string) bool {
	toks := code(lex(text))
	return keywordAt(toks, 0, "BEGIN") && keywordAt(toks, 1, "BATCH")
}

// ParseBatch reads a batch statement:
//
//	BEGIN BATCH <database>.<container> PARTITION <key> [, <key>...];
//	  <operation>;
//	  ...
//	COMMIT
//
// A statement without its COMMIT does not parse, so a half-typed batch can
// never run.
func ParseBatch(text string) (adapter.Batch, error) {
	p := &batchParser{text: text, toks: code(lex(text))}
	return p.parse()
}

type batchParser struct {
	text string
	toks []token
	i    int
}

func (p *batchParser) parse() (adapter.Batch, error) {
	var b adapter.Batch
	var err error
	if b.Scope, err = p.parseHeader(); err != nil {
		return adapter.Batch{}, err
	}
	if b.PartitionKey, err = p.parsePartition(); err != nil {
		return adapter.Batch{}, err
	}
	if b.Operations, err = p.parseOperations(); err != nil {
		return adapter.Batch{}, err
	}
	return b, p.parseEnd()
}

func (p *batchParser) parseHeader() ([]string, error) {
	if !p.keyword("BEGIN") || !p.keyword("BATCH") {
		return nil, p.fail("a batch starts BEGIN BATCH")
	}
	database, ok := p.identifier()
	if !ok {
		return nil, p.fail("BEGIN BATCH names its target as <database>.<container>")
	}
	if !p.at(tokDot) {
		return nil, p.fail("BEGIN BATCH names its target as <database>.<container>, not a container alone")
	}
	p.i++
	container, ok := p.identifier()
	if !ok {
		return nil, p.fail("BEGIN BATCH names its target as <database>.<container>")
	}
	return []string{database, container}, nil
}

func (p *batchParser) parsePartition() (adapter.PartitionKey, error) {
	if !p.keyword("PARTITION") {
		return nil, p.fail("expected PARTITION and the partition key value")
	}
	var key adapter.PartitionKey
	for {
		value, err := p.parseKeyValue()
		if err != nil {
			return nil, err
		}
		key = append(key, value)
		if p.at(tokComma) {
			p.i++
			continue
		}
		if !p.symbol(";") {
			return nil, p.fail("expected ; after the partition key")
		}
		return key, nil
	}
}

// parseKeyValue reads one scalar and returns it as JSON.
func (p *batchParser) parseKeyValue() (json.RawMessage, error) {
	start := p.i
	switch {
	case p.at(tokString):
		value, err := p.stringLiteral()
		if err != nil {
			return nil, err
		}
		return jsonString(value), nil
	case p.keyword("TRUE"):
		return json.RawMessage("true"), nil
	case p.keyword("FALSE"):
		return json.RawMessage("false"), nil
	case p.keyword("NULL"):
		return json.RawMessage("null"), nil
	}
	number := ""
	if p.symbol("-") {
		number = "-"
	}
	if !p.at(tokNumber) {
		p.i = start
		return nil, p.fail("a partition key value is a string, a number, TRUE, FALSE or NULL")
	}
	number += p.tokenText()
	p.i++
	if !json.Valid([]byte(number)) {
		p.i = start
		return nil, p.fail(fmt.Sprintf("%s is not a number the service reads", number))
	}
	return json.RawMessage(number), nil
}

func (p *batchParser) parseOperations() ([]adapter.Operation, error) {
	var ops []adapter.Operation
	for !p.keyword("COMMIT") {
		if p.done() {
			return nil, p.fail("no COMMIT: a batch runs only once it is closed")
		}
		op, err := p.parseOperation()
		if err != nil {
			return nil, err
		}
		if !p.symbol(";") {
			return nil, p.fail("expected ; after the operation")
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		p.i--
		return nil, p.fail("a batch holds at least one operation")
	}
	return ops, nil
}

func (p *batchParser) parseEnd() error {
	p.symbol(";")
	switch {
	case p.done():
		return nil
	case keywordAt(p.toks, p.i, "BEGIN"):
		return p.fail(errSecondBatch)
	}
	return p.fail("nothing may follow COMMIT")
}

func (p *batchParser) parseOperation() (adapter.Operation, error) {
	if keywordAt(p.toks, p.i, "BEGIN") {
		return adapter.Operation{}, p.fail(errSecondBatch)
	}
	var kind adapter.OperationKind
	var ok bool
	if p.at(tokIdent) {
		kind, ok = operationKeywords[p.toks[p.i].upper]
	}
	if !ok {
		return adapter.Operation{}, p.fail("expected CREATE, UPSERT, REPLACE, DELETE, READ, PATCH or COMMIT")
	}
	p.i++
	op := adapter.Operation{Kind: kind}
	if err := p.parseArguments(&op); err != nil {
		return adapter.Operation{}, err
	}
	return op, p.parseIfMatch(&op)
}

func (p *batchParser) parseArguments(op *adapter.Operation) error {
	var err error
	if op.Kind != adapter.OperationCreate && op.Kind != adapter.OperationUpsert {
		if op.ID, err = p.expectString("an id"); err != nil {
			return err
		}
	}
	switch op.Kind {
	case adapter.OperationCreate, adapter.OperationUpsert, adapter.OperationReplace:
		op.Body, err = p.jsonValue("{", "}", "a JSON object")
	case adapter.OperationPatch:
		if op.Body, err = p.jsonValue("[", "]", "a JSON array of patch entries"); err != nil {
			return err
		}
		if p.keyword("WHERE") {
			op.Condition, err = p.expectString("a condition")
		}
	}
	return err
}

// parseIfMatch reads the precondition an operation may carry. One the
// service would ignore is refused rather than dropped silently.
func (p *batchParser) parseIfMatch(op *adapter.Operation) error {
	if !keywordAt(p.toks, p.i, "IF") {
		return nil
	}
	if op.Kind == adapter.OperationCreate || op.Kind == adapter.OperationRead {
		return p.fail(fmt.Sprintf("IF MATCH does not apply to %s: the service would ignore it", strings.ToUpper(string(op.Kind))))
	}
	p.i++
	if !p.keyword("MATCH") {
		return p.fail("expected MATCH after IF")
	}
	var err error
	op.IfMatch, err = p.expectString("a version tag")
	return err
}

// jsonValue reads the JSON value between a matching open and close. The
// lexer keeps string literals whole, so a brace inside a string cannot
// unbalance the match.
func (p *batchParser) jsonValue(open, closing, what string) (json.RawMessage, error) {
	if !isSymbolAt(p.toks, p.i, open) {
		return nil, p.fail("expected " + what)
	}
	start, end := p.i, p.matchBrackets()
	if end < 0 {
		return nil, p.fail(fmt.Sprintf("%s has no matching %s", open, closing))
	}
	raw := p.text[p.toks[start].start:p.toks[end].end]
	var body bytes.Buffer
	if err := json.Compact(&body, []byte(raw)); err != nil {
		return nil, p.fail(fmt.Sprintf("%s is not valid JSON: %v", what, err))
	}
	p.i = end + 1
	return body.Bytes(), nil
}

// matchBrackets finds the token closing the bracket at p.i, or -1.
func (p *batchParser) matchBrackets() int {
	var open []string
	pairs := map[string]string{"{": "}", "[": "]"}
	for i := p.i; i < len(p.toks); i++ {
		tok := p.toks[i]
		if tok.kind != tokOther {
			continue
		}
		if closing, ok := pairs[tok.text]; ok {
			open = append(open, closing)
			continue
		}
		if tok.text != "}" && tok.text != "]" {
			continue
		}
		if len(open) == 0 || open[len(open)-1] != tok.text {
			return -1
		}
		if open = open[:len(open)-1]; len(open) == 0 {
			return i
		}
	}
	return -1
}

func (p *batchParser) expectString(what string) (string, error) {
	if !p.at(tokString) {
		return "", p.fail("expected " + what + ", as a quoted string")
	}
	return p.stringLiteral()
}

func (p *batchParser) stringLiteral() (string, error) {
	tok := p.toks[p.i]
	if tok.open {
		return "", p.fail("this string is never closed")
	}
	value, err := unquote(p.text[tok.start:tok.end])
	if err != nil {
		return "", p.fail("this string does not read: " + err.Error())
	}
	p.i++
	return value, nil
}

func (p *batchParser) identifier() (string, bool) {
	if !p.at(tokIdent) {
		return "", false
	}
	name := p.toks[p.i].text
	p.i++
	return name, true
}

func (p *batchParser) keyword(kw string) bool {
	if !keywordAt(p.toks, p.i, kw) {
		return false
	}
	p.i++
	return true
}

func (p *batchParser) symbol(s string) bool {
	if !isSymbolAt(p.toks, p.i, s) {
		return false
	}
	p.i++
	return true
}

func (p *batchParser) at(kind int) bool {
	return p.i < len(p.toks) && p.toks[p.i].kind == kind
}

func (p *batchParser) done() bool {
	return p.i >= len(p.toks)
}

func (p *batchParser) tokenText() string {
	tok := p.toks[p.i]
	return p.text[tok.start:tok.end]
}

// fail reports message at the token parsing stopped on, or at the end of
// the text when there is none.
func (p *batchParser) fail(message string) error {
	offset := len(p.text)
	if p.i < len(p.toks) {
		offset = p.toks[p.i].start
	}
	line, column := position(p.text, offset)
	return &BatchSyntaxError{Line: line, Column: column, Message: message, offset: offset}
}

func position(text string, offset int) (line, column int) {
	before := text[:offset]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[lineStart:]) + 1
}

func isSymbolAt(toks []token, i int, symbol string) bool {
	return i < len(toks) && isSymbol(toks[i], symbol)
}

// unquote reads a string literal in either quote, with JSON's backslash
// escapes plus \' for a single quote.
func unquote(literal string) (string, error) {
	inner := literal[1 : len(literal)-1]
	var quoted strings.Builder
	quoted.WriteByte('"')
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '\\' && i+1 < len(inner) && inner[i+1] == '\'':
			quoted.WriteByte('\'')
			i++
		case c == '\\' && i+1 < len(inner):
			quoted.WriteString(inner[i : i+2])
			i++
		case c == '"':
			quoted.WriteString(`\"`)
		default:
			quoted.WriteByte(c)
		}
	}
	quoted.WriteByte('"')
	var value string
	if err := json.Unmarshal([]byte(quoted.String()), &value); err != nil {
		return "", err
	}
	return value, nil
}

// jsonString spells s as a JSON string, which is also a literal the batch
// grammar reads back as s.
func jsonString(s string) json.RawMessage {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s) // a string always encodes
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}
