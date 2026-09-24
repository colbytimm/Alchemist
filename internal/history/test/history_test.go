package history_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/history"
)

var recorded = time.Date(2026, 8, 27, 21, 4, 5, 0, time.UTC)

func succeeded(query string) history.Entry {
	return history.Entry{
		Time:          recorded,
		Profile:       "emulator",
		Scope:         []string{"sales", "orders"},
		Query:         query,
		OK:            true,
		Rows:          42,
		RequestCharge: 12.6,
		ElapsedMillis: 180,
	}
}

func failed(query, message string) history.Entry {
	return history.Entry{
		Time:    recorded,
		Profile: "emulator",
		Scope:   []string{"sales", "orders"},
		Query:   query,
		Error:   message,
	}
}

func openLog(t *testing.T) (history.File, string) {
	t.Helper()
	dir := t.TempDir()
	log, err := history.Open(dir)
	require.NoError(t, err)
	return log, filepath.Join(dir, history.FileName)
}

// writeLines builds the file by hand, the way a session that appended
// each line one at a time would have left it.
func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
}

func line(t *testing.T, entry history.Entry) string {
	t.Helper()
	encoded, err := json.Marshal(entry)
	require.NoError(t, err)
	return string(encoded)
}

func queries(entries []history.Entry) []string {
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		texts = append(texts, entry.Query)
	}
	return texts
}

func TestAppendThenRecentRoundTripsNewestFirst(t *testing.T) {
	log, _ := openLog(t)
	first, second := succeeded("SELECT 1"), failed("SELECT 2", "syntax error")

	require.NoError(t, log.Append(first))
	require.NoError(t, log.Append(second))
	entries, err := log.Recent("emulator", 10)

	require.NoError(t, err)
	assert.Equal(t, []history.Entry{second, first}, entries)
}

func TestRecentReturnsAtMostNEntries(t *testing.T) {
	log, _ := openLog(t)
	for i := range 5 {
		require.NoError(t, log.Append(succeeded(fmt.Sprintf("SELECT %d", i))))
	}

	entries, err := log.Recent("emulator", 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"SELECT 4", "SELECT 3"}, queries(entries))
}

func TestRecentOfAnEmptyLogIsEmpty(t *testing.T) {
	log, _ := openLog(t)

	entries, err := log.Recent("emulator", 10)

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRecentSkipsLinesItCannotParse(t *testing.T) {
	log, path := openLog(t)
	writeLines(t, path,
		line(t, succeeded("SELECT 1")),
		"this is not json",
		line(t, succeeded("SELECT 2")),
		`{"ts":"2026-08-27T21:04:05Z","query":"SELEC`,
	)

	entries, err := log.Recent("emulator", 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"SELECT 2", "SELECT 1"}, queries(entries))
}

func TestRecentCountsOnlyTheEntriesItCanParse(t *testing.T) {
	log, path := openLog(t)
	writeLines(t, path, line(t, succeeded("SELECT 1")), line(t, succeeded("SELECT 2")), "garbage")

	entries, err := log.Recent("emulator", 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"SELECT 2", "SELECT 1"}, queries(entries), "a corrupt line does not use up a slot")
}

func TestEveryLineIsJSONWithTheDocumentedFields(t *testing.T) {
	log, path := openLog(t)
	require.NoError(t, log.Append(succeeded("SELECT * FROM c")))
	require.NoError(t, log.Append(failed("SELECT * FRM c", "syntax error")))

	contents, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	require.Len(t, lines, 2)
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &fields))
	for _, want := range []string{"ts", "profile", "scope", "query", "ok", "rows", "ru", "elapsed_ms"} {
		assert.Contains(t, fields, want)
	}
	assert.NotContains(t, fields, "error", "a run that succeeded has no error to record")
	assert.Contains(t, lines[1], `"ok":false`)
	assert.Contains(t, lines[1], `"error":"syntax error"`)
}

func TestAppendCutsALongErrorMessage(t *testing.T) {
	log, _ := openLog(t)

	require.NoError(t, log.Append(failed("SELECT 1", strings.Repeat("x", 500))))
	entries, err := log.Recent("emulator", 1)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Len(t, entries[0].Error, 200)
}

func TestOpenCreatesAPrivateFile(t *testing.T) {
	_, path := openLog(t)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRecentReportsAFileItCannotRead(t *testing.T) {
	log, path := openLog(t)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0o700))

	_, err := log.Recent("emulator", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), path, "the message must say which file")
}

func TestOpenRefusesALocationItCannotWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.WriteFile(dir, []byte("a file where the directory should be"), 0o600))

	_, err := history.Open(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), dir, "the message must say which location")
}

func TestOpenTrimsALogPastTheThreshold(t *testing.T) {
	tests := []struct {
		name      string
		lines     int
		wantLines int
	}{
		{name: "at the threshold is left alone", lines: history.MaxEntries, wantLines: history.MaxEntries},
		{name: "past the threshold keeps the newest", lines: history.MaxEntries + 1, wantLines: history.KeepEntries},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, history.FileName)
			lines := make([]string, 0, tt.lines)
			for i := range tt.lines {
				lines = append(lines, line(t, succeeded(fmt.Sprintf("SELECT %d", i))))
			}
			writeLines(t, path, lines...)

			log, err := history.Open(dir)
			require.NoError(t, err)

			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Len(t, strings.Split(strings.TrimSpace(string(contents)), "\n"), tt.wantLines)
			newest, err := log.Recent("emulator", 1)
			require.NoError(t, err)
			assert.Equal(t, []string{fmt.Sprintf("SELECT %d", tt.lines-1)}, queries(newest))
		})
	}
}

func TestDiscardRecordsNothing(t *testing.T) {
	var store history.Store = history.Discard{}

	require.NoError(t, store.Append(succeeded("SELECT 1")))
	entries, err := store.Recent("emulator", 10)

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRecentListsOnlyTheEntriesOfTheAccountAskedFor(t *testing.T) {
	log, _ := openLog(t)
	staging := func(query string) history.Entry {
		entry := succeeded(query)
		entry.Profile = "staging"
		return entry
	}
	for _, entry := range []history.Entry{
		succeeded("SELECT 1"), succeeded("SELECT 2"),
		staging("SELECT 3"), staging("SELECT 4"), staging("SELECT 5"),
	} {
		require.NoError(t, log.Append(entry))
	}

	entries, err := log.Recent("emulator", 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"SELECT 2", "SELECT 1"}, queries(entries))
}

func TestRecentOfAnAccountWithNoEntriesIsEmpty(t *testing.T) {
	log, _ := openLog(t)
	require.NoError(t, log.Append(succeeded("SELECT 1")))

	entries, err := log.Recent("staging", 10)

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAnEntryWithNoAccountBelongsToNone(t *testing.T) {
	log, _ := openLog(t)
	anonymous := succeeded("SELECT 1")
	anonymous.Profile = ""
	require.NoError(t, log.Append(anonymous))

	for _, account := range []string{"emulator", "staging"} {
		entries, err := log.Recent(account, 10)
		require.NoError(t, err)
		assert.Empty(t, entries, "Recent(%q)", account)
	}
}
