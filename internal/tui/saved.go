package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

var (
	errNothingToSave = errors.New("nothing to save: type a query in the editor")
	errNoSavedStore  = errors.New("saved queries are off: no store was configured")
)

// savedScope is the scope a query is saved with: none when its text names
// every container it reads, current otherwise. A draft the planner refuses
// is still worth saving, with current. A batch or an update names its own
// target and never reads the scope, complete or not, so it is asked about
// first: the planner would refuse it.
func savedScope(text string, current []string) []string {
	if query.IsBatch(text) || query.IsMutation(text) {
		return nil
	}
	plan, err := query.BuildPlan(text)
	if err == nil && !plan.NeedsDefaultScope() {
		return nil
	}
	return current
}

// openSavePrompt asks for a name to save the editor's text under, for the
// active account. The name of a query recalled from the saved overlay is
// offered, so updating it is recall, edit, save.
func (m Model) openSavePrompt() (Model, tea.Cmd) {
	if m.accounts.active == "" {
		return m.notify(errNoAccount.Error())
	}
	text := m.editor.Value()
	if strings.TrimSpace(text) == "" {
		return m.notify(errNothingToSave.Error())
	}
	return m.showSavePrompt(panes.SaveDraft{
		Account: m.accounts.active,
		Text:    text,
		Scope:   savedScope(text, m.activeScope()),
		Name:    m.recalledName,
	}), nil
}

// saveHistoryEntry asks for a name to save the entry under the cursor of the
// history overlay, which is back once the prompt closes.
func (m Model) saveHistoryEntry() Model {
	entry, ok := m.historyPane.Selected()
	if !ok || m.accounts.active == "" {
		return m
	}
	return m.showSavePrompt(panes.SaveDraft{
		Account: m.accounts.active,
		Text:    entry.Query,
		Scope:   savedScope(entry.Query, entry.Scope),
	})
}

func (m Model) showSavePrompt(draft panes.SaveDraft) Model {
	m.savePrompt = m.savePrompt.OpenSave(draft)
	m.promptReturn = m.overlay
	m.overlay = overlaySavePrompt
	return m
}

func (m Model) openRename() Model {
	q, ok := m.savedPane.Selected()
	if !ok {
		return m
	}
	m.savePrompt = m.savePrompt.OpenRename(panes.SaveDraft{
		Account: m.accounts.active,
		Text:    q.Text,
		Scope:   q.Scope,
		Name:    q.Name,
	})
	m.promptReturn = overlaySaved
	m.overlay = overlaySavePrompt
	return m
}

// handleSavePromptKey drives the prompt. Typed characters belong to the name,
// which is why q cannot quit here. A write in flight holds the prompt open,
// so its outcome always lands on the prompt that asked for it.
func (m Model) handleSavePromptKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.savePrompt.Saving() {
		return m.handleSavingKey(msg)
	}
	if typesIntoBuffer(msg) {
		return m.savePromptUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = m.promptReturn
		return m, nil
	case key.Matches(msg, m.keys.Save):
		return m.submitSave()
	}
	return m.savePromptUpdate(msg)
}

func (m Model) savePromptUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.savePrompt, cmd = m.savePrompt.Update(msg)
	return m, cmd
}

// submitSave writes to the account the prompt was opened for, whichever
// account is active by now.
func (m Model) submitSave() (Model, tea.Cmd) {
	target, draft := m.savePrompt.Target(), m.savePrompt.Draft()
	m.savePrompt = m.savePrompt.StartSaving()
	if m.savePrompt.Renaming() {
		return m, renameQuery(m.saved, target.Account, draft.Name, target.Name)
	}
	return m, saveQuery(m.saved, target, draft)
}

func (m Model) failSave(err error) Model {
	m.savePrompt = m.savePrompt.Fail(m.explainSaveFailure(err))
	return m
}

// explainSaveFailure words a refusal for the prompt, where the store's own
// wording would say less than the person needs.
func (m Model) explainSaveFailure(err error) error {
	target := m.savePrompt.Target()
	switch {
	case errors.Is(err, saved.ErrExists) && m.savePrompt.Renaming():
		return fmt.Errorf("%q is already saved for %s", target.Name, target.Account)
	case errors.Is(err, saved.ErrExists):
		return fmt.Errorf("%q is already saved for %s: end the name with ! to replace it", target.Name, target.Account)
	case errors.Is(err, saved.ErrInvalidName):
		return fmt.Errorf("%q is not a name that can be saved", target.Name)
	}
	return err
}

// finishSave closes the prompt onto what it was opened over, and lists the
// saved queries afresh when that is their overlay.
func (m Model) finishSave(msg QuerySavedMsg) (Model, tea.Cmd) {
	m.overlay = m.promptReturn
	notice := fmt.Sprintf("saved %q to %s", msg.Name, msg.Account)
	if msg.From != "" {
		notice = fmt.Sprintf("renamed %q to %q", msg.From, msg.Name)
		if strings.EqualFold(m.recalledName, msg.From) {
			m.recalledName = msg.Name
		}
	}
	m.logger.Info(notice, "account", msg.Account)
	m, expiry := m.notify(notice)
	return m, tea.Batch(expiry, m.reloadSavedOnScreen(msg.Name))
}

// openSavedOrRefuse asks for the active account's saved queries. With no
// account there is nothing to ask for, and the overlay says why.
func (m Model) openSavedOrRefuse() (Model, tea.Cmd) {
	if m.accounts.active == "" {
		m.savedPane = m.savedPane.SetAccount("").Fail(errNoAccount)
		m.overlay = overlaySaved
		return m, nil
	}
	return m, loadSaved(m.saved, m.accounts.active)
}

// savedOnScreen reports whether the saved queries overlay is showing, or is
// what the save prompt on screen returns to.
func (m Model) savedOnScreen() bool {
	return m.overlay == overlaySaved || m.overlay == overlaySavePrompt && m.promptReturn == overlaySaved
}

// reloadSavedOnScreen lists the active account's saved queries again for the
// overlay, when it is on screen and there is an account to list, with the
// cursor on selectName when it is listed.
func (m Model) reloadSavedOnScreen(selectName string) tea.Cmd {
	if !m.savedOnScreen() || m.accounts.active == "" {
		return nil
	}
	return reloadSaved(m.saved, m.accounts.active, selectName)
}

// applySaved shows a listing that is still the active account's. The overlay
// opens on the response rather than on the key press, as history's does, and
// only over the main layout: something opened meanwhile keeps the screen.
func (m Model) applySaved(msg SavedLoadedMsg) Model {
	if msg.Account != m.accounts.active {
		return m
	}
	for _, skipped := range msg.Listing.Skipped {
		m.logger.Warn("saved query skipped", "file", skipped.File, "error", skipped.Err)
	}
	switch {
	case m.savedOnScreen():
	case !msg.reload && m.overlay == overlayNone:
		m.savedPane = m.savedPane.SetAccount(msg.Account)
		m.overlay = overlaySaved
	default:
		return m
	}
	m.savedPane = m.savedPane.SetListing(msg.Account, msg.Listing, time.Now())
	if msg.selectName != "" {
		m.savedPane = m.savedPane.Select(msg.selectName)
	}
	return m
}

// failSaved opens the overlay on why the listing a key press asked for could
// not be read, when nothing else has been opened meanwhile.
func (m Model) failSaved(msg ErrMsg) Model {
	if msg.Account != m.accounts.active || m.overlay != overlayNone {
		return m
	}
	m.savedPane = m.savedPane.SetAccount(msg.Account).Fail(msg.Err)
	m.overlay = overlaySaved
	return m
}

// failSavedReload shows why a reload could not be read, in an overlay still
// on screen; a closed one stays closed.
func (m Model) failSavedReload(msg ErrMsg) Model {
	if msg.Account != m.accounts.active || !m.savedOnScreen() {
		return m
	}
	m.savedPane = m.savedPane.Fail(msg.Err)
	return m
}

// handleSavedKey drives the overlay. While the filter line has the keyboard,
// typed characters narrow the list — which is why q, r and d mean nothing
// there, and only the arrow keys move the cursor.
func (m Model) handleSavedKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.savedPane.Deleting() {
		return m.answerDelete(msg)
	}
	if m.savedPane.Filtering() && typesIntoBuffer(msg) {
		return m.savedFilterUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeSaved(), nil
	case key.Matches(msg, m.keys.Saved):
		m.overlay = overlayNone
	case key.Matches(msg, m.keys.Up):
		m.savedPane = m.savedPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.savedPane = m.savedPane.CursorDown()
	case key.Matches(msg, m.keys.Filter):
		m.savedPane = m.savedPane.StartFilter()
	case key.Matches(msg, m.keys.Recall):
		model, _ := m.recallSaved()
		return model, nil
	case key.Matches(msg, m.keys.Rerun):
		return m.rerunSaved()
	case key.Matches(msg, m.keys.Rename):
		return m.openRename(), nil
	case key.Matches(msg, m.keys.DeleteQuery):
		m.savedPane = m.savedPane.AskDelete()
	case m.savedPane.Filtering():
		return m.savedFilterUpdate(msg)
	}
	return m, nil
}

func (m Model) savedFilterUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.savedPane, cmd = m.savedPane.Update(msg)
	return m, cmd
}

func (m Model) closeSaved() Model {
	if m.savedPane.Filtering() {
		m.savedPane = m.savedPane.ClearFilter()
		return m
	}
	m.overlay = overlayNone
	return m
}

// answerDelete deletes on the one key that confirms it; any other key keeps
// the query and is spent on the answer.
func (m Model) answerDelete(msg tea.KeyMsg) (Model, tea.Cmd) {
	m.savedPane = m.savedPane.CancelDelete()
	q, ok := m.savedPane.Selected()
	if !ok || !key.Matches(msg, m.keys.Confirm) {
		return m, nil
	}
	return m, removeQuery(m.saved, m.accounts.active, q.Name)
}

func (m Model) finishRemove(msg QueryRemovedMsg) (Model, tea.Cmd) {
	notice := fmt.Sprintf("deleted %q from %s", msg.Name, msg.Account)
	m.logger.Info(notice)
	if strings.EqualFold(m.recalledName, msg.Name) && msg.Account == m.accounts.active {
		m.recalledName = ""
	}
	m, expiry := m.notify(notice)
	return m, tea.Batch(expiry, m.reloadSavedOnScreen(""))
}

// recallSaved mirrors recall: the text goes to the editor, and the scope is
// restored only when one was saved, since a query without one names its own
// containers.
func (m Model) recallSaved() (Model, bool) {
	q, ok := m.savedPane.Selected()
	if !ok {
		return m, false
	}
	m.editor = m.editor.SetValue(q.Text)
	if len(q.Scope) > 0 {
		m = m.setScope(m.accounts.active, q.Scope)
	}
	m.recalledName = q.Name
	m.overlay = overlayNone
	return m.setFocus(focusEditor), true
}

func (m Model) rerunSaved() (Model, tea.Cmd) {
	recalled, ok := m.recallSaved()
	if !ok {
		return m, nil
	}
	return recalled.startRun()
}

// followActiveAccount is setActive's hook: the saved queries shown, and the
// name a recall offers, belong to the account being left.
func (m Model) followActiveAccount(account string) (Model, tea.Cmd) {
	m.recalledName = ""
	m.savedPane = m.savedPane.SetAccount(account)
	switch {
	case !m.savedOnScreen():
		return m, nil
	case account == "":
		m.savedPane = m.savedPane.Fail(errNoAccount)
		return m, nil
	}
	return m, m.reloadSavedOnScreen("")
}

func (m Model) notify(notice string) (Model, tea.Cmd) {
	var expiry tea.Cmd
	m.statusBar, expiry = m.statusBar.SetNotice(notice)
	return m, expiry
}

func loadSaved(store saved.Store, account string) tea.Cmd {
	return func() tea.Msg {
		listing, err := store.List(account)
		if err != nil {
			return ErrMsg{Account: account, Op: OpSavedList, Err: err}
		}
		return SavedLoadedMsg{Account: account, Listing: listing}
	}
}

func reloadSaved(store saved.Store, account, selectName string) tea.Cmd {
	return func() tea.Msg {
		listing, err := store.List(account)
		if err != nil {
			return ErrMsg{Account: account, Op: OpSavedReload, Err: err}
		}
		return SavedLoadedMsg{Account: account, Listing: listing, reload: true, selectName: selectName}
	}
}

func saveQuery(store saved.Store, target panes.SaveTarget, draft panes.SaveDraft) tea.Cmd {
	write := store.Create
	if target.Replace {
		write = store.Replace
	}
	q := saved.Query{Name: target.Name, Text: draft.Text, Scope: draft.Scope}
	return func() tea.Msg {
		if err := write(target.Account, q); err != nil {
			return ErrMsg{Account: target.Account, Op: OpSaveQuery, Err: err}
		}
		return QuerySavedMsg{Account: target.Account, Name: target.Name}
	}
}

func renameQuery(store saved.Store, account, from, to string) tea.Cmd {
	return func() tea.Msg {
		if err := store.Rename(account, from, to); err != nil {
			return ErrMsg{Account: account, Op: OpSaveQuery, Err: err}
		}
		return QuerySavedMsg{Account: account, Name: to, From: from}
	}
}

func removeQuery(store saved.Store, account, name string) tea.Cmd {
	return func() tea.Msg {
		if err := store.Remove(account, name); err != nil {
			return ErrMsg{Account: account, Op: OpRemoveQuery, Err: err}
		}
		return QueryRemovedMsg{Account: account, Name: name}
	}
}
