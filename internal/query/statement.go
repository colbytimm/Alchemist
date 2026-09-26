package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// statementReader walks the code tokens of a statement one at a time, for
// the parsers of the statements that write. syntaxError builds the error
// each parser reports a failure with.
type statementReader struct {
	text        string
	toks        []token
	i           int
	syntaxError func(line, column int, message string) error
}

func newStatementReader(text string, syntaxError func(line, column int, message string) error) statementReader {
	return statementReader{text: text, toks: code(lex(text)), syntaxError: syntaxError}
}

// jsonValue reads the JSON value between a matching open and close. The
// lexer keeps string literals whole, so a brace inside a string cannot
// unbalance the match.
func (p *statementReader) jsonValue(open, closing, what string) (json.RawMessage, error) {
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
func (p *statementReader) matchBrackets() int {
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

func (p *statementReader) expectString(what string) (string, error) {
	if !p.at(tokString) {
		return "", p.fail("expected " + what + ", as a quoted string")
	}
	return p.stringLiteral()
}

func (p *statementReader) stringLiteral() (string, error) {
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

func (p *statementReader) identifier() (string, bool) {
	if !p.at(tokIdent) {
		return "", false
	}
	name := p.toks[p.i].text
	p.i++
	return name, true
}

func (p *statementReader) keyword(kw string) bool {
	if !keywordAt(p.toks, p.i, kw) {
		return false
	}
	p.i++
	return true
}

func (p *statementReader) symbol(s string) bool {
	if !isSymbolAt(p.toks, p.i, s) {
		return false
	}
	p.i++
	return true
}

func (p *statementReader) at(kind int) bool {
	return p.i < len(p.toks) && p.toks[p.i].kind == kind
}

func (p *statementReader) done() bool {
	return p.i >= len(p.toks)
}

func (p *statementReader) tokenText() string {
	tok := p.toks[p.i]
	return p.text[tok.start:tok.end]
}

// fail reports message at the token parsing stopped on, or at the end of
// the text when there is none.
func (p *statementReader) fail(message string) error {
	offset := len(p.text)
	if p.i < len(p.toks) {
		offset = p.toks[p.i].start
	}
	line, column := position(p.text, offset)
	return p.syntaxError(line, column, message)
}

func position(text string, offset int) (line, column int) {
	before := text[:offset]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[lineStart:]) + 1
}

func isSymbolAt(toks []token, i int, symbol string) bool {
	return i < len(toks) && isSymbol(toks[i], symbol)
}
