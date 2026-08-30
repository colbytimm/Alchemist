package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// loadTimeout bounds a single catalog fetch. Bubble Tea commands run detached
// from the command-line context, so each one carries its own deadline.
const loadTimeout = 15 * time.Second

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

func scopeChanged(scope []string) tea.Cmd {
	return func() tea.Msg {
		return ScopeChangedMsg{Scope: scope}
	}
}
