package tui_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// A generous overlay so the drift test measures the keymap, not the wrapping.
const (
	helpWidth  = 120
	helpHeight = 20
)

func TestEveryBindingIsGrouped(t *testing.T) {
	fields := reflect.TypeOf(tui.KeyMap{}).NumField()

	assert.Len(t, tui.DefaultKeyMap().Bindings(), fields,
		"a binding added to KeyMap must also be placed in FullHelp")
}

func TestEveryBindingHasKeysAndHelp(t *testing.T) {
	for _, binding := range tui.DefaultKeyMap().Bindings() {
		require.NotEmpty(t, binding.Keys())
		assert.NotEmpty(t, binding.Help().Key)
		assert.NotEmpty(t, binding.Help().Desc)
	}
}

func TestHelpOverlayListsEveryEnabledBinding(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(helpWidth, helpHeight).View()

	for _, binding := range keys.Bindings() {
		if !binding.Enabled() {
			continue
		}
		assert.Contains(t, view, binding.Help().Key, "help should list the key")
		assert.Contains(t, view, binding.Help().Desc, "help should list the description")
	}
}

func TestHelpOverlayHidesDisabledBindings(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(helpWidth, helpHeight).View()

	for _, binding := range disabled(keys.Bindings()) {
		assert.NotContains(t, view, binding.Help().Desc,
			"a binding waiting on a later iteration must not be advertised")
	}
}

func TestHelpOverlayFitsTheMinimumTerminal(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(testWidth, testHeight).View()

	for _, binding := range keys.Bindings() {
		if !binding.Enabled() {
			continue
		}
		assert.Contains(t, view, binding.Help().Desc, "column dropped at %d columns", testWidth)
	}
}

func TestShortHelpIsASubsetOfTheKeymap(t *testing.T) {
	keys := tui.DefaultKeyMap()
	all := strings.Join(describe(keys.Bindings()), "\n")

	require.NotEmpty(t, keys.ShortHelp())
	for _, binding := range keys.ShortHelp() {
		assert.Contains(t, all, binding.Help().Desc)
	}
}

func disabled(bindings []key.Binding) []key.Binding {
	var out []key.Binding
	for _, binding := range bindings {
		if !binding.Enabled() {
			out = append(out, binding)
		}
	}
	return out
}

func describe(bindings []key.Binding) []string {
	descriptions := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		descriptions = append(descriptions, binding.Help().Desc)
	}
	return descriptions
}
