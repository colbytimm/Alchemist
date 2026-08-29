package cosmos

import (
	"regexp"
	"strings"
)

// doubtRe matches constructs under which pinning a partition key is unsafe
// (disjunctions, negations, memberships, joins, ranges).
var doubtRe = regexp.MustCompile(`(?i)\b(OR|NOT|IN|JOIN|BETWEEN|LIKE)\b`)

// hasPinCandidate cheaply reports whether text could possibly pin a
// partition key: it needs a WHERE clause, an equality, and a string literal.
func hasPinCandidate(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "WHERE") &&
		strings.Contains(text, "=") &&
		strings.ContainsAny(text, `'"`)
}

// PinnedKey returns the partition key literal when the WHERE clause of text
// pins the container's partition key path (e.g. "/pk") to exactly one
// string literal via `alias.pk = "value"`. It returns "" on any doubt —
// cross-partition execution is always correct, pinning is only an RU
// optimization.
func PinnedKey(text, pkPath string) string {
	if !strings.HasPrefix(pkPath, "/") || strings.Count(pkPath, "/") != 1 {
		return "" // nested or absent partition key path
	}
	field := pkPath[1:]
	if field == "" || doubtRe.MatchString(text) {
		return ""
	}
	whereAt := strings.Index(strings.ToUpper(text), "WHERE")
	if whereAt < 0 {
		return ""
	}
	eqRe := regexp.MustCompile(`[\w$]+\s*\.\s*` + regexp.QuoteMeta(field) + `\b\s*=\s*(?:"([^"\\]*)"|'([^'\\]*)')`)
	matches := eqRe.FindAllStringSubmatchIndex(text[whereAt:], -1)
	if len(matches) != 1 {
		return "" // zero or multiple pins: stay cross-partition
	}
	m := matches[0]
	for _, group := range []int{2, 4} { // double- then single-quoted capture
		if m[group] >= 0 {
			return text[whereAt+m[group] : whereAt+m[group+1]]
		}
	}
	return ""
}
