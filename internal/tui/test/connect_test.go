package tui_test

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// connector records what the connect screen submits and answers with a
// fixture connection, after failing the first failFirst attempts.
type connector struct {
	t         *testing.T
	forms     []panes.ConnectForm
	failFirst int
}

func (c *connector) connect(_ context.Context, form panes.ConnectForm) (adapter.Connection, error) {
	c.forms = append(c.forms, form)
	if c.failFirst > 0 {
		c.failFirst--
		return nil, errors.New("cosmos: ping: 401 Unauthorized")
	}
	return newConnection(c.t), nil
}

func newConnectModel(t *testing.T, c *connector, form panes.ConnectForm) tea.Model {
	t.Helper()
	m := tui.New(tui.Options{Icons: theme.Icons(), Connect: c.connect, Form: form})
	model, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	model, _ = settle(model, model.Init())
	return model
}

func fillAndSubmit(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m,
		keyText("quill"), keyMsg(tea.KeyTab),
		keyText("https://localhost:8081"), keyMsg(tea.KeyTab),
		keyText("typed-key"), keyMsg(tea.KeyEnter))
}

func TestSessionWithoutAConnectionOpensOnTheConnectScreen(t *testing.T) {
	view := newConnectModel(t, &connector{t: t}, panes.ConnectForm{}).View()

	assert.Contains(t, view, "Connect")
	assert.Contains(t, view, "No profile yet")
	assert.NotContains(t, view, catalogTitle)
}

func TestSubmittingTheFormConnectsAndOpensTheCatalog(t *testing.T) {
	c := &connector{t: t}

	m := fillAndSubmit(t, newConnectModel(t, c, panes.ConnectForm{StoreKey: true}))

	require.Len(t, c.forms, 1)
	assert.Equal(t, panes.ConnectForm{
		Profile:  "quill",
		Endpoint: "https://localhost:8081",
		Key:      "typed-key",
		StoreKey: true,
	}, c.forms[0])
	assert.Contains(t, m.View(), catalogTitle)
	assert.Contains(t, m.View(), firstDatabase, "the catalog loads on the new connection")
	assert.Contains(t, m.View(), "quill", "the status bar names the profile")
}

func TestAFailedAttemptKeepsTheFormAndTheNextCanSucceed(t *testing.T) {
	c := &connector{t: t, failFirst: 1}
	m := fillAndSubmit(t, newConnectModel(t, c, panes.ConnectForm{}))

	assert.Contains(t, m.View(), "401 Unauthorized")
	assert.NotContains(t, m.View(), catalogTitle)

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Len(t, c.forms, 2)
	assert.Contains(t, m.View(), catalogTitle)
}

func TestAnIncompleteFormNeverReachesTheConnector(t *testing.T) {
	c := &connector{t: t}

	m := pressAll(t, newConnectModel(t, c, panes.ConnectForm{}), keyMsg(tea.KeyEnter))

	assert.Empty(t, c.forms)
	assert.Contains(t, m.View(), "name the profile")
}

func TestSeededProfileNeedsOnlyTheKey(t *testing.T) {
	c := &connector{t: t}
	m := newConnectModel(t, c, panes.ConnectForm{Profile: "prod", Endpoint: "https://x", StoreKey: true})

	m = pressAll(t, m, keyText("typed-key"), keyMsg(tea.KeyEnter))

	require.Len(t, c.forms, 1)
	assert.Equal(t, "prod", c.forms[0].Profile)
	assert.Equal(t, "typed-key", c.forms[0].Key)
	assert.Contains(t, m.View(), catalogTitle)
}

func TestEscQuitsTheConnectScreenButQIsALetterThere(t *testing.T) {
	m := newConnectModel(t, &connector{t: t}, panes.ConnectForm{})

	_, msgs := press(t, m, keyRune('q'))
	assert.False(t, hasMsg[tea.QuitMsg](msgs))

	_, msgs = press(t, m, keyMsg(tea.KeyEscape))
	assert.True(t, hasMsg[tea.QuitMsg](msgs))
}

func TestQuittingClosesAConnectionTheScreenOpened(t *testing.T) {
	c := &connector{t: t}
	m := fillAndSubmit(t, newConnectModel(t, c, panes.ConnectForm{}))

	_, msgs := press(t, m, keyRune('q'))

	assert.True(t, hasMsg[tea.QuitMsg](msgs))
}
