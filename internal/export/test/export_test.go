package export_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/export"
)

// fetchedPages is a result set whose second page introduces a column the
// first never carried, so its rows are wider than the first page's.
func fetchedPages() []adapter.Page {
	return []adapter.Page{
		{
			Columns: []string{"id", "name", "tags", "address", "note", "count"},
			Rows: [][]string{
				{"1", `Zoë, "the" alchemist`, `["a","b"]`, `{"city":"Paris"}`, "", ""},
				{"2", "line one\nline two", "", "", "", "3"},
			},
			Raw: []json.RawMessage{
				json.RawMessage(`{"id":"1","name":"Zoë, \"the\" alchemist","tags":["a","b"],"address":{"city":"Paris"},"note":null}`),
				json.RawMessage(`{"id":"2","name":"line one\nline two","count":3}`),
			},
		},
		{
			Columns: []string{"id", "name", "tags", "address", "note", "count", "late"},
			Rows:    [][]string{{"3", "", "", "", "", "", "true"}},
			Raw:     []json.RawMessage{json.RawMessage(`{"id":"3","late":true}`)},
		},
	}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func exported(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestJSONWritesTheOriginalDocuments(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, export.JSON(&out, fetchedPages()))

	require.Equal(t, golden(t, "results.json"), out.String())
}

func TestJSONOfNoDocumentsIsAnEmptyArray(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, export.JSON(&out, nil))

	require.Equal(t, golden(t, "empty.json"), out.String())
}

func TestJSONHoldsOneDocumentPerRow(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, export.JSON(&out, fetchedPages()))

	var documents []json.RawMessage
	require.NoError(t, json.Unmarshal(out.Bytes(), &documents))

	require.Len(t, documents, 3)
}

func TestJSONRefusesAMalformedDocument(t *testing.T) {
	pages := []adapter.Page{{Raw: []json.RawMessage{json.RawMessage(`{"id":`)}}}

	err := export.JSON(&bytes.Buffer{}, pages)

	require.Error(t, err)
}

func TestCSVKeepsColumnOrderAndPadsEarlierPages(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, export.CSV(&out, fetchedPages()))

	require.Equal(t, golden(t, "results.csv"), out.String())
}

func TestCSVOfNoColumnsIsEmpty(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, export.CSV(&out, nil))

	require.Empty(t, out.String())
}

func TestWriteFilePicksTheFormatFromTheExtension(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		golden string
	}{
		{name: "json", file: "out.json", golden: "results.json"},
		{name: "csv", file: "out.csv", golden: "results.csv"},
		{name: "extension case is ignored", file: "out.CSV", golden: "results.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.file)

			require.NoError(t, export.WriteFile(path, fetchedPages()))

			require.Equal(t, golden(t, tt.golden), exported(t, path))
		})
	}
}

func TestWriteFileRefusesAnUnknownExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")

	err := export.WriteFile(path, fetchedPages())

	require.ErrorIs(t, err, export.ErrUnknownFormat)
	require.NoFileExists(t, path)
}

func TestWriteFileRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o600))

	err := export.WriteFile(path, fetchedPages())

	require.ErrorIs(t, err, export.ErrFileExists)
	require.Equal(t, "keep me", exported(t, path))
}

func TestOverwriteFileReplacesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, os.WriteFile(path, []byte("a much longer file than the export that replaces it"+golden(t, "results.json")), 0o600))

	require.NoError(t, export.OverwriteFile(path, fetchedPages()))

	require.Equal(t, golden(t, "results.json"), exported(t, path))
}

func TestOverwriteFileCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.csv")

	require.NoError(t, export.OverwriteFile(path, fetchedPages()))

	require.Equal(t, golden(t, "results.csv"), exported(t, path))
}
