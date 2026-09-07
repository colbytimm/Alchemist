package panes_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	connectWidth  = 80
	connectHeight = 24
)

func newConnect(form panes.ConnectForm) panes.Connect {
	return panes.NewConnect(theme.Icons(), form).SetSize(connectWidth, connectHeight)
}

func typeInto(c panes.Connect, text string) panes.Connect {
	for _, r := range text {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return c
}

func spaceKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
}

func TestConnectEmptyFormOpensOnTheProfile(t *testing.T) {
	c := typeInto(newConnect(panes.ConnectForm{}), "emulator")
	c = typeInto(c.NextField(), "https://localhost:8081")
	c = typeInto(c.NextField(), "typed-key")

	form, err := c.Form()

	require.NoError(t, err)
	assert.Equal(t, panes.ConnectForm{Profile: "emulator", Endpoint: "https://localhost:8081", Key: "typed-key"}, form)
}

func TestConnectSeededProfileOpensOnTheKey(t *testing.T) {
	c := newConnect(panes.ConnectForm{Profile: "prod", Endpoint: "https://x", StoreKey: true})

	form, err := typeInto(c, "typed-key").Form()

	require.NoError(t, err)
	assert.Equal(t, panes.ConnectForm{Profile: "prod", Endpoint: "https://x", Key: "typed-key", StoreKey: true}, form)
}

func TestConnectRefusesAnIncompleteForm(t *testing.T) {
	tests := []struct {
		name string
		form panes.ConnectForm
		want string
	}{
		{name: "nothing", form: panes.ConnectForm{}, want: "profile"},
		{name: "no endpoint", form: panes.ConnectForm{Profile: "p"}, want: "endpoint"},
		{name: "no key", form: panes.ConnectForm{Profile: "p", Endpoint: "https://x"}, want: "key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newConnect(tt.form).Form()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want, "the message must name the field")
		})
	}
}

func TestConnectMasksTheKey(t *testing.T) {
	c := typeInto(newConnect(panes.ConnectForm{Profile: "p", Endpoint: "https://x"}), "typed-key")

	view := plain(c.View())

	assert.NotContains(t, view, "typed-key")
	assert.Contains(t, view, "https://x")
}

func TestConnectSpaceTogglesACheckboxAndTypesIntoAField(t *testing.T) {
	c := newConnect(panes.ConnectForm{Profile: "p", Endpoint: "https://x", Key: "k", StoreKey: true})

	c, _ = c.NextField().Update(spaceKey()) // skip verify
	c, _ = c.NextField().Update(spaceKey()) // store key
	form, err := c.Form()
	require.NoError(t, err)
	assert.True(t, form.SkipVerify)
	assert.False(t, form.StoreKey)

	c, _ = c.PrevField().PrevField().Update(spaceKey()) // back on the key
	form, err = typeInto(c, "a").Form()
	require.NoError(t, err)
	assert.Equal(t, "k a", form.Key)
}

func TestConnectShowsTheFailureAndStopsTheSpinner(t *testing.T) {
	c, tick := newConnect(panes.ConnectForm{}).StartConnecting()
	require.NotNil(t, tick)
	require.True(t, c.Connecting())
	assert.Contains(t, plain(c.View()), "Connecting")

	c = c.Fail(errors.New("cosmos: ping: 401 Unauthorized"))

	assert.False(t, c.Connecting())
	assert.Contains(t, plain(c.View()), "401 Unauthorized")
	assert.NotContains(t, plain(c.View()), "Connecting")
}

func TestConnectIntroSaysWhatIsMissing(t *testing.T) {
	assert.Contains(t, plain(newConnect(panes.ConnectForm{}).View()), "No profile yet")
	assert.Contains(t, plain(newConnect(panes.ConnectForm{Profile: "prod", Endpoint: "https://x"}).View()),
		"Profile prod has no key")
}

func TestConnectNeverOutgrowsItsFrame(t *testing.T) {
	c := newConnect(panes.ConnectForm{}).SetSize(40, 12).Fail(errors.New("a failure long enough to need wrapping inside a narrow frame"))

	view := c.View()

	assert.LessOrEqual(t, lipgloss.Width(view), 40)
	assert.LessOrEqual(t, strings.Count(view, "\n")+1, 12)
}
