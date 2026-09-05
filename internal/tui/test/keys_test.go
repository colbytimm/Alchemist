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

// TestEveryBindingIsGroupedExactlyOnce is the drift guard: FullHelp is
// hand-grouped, and a binding it omits would silently vanish from the overlay
// while a duplicated one would keep a plain count looking right.
func TestEveryBindingIsGroupedExactlyOnce(t *testing.T) {
	keys := tui.DefaultKeyMap()
	grouped := map[string]int{}
	for _, binding := range bindings(keys) {
		grouped[binding.Help().Key]++
	}

	fields := reflect.TypeOf(keys).NumField()
	value := reflect.ValueOf(keys)
	for i := range fields {
		binding, ok := value.Field(i).Interface().(key.Binding)
		require.True(t, ok, "KeyMap fields must all be bindings")
		assert.Equal(t, 1, grouped[binding.Help().Key],
			"%s should appear once in FullHelp", reflect.TypeOf(keys).Field(i).Name)
	}
	assert.Len(t, bindings(keys), fields, "FullHelp should list nothing beyond the keymap")
}

// bindings flattens the overlay's columns into the bindings it will render.
func bindings(keys tui.KeyMap) []key.Binding {
	var all []key.Binding
	for _, group := range keys.FullHelp() {
		all = append(all, group...)
	}
	return all
}

func TestEveryBindingHasKeysAndHelp(t *testing.T) {
	for _, binding := range bindings(tui.DefaultKeyMap()) {
		require.NotEmpty(t, binding.Keys())
		assert.NotEmpty(t, binding.Help().Key)
		assert.NotEmpty(t, binding.Help().Desc)
	}
}

func TestHelpOverlayListsEveryEnabledBinding(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(helpWidth, helpHeight).View()

	for _, binding := range bindings(keys) {
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

	for _, binding := range disabled(bindings(keys)) {
		assert.NotContains(t, view, binding.Help().Desc,
			"a binding waiting on a later iteration must not be advertised")
	}
}

func TestHelpOverlayFitsTheMinimumTerminal(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(testWidth, testHeight).View()

	for _, binding := range bindings(keys) {
		if !binding.Enabled() {
			continue
		}
		assert.Contains(t, view, binding.Help().Desc, "column dropped at %d columns", testWidth)
	}
}

func TestShortHelpIsASubsetOfTheKeymap(t *testing.T) {
	keys := tui.DefaultKeyMap()
	all := strings.Join(describe(bindings(keys)), "\n")

	require.NotEmpty(t, keys.ShortHelp())
	for _, binding := range keys.ShortHelp() {
		assert.Contains(t, all, binding.Help().Desc)
	}
}

func disabled(all []key.Binding) []key.Binding {
	var out []key.Binding
	for _, binding := range all {
		if !binding.Enabled() {
			out = append(out, binding)
		}
	}
	return out
}

func describe(all []key.Binding) []string {
	descriptions := make([]string, 0, len(all))
	for _, binding := range all {
		descriptions = append(descriptions, binding.Help().Desc)
	}
	return descriptions
}
