package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
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
	model, refresh := m.refreshSuggestions()
	return model, tea.Batch(cmd, refresh)
}

// cursorEndsName reports whether the cursor sits at the end of an identifier
// or a dot. A number is neither: nothing completes it, and a list opened
// after one would glue a keyword to it.
func (m Model) cursorEndsName() bool {
	text, offset := m.editor.Cursor()
	if offset == 0 {
		return false
	}
	if text[offset-1] == '.' {
		return true
	}
	start := offset
	for start > 0 && isIdentifierPart(text[start-1]) {
		start--
	}
	return start < offset && isIdentifierStart(text[start])
}

func isIdentifierStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || c >= '0' && c <= '9'
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
	return m.refreshSuggestions()
}

// refreshSuggestions recomputes the list for the cursor, asking for the
// containers or the sample it still lacks. A dismissal holds until the
// cursor is on another token, then is forgotten.
func (m Model) refreshSuggestions() (Model, tea.Cmd) {
	if !m.completing || m.focus != focusEditor {
		return m, nil
	}
	text, offset := m.editor.Cursor()
	c := query.Context(text, offset).WithDefaultScope(m.scope)
	if m.dismissed.covers(c) {
		return m.closeSuggestions(), nil
	}
	m.dismissed = dismissal{}
	m.completion = c
	model, cmd := m.fetchFor(c)
	model.editor = model.editor.SetSuggestions(model.index.Suggest(c), model.note(c))
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

// fetchFor issues what the context needs and the session lacks: the
// containers of a database the tree has not listed, through the tree's own
// request so the two cannot disagree, and one sample per container the
// first time a field of it is completed.
func (m Model) fetchFor(c query.Completion) (Model, tea.Cmd) {
	switch c.Kind {
	case query.CompleteContainer:
		if m.index.HasContainers(c.Database) {
			return m, nil
		}
		pane, fetch, tick := m.catalogPane.LoadPath([]string{c.Database})
		m.catalogPane = pane
		if !fetch.Needed {
			return m, tick
		}
		return m, tea.Batch(tick, m.load(fetch))
	case query.CompleteField:
		return m.sampleFor(c.Aliases)
	}
	return m, nil
}

func (m Model) sampleFor(aliases []query.Alias) (Model, tea.Cmd) {
	if !m.sampleFields || m.management.Sampler == nil {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, alias := range aliases {
		for _, scope := range alias.Scopes {
			key := scopeKey(scope)
			if m.samples[key] != sampleUnrequested {
				continue
			}
			m.samples[key] = samplePending
			cmds = append(cmds, m.sampleContainer(adapter.Node{Kind: adapter.NodeContainer, Name: scope[len(scope)-1], Path: scope}))
		}
	}
	return m, tea.Batch(cmds...)
}

// note is what the hint line says while something the list needs is still
// on its way.
func (m Model) note(c query.Completion) string {
	switch c.Kind {
	case query.CompleteContainer:
		if m.catalogPane.Loading([]string{c.Database}) {
			return "loading " + c.Database + "…"
		}
	case query.CompleteField:
		for _, alias := range c.Aliases {
			for _, scope := range alias.Scopes {
				if m.samples[scopeKey(scope)] == samplePending {
					return "sampling " + scope[len(scope)-1] + "…"
				}
			}
		}
	}
	return ""
}

// fileSample keeps what a sample found, unless the container was dropped
// from the catalog while the sample was out: its fields would describe a
// container that no longer exists, or a new one of the same name.
func (m Model) fileSample(msg FieldsSampledMsg) (Model, tea.Cmd) {
	key := scopeKey(msg.Path)
	if m.samples[key] != samplePending {
		return m, nil
	}
	m.samples[key] = sampleDone
	m.index.AddFields(msg.Path, msg.Sample.Fields)
	m.logger.Info("sampled fields", "container", strings.Join(msg.Path, "."),
		"fields", len(msg.Sample.Fields), "ru", fmt.Sprintf("%.2f", msg.Sample.Stats.RequestCharge))
	return m.refreshSuggestions()
}

func (m Model) failSample(path []string) (Model, tea.Cmd) {
	m.samples[scopeKey(path)] = sampleFailed
	return m.refreshSuggestions()
}

// fileCatalog feeds the index what the tree just accepted, forgets the
// samples of containers that went with it, then settles a list that was
// waiting for it.
func (m Model) fileCatalog(msg CatalogLoadedMsg) (Model, tea.Cmd) {
	var dropped [][]string
	switch len(msg.Parent) {
	case 0:
		names := make([]string, 0, len(msg.Nodes))
		for _, node := range msg.Nodes {
			names = append(names, node.Name)
		}
		dropped = m.index.SetDatabases(names)
	case 1:
		dropped = m.index.SetContainers(msg.Parent[0], msg.Nodes)
	}
	for _, container := range dropped {
		delete(m.samples, scopeKey(container))
	}
	return m.refreshSuggestions()
}

// observePage files the fields of a page's items under the containers they
// came from. Only items as stored are fields of a container: a projection
// names what the query made of them. A join side is the exception, since a
// projected half still holds top-level fields of its container, apart from
// one the SELECT list renamed.
func (m Model) observePage(page adapter.Page) {
	for side, items := range m.plan.LeafItems(page.Raw) {
		leaf := m.plan.Leaves[side]
		if m.plan.Merge != query.HashJoin && !leaf.WholeItems() {
			continue
		}
		fields := adapter.FlattenFields(items)
		for _, column := range m.plan.Join.Columns {
			if column.Side == side && column.As != "" {
				fields = slices.DeleteFunc(fields, func(f adapter.Field) bool { return f.Path == column.As })
			}
		}
		m.index.AddFields(leaf.Query.Scope, fields)
	}
}

func scopeKey(scope []string) string {
	return strings.Join(scope, "\x00")
}
