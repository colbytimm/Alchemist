package tui

import "github.com/colbytimm/alchemist/internal/adapter"

// Operations named by ErrMsg.Op.
const (
	OpCatalogRoot     = "catalog root"
	OpCatalogChildren = "catalog children"
)

// runID identifies one query run. Every page and failure carries the run it
// came from, so a run the user has already replaced is discarded on arrival
// instead of overwriting the newer one.
type runID int

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

// PageLoadedMsg delivers the first page of a run, handing the cursor that
// produced it over to the model.
type PageLoadedMsg struct {
	Page   adapter.Page
	cursor adapter.Cursor
	run    runID
}

// PageAppendedMsg delivers a further page of the result set already on
// screen, handing its cursor back to the model.
type PageAppendedMsg struct {
	Page   adapter.Page
	cursor adapter.Cursor
	run    runID
}

// QueryFailedMsg reports a run that produced no page at all.
type QueryFailedMsg struct {
	Err error
	run runID
}

// PageFailedMsg reports a further page that never arrived. The result set
// already on screen outlives it; only the cursor is lost. The command that
// raised either failure has closed whatever cursor it held.
type PageFailedMsg struct {
	Err error
	run runID
}

// ErrMsg reports a failed operation. Path names the catalog node the failure
// belongs to so it can be rendered under that node; it is empty when the
// failure has no place in the tree.
type ErrMsg struct {
	Op   string
	Path []string
	Err  error
}
