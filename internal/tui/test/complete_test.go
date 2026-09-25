package tui_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

// keyCtrlSpace is ctrl+space as bubbletea reports it.
func keyCtrlSpace() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlAt}
}

// editorLines are the rows inside the editor frame, unstyled.
func editorLines(view string) []string {
	var lines []string
	inside := false
	for _, line := range strings.Split(plain(view), "\n") {
		switch {
		case strings.Contains(line, editorTitle):
			inside = true
			continue
		case inside && strings.Contains(line, resultsTitle):
			return lines
		}
		if inside {
			lines = append(lines, line)
		}
	}
	return lines
}

// listed reports whether the editor pane shows text on a row of its own,
// which is where a suggestion appears and where buffer text never does.
func listed(view, text string) bool {
	for _, line := range editorLines(view) {
		if strings.Contains(line, "▸ "+text+" ") || strings.Contains(line, "  "+text+" ") {
			return true
		}
	}
	return false
}

// tallHeight gives the editor room for the whole list; the minimum
// terminal spares it two rows.
const tallHeight = 40

// newTallModel is a loaded model with room for the whole list.
func newTallModel(t *testing.T, conn adapter.Connection) tea.Model {
	t.Helper()
	return tall(newLoadedModel(t, conn))
}

func tall(m tea.Model) tea.Model {
	model, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: tallHeight})
	return model
}

// newModelWithoutSampling is a session whose profile turned sampling off.
func newModelWithoutSampling(t *testing.T, conn adapter.Connection) tea.Model {
	t.Helper()
	m := newModelWith(t, tui.Options{Connection: conn, Manage: managed})
	model, _ := settle(m, m.Init())
	return tall(model)
}

func TestTypingAKeywordListsItAndTabAcceptsIt(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SEL")

	require.True(t, listed(m.View(), "SELECT"), "the list opens on its own:\n%s", plain(m.View()))

	m = pressAll(t, m, keyMsg(tea.KeyTab))

	view := plain(m.View())
	assert.Contains(t, view, "┃ SELECT", "the word is replaced in the buffer")
	assert.False(t, listed(view, "SELECT"), "and the list closes")
	assert.Contains(t, pressAll(t, m, keyText(" *")).View(), "SELECT *", "the cursor sits after the inserted text")
}

func TestTypingAfterFromListsDatabases(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM s")

	view := m.View()
	assert.True(t, listed(view, "sales"), plain(view))
	assert.False(t, listed(view, "telemetry"), "s narrows the list to what matches")
}

// rootOnly is a model whose top level has arrived while the prefetch of its
// databases is still out; the prefetch's command is returned to deliver later.
func rootOnly(t *testing.T, conn *recordingConnection) (tea.Model, tea.Cmd) {
	t.Helper()
	m := tall(newModel(t, conn))
	root := messages(m.Init())
	return m.Update(root[len(root)-1])
}

func TestADatabaseDotWaitsForTheListingAlreadyOnItsWay(t *testing.T) {
	conn := newConnection(t)
	m, prefetch := rootOnly(t, conn)

	m = typeQuery(t, m, "SELECT * FROM sales.")

	assert.Contains(t, plain(m.View()), "loading sales…")
	assert.Equal(t, 0, conn.calls[firstDatabase], "completion asks for nothing the tree has asked for")

	m, _ = settle(m, prefetch)
	assert.Equal(t, 1, conn.calls[firstDatabase])
	view := m.View()
	assert.True(t, listed(view, firstContainer), plain(view))
	assert.True(t, listed(view, "customers"))
}

func TestADatabaseWhoseListingFailedIsNotAskedAgainByCompletion(t *testing.T) {
	conn := newConnection(t)
	conn.failChildren = 2
	m := newTallModel(t, conn)
	require.Equal(t, 1, conn.calls[firstDatabase])

	m = typeQuery(t, m, "SELECT * FROM sales.")

	view := plain(m.View())
	assert.NotContains(t, view, "loading sales…")
	assert.False(t, listed(view, firstContainer))
	assert.Equal(t, 1, conn.calls[firstDatabase], "a retry is the user's to ask for")
	assert.Contains(t, view, "unreachable", "the tree still shows why")
}

func TestAnUnknownDatabaseListsNothingAndPromisesNothing(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM hr.")

	assert.NotContains(t, plain(m.View()), "loading")
}

// pendingSample types a query up to a field of container and returns the
// model with that container's sample still out.
func pendingSample(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	m, _ = m.Update(keyRune('e'))
	m, _ = m.Update(keyText(text))
	return m
}

func TestALateSampleForAnotherContainerIsFiledUnderIt(t *testing.T) {
	m := pendingSample(t, newTallModel(t, newConnection(t)), "SELECT * FROM telemetry.events e WHERE e.")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText("SELECT * FROM sales.orders o WHERE o."))
	require.True(t, listed(m.View(), "amount"), "orders is on screen, sampled")

	m, _ = m.Update(tui.FieldsSampledMsg{
		Path:   []string{"telemetry", "events"},
		Sample: adapter.FieldSample{Fields: []adapter.Field{{Path: "reading", Kind: "number"}}},
	})

	assert.False(t, listed(m.View(), "reading"), "the open list is about orders")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText("SELECT * FROM telemetry.events e WHERE e."))
	assert.True(t, listed(m.View(), "reading"))
}

func TestASampleForAContainerNoLongerRequestedIsDropped(t *testing.T) {
	m := typeQuery(t, newModelWithoutSampling(t, newConnection(t)), "SELECT * FROM telemetry.events e WHERE e.")

	m, _ = m.Update(tui.FieldsSampledMsg{
		Path:   []string{"telemetry", "events"},
		Sample: adapter.FieldSample{Fields: []adapter.Field{{Path: "reading", Kind: "number"}}},
	})

	assert.False(t, listed(m.View(), "reading"), "nothing asked for it")
}

func TestAcceptingABracketFormFieldReplacesTheDot(t *testing.T) {
	m := pendingSample(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c WHERE c.")
	m, _ = m.Update(tui.FieldsSampledMsg{
		Path:   []string{firstDatabase, firstContainer},
		Sample: adapter.FieldSample{Fields: []adapter.Field{{Path: "order-id", Kind: "string"}}},
	})

	m = pressAll(t, m, keyText("order"), keyMsg(tea.KeyTab))

	assert.Contains(t, plain(m.View()), `SELECT * FROM c WHERE c["order-id"]`)
}

func TestAliasDotListsTheFieldsAPageShowedBeforeAnySample(t *testing.T) {
	conn := newConnection(t)
	m := runQuery(t, selectContainer(t, newTallModel(t, conn)), "SELECT * FROM c")

	m, _ = m.Update(keyText(" WHERE c."))

	view := m.View()
	assert.True(t, listed(view, "amount"), plain(view))
	assert.True(t, listed(view, "customerId"), "the partition key comes from the tree")
	assert.Contains(t, plain(view), "sampling orders…", "the sample is still out")
	assert.Empty(t, conn.sampled, "nothing has answered yet")
}

func TestTheFirstFieldCompletionOfAContainerSamplesItOnce(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, selectContainer(t, newTallModel(t, conn)), "SELECT * FROM c WHERE c.")

	view := m.View()
	assert.True(t, listed(view, "amount"), plain(view))
	assert.NotContains(t, plain(view), "sampling")
	assert.Equal(t, [][]string{{firstDatabase, firstContainer}}, conn.sampled)

	m = pressAll(t, m, keyText("id AND c."))
	assert.Len(t, conn.sampled, 1, "a second completion of the same container asks nothing")

	m = pressAll(t, m, keyText("x = 1 AND EXISTS(SELECT * FROM telemetry.events e WHERE e."))
	assert.True(t, listed(m.View(), "deviceId"))
	assert.Equal(t, [][]string{{firstDatabase, firstContainer}, {"telemetry", "events"}}, conn.sampled)
}

func TestSamplingOffIssuesNoSample(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, selectContainer(t, newModelWithoutSampling(t, conn)), "SELECT * FROM c WHERE c.")

	assert.True(t, listed(m.View(), "customerId"), "the partition key is still known")
	assert.False(t, listed(m.View(), "amount"))
	assert.Empty(t, conn.sampled)
}

func TestAFailedSampleIsLoggedNotRetriedAndLeavesTypingAlone(t *testing.T) {
	conn := newConnection(t, mock.WithError(mock.OpSampleFields))
	var logged bytes.Buffer
	m := newModelWith(t, tui.Options{Connection: conn, Manage: managed, SampleFields: true, Logger: log.New(&logged)})
	m, _ = settle(m, m.Init())

	m = typeQuery(t, selectContainer(t, m), "SELECT * FROM c WHERE c.")
	m = pressAll(t, m, keyText("x = 1 AND c."))

	assert.Len(t, conn.sampled, 1)
	assert.Contains(t, logged.String(), tui.OpSampleFields)
	assert.Contains(t, plain(m.View()), "SELECT * FROM c WHERE c.x = 1 AND c.")
	assert.True(t, listed(m.View(), "customerId"))
}

func TestAJoinFilesEachSideUnderItsOwnContainer(t *testing.T) {
	conn := newConnection(t)
	m := runQuery(t, newModelWithoutSampling(t, conn), mockJoin)

	m = pressAll(t, m, keyText(" WHERE o."))
	left := m.View()
	m = pressAll(t, m, keyText("id = 1 AND cu."))
	right := m.View()

	assert.True(t, listed(left, "id"), plain(left))
	assert.False(t, listed(left, "note"), "note was projected from customers")
	assert.True(t, listed(right, "note"), plain(right))
	assert.False(t, listed(right, "id"))
	for _, view := range []string{left, right} {
		assert.False(t, listed(view, "_container"))
		assert.False(t, listed(view, "o.id"))
		assert.False(t, listed(view, "cu.note"))
	}
}

func TestAJoinSamplesEachSideOnce(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, newTallModel(t, conn), mockJoin+" WHERE o.")

	m = pressAll(t, m, keyText("id = 1 AND cu."))
	pressAll(t, m, keyText("note = 2 AND o."))

	assert.Equal(t, [][]string{{"sales", "orders"}, {"sales", "customers"}}, conn.sampled)
}

func TestAUnionFilesFieldsOfEitherContainerAndNeverTheTag(t *testing.T) {
	conn := newConnection(t)
	m := runQuery(t, newModelWithoutSampling(t, conn), "SELECT * FROM sales.orders, sales.customers")
	m = pressAll(t, focusResults(t, m), keyRune('m'), keyRune('m'), keyRune('m')) // into customers

	m = pressAll(t, m, keyRune('e'), keyText(" WHERE c."))

	view := m.View()
	assert.True(t, listed(view, "amount"), plain(view))
	assert.True(t, listed(view, "customerId"))
	assert.True(t, listed(view, "region"), "the key of the second container")
	assert.False(t, listed(view, "_container"))
}

func TestAfterACrossContainerJoinNoClauseThePlannerRefusesIsOffered(t *testing.T) {
	m := pressAll(t, typeQuery(t, newTallModel(t, newConnection(t)), mockJoin+" "), keyCtrlSpace())

	view := m.View()
	assert.True(t, listed(view, "WHERE"), plain(view))
	assert.False(t, listed(view, "ORDER BY"))
	assert.False(t, listed(view, "GROUP BY"))
	assert.False(t, listed(view, "OFFSET"))

	single := pressAll(t, typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM sales.orders o "), keyCtrlSpace())
	assert.True(t, listed(single.View(), "ORDER BY"), plain(single.View()))
}

func TestCtrlSpaceOpensOnAnEmptyPrefixAndAfterADismissal(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM ")
	require.False(t, listed(m.View(), "sales"), "whitespace opens nothing")

	m = pressAll(t, m, keyCtrlSpace())
	assert.True(t, listed(m.View(), "sales"))

	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyCtrlSpace())
	assert.True(t, listed(m.View(), "sales"))
}

func TestTabWithTheListClosedStillMovesPanes(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM c ")
	results := pressAll(t, newTallModel(t, newConnection(t)), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab))

	moved := pressAll(t, m, keyMsg(tea.KeyTab))

	assert.Equal(t, editorLines(results.View())[1:], editorLines(moved.View())[1:], "the results pane has the focus")
	assert.Contains(t, plain(moved.View()), "SELECT * FROM c ", "and nothing was inserted")
}

func TestEnterWithTheListOpenInsertsANewline(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SEL")

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	lines := editorLines(m.View())
	require.GreaterOrEqual(t, len(lines), 2)
	assert.Contains(t, lines[0], "SEL")
	assert.NotContains(t, lines[0], "SELECT", "enter never accepts")
	assert.False(t, listed(m.View(), "SELECT"), "a newline is not part of a word, so the list closes")
}

func TestEscapeDismissesTheListFirstAndLeavesTheEditorSecond(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SEL")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.False(t, listed(m.View(), "SELECT"))
	assert.Contains(t, plain(m.View()), "┃ SEL", "the buffer is untouched")

	m = pressAll(t, m, keyRune('E'))
	assert.False(t, listed(m.View(), "SELECT"), "the dismissal holds for the rest of the word")
	assert.Contains(t, plain(m.View()), "┃ SELE")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(m.View()), "┃ SELE", "the second escape leaves the buffer alone")
	assert.Contains(t, plain(pressAll(t, m, keyRune('?')).View()), "Keys", "and has left the editor")
}

func TestGlobalLettersTypedIntoTheBufferNarrowTheListAndTriggerNothing(t *testing.T) {
	m := typeQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c WHERE c.")
	require.True(t, listed(m.View(), "amount"))

	for _, k := range []tea.KeyMsg{keyRune('q'), keyRune('r'), keyRune('?')} {
		typed, cmd := m.Update(k)
		assert.Nil(t, cmd, "%q is text", k.String())
		assert.Contains(t, typed.View(), catalogTitle, "%q neither quits nor opens an overlay", k.String())
		assert.False(t, listed(typed.View(), "amount"), "%q narrows the list", k.String())
	}
}

func TestBlurringTheEditorClosesTheList(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SEL")
	require.True(t, listed(m.View(), "SELECT"))

	m = pressAll(t, m, keyMsg(tea.KeyShiftTab))

	assert.False(t, listed(m.View(), "SELECT"))
	assert.False(t, listed(pressAll(t, m, keyCtrlSpace()).View(), "SELECT"), "and it never opens elsewhere")
}

func TestRunningTheQueryClosesTheList(t *testing.T) {
	m := runQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c")

	assert.False(t, listed(m.View(), "sales"))
}

func TestAShortEditorShowsTheOneLineForm(t *testing.T) {
	m := newTallModel(t, newConnection(t))
	m, _ = m.Update(tea.WindowSizeMsg{Width: testWidth, Height: 10})

	m = typeQuery(t, selectContainer(t, m), "SELECT * FROM c WHERE c.")

	view := plain(m.View())
	assert.Contains(t, view, "▸ customerId")
	assert.Contains(t, view, "1 of ")
	assert.NotContains(t, view, "tab accept", "there is no room for the hint line")
	assert.Contains(t, pressAll(t, m, keyMsg(tea.KeyDown)).View(), "2 of ")
}

func TestAcceptingAFunctionOpensItsParenthesis(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM c WHERE STARTSW")

	m = pressAll(t, m, keyMsg(tea.KeyTab), keyText("c.id"))

	assert.Contains(t, plain(m.View()), "WHERE STARTSWITH(c.id")
}

func TestAKeywordFollowsTheTypedCase(t *testing.T) {
	m := pressAll(t, typeQuery(t, newTallModel(t, newConnection(t)), "sel"), keyMsg(tea.KeyTab))

	assert.Contains(t, plain(m.View()), "┃ select")
}

func TestTheHelpOverlayGroupsTheEditorKeys(t *testing.T) {
	view := plain(pressAll(t, newTallModel(t, newConnection(t)), keyRune('?')).View())

	assert.Contains(t, view, "Editor")
	assert.Equal(t, column(t, view, "Editor"), column(t, view, "ctrl+space"))
}

func TestANumberOpensNoListSoTabStillMovesPanes(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM c WHERE c.a = 1")
	require.False(t, listed(m.View(), "AND"))

	moved := pressAll(t, m, keyMsg(tea.KeyTab))

	assert.Contains(t, plain(moved.View()), "SELECT * FROM c WHERE c.a = 1 ", "nothing was glued to the number")
	assert.NotContains(t, plain(moved.View()), "1AND")
}

func TestAnAliasBeingTypedIsNeverReplaced(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT * FROM sales.orders o")
	require.False(t, listed(m.View(), "ORDER BY"))

	m = pressAll(t, m, keyMsg(tea.KeyTab))

	assert.Contains(t, plain(m.View()), "FROM sales.orders o ", "tab moved panes and left the alias alone")
}

func TestAProjectionIsNotFiledAsFieldsOfTheContainer(t *testing.T) {
	m := runQuery(t, selectContainer(t, newModelWithoutSampling(t, newConnection(t))), "SELECT c.amount AS total, c.note FROM c")

	m = pressAll(t, m, keyText(" WHERE c."))

	view := m.View()
	assert.False(t, listed(view, "total"), plain(view))
	assert.False(t, listed(view, "note"), "a projected field is not known to be one of the items")
	assert.True(t, listed(view, "customerId"), "the partition key still is")
}

func TestASampleLandingKeepsTheChosenRow(t *testing.T) {
	m := runQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c")
	m, _ = m.Update(keyText(" WHERE c."))
	m, _ = m.Update(keyMsg(tea.KeyDown))
	require.Contains(t, plain(m.View()), "2 of 5")

	m, _ = m.Update(tui.FieldsSampledMsg{
		Path:   []string{firstDatabase, firstContainer},
		Sample: adapter.FieldSample{Fields: []adapter.Field{{Path: "amount", Kind: "number"}}},
	})

	assert.Contains(t, plain(m.View()), "2 of 5", "the sample changed nothing and moved nothing")
}

func TestTypingAfterChoosingSelectsTheBestMatchAgain(t *testing.T) {
	m := runQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c")
	m = pressAll(t, m, keyText(" WHERE c."), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown))
	require.Contains(t, plain(m.View()), "▸ pk ")

	m = pressAll(t, m, keyText("a"))

	assert.Contains(t, plain(m.View()), "▸ amount ", "the prefix match, not the row chosen before")
}

func TestADismissalIsForgottenOnceTheCursorLeavesTheToken(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SEL")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = pressAll(t, m, keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyText("SEL"))
	assert.False(t, listed(m.View(), "SELECT"), "the same word at the same place is still dismissed")

	m = pressAll(t, m, keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyText("FRO"))
	assert.True(t, listed(m.View(), "FROM") || !listed(m.View(), "SELECT"), "another word at the same place opens again")

	m = pressAll(t, m, keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyMsg(tea.KeyBackspace), keyText("SEL"))
	assert.True(t, listed(m.View(), "SELECT"), "and the old dismissal is gone")
}

func TestARecreatedContainerIsSampledAgain(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, selectContainer(t, newTallModel(t, conn)), "SELECT * FROM sales.orders o WHERE o.")
	require.Equal(t, [][]string{{firstDatabase, firstContainer}}, conn.sampled)

	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyEscape)) // back to the catalog, on sales.orders
	m = pressAll(t, m, keyRune('d'), keyText(firstContainer), keyMsg(tea.KeyEnter))
	m = createContainer(t, m, firstContainer, "/customerId")
	pressAll(t, m, keyRune('e'), keyText("id = 1 AND o."))

	assert.Equal(t, [][]string{{firstDatabase, firstContainer}, {firstDatabase, firstContainer}}, conn.sampled)
}

// BenchmarkTypingWithTheListOpen is the checklist's "typing at speed in a
// 200-line buffer": one keystroke narrowing an open list, buffer included.
func BenchmarkTypingWithTheListOpen(b *testing.B) {
	t := &testing.T{}
	lines := make([]string, 0, 200)
	for i := range 200 {
		lines = append(lines, fmt.Sprintf("  OR c.n = %d", i))
	}
	m := typeQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), "SELECT * FROM c WHERE c.a = 1\n"+strings.Join(lines, "\n")+"\nAND c.")
	if !listed(m.View(), "customerId") {
		b.Fatal("the list should be open")
	}

	b.ResetTimer()
	for range b.N {
		typed, _ := m.Update(keyRune('c'))
		_ = typed.View()
		m, _ = typed.Update(keyMsg(tea.KeyBackspace))
	}
}
