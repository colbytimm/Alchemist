package tui_test

import (
	"reflect"
	"slices"
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

// TestEveryBindingIsGroupedExactlyOnce is the drift guard: FullHelp,
// ConnectKeys and HistoryKeys are hand-grouped, and a binding they omit would
// silently vanish from the overlay while a duplicated one would keep a plain
// count looking right.
func TestEveryBindingIsGroupedExactlyOnce(t *testing.T) {
	keys := tui.DefaultKeyMap()
	advertised := slices.Concat(bindings(keys), keys.ConnectKeys(), keys.HistoryKeys())
	grouped := map[string]int{}
	for _, binding := range advertised {
		grouped[identity(binding)]++
	}

	fields := reflect.TypeOf(keys).NumField()
	value := reflect.ValueOf(keys)
	for i := range fields {
		binding, ok := value.Field(i).Interface().(key.Binding)
		require.True(t, ok, "KeyMap fields must all be bindings")
		assert.Equal(t, 1, grouped[identity(binding)],
			"%s should be advertised once", reflect.TypeOf(keys).Field(i).Name)
	}
	assert.Len(t, advertised, fields, "the groups should list nothing beyond the keymap")
}

// identity tells bindings apart by what they advertise; two may share a key
// in different contexts, as enter does.
func identity(binding key.Binding) string {
	return binding.Help().Key + " " + binding.Help().Desc
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

func TestHelpOverlayListsEveryBinding(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(helpWidth, helpHeight).View()

	for _, binding := range bindings(keys) {
		assert.Contains(t, view, binding.Help().Key, "help should list the key")
		assert.Contains(t, view, binding.Help().Desc, "help should list the description")
	}
}

func TestHelpOverlayFitsTheMinimumTerminal(t *testing.T) {
	keys := tui.DefaultKeyMap()
	view := panes.NewHelp(keys).SetSize(testWidth, testHeight).View()

	for _, binding := range bindings(keys) {
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

func describe(all []key.Binding) []string {
	descriptions := make([]string, 0, len(all))
	for _, binding := range all {
		descriptions = append(descriptions, binding.Help().Desc)
	}
	return descriptions
}
