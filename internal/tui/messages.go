package tui

import "github.com/colbytimm/alchemist/internal/adapter"

// Operations named by ErrMsg.Op.
const (
	OpCatalogRoot     = "catalog root"
	OpCatalogChildren = "catalog children"
)

// CatalogLoadedMsg carries the nodes fetched for Parent. An empty Parent means
// the top level of the tree.
type CatalogLoadedMsg struct {
	Parent []string
	Nodes  []adapter.Node
}

// ScopeChangedMsg announces the container queries should target by default.
type ScopeChangedMsg struct {
	Scope []string
}

// ErrMsg reports a failed operation. Path names the catalog node the failure
// belongs to so it can be rendered under that node; it is empty when the
// failure has no place in the tree.
type ErrMsg struct {
	Op   string
	Path []string
	Err  error
}
