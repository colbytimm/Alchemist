package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/colbytimm/alchemist/internal/canonical"
)

// Operations of a FieldChange, as RFC 6902 names them.
const (
	OpAdd     = "add"
	OpRemove  = "remove"
	OpReplace = "replace"
)

// FieldChange is one difference between two documents at an RFC 6901
// path. A list of them applied in order as a JSON Patch turns the first
// document into the second; Before is carried for a reader, and ignored by
// anything applying the patch.
type FieldChange struct {
	Op     string          `json:"op"`
	Path   string          `json:"path"`
	After  json.RawMessage `json:"value,omitempty"`
	Before json.RawMessage `json:"before,omitempty"`
}

// Summary describes the change for a list: the values when they are
// scalars, and what happened when either is an object or an array.
func (c FieldChange) Summary() string {
	switch {
	case c.Op == OpAdd && scalarJSON(c.After):
		return "absent → " + string(c.After)
	case c.Op == OpRemove && scalarJSON(c.Before):
		return string(c.Before) + " → absent"
	case c.Op == OpReplace && scalarJSON(c.Before) && scalarJSON(c.After):
		return string(c.Before) + " → " + string(c.After)
	case c.Op == OpAdd:
		return "added"
	case c.Op == OpRemove:
		return "removed"
	}
	return "replaced"
}

func scalarJSON(value json.RawMessage) bool {
	return len(value) > 0 && value[0] != '{' && value[0] != '['
}

// Structural compares two documents by structure: objects by key, numbers
// by value, arrays of equal length by position; an array whose length
// changed is replaced whole, so the patch is always valid if not always
// minimal.
func Structural(before, after []byte) ([]FieldChange, error) {
	a, err := decodeCanonical(before)
	if err != nil {
		return nil, err
	}
	b, err := decodeCanonical(after)
	if err != nil {
		return nil, err
	}
	var changes []FieldChange
	if err := compareValues("", a, b, &changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func decodeCanonical(document []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("snapshot: compare: %w", err)
	}
	return canonical.Numbers(value), nil
}

func compareValues(path string, a, b any, changes *[]FieldChange) error {
	objectA, isObjectA := a.(map[string]any)
	objectB, isObjectB := b.(map[string]any)
	if isObjectA && isObjectB {
		return compareObjects(path, objectA, objectB, changes)
	}
	arrayA, isArrayA := a.([]any)
	arrayB, isArrayB := b.([]any)
	if isArrayA && isArrayB && len(arrayA) == len(arrayB) {
		for i := range arrayA {
			if err := compareValues(path+"/"+strconv.Itoa(i), arrayA[i], arrayB[i], changes); err != nil {
				return err
			}
		}
		return nil
	}
	if reflect.DeepEqual(a, b) {
		return nil
	}
	return appendChange(changes, OpReplace, path, a, b)
}

func compareObjects(path string, a, b map[string]any, changes *[]FieldChange) error {
	names := make([]string, 0, len(a)+len(b))
	for name := range a {
		names = append(names, name)
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		member := path + "/" + escapePointer(name)
		valueA, inA := a[name]
		valueB, inB := b[name]
		var err error
		switch {
		case !inA:
			err = appendChange(changes, OpAdd, member, nil, valueB)
		case !inB:
			err = appendChange(changes, OpRemove, member, valueA, nil)
		default:
			err = compareValues(member, valueA, valueB, changes)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func appendChange(changes *[]FieldChange, op, path string, before, after any) error {
	change := FieldChange{Op: op, Path: path}
	var err error
	if op != OpAdd {
		if change.Before, err = canonical.Encode(before); err != nil {
			return err
		}
	}
	if op != OpRemove {
		if change.After, err = canonical.Encode(after); err != nil {
			return err
		}
	}
	*changes = append(*changes, change)
	return nil
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func escapePointer(name string) string { return pointerEscaper.Replace(name) }

// FieldNames names the fields a list of changes touches, each once, as a
// path without its leading slash.
func FieldNames(changes []FieldChange) []string {
	var names []string
	for _, change := range changes {
		name := strings.TrimPrefix(change.Path, "/")
		if name == "" {
			name = "(document)"
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// LineKind is a line's part in a line diff.
type LineKind int

const (
	LineSame LineKind = iota
	LineRemoved
	LineAdded
)

type Line struct {
	Kind LineKind
	Text string
}

// MaxLineDiff is the longest body, in indented lines, that a line diff
// takes on; past it, the structural list serves instead.
const MaxLineDiff = 2000

const lineIndent = "  "

// Lines diffs two canonical bodies line by line once indented. Their keys
// are already sorted, so a line diff is a semantic one: an element added
// in the middle of an array is one added line. A line's trailing comma is
// ignored, so adding a last member does not mark its neighbor. ok is false
// when either body is past MaxLineDiff lines.
func Lines(before, after []byte) (lines []Line, ok bool) {
	a, b := indentedLines(before), indentedLines(after)
	if len(a) > MaxLineDiff || len(b) > MaxLineDiff {
		return nil, false
	}
	prefix := commonPrefix(a, b)
	suffix := commonSuffix(a[prefix:], b[prefix:])
	for _, text := range b[:prefix] {
		lines = append(lines, Line{Kind: LineSame, Text: text})
	}
	lines = append(lines, lcsLines(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])...)
	for _, text := range b[len(b)-suffix:] {
		lines = append(lines, Line{Kind: LineSame, Text: text})
	}
	return lines, true
}

func indentedLines(body []byte) []string {
	if body == nil {
		return nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", lineIndent); err != nil {
		return strings.Split(string(body), "\n")
	}
	return strings.Split(out.String(), "\n")
}

func sameLine(a, b string) bool {
	return strings.TrimSuffix(a, ",") == strings.TrimSuffix(b, ",")
}

func commonPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && sameLine(a[n], b[n]) {
		n++
	}
	return n
}

func commonSuffix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && sameLine(a[len(a)-1-n], b[len(b)-1-n]) {
		n++
	}
	return n
}

// lcsLines is the classic longest-common-subsequence table walked from the
// top: a removal is listed before the addition that replaces it.
func lcsLines(a, b []string) []Line {
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if sameLine(a[i], b[j]) {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	var lines []Line
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && sameLine(a[i], b[j]):
			lines = append(lines, Line{Kind: LineSame, Text: b[j]})
			i, j = i+1, j+1
		case j == len(b) || (i < len(a) && table[i+1][j] >= table[i][j+1]):
			lines = append(lines, Line{Kind: LineRemoved, Text: a[i]})
			i++
		default:
			lines = append(lines, Line{Kind: LineAdded, Text: b[j]})
			j++
		}
	}
	return lines
}
