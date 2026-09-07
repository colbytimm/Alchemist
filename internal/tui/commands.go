package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Bubble Tea commands run detached from the command-line context, so each one
// carries its own deadline.
const (
	connectTimeout = 30 * time.Second
	loadTimeout    = 15 * time.Second
	queryTimeout   = 60 * time.Second
)

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

func (m Model) loadRoot() tea.Cmd {
	catalog := m.catalog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		nodes, err := catalog.Root(ctx)
		if err != nil {
			return ErrMsg{Op: OpCatalogRoot, Err: err}
		}
		return CatalogLoadedMsg{Nodes: nodes}
	}
}

func (m Model) loadChildren(node adapter.Node) tea.Cmd {
	catalog := m.catalog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		nodes, err := catalog.Children(ctx, node)
		if err != nil {
			return ErrMsg{Op: OpCatalogChildren, Path: node.Path, Err: err}
		}
		return CatalogLoadedMsg{Parent: node.Path, Nodes: nodes}
	}
}

// runQuery opens a cursor and fetches its first page. ctx belongs to the
// model, which cancels it when a newer run supersedes this one.
func (m Model) runQuery(ctx context.Context, q adapter.Query) tea.Cmd {
	run, connection, logger := m.run, m.connection, m.logger
	return func() tea.Msg {
		cursor, err := connection.Query(ctx, q)
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
