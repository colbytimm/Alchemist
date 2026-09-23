package tui

import (
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Operations named by ErrMsg.Op, and by CatalogChangedMsg.Op for the ones
// that change the catalog.
const (
	OpCatalogRoot     = "catalog root"
	OpCatalogChildren = "catalog children"
	OpHistory         = "history"
	OpExport          = "export"
	OpCreateDatabase  = "create database"
	OpDeleteDatabase  = "delete database"
	OpCreateContainer = "create container"
	OpDeleteContainer = "delete container"
	OpReadThroughput  = "read throughput"
	OpSetThroughput   = "set throughput"
	OpInspect         = "inspect"
	OpSampleFields    = "sample fields"
)

// runID identifies one query run. Every page and failure carries the run it
// came from, so a run the user has already replaced is discarded on arrival
// instead of overwriting the newer one.
type runID int

// dialogID identifies one opened dialog. Every mutation carries the dialog
// that asked for it, so an outcome the user has walked away from is discarded
// on arrival instead of landing on whatever is open now — even when the two
// are the same kind of dialog on different nodes.
type dialogID int

// CatalogLoadedMsg carries the nodes fetched for Parent under the token the
// pane issued. An empty Parent means the top level of the tree.
type CatalogLoadedMsg struct {
	Parent []string
	Nodes  []adapter.Node
	Token  panes.Token
}

// CatalogChangedMsg reports a completed mutation: Parent is the subtree to
// reload, and Target the node the change was about.
type CatalogChangedMsg struct {
	Op     string
	Target []string
	Parent []string
	dialog dialogID
}

// ThroughputReadMsg delivers the capacity the throughput dialog opens on.
type ThroughputReadMsg struct {
	Path       []string
	Throughput adapter.Throughput
}

// DetailsLoadedMsg delivers what the adapter knows about the node at Path.
type DetailsLoadedMsg struct {
	Path    []string
	Details adapter.Details
}

// FieldsSampledMsg delivers what one look at the container at Path found.
type FieldsSampledMsg struct {
	Path   []string
	Sample adapter.FieldSample
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
	Op     string
	Path   []string
	Token  panes.Token
	Err    error
	dialog dialogID
}

// ConnectedMsg delivers the connection the connect screen opened, and the
// name of the profile it belongs to.
type ConnectedMsg struct {
	Connection adapter.Connection
	Profile    string
}

// ConnectFailedMsg reports why the connect screen's attempt did not connect.
type ConnectFailedMsg struct {
	Err error
}

// HistoryLoadedMsg delivers the recorded runs, newest first.
type HistoryLoadedMsg struct {
	Entries []history.Entry
}

// ExportedMsg reports the file a result set was written to, as it was typed.
type ExportedMsg struct {
	Path string
	Rows int
}
