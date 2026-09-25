package snapshot_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/canonical"
	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

// summary is a diff's items as "<sign> <partition key> <id>".
func summary(d snapshot.Diff) []string {
	signs := map[snapshot.ChangeKind]string{snapshot.Added: "+", snapshot.Removed: "-", snapshot.Modified: "~"}
	lines := make([]string, 0, len(d.Items))
	for _, item := range d.Items {
		lines = append(lines, signs[item.Kind]+" "+item.Identity.PartitionKeyText()+" "+item.Identity.ID)
	}
	return lines
}

func TestADiffListsAddedRemovedAndModifiedItems(t *testing.T) {
	f := newFixture(t, orders(10)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(order("o-0003", "c03", 99), order("o-new", "c01", 1))
	f.remove("c05", "o-0005")
	second := f.take(snapshot.CaptureOptions{})

	d, err := f.open().Diff(first.ID, second.ID)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"~ c03 o-0003", "+ c01 o-new", "- c05 o-0005"}, summary(d))
	assert.Equal(t, 1, d.Count(snapshot.Added))
	assert.Equal(t, snapshot.DefinitionCompared, d.Definition.State)
	assert.Empty(t, d.Definition.Changes)
}

func TestADiffFromTheNewerSideIsTheInverse(t *testing.T) {
	f := newFixture(t, orders(5)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(order("o-new", "c01", 1))
	second := f.take(snapshot.CaptureOptions{})

	d, err := f.open().Diff(second.ID, first.ID)

	require.NoError(t, err)
	assert.Equal(t, []string{"- c01 o-new"}, summary(d))
}

func TestAChangedPartitionKeyIsARemovalAndAnAddition(t *testing.T) {
	f := newFixture(t, orders(5)...)
	first := f.take(snapshot.CaptureOptions{})
	f.remove("c02", "o-0002")
	f.put(order("o-0002", "c09", 2))
	second := f.take(snapshot.CaptureOptions{})

	d, err := f.open().Diff(first.ID, second.ID)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"- c02 o-0002", "+ c09 o-0002"}, summary(d))
	assert.Equal(t, []string{"o-0002"}, d.MovedIDs())
}

func TestComposingThreeChangeSetsEqualsDiffingTheEnds(t *testing.T) {
	f := newFixture(t, orders(20)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(order("o-0001", "c01", 5), order("o-a", "c01", 1))
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0001", "c01", 1), order("o-b", "c02", 1))
	f.remove("c01", "o-a")
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0002", "c02", 3))
	last := f.take(snapshot.CaptureOptions{})

	d, err := f.open().Diff(first.ID, last.ID)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"+ c02 o-b", "~ c02 o-0002"}, summary(d),
		"o-0001 changed and changed back; o-a came and went")
}

func TestAnItemDiffReadsBothBodies(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(`{"id":"o-0001","customerId":"c01","status":"shipped","total":1}`)
	second := f.take(snapshot.CaptureOptions{})
	store := f.open()
	d, err := store.Diff(first.ID, second.ID)
	require.NoError(t, err)
	require.Len(t, d.Items, 1)

	fields, err := store.ChangedFields(d.Items[0])

	require.NoError(t, err)
	assert.Equal(t, []string{"status"}, fields)
	before, after, err := store.ItemBodies(d.Items[0])
	require.NoError(t, err)
	lines, ok := snapshot.Lines(before, after)
	require.True(t, ok)
	assert.Equal(t, []string{`-   "status": "open",`, `+   "status": "shipped",`}, changedLines(lines))
}

func changedLines(lines []snapshot.Line) []string {
	var changed []string
	for _, line := range lines {
		switch line.Kind {
		case snapshot.LineRemoved:
			changed = append(changed, "- "+line.Text)
		case snapshot.LineAdded:
			changed = append(changed, "+ "+line.Text)
		}
	}
	return changed
}

func TestStructuralDiffs(t *testing.T) {
	tests := []struct {
		name          string
		before, after string
		want          []string
	}{
		{name: "nothing", before: `{"a":1}`, after: `{"a":1}`},
		{name: "a nested member", before: `{"a":{"b":1,"c":2}}`, after: `{"a":{"b":1,"c":3}}`, want: []string{"replace /a/c 2 → 3"}},
		{name: "a member added and one removed", before: `{"a":1}`, after: `{"b":{"x":1}}`, want: []string{"remove /a 1", "add /b {\"x\":1}"}},
		{name: "arrays of one length, by position", before: `{"a":[1,2,3]}`, after: `{"a":[1,5,3]}`, want: []string{"replace /a/1 2 → 5"}},
		{name: "arrays of two lengths, whole", before: `{"a":[1,2]}`, after: `{"a":[1,2,3]}`, want: []string{"replace /a [1,2] → [1,2,3]"}},
		{name: "a respelled number", before: `{"a":1.0}`, after: `{"a":1e0}`},
		{name: "a type change", before: `{"a":"1"}`, after: `{"a":1}`, want: []string{`replace /a "1" → 1`}},
		{name: "a name with a slash and a tilde", before: `{"a/b~":1}`, after: `{}`, want: []string{"remove /a~1b~0 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes, err := snapshot.Structural([]byte(tt.before), []byte(tt.after))

			require.NoError(t, err)
			var got []string
			for _, c := range changes {
				switch c.Op {
				case snapshot.OpAdd:
					got = append(got, fmt.Sprintf("add %s %s", c.Path, c.After))
				case snapshot.OpRemove:
					got = append(got, fmt.Sprintf("remove %s %s", c.Path, c.Before))
				default:
					got = append(got, fmt.Sprintf("replace %s %s → %s", c.Path, c.Before, c.After))
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func FuzzAStructuralDiffAppliedAsAPatchGivesTheAfter(f *testing.F) {
	f.Add(`{"a":1,"b":[1,2],"c":{"d":null}}`, `{"a":2,"b":[1,2,3],"c":{"e":true}}`)
	f.Add(`{"x/y":{"~":[{"a":1}]}}`, `{"x/y":{"~":[{"a":2}]}}`)
	f.Add(`[1,2]`, `{"a":1}`)
	f.Fuzz(func(t *testing.T, before, after string) {
		changes, err := snapshot.Structural([]byte(before), []byte(after))
		if err != nil {
			return
		}
		want, err := canonical.Marshal([]byte(after))
		if err != nil {
			return
		}
		got := applyPatch(t, decode(t, before), changes)
		encoded, err := canonical.Encode(got)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(encoded))
	})
}

func decode(t *testing.T, document string) any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	var value any
	require.NoError(t, decoder.Decode(&value))
	return canonical.Numbers(value)
}

// applyPatch is a minimal RFC 6902 applier for add, remove and replace.
func applyPatch(t *testing.T, document any, changes []snapshot.FieldChange) any {
	t.Helper()
	for _, change := range changes {
		var value any
		if change.Op != snapshot.OpRemove {
			value = decode(t, string(change.After))
		}
		document = applyAt(t, document, pointer(change.Path), change.Op, value)
	}
	return document
}

func pointer(path string) []string {
	if path == "" {
		return nil
	}
	tokens := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, token := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens
}

func applyAt(t *testing.T, target any, tokens []string, op string, value any) any {
	t.Helper()
	if len(tokens) == 0 {
		return value
	}
	switch container := target.(type) {
	case map[string]any:
		if len(tokens) == 1 && op == snapshot.OpRemove {
			delete(container, tokens[0])
			return container
		}
		container[tokens[0]] = applyAt(t, container[tokens[0]], tokens[1:], op, value)
		return container
	case []any:
		i, err := strconv.Atoi(tokens[0])
		require.NoError(t, err)
		container[i] = applyAt(t, container[i], tokens[1:], op, value)
		return container
	}
	require.Failf(t, "patch path runs through a scalar", "%v", tokens)
	return nil
}

func TestTheLineDiffIgnoresATrailingComma(t *testing.T) {
	lines, ok := snapshot.Lines([]byte(`{"a":1,"b":2}`), []byte(`{"a":1,"b":2,"c":3}`))

	require.True(t, ok)
	assert.Equal(t, []string{`+   "c": 3`}, changedLines(lines), "b gaining a comma is not a change")
}

func TestTheLineDiffShowsAnElementInsertedMidArrayAsOneLine(t *testing.T) {
	lines, ok := snapshot.Lines([]byte(`{"a":[1,2,3,4]}`), []byte(`{"a":[1,2,9,3,4]}`))

	require.True(t, ok)
	assert.Equal(t, []string{`+     9,`}, changedLines(lines))
}

func TestTheLineDiffGivesUpPastItsLimit(t *testing.T) {
	long := `{"a":[` + strings.Repeat("1,", snapshot.MaxLineDiff) + `1]}`

	_, ok := snapshot.Lines([]byte(long), []byte(`{}`))

	assert.False(t, ok)
}

func TestADiffExportsAsJSONAndCSVAndNeverReplacesAFile(t *testing.T) {
	f := newFixture(t, orders(5)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(`{"id":"o-0001","customerId":"c01","status":"shipped","total":1}`, order("o-new", "c03", 4))
	f.remove("c02", "o-0002")
	second := f.take(snapshot.CaptureOptions{})
	store := f.open()
	d, err := store.Diff(first.ID, second.ID)
	require.NoError(t, err)
	dir := t.TempDir()

	require.NoError(t, store.WriteDiff(filepath.Join(dir, "diff.json"), d))
	require.NoError(t, store.WriteDiff(filepath.Join(dir, "diff.csv"), d))

	var document struct {
		Summary map[string]int `json:"summary"`
		Changes []struct {
			Change       string            `json:"change"`
			PartitionKey []json.RawMessage `json:"partitionKey"`
			ID           string            `json:"id"`
			Patch        []map[string]any  `json:"patch"`
			Body         json.RawMessage   `json:"body"`
		} `json:"changes"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "diff.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &document))
	assert.Equal(t, map[string]int{"added": 1, "removed": 1, "modified": 1}, document.Summary)
	require.Len(t, document.Changes, 3)
	for _, change := range document.Changes {
		if change.Change == "modified" {
			assert.Equal(t, []map[string]any{{"op": "replace", "path": "/status", "value": "shipped", "before": "open"}}, change.Patch)
			continue
		}
		assert.NotEmpty(t, change.Body, change.Change)
	}
	rows := readCSV(t, filepath.Join(dir, "diff.csv"))
	assert.Equal(t, []string{"change", "partition_key", "id", "fields", "modified_before", "modified_after"}, rows[0])
	assert.Len(t, rows, 4)
	assert.ErrorIs(t, store.WriteDiff(filepath.Join(dir, "diff.csv"), d), export.ErrFileExists)
	assert.ErrorIs(t, store.WriteDiff(filepath.Join(dir, "diff.txt"), d), export.ErrUnknownFormat)
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	return rows
}

func TestContentsExportAsAJSONArray(t *testing.T) {
	f := newFixture(t, orders(3)...)
	record := f.take(snapshot.CaptureOptions{})
	path := filepath.Join(t.TempDir(), "items.json")

	require.NoError(t, f.open().WriteItems(path, record.ID))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(data, &items))
	assert.Len(t, items, 3)
	assert.ErrorIs(t, f.open().WriteItems(path, record.ID), export.ErrFileExists)
}
