package tui_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The overlay title as it appears inside the top border.
const exportTitle = " Export "

// newResultsModel has the first page of a run on screen and the focus on it.
func newResultsModel(t *testing.T) tea.Model {
	t.Helper()
	m := runQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c")
	return focusResults(t, m)
}

func openExport(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyCtrlE))
}

// exportTo replaces the suggested name with name and confirms it.
func exportTo(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	return pressAll(t, openExport(t, m), keyMsg(tea.KeyCtrlU), keyText(name), keyMsg(tea.KeyEnter))
}

func exportedDocuments(t *testing.T, path string) []json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var documents []json.RawMessage
	require.NoError(t, json.Unmarshal(data, &documents))
	return documents
}

func TestCtrlEOnTheResultsSuggestsAFileName(t *testing.T) {
	view := plain(openExport(t, newResultsModel(t)).View())

	assert.Contains(t, view, exportTitle)
	assert.Contains(t, view, "results.json")
}

func TestCtrlEWithNothingLoadedDoesNothing(t *testing.T) {
	m := focusResults(t, typeQuery(t, newLoadedModel(t, newConnection(t)), ""))

	assert.NotContains(t, openExport(t, m).View(), exportTitle)
}

func TestExportingWritesTheFetchedDocumentsAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")

	m := exportTo(t, newResultsModel(t), path)

	assert.Len(t, exportedDocuments(t, path), 10)
	view := plain(m.View())
	assert.NotContains(t, view, exportTitle, "a finished export closes the prompt")
	assert.Contains(t, view, "exported 10 rows")
}

func TestExportingCoversEveryPageFetchedSoFar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	m := pressAll(t, newResultsModel(t), keyRune('m'))

	exportTo(t, m, path)

	assert.Len(t, exportedDocuments(t, path), 20)
}

func TestTheExtensionPicksCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.csv")

	exportTo(t, newResultsModel(t), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(string(data)), "\n"), 11, "a header and ten rows")
}

func TestTheSuggestedNameLandsInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyEnter))

	assert.FileExists(t, filepath.Join(dir, "results.json"))
}

func TestAnExistingFileIsKeptUntilTheNameEndsInABang(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o600))

	refused := exportTo(t, newResultsModel(t), path)

	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(kept))
	view := plain(refused.View())
	assert.Contains(t, view, exportTitle, "the prompt stays open on a refusal")
	assert.Contains(t, view, "file exists")

	pressAll(t, refused, keyText("!"), keyMsg(tea.KeyEnter))

	assert.Len(t, exportedDocuments(t, path), 10)
}

func TestAnUnknownExtensionIsRefusedInThePrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.xlsx")

	view := plain(exportTo(t, newResultsModel(t), path).View())

	assert.Contains(t, view, exportTitle)
	assert.Contains(t, view, "unknown format")
	assert.NoFileExists(t, path)
}

func TestEscapeLeavesThePromptWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyEscape))

	assert.NotContains(t, m.View(), exportTitle)
	assert.NoFileExists(t, filepath.Join(dir, "results.json"))
}

func TestQIsPartOfAFileName(t *testing.T) {
	m := pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyCtrlU))

	typed, cmd := m.Update(keyRune('q'))

	assert.Nil(t, cmd)
	assert.Contains(t, plain(pressAll(t, typed, keyText(".csv")).View()), "q.csv")
}

func TestANewRunClearsTheExportNotice(t *testing.T) {
	m := exportTo(t, newResultsModel(t), filepath.Join(t.TempDir(), "orders.json"))

	rerun := pressAll(t, m, keyMsg(tea.KeyCtrlR))

	assert.NotContains(t, plain(rerun.View()), "exported")
}

func TestFetchingMoreClearsTheExportNotice(t *testing.T) {
	m := exportTo(t, newResultsModel(t), filepath.Join(t.TempDir(), "orders.json"))

	grown := pressAll(t, m, keyRune('m'))

	assert.NotContains(t, plain(grown.View()), "exported", "the count it names is no longer the count on screen")
}

func TestThePromptOffersBothFormats(t *testing.T) {
	view := plain(openExport(t, newResultsModel(t)).View())

	assert.Contains(t, view, "JSON")
	assert.Contains(t, view, "CSV")
}

func TestTabSwitchesTheSuggestedNameToCSVAndBack(t *testing.T) {
	m := openExport(t, newResultsModel(t))

	csv := pressAll(t, m, keyMsg(tea.KeyTab))
	assert.Contains(t, plain(csv.View()), "results.csv")

	assert.Contains(t, plain(pressAll(t, csv, keyMsg(tea.KeyTab)).View()), "results.json")
}

func TestTabThenEnterExportsCSV(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyTab), keyMsg(tea.KeyEnter))

	data, err := os.ReadFile(filepath.Join(dir, "results.csv"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "id,"), "a header row, not a JSON array")
}

func TestSwitchingFormatKeepsTheRestOfTheName(t *testing.T) {
	tests := []struct {
		name  string
		typed string
		want  string
	}{
		{name: "the bang survives", typed: "orders.json!", want: "orders.csv!"},
		{name: "a dot that names no format is part of the name", typed: "orders.2024", want: "orders.2024.csv"},
		{name: "an empty name falls back to the suggestion", typed: "", want: "results.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyCtrlU), keyText(tt.typed))

			assert.Contains(t, plain(pressAll(t, m, keyMsg(tea.KeyTab)).View()), tt.want)
		})
	}
}

func TestEscapeWaitsForAWriteInFlight(t *testing.T) {
	t.Chdir(t.TempDir())
	saving, _ := openExport(t, newResultsModel(t)).Update(keyMsg(tea.KeyEnter))

	m, _ := saving.Update(keyMsg(tea.KeyEscape))

	assert.Contains(t, plain(m.View()), exportTitle, "the outcome has to land on the prompt that asked for it")
}

// wideEnough keeps a temporary directory's long path on one line.
const wideEnough = 240

func openWideExport(t *testing.T) tea.Model {
	t.Helper()
	wide, _ := newResultsModel(t).Update(tea.WindowSizeMsg{Width: wideEnough, Height: testHeight})
	return openExport(t, wide)
}

func TestThePromptSaysWhereTheFileWillLand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	view := plain(openWideExport(t).View())

	assert.Contains(t, view, "saves to "+filepath.Join(dir, "results.json"))
}

func TestThePromptFollowsAPathAsItIsTyped(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	m := pressAll(t, openWideExport(t), keyMsg(tea.KeyCtrlU), keyText("~/exports/orders.csv!"))

	assert.Contains(t, plain(m.View()), "saves to "+filepath.Join(home, "exports", "orders.csv"))
}

func TestThePromptSaysAPathIsWelcome(t *testing.T) {
	assert.Contains(t, plain(openExport(t, newResultsModel(t)).View()), "or a path")
}

func TestExportingIntoAFolderThatDoesNotExistCreatesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exports", "2026", "orders.json")

	exportTo(t, newResultsModel(t), path)

	assert.Len(t, exportedDocuments(t, path), 10)
}
