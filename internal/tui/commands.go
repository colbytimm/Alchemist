package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Bubble Tea commands run detached from the command-line context, so each one
// carries its own deadline.
const (
	connectTimeout = 30 * time.Second
	loadTimeout    = 15 * time.Second
	queryTimeout   = 60 * time.Second
	manageTimeout  = 30 * time.Second
)

// recentHistory is how much of the log the history overlay lists.
const recentHistory = 500

// openConnection hands the form to the connector the session was built with.
func (m Model) openConnection(form panes.ConnectForm) tea.Cmd {
	connect := m.connect
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		conn, err := connect(ctx, form)
		if err != nil {
			return ConnectFailedMsg{Err: err}
		}
		return ConnectedMsg{Connection: conn, Profile: form.Profile}
	}
}

func (m Model) loadRoot(token panes.Token) tea.Cmd {
	catalog := m.catalog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		nodes, err := catalog.Root(ctx)
		if err != nil {
			return ErrMsg{Op: OpCatalogRoot, Token: token, Err: err}
		}
		return CatalogLoadedMsg{Nodes: nodes, Token: token}
	}
}

func (m Model) loadChildren(node adapter.Node, token panes.Token) tea.Cmd {
	catalog := m.catalog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		nodes, err := catalog.Children(ctx, node)
		if err != nil {
			return ErrMsg{Op: OpCatalogChildren, Path: node.Path, Token: token, Err: err}
		}
		return CatalogLoadedMsg{Parent: node.Path, Nodes: nodes, Token: token}
	}
}

// manage runs one management call under a deadline of its own, so a slow
// service never blocks Update, and reports the change or why it did not
// happen back to the dialog that asked. The service's own words are passed on
// untouched.
func manage(dialog dialogID, change CatalogChangedMsg, call func(context.Context) error) tea.Cmd {
	change.dialog = dialog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), manageTimeout)
		defer cancel()
		if err := call(ctx); err != nil {
			return ErrMsg{Op: change.Op, dialog: dialog, Err: err}
		}
		return change
	}
}

func (m Model) createDatabase(spec adapter.DatabaseSpec) tea.Cmd {
	admin := m.management.Admin
	return manage(m.dialog,
		CatalogChangedMsg{Op: OpCreateDatabase, Target: []string{spec.Name}},
		func(ctx context.Context) error { return admin.CreateDatabase(ctx, spec) },
	)
}

func (m Model) deleteDatabase(name string) tea.Cmd {
	admin := m.management.Admin
	return manage(m.dialog,
		CatalogChangedMsg{Op: OpDeleteDatabase, Target: []string{name}},
		func(ctx context.Context) error { return admin.DeleteDatabase(ctx, name) },
	)
}

func (m Model) createContainer(spec adapter.ContainerSpec) tea.Cmd {
	admin := m.management.Admin
	return manage(m.dialog,
		CatalogChangedMsg{
			Op:     OpCreateContainer,
			Target: []string{spec.Database, spec.Name},
			Parent: []string{spec.Database},
		},
		func(ctx context.Context) error { return admin.CreateContainer(ctx, spec) },
	)
}

func (m Model) deleteContainer(path []string) tea.Cmd {
	admin := m.management.Admin
	return manage(m.dialog,
		CatalogChangedMsg{Op: OpDeleteContainer, Target: path, Parent: path[:1]},
		func(ctx context.Context) error { return admin.DeleteContainer(ctx, path) },
	)
}

func (m Model) setThroughput(path []string, t adapter.Throughput) tea.Cmd {
	editor := m.management.Throughput
	return manage(m.dialog,
		CatalogChangedMsg{Op: OpSetThroughput, Target: path},
		func(ctx context.Context) error { return editor.SetThroughput(ctx, path, t) },
	)
}

func (m Model) readThroughput(path []string) tea.Cmd {
	editor := m.management.Throughput
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), manageTimeout)
		defer cancel()
		current, err := editor.Throughput(ctx, path)
		if err != nil {
			return ErrMsg{Op: OpReadThroughput, Path: path, Err: err}
		}
		return ThroughputReadMsg{Path: path, Throughput: current}
	}
}

func (m Model) inspect(node adapter.Node) tea.Cmd {
	inspector := m.management.Inspector
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		details, err := inspector.Inspect(ctx, node)
		if err != nil {
			return ErrMsg{Op: OpInspect, Path: node.Path, Err: err}
		}
		return DetailsLoadedMsg{Path: node.Path, Details: details}
	}
}

// runPlan opens a cursor and fetches its first page. ctx belongs to the
// model, which cancels it when a newer run supersedes this one.
func (m Model) runPlan(ctx context.Context, plan query.Plan) tea.Cmd {
	run, logger := m.run, m.logger
	engine := query.Engine{Connection: m.connection, MaxJoinRows: m.maxJoinRows}
	return func() tea.Msg {
		cursor, err := engine.Execute(ctx, plan)
		if err != nil {
			return QueryFailedMsg{run: run, Err: err}
		}
		page, err := cursor.NextPage(ctx)
		if err != nil {
			closeCursor(logger, cursor)
			return QueryFailedMsg{run: run, Err: err}
		}
		return PageLoadedMsg{run: run, cursor: cursor, Page: page}
	}
}

// fetchPage reads the next page of an open cursor. The command owns that
// cursor until it hands it back, which is what keeps the model from closing
// one another goroutine is still reading.
func (m Model) fetchPage(ctx context.Context, cursor adapter.Cursor) tea.Cmd {
	run, logger := m.run, m.logger
	return func() tea.Msg {
		page, err := cursor.NextPage(ctx)
		if err != nil {
			closeCursor(logger, cursor)
			return PageFailedMsg{run: run, Err: err}
		}
		return PageAppendedMsg{run: run, cursor: cursor, Page: page}
	}
}

func (m Model) loadHistory() tea.Cmd {
	store := m.history
	return func() tea.Msg {
		entries, err := store.Recent(recentHistory)
		if err != nil {
			return ErrMsg{Op: OpHistory, Err: err}
		}
		return HistoryLoadedMsg{Entries: entries}
	}
}

// record appends entry off the main goroutine. A log that refuses the
// write is worth a warning and nothing more: the run it describes is on
// screen regardless.
func (m Model) record(entry history.Entry) tea.Cmd {
	store, logger := m.history, m.logger
	return func() tea.Msg {
		if err := store.Append(entry); err != nil {
			logger.Warn("query not recorded", "error", err)
		}
		return nil
	}
}

func scopeChanged(scope []string) tea.Cmd {
	return func() tea.Msg {
		return ScopeChangedMsg{Scope: scope}
	}
}

// closeCursor releases a cursor whose pages nothing will read. A failure
// there is worth a log line and nothing more: the run it belonged to is over.
func closeCursor(logger *log.Logger, cursor adapter.Cursor) {
	if cursor == nil {
		return
	}
	if err := cursor.Close(); err != nil {
		logger.Error("close cursor", "error", err)
	}
}
