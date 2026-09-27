package tui_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the golden renders in testdata")

// highlightedQuery holds a token of every syntax class the editor colors.
const highlightedQuery = `SELECT c.id, UPPER(c.status) AS s FROM c WHERE c.total > @min AND c.tag = 'rush' AND c.qty >= 2.5 AND c.paid = true -- open`

// TestTheDefaultThemeRendersAsItAlwaysHas pins the default look byte for
// byte: routing every color through a theme must not change a single escape
// code under the default theme.
func TestTheDefaultThemeRendersAsItAlwaysHas(t *testing.T) {
	for name, view := range goldenScreens(t) {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "golden", name+".ansi")
			if *updateGolden {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(view), 0o600))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), view)
		})
	}
}

// goldenScreens are the screens every role shows on: the workspace with a
// highlighted query, its results and the charge, with the editor focused
// and blurred, the batch review, and the help overlay.
func goldenScreens(t *testing.T) map[string]string {
	t.Helper()
	editing := runQuery(t, selectContainer(t, newLoadedModel(t, newOrdersConnection(t))), highlightedQuery)
	browsing := pressAll(t, editing, keyMsg(tea.KeyTab))
	review := openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch)
	help := pressAll(t, newLoadedModel(t, newConnection(t)), keyRune('?'))
	return map[string]string{
		"editor-focused": editing.View(),
		"editor-blurred": browsing.View(),
		"batch-review":   review.View(),
		"help":           help.View(),
	}
}
