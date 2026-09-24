package tui_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The overlay titles as they appear inside the top border.
const (
	savedTitle  = " Saved queries · "
	promptTitle = " Save query "
	renameTitle = " Rename query "
)

// failingStore refuses whatever a test tells it to, and lists nothing.
type failingStore struct {
	saved.Unavailable
	failList error
}

func (s failingStore) List(string) (saved.Listing, error) {
	return saved.Listing{}, s.failList
}

func newSavedStore(t *testing.T) saved.Dir {
	t.Helper()
	return saved.Open(filepath.Join(t.TempDir(), saved.DirName))
}

// newSavedModel is a session on the mock account, its catalog loaded and
// sales.orders selected, saving to store.
func newSavedModel(t *testing.T, conn *recordingConnection, store saved.Store) tea.Model {
	t.Helper()
	m := newModelWith(t, conn, tui.Options{Saved: store, History: &recordingStore{}})
	model, _ := settle(m, m.Init())
	return selectContainer(t, model)
}

func saveAs(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyCtrlS), keyText(name), keyMsg(tea.KeyEnter))
}

func openSaved(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyCtrlL))
}

func create(t *testing.T, store saved.Store, account string, q saved.Query) {
	t.Helper()
	require.NoError(t, store.Create(account, q))
}

func only(t *testing.T, store saved.Store, account string) saved.Query {
	t.Helper()
	listing, err := store.List(account)
	require.NoError(t, err)
	require.Len(t, listing.Queries, 1)
	return listing.Queries[0]
}

func editorText(m tea.Model) string {
	return plain(m.View())
}

func TestCtrlSInTheEditorOpensThePromptAndTypesNothing(t *testing.T) {
	m := typeQuery(t, newSavedModel(t, newConnection(t), newSavedStore(t)), "SELECT * FROM c")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS))

	view := plain(m.View())
	assert.Contains(t, view, promptTitle)
	assert.Contains(t, view, "account  "+mock.Name)
	assert.Contains(t, view, "query    SELECT * FROM c")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.NotContains(t, plain(m.View()), "SELECT * FROM cs", "nothing reached the buffer")
}

func TestLettersTypedIntoThePromptAreTheName(t *testing.T) {
	m := typeQuery(t, newSavedModel(t, newConnection(t), newSavedStore(t)), "SELECT * FROM c")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS), keyText("qrd!"))

	assert.Contains(t, plain(m.View()), "qrd!", "q, r, d and ! are text in the name")
	assert.Contains(t, plain(m.View()), promptTitle, "q did not quit")
}

func TestEnterSavesTheQueryToAFileAndSaysWhere(t *testing.T) {
	store := newSavedStore(t)
	m := typeQuery(t, newSavedModel(t, newConnection(t), store), "SELECT * FROM c")

	m = saveAs(t, m, "open orders")

	view := plain(m.View())
	assert.NotContains(t, view, promptTitle)
	assert.Contains(t, statusBar(m), `saved "open orders" to mock`)
	data, err := os.ReadFile(filepath.Join(store.AccountPath(mock.Name), "open orders.sql"))
	require.NoError(t, err)
	assert.Equal(t, "-- alchemist: scope=sales/orders\nSELECT * FROM c\n", string(data))
	m = pressAll(t, m, keyText("x"))
	assert.Contains(t, plain(m.View()), "SELECT * FROM cx", "the prompt closed onto the editor")
}

func TestSavingAnEmptyBufferSaysSoAndOpensNothing(t *testing.T) {
	m := newSavedModel(t, newConnection(t), newSavedStore(t))

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS))

	assert.NotContains(t, plain(m.View()), promptTitle)
	assert.Contains(t, statusBar(m), "nothing to save")
}

func TestSavingWithNoAccountSaysSoAndOpensNothing(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Saved: newSavedStore(t)}),
		keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))
	m = typeQuery(t, m, "SELECT * FROM c")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS))

	assert.NotContains(t, plain(m.View()), promptTitle)
	assert.Contains(t, statusBar(m), "no account connected")
}

func TestATakenNameIsRefusedUntilItEndsWithTheReplaceMark(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "open orders", Text: "SELECT 1"})
	m := typeQuery(t, newSavedModel(t, newConnection(t), store), "SELECT * FROM c")

	m = saveAs(t, m, "Open Orders")

	view := plain(m.View())
	assert.Contains(t, view, promptTitle, "the prompt stays open")
	assert.Contains(t, view, `"Open Orders" is already saved for mock: end the name with ! to replace it`)
	assert.Equal(t, "SELECT 1", only(t, store, mock.Name).Text)

	m = pressAll(t, m, keyRune('!'), keyMsg(tea.KeyEnter))

	assert.NotContains(t, plain(m.View()), promptTitle)
	replaced := only(t, store, mock.Name)
	assert.Equal(t, "open orders", replaced.Name, "the spelling on disk is kept")
	assert.Equal(t, "SELECT * FROM c", replaced.Text)
}

func TestTheSavedScopeIsKeptOnlyWhenTheTextNeedsOne(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "bare source", text: "SELECT * FROM c", want: []string{"sales", "orders"}},
		{name: "named container", text: "SELECT * FROM sales.orders c"},
		{name: "union", text: "SELECT * FROM sales.orders, sales.archive AS c"},
		{name: "join", text: "SELECT o.id FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"},
		{name: "draft the planner refuses", text: "SELEC * FORM c", want: []string{"sales", "orders"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newSavedStore(t)
			m := typeQuery(t, newSavedModel(t, newConnection(t), store), tt.text)

			saveAs(t, m, "q")

			assert.Equal(t, tt.want, only(t, store, mock.Name).Scope)
		})
	}
}

func TestCtrlLListsTheAccountsQueriesByName(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "zeta", Text: "SELECT 2"})
	create(t, store, mock.Name, saved.Query{Name: "Alpha", Text: "SELECT 1", Scope: []string{"sales", "orders"}})

	view := plain(openSaved(t, newSavedModel(t, newConnection(t), store)).View())

	assert.Contains(t, view, savedTitle+mock.Name)
	alpha, zeta := strings.Index(view, "Alpha"), strings.Index(view, "zeta")
	require.GreaterOrEqual(t, alpha, 0)
	assert.Less(t, alpha, zeta, "sorted by name, ignoring case")
	assert.Contains(t, view, "sales.orders")
}

func TestCtrlLClosesTheOverlayItOpened(t *testing.T) {
	m := openSaved(t, openSaved(t, newSavedModel(t, newConnection(t), newSavedStore(t))))

	assert.NotContains(t, plain(m.View()), savedTitle)
}

func TestAnAccountWithNothingSavedSaysSo(t *testing.T) {
	m := openSaved(t, newSavedModel(t, newConnection(t), newSavedStore(t)))

	assert.Contains(t, plain(m.View()), "nothing saved for mock yet: ctrl+s saves the query in the editor")
}

func TestSavedQueriesWithNoAccountSayWhy(t *testing.T) {
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Saved: newSavedStore(t)}),
		keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))

	m = openSaved(t, m)

	assert.Contains(t, plain(m.View()), "no account connected: ctrl+g to choose one")
}

func TestSlashFiltersByNameTextAndScope(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "open orders", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "events", Text: "SELECT jd FROM c", Scope: []string{"telemetry", "events"}})
	create(t, store, mock.Name, saved.Query{Name: "staff", Text: "SELECT 3", Scope: []string{"hr", "employees"}})
	m := openSaved(t, newSavedModel(t, newConnection(t), store))

	byName := plain(pressAll(t, m, keyRune('/'), keyText("open")).View())
	byText := plain(pressAll(t, m, keyRune('/'), keyRune('j'), keyRune('d')).View())
	byScope := plain(pressAll(t, m, keyRune('/'), keyText("hr.")).View())

	assert.Contains(t, byName, "open orders")
	assert.NotContains(t, byName, "staff")
	assert.Contains(t, byText, "events", "j and d typed into the filter are text")
	assert.NotContains(t, byText, "open orders")
	assert.Contains(t, byScope, "staff")
	assert.NotContains(t, byScope, "open orders")
}

func TestEscapeClearsTheSavedFilterBeforeItClosesTheOverlay(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "open orders", Text: "SELECT 1"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('/'), keyText("zzz"))

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(m.View()), "open orders", "the filter is cleared")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.NotContains(t, plain(m.View()), savedTitle)
}

func TestEnterRecallsTheTextAndTheSavedScope(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "events", Text: "SELECT * FROM c WHERE c.kind = 1", Scope: []string{"telemetry", "events"}})
	m := openSaved(t, newSavedModel(t, newConnection(t), store))

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.NotContains(t, plain(m.View()), savedTitle)
	assert.Contains(t, editorText(m), "SELECT * FROM c WHERE c.kind = 1")
	assert.Contains(t, statusBar(m), "telemetry.events")
	m = pressAll(t, m, keyText("x"))
	assert.Contains(t, editorText(m), "c.kind = 1x", "the editor has the keyboard")
}

func TestRecallingAQueryWithNoScopeLeavesTheScopeAlone(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "events", Text: "SELECT * FROM telemetry.events e"})
	m := openSaved(t, newSavedModel(t, newConnection(t), store))

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Contains(t, statusBar(m), "sales.orders")
}

func TestCtrlRRecallsAndRunsTheSavedQuery(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "events", Text: "SELECT * FROM c", Scope: []string{"telemetry", "events"}})
	conn := newConnection(t)
	m := openSaved(t, newSavedModel(t, conn, store))

	pressAll(t, m, keyMsg(tea.KeyCtrlR))

	require.Len(t, conn.queries, 1)
	assert.Equal(t, []string{"telemetry", "events"}, conn.queries[0].Scope)
}

func TestAfterARecallCtrlSOffersTheRecalledName(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "open orders", Text: "SELECT 1"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyMsg(tea.KeyEnter))

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS))

	assert.Contains(t, plain(m.View()), "│open orders ", "the name is offered, without the replace mark")
	assert.NotContains(t, plain(m.View()), "open orders!")
}

func TestAfterAHistoryRecallCtrlSOffersNoName(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "open orders", Text: "SELECT 1"})
	m := newModelWith(t, newConnection(t), tui.Options{Saved: store, History: pastRuns()})
	m, _ = settle(m, m.Init())
	m = pressAll(t, openSaved(t, m), keyMsg(tea.KeyEnter))

	m = pressAll(t, openHistory(t, m), keyMsg(tea.KeyEnter), keyMsg(tea.KeyCtrlS))

	assert.NotContains(t, plain(m.View()), "│open orders")
}

func TestDThenYDeletesTheQuery(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "alpha", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "beta", Text: "SELECT 2"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('d'))

	assert.Contains(t, plain(m.View()), `delete "alpha"?`)
	m = pressAll(t, m, keyRune('y'))

	assert.Equal(t, "beta", only(t, store, mock.Name).Name)
	view := plain(m.View())
	assert.NotContains(t, view, "alpha")
	assert.Contains(t, view, savedTitle, "the overlay stays open")
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, editorText(m), "SELECT 2", "the cursor moved to the neighbor")
}

func TestDThenAnyOtherKeyKeepsTheQueryAndIsSpentOnTheAnswer(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "alpha", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "beta", Text: "SELECT 2"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('d'), keyRune('j'))

	listing, err := store.List(mock.Name)
	require.NoError(t, err)
	assert.Len(t, listing.Queries, 2)
	assert.NotContains(t, plain(m.View()), `delete "alpha"?`)
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, editorText(m), "SELECT 1", "j answered the question and did not move the cursor")
}

func TestRRenamesThroughThePromptAndReturnsToTheOverlay(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "alpha", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "beta", Text: "SELECT 2"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('r'))
	assert.Contains(t, plain(m.View()), renameTitle)

	m = pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText("zulu"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), savedTitle)
	listing, err := store.List(mock.Name)
	require.NoError(t, err)
	assert.Equal(t, []string{"beta", "zulu"}, []string{listing.Queries[0].Name, listing.Queries[1].Name})
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, editorText(m), "SELECT 1", "the cursor followed the renamed query")
}

func TestRenameRefusesATakenNameAndTheReplaceMark(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "alpha", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "beta", Text: "SELECT 2"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('r'), keyMsg(tea.KeyCtrlU))

	taken := pressAll(t, m, keyText("beta"), keyMsg(tea.KeyEnter))
	marked := pressAll(t, m, keyText("beta!"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(taken.View()), `"beta" is already saved for mock`)
	assert.Contains(t, plain(marked.View()), `"beta!" is not a name that can be saved`)
	listing, err := store.List(mock.Name)
	require.NoError(t, err)
	assert.Len(t, listing.Queries, 2)
}

func TestEachAccountListsOnlyItsOwnQueries(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, "prod", saved.Query{Name: "prod query", Text: "SELECT 1"})
	create(t, store, "staging", saved.Query{Name: "staging query", Text: "SELECT 2"})
	m := newAccountsModel(t, newOpener(t), tui.Options{Saved: store})

	prod := plain(openSaved(t, m).View())
	staging := plain(openSaved(t, switchTo(t, m, "staging")).View())

	assert.Contains(t, prod, savedTitle+"prod")
	assert.Contains(t, prod, "prod query")
	assert.NotContains(t, prod, "staging query")
	assert.Contains(t, staging, savedTitle+"staging")
	assert.Contains(t, staging, "staging query")
	assert.NotContains(t, staging, "prod query")
}

func TestASaveGoesToTheActiveAccount(t *testing.T) {
	store := newSavedStore(t)
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{Saved: store}), "staging")

	saveAs(t, typeQuery(t, m, "SELECT * FROM sales.orders c"), "q")

	assert.Equal(t, "q", only(t, store, "staging").Name)
	listing, err := store.List("prod")
	require.NoError(t, err)
	assert.Empty(t, listing.Queries)
}

func TestCtrlGInsideTheSavedOverlayOrPromptDoesNotSwitch(t *testing.T) {
	m := newAccountsModel(t, newOpener(t), tui.Options{Saved: newSavedStore(t)})

	overlay := pressAll(t, openSaved(t, m), keyMsg(tea.KeyCtrlG))
	prompt := pressAll(t, typeQuery(t, m, "SELECT 1"), keyMsg(tea.KeyCtrlS), keyMsg(tea.KeyCtrlG))

	assert.NotContains(t, plain(overlay.View()), accountsTitle)
	assert.NotContains(t, plain(prompt.View()), accountsTitle)
}

func TestAListingForTheAccountLeftBehindIsDropped(t *testing.T) {
	m := switchTo(t, newAccountsModel(t, newOpener(t), tui.Options{Saved: newSavedStore(t)}), "staging")

	late, _ := m.Update(tui.SavedLoadedMsg{Account: "prod", Listing: saved.Listing{Queries: []saved.Query{{Name: "late", Text: "SELECT 1"}}}})
	failed, _ := m.Update(tui.ErrMsg{Account: "prod", Op: tui.OpSavedList, Err: errors.New("disk on fire")})

	assert.NotContains(t, plain(late.View()), savedTitle)
	assert.NotContains(t, plain(failed.View()), "disk on fire")
}

func TestTheOverlayFollowsAnAccountThatConnectsWhileItIsOpen(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, "staging", saved.Query{Name: "staging query", Text: "SELECT 2"})
	m := pressAll(t, newAccountsModel(t, newOpener(t), tui.Options{Saved: store}), keyMsg(tea.KeyCtrlG), keyRune('x'))
	m = highlight(t, m, "staging")
	m, connect := m.Update(keyMsg(tea.KeyEnter))
	m = openSaved(t, pressAll(t, m, keyMsg(tea.KeyEscape)))
	require.Contains(t, plain(m.View()), "no account connected")

	m, _ = settle(m, connect)

	view := plain(m.View())
	assert.Contains(t, view, savedTitle+"staging")
	assert.Contains(t, view, "staging query")
}

func TestCtrlSInHistorySavesTheEntryAndReturnsToHistory(t *testing.T) {
	store := newSavedStore(t)
	m := newModelWith(t, newConnection(t), tui.Options{Saved: store, History: pastRuns()})
	m, _ = settle(m, m.Init())
	m = pressAll(t, openHistory(t, m), keyMsg(tea.KeyDown))

	m = pressAll(t, m, keyMsg(tea.KeyCtrlS))
	assert.Contains(t, plain(m.View()), "query    SELECT * FROM c WHERE c.amount > 100")
	m = pressAll(t, m, keyText("big orders"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), historyTitle, "the history overlay is back")
	q := only(t, store, mock.Name)
	assert.Equal(t, "SELECT * FROM c WHERE c.amount > 100", q.Text)
	assert.Equal(t, []string{"sales", "orders"}, q.Scope, "the entry's scope stands in for the current one")
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	assert.Contains(t, editorText(m), "c.amount > 100", "the cursor stayed on the entry")
}

func TestEscapeFromThePromptReturnsToHistory(t *testing.T) {
	m := newModelWith(t, newConnection(t), tui.Options{Saved: newSavedStore(t), History: pastRuns()})
	m, _ = settle(m, m.Init())

	m = pressAll(t, openHistory(t, m), keyMsg(tea.KeyCtrlS), keyMsg(tea.KeyEscape))

	assert.Contains(t, plain(m.View()), historyTitle)
}

func TestAStoreThatCannotListSaysSoInTheOverlay(t *testing.T) {
	m := openSaved(t, newSavedModel(t, newConnection(t), failingStore{failList: errors.New("permission denied")}))

	view := plain(m.View())
	assert.Contains(t, view, savedTitle+mock.Name)
	assert.Contains(t, view, "permission denied")
}

func TestAStoreThatCannotWriteSaysSoInThePrompt(t *testing.T) {
	store := failingStore{Unavailable: saved.Unavailable{Err: errors.New("read-only file system")}}
	m := typeQuery(t, newSavedModel(t, newConnection(t), store), "SELECT 1")

	m = saveAs(t, m, "q")

	view := plain(m.View())
	assert.Contains(t, view, promptTitle, "the prompt stays open")
	assert.Contains(t, view, "read-only file system")
}

func TestSkippedFilesAreCountedInTheOverlay(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "good", Text: "SELECT 1"})
	require.NoError(t, os.WriteFile(filepath.Join(store.AccountPath(mock.Name), "bad name?.sql"), []byte("SELECT 1"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(store.AccountPath(mock.Name), "empty.sql"), nil, 0o600))

	view := plain(openSaved(t, newSavedModel(t, newConnection(t), store)).View())

	assert.Contains(t, view, "good")
	assert.Contains(t, view, "2 files skipped, see the log")
}

func TestASessionWithNoStoreSaysWhyOnSave(t *testing.T) {
	m := newModelWith(t, newConnection(t), tui.Options{})
	m, _ = settle(m, m.Init())

	m = saveAs(t, typeQuery(t, selectContainer(t, m), "SELECT 1"), "q")

	assert.Contains(t, plain(m.View()), "no store was configured")
}

// switchableStore lists from a Dir until told to fail.
type switchableStore struct {
	saved.Dir
	failList *error
}

func (s switchableStore) List(account string) (saved.Listing, error) {
	if *s.failList != nil {
		return saved.Listing{}, *s.failList
	}
	return s.Dir.List(account)
}

func TestCtrlLOpensFromTheEditorAndTypesNothing(t *testing.T) {
	m := typeQuery(t, newSavedModel(t, newConnection(t), newSavedStore(t)), "SELECT 1")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlL))

	assert.Contains(t, plain(m.View()), savedTitle)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, editorText(m), "SELECT 1")
	assert.NotContains(t, editorText(m), "SELECT 1l")
}

func TestThePromptHoldsWhileTheSaveIsInFlight(t *testing.T) {
	store := newSavedStore(t)
	m := typeQuery(t, newSavedModel(t, newConnection(t), store), "SELECT 1")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlS), keyText("q"))

	m, write := m.Update(keyMsg(tea.KeyEnter))
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyText("zz"))

	assert.Contains(t, plain(m.View()), promptTitle, "esc waits for the outcome")
	m, _ = settle(m, write)
	assert.NotContains(t, plain(m.View()), promptTitle)
	assert.Equal(t, "q", only(t, store, mock.Name).Name, "nothing typed meanwhile reached the name")
}

func TestAFailedReloadDoesNotReopenAClosedOverlay(t *testing.T) {
	var failList error
	store := switchableStore{Dir: newSavedStore(t), failList: &failList}
	create(t, store, mock.Name, saved.Query{Name: "alpha", Text: "SELECT 1"})
	create(t, store, mock.Name, saved.Query{Name: "beta", Text: "SELECT 2"})
	m := pressAll(t, openSaved(t, newSavedModel(t, newConnection(t), store)), keyRune('d'))
	m, remove := m.Update(keyRune('y'))
	removed := messages(remove)
	require.Len(t, removed, 1)
	m, reload := m.Update(removed[0])

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	failList = errors.New("disk gone")
	m, _ = settle(m, reload)

	assert.NotContains(t, plain(m.View()), savedTitle)
	assert.NotContains(t, plain(m.View()), "disk gone")
}

// launchingModel is a session whose launch account is still connecting, with
// the attempt held back until the test delivers it.
func launchingModel(t *testing.T, o *opener, store saved.Store) (tea.Model, tea.Cmd) {
	t.Helper()
	m, _ := tui.New(tui.Options{
		Icons:    theme.Icons(),
		Accounts: fixtureAccounts(),
		Launch:   "prod",
		Open:     o.open,
		Manage:   managed,
		Saved:    store,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return m, m.Init()
}

func TestTheOverlayUnderTheRenamePromptFollowsTheAccountAway(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, "prod", saved.Query{Name: "alpha", Text: "SELECT 1"})
	o := newOpener(t)
	o.failOpen["prod"] = errors.New("unreachable")
	m, launch := launchingModel(t, o, store)
	m = pressAll(t, openSaved(t, m), keyRune('r'))
	require.Contains(t, plain(m.View()), renameTitle)

	m, _ = settle(m, launch)

	assert.Contains(t, plain(m.View()), renameTitle, "the prompt stays up")
	back := pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(back.View()), "no account connected: ctrl+g to choose one")
	renamed := pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText("beta"), keyMsg(tea.KeyEnter))
	assert.Equal(t, "beta", only(t, store, "prod").Name, "the rename went to the account it was opened for")
	assert.Contains(t, plain(renamed.View()), "no account connected: ctrl+g to choose one")
}
