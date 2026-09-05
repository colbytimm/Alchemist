package cosmos

import (
	"regexp"
	"strings"
)

var (
	unpinnableRe = regexp.MustCompile(`(?i)\b(OR|NOT|IN|JOIN|BETWEEN|LIKE)\b`)
	eqRe         = regexp.MustCompile(`[\w$]+\s*\.\s*([\w$]+)\s*=\s*(?:"([^"\\]*)"|'([^'\\]*)')`)
)

func hasPinCandidate(text string) bool {
	return whereClause(text) != "" &&
		strings.ContainsAny(text, `'"`) &&
		!unpinnableRe.MatchString(text)
}

// PinnedKey reports whether the WHERE clause of text pins pkPath to exactly one
// string literal via `alias.pk = "value"`, and returns it. Cross-partition
// execution is always correct, so anything ambiguous returns false.
func PinnedKey(text, pkPath string) (string, bool) {
	field, ok := partitionKeyField(pkPath)
	if !ok || !hasPinCandidate(text) {
		return "", false
	}
	return findSoleEquality(whereClause(text), field)
}

func whereClause(text string) string {
	at := strings.Index(strings.ToUpper(text), "WHERE")
	if at < 0 {
		return ""
	}
	return text[at:]
}

func partitionKeyField(pkPath string) (string, bool) {
	if !strings.HasPrefix(pkPath, "/") || strings.Count(pkPath, "/") != 1 {
		return "", false
	}
	field := pkPath[1:]
	return field, field != ""
}

func findSoleEquality(where, field string) (string, bool) {
	var literal string
	found := false
	for _, match := range eqRe.FindAllStringSubmatch(where, -1) {
		if match[1] != field {
			continue
		}
		if found {
			return "", false
		}
		literal, found = quotedLiteral(match), true
	}
	return literal, found
}

func quotedLiteral(match []string) string {
	doubleQuoted, singleQuoted := match[2], match[3]
	if doubleQuoted != "" {
		return doubleQuoted
	}
	return singleQuoted
}
