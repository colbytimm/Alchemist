package tui_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

func TestThemesDocListsEveryBuiltin(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/using/themes.md")
	require.NoError(t, err)

	for _, name := range theme.BuiltinNames() {
		assert.Contains(t, string(doc), "`"+name+"`")
		if name == theme.DefaultName {
			assert.FileExists(t, "../../../docs/images/theme-"+name+"-dark.png")
			assert.FileExists(t, "../../../docs/images/theme-"+name+"-light.png")
			continue
		}
		assert.FileExists(t, "../../../docs/images/theme-"+name+".png")
	}
	assert.FileExists(t, "../../../docs/images/theme-picker.png")
}
