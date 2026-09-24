package saved

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxFileSize bounds what the store reads: far beyond any query typed by
// hand, well short of anything that would stall the overlay.
const maxFileSize = 1 << 20

const (
	headerPrefix = "-- alchemist:"
	scopeKey     = "scope"
	// scopeSeparator splits a scope path without ambiguity: Cosmos forbids '/'
	// in a database or container id and allows '.'.
	scopeSeparator = "/"
)

var (
	errTooLarge   = fmt.Errorf("saved: over %d bytes", maxFileSize)
	errNotUTF8    = errors.New("saved: not UTF-8 text")
	errEmptyQuery = fmt.Errorf("saved: %w", ErrEmptyQuery)
)

// encode writes q as its file: the header line when q has a scope, then the
// text.
func encode(q Query) []byte {
	var file strings.Builder
	if len(q.Scope) > 0 {
		file.WriteString(headerPrefix + " " + scopeKey + "=" + strings.Join(q.Scope, scopeSeparator) + "\n")
	}
	file.WriteString(normalizeText(q.Text) + "\n")
	return []byte(file.String())
}

// decode reads the file of the query called name. Header lines are stripped,
// so the text never holds one; one the store does not understand is dropped.
func decode(name string, data []byte) (Query, error) {
	if len(data) > maxFileSize {
		return Query{}, errTooLarge
	}
	if !utf8.Valid(data) {
		return Query{}, errNotUTF8
	}
	q := Query{Name: name}
	rest := strings.ReplaceAll(string(data), "\r\n", "\n")
	for {
		line, after, found := strings.Cut(rest, "\n")
		if !strings.HasPrefix(line, headerPrefix) {
			break
		}
		q.Scope = headerScope(line, q.Scope)
		rest = after
		if !found {
			break
		}
	}
	q.Text = normalizeText(rest)
	if strings.TrimSpace(q.Text) == "" {
		return Query{}, errEmptyQuery
	}
	return q, nil
}

// headerScope reads the scope a header line sets, keeping current when the
// line sets none. A scope with an empty segment is no scope.
func headerScope(line string, current []string) []string {
	key, value, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, headerPrefix)), "=")
	if strings.TrimSpace(key) != scopeKey {
		return current
	}
	scope := strings.Split(strings.TrimSpace(value), scopeSeparator)
	for _, segment := range scope {
		if segment == "" {
			return nil
		}
	}
	return scope
}

// normalizeText drops trailing blank lines, and a carriage return ending the
// text, which would otherwise read back as half of a CRLF.
func normalizeText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\r")
}
