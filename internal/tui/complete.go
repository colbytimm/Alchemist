package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// sampleState is how far one container's field sample has got. A sample
// that failed is never asked for again in the session.
type sampleState int

const (
	sampleUnrequested sampleState = iota
	samplePending
	sampleDone
	sampleFailed
)

// dismissal is the token a closed list was about: the list stays closed
// while the cursor is still on that token, and no longer.
type dismissal struct {
	active bool
	start  int
	word   string
}

// covers reports whether c is still the dismissed token: at the same start,
// with the word typed so far on the way to or back from what it was.
func (d dismissal) covers(c query.Completion) bool {
	if !d.active || c.Start != d.start {
		return false
	}
	return strings.HasPrefix(c.Word, d.word) || strings.HasPrefix(d.word, c.Word)
}

// editorEdit hands msg to the buffer and settles the list after it: typing
// an identifier or a dot opens or narrows it, any other key closes it, and
// a key that types nothing only ever updates a list already open.
func (m Model) editorEdit(msg tea.KeyMsg) (Model, tea.Cmd) {
	m, cmd := m.editorUpdate(msg)
	if !typesIntoBuffer(msg) && !m.completing {
		return m, cmd
	}
	if !m.cursorEndsName() {
		return m.closeSuggestions(), cmd
	}
	m.completing = true
	model, refresh := m.suggest()
	return model, tea.Batch(cmd, refresh)
}

// cursorEndsName reports whether the cursor sits at the end of an identifier
// or a dot. A number is neither: nothing completes it, and a list opened
// after one would glue a keyword to it.
func (m Model) cursorEndsName() bool {
	text, offset := m.editor.Cursor()
	return query.EndsName(text[:offset])
}

// handleSuggestionKey answers the keys the open list owns, and reports
// false for every other key, which keeps its usual meaning.
func (m Model) handleSuggestionKey(msg tea.KeyMsg) (Model, bool) {
	switch {
	case key.Matches(msg, m.keys.Accept):
		return m.acceptSuggestion(), true
	case key.Matches(msg, m.keys.Up):
		return m.chooseSuggestion(m.editor.PrevSuggestion()), true
	case key.Matches(msg, m.keys.Down):
		return m.chooseSuggestion(m.editor.NextSuggestion()), true
	case key.Matches(msg, m.keys.Close):
		return m.dismissSuggestions(), true
	}
	return m, false
}

func (m Model) chooseSuggestion(editor panes.Editor) Model {
	m.editor = editor
	return m
}

// openSuggestions is the explicit request: it overrides a dismissal and
// opens on an empty prefix.
func (m Model) openSuggestions() (Model, tea.Cmd) {
	m.dismissed = dismissal{}
	m.completing = true
	return m.suggest()
}

// suggest computes the list for what the user just typed, selecting the
// best match.
func (m Model) suggest() (Model, tea.Cmd) {
	return m.settleSuggestions(panes.Editor.SetSuggestions)
}

// refreshSuggestions recomputes the list after something arrived for it,
// leaving a row the user chose chosen.
func (m Model) refreshSuggestions() (Model, tea.Cmd) {
	return m.settleSuggestions(panes.Editor.RefreshSuggestions)
}

// settleSuggestions computes the list for the cursor, asks for the
// containers or the sample it still lacks, and shows it through show. A
// dismissal holds until the cursor is on another token, then is forgotten.
func (m Model) settleSuggestions(show func(panes.Editor, []complete.Suggestion, string) panes.Editor) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(m.accounts.active)
	if !m.completing || m.focus != focusEditor || !ok {
		return m, nil
	}
	c := m.editor.Context().WithDefaultScope(entry.scope)
	if m.dismissed.covers(c) {
		return m.closeSuggestions(), nil
	}
	m.dismissed = dismissal{}
	m.completion = c
	model, cmd := m.fetchFor(c)
	model.editor = show(model.editor, entry.index.Suggest(c), model.note(c))
	return model, cmd
}

func (m Model) acceptSuggestion() Model {
	suggestion, ok := m.editor.Selected()
	if !ok {
		return m
	}
	start := m.completion.Start
	text, _ := m.editor.Cursor()
	if suggestion.Bracketed && start > 0 && text[start-1] == '.' {
		start--
	}
	m.editor = m.editor.Replace(start, m.completion.End, suggestion.Insert)
	return m.closeSuggestions()
}

func (m Model) dismissSuggestions() Model {
	m = m.closeSuggestions()
	m.dismissed = dismissal{active: true, start: m.completion.Start, word: m.completion.Word}
	return m
}

func (m Model) closeSuggestions() Model {
	m.completing = false
	m.editor = m.editor.ClearSuggestions()
	return m
}

// fetchFor issues what the context needs and the active account lacks: the
// containers of a database its tree has not listed, through the tree's own
// request so the two cannot disagree, and one sample per container the
// first time a field of it is completed.
func (m Model) fetchFor(c query.Completion) (Model, tea.Cmd) {
	entry, ok := m.activeConnection()
	if !ok {
		return m, nil
	}
	switch c.Kind {
	case query.CompleteContainer:
		if entry.index.HasContainers(c.Database) {
			return m, nil
		}
		pane, fetch, tick := entry.pane.LoadPath([]string{c.Database})
		entry.pane = pane
		m.accounts.put(entry)
		if !fetch.Needed {
			return m, tick
		}
		return m, tea.Batch(tick, m.load(entry, fetch))
	case query.CompleteField:
		return m, m.sampleFor(entry, c.Aliases)
	}
	return m, nil
}

// sampleFor asks for the fields of every container the aliases range over
// that the account has not sampled yet. The samples map is shared with the
// account's entry, so marking one pending here is seen there.
func (m Model) sampleFor(entry accountEntry, aliases []query.Alias) tea.Cmd {
	if !m.sampleFields || !entry.account.SampleFields || entry.management.Sampler == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, alias := range aliases {
		for _, scope := range alias.Scopes {
			key := scopeKey(scope)
			if entry.samples[key] != sampleUnrequested {
				continue
			}
			entry.samples[key] = samplePending
			cmds = append(cmds, sampleContainer(entry, adapter.Node{Kind: adapter.NodeContainer, Name: scope[len(scope)-1], Path: scope}))
		}
	}
	return tea.Batch(cmds...)
}

// note is what the hint line says while something the list needs is still
// on its way.
func (m Model) note(c query.Completion) string {
	switch c.Kind {
	case query.CompleteContainer:
		if m.catalogPane().Loading([]string{c.Database}) {
			return "loading " + c.Database + "…"
		}
	case query.CompleteField:
		entry, _ := m.accounts.get(m.accounts.active)
		for _, alias := range c.Aliases {
			for _, scope := range alias.Scopes {
				if entry.samples[scopeKey(scope)] == samplePending {
					return "sampling " + scope[len(scope)-1] + "…"
				}
			}
		}
	}
	return ""
}

// fileSample keeps what a sample found in the account it was taken on,
// unless the container was dropped from the catalog while the sample was out,
// or the account disconnected: either way nothing is pending for it any more,
// since a reconnect starts the account's samples over.
func (m Model) fileSample(msg FieldsSampledMsg) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(msg.Account)
	key := scopeKey(msg.Path)
	if !ok || entry.samples[key] != samplePending {
		return m, nil
	}
	entry.samples[key] = sampleDone
	entry.index.AddFields(msg.Path, msg.Sample.Fields)
	m.logger.Info("sampled fields", "account", msg.Account, "container", strings.Join(msg.Path, "."),
		"fields", len(msg.Sample.Fields), "ru", fmt.Sprintf("%.2f", msg.Sample.Stats.RequestCharge))
	return m.refreshSuggestions()
}

func (m Model) failSample(msg ErrMsg) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(msg.Account)
	key := scopeKey(msg.Path)
	if !ok || entry.samples[key] != samplePending {
		return m, nil
	}
	entry.samples[key] = sampleFailed
	return m.refreshSuggestions()
}

// fileCatalog feeds entry's index what its tree just accepted, forgets the
// samples of containers that went with it, then settles a list that was
// waiting for it.
func (m Model) fileCatalog(entry accountEntry, msg CatalogLoadedMsg) (Model, tea.Cmd) {
	var dropped [][]string
	switch len(msg.Parent) {
	case 0:
		names := make([]string, 0, len(msg.Nodes))
		for _, node := range msg.Nodes {
			names = append(names, node.Name)
		}
		dropped = entry.index.SetDatabases(names)
	case 1:
		dropped = entry.index.SetContainers(msg.Parent[0], msg.Nodes)
	}
	for _, container := range dropped {
		delete(entry.samples, scopeKey(container))
	}
	return m.refreshSuggestions()
}

// observePage files the fields of a page's items under the containers they
// came from.
func (m Model) observePage(page adapter.Page) {
	entry, ok := m.accounts.get(m.runAccount)
	if !ok || !entry.connected() {
		return
	}
	for leaf, fields := range m.plan.ObservedFields(page.Raw) {
		entry.index.AddFields(m.plan.Leaves[leaf].Query.Scope, fields)
	}
}

func scopeKey(scope []string) string {
	return strings.Join(scope, "\x00")
}
