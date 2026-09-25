package tui

import (
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Operations named by ErrMsg.Op, and by CatalogChangedMsg.Op for the ones
// that change the catalog.
const (
	OpConnect         = "connect"
	OpListAccounts    = "list accounts"
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
	OpSavedList       = "saved queries"
	OpSavedReload     = "reload saved queries"
	OpSaveQuery       = "save query"
	OpRemoveQuery     = "remove saved query"
	OpClone           = "clone"
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

// CatalogLoadedMsg carries the nodes fetched for Parent in Account's tree,
// under the token that tree issued. An empty Parent means the top level.
type CatalogLoadedMsg struct {
	Account string
	Parent  []string
	Nodes   []adapter.Node
	Token   panes.Token
}

// CatalogChangedMsg reports a completed mutation in Account: Parent is the
// subtree to reload, and Target the node the change was about.
type CatalogChangedMsg struct {
	Account string
	Op      string
	Target  []string
	Parent  []string
	dialog  dialogID
}

// ThroughputReadMsg delivers the capacity the throughput dialog opens on.
type ThroughputReadMsg struct {
	Account    string
	Path       []string
	Throughput adapter.Throughput
}

// DetailsLoadedMsg delivers what the adapter knows about the node at Path in
// Account.
type DetailsLoadedMsg struct {
	Account string
	Path    []string
	Details adapter.Details
	// attempt is the connection of Account the details were read on.
	attempt int
}

// FieldsSampledMsg delivers what one look at the container at Path in
// Account found.
type FieldsSampledMsg struct {
	Account string
	Path    []string
	Sample  adapter.FieldSample
}

// ScopeChangedMsg announces the container queries on Account should target
// by default.
type ScopeChangedMsg struct {
	Account string
	Scope   []string
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

// BatchDoneMsg reports a batch the backend answered, committed or rolled
// back, on Account. Unlike a late page it is never dropped: it says what
// became of a write.
type BatchDoneMsg struct {
	Account string
	Batch   adapter.Batch
	Result  adapter.BatchResult
}

// BatchFailedMsg reports a batch with no answer from the backend on Account:
// refused whole before it could apply, or sent and never answered. Like
// BatchDoneMsg it is never dropped.
type BatchFailedMsg struct {
	Account string
	Batch   adapter.Batch
	Err     error
}

// ErrMsg reports a failed operation on Account. Path names the catalog node
// the failure belongs to so it can be rendered under that node; it is empty
// when the failure has no place in the tree.
type ErrMsg struct {
	Account string
	Op      string
	Path    []string
	Token   panes.Token
	Err     error
	dialog  dialogID
	// attempt is the number of a failed attempt to connect Account.
	attempt int
}

type AccountConnectedMsg struct {
	Account    string
	Connection adapter.Connection
	// attempt is the number of the attempt that connected it.
	attempt int
	// saved is the account a connect form connected, as its profile was
	// saved.
	saved Account
}

// AccountsListedMsg carries the profiles as they stand now.
type AccountsListedMsg struct {
	Accounts []Account
}

// ConnectFailedMsg reports why a connect form's attempt did not connect.
type ConnectFailedMsg struct {
	Err     error
	account string
	attempt int
}

// HistoryLoadedMsg delivers the runs recorded on Account, newest first.
type HistoryLoadedMsg struct {
	Account string
	Entries []history.Entry
}

// ExportedMsg reports the file a result set was written to, as it was typed.
type ExportedMsg struct {
	Path string
	Rows int
}

// SavedLoadedMsg delivers the saved queries of Account.
type SavedLoadedMsg struct {
	Account string
	Listing saved.Listing
	// reload marks a listing for an overlay already open, which it must not
	// reopen once closed; selectName is the query to put the cursor on.
	reload     bool
	selectName string
}

// QuerySavedMsg reports a create, a replace, or a rename, from From when
// that is not empty.
type QuerySavedMsg struct {
	Account string
	Name    string
	From    string
}

type QueryRemovedMsg struct {
	Account string
	Name    string
}

// ClonePreparedMsg delivers what the source of the clone prompt holds, or
// why it could not be read.
type ClonePreparedMsg struct {
	Survey clone.Survey
	Err    error
	dialog dialogID
}

// ClonePlannedMsg delivers the plan the clone review restates, or why the
// target cannot take it.
type ClonePlannedMsg struct {
	Plan   clone.Plan
	Err    error
	dialog dialogID
}

// cloneCreated is what a clone step has created on the target.
type cloneCreated struct {
	database  bool
	container bool
}

// CloneTargetCreatedMsg reports the container a clone step created, or
// reopened to resume, and hands over the copy that fills it; Copy is nil
// when only the definition is copied.
type CloneTargetCreatedMsg struct {
	Copy    *clone.Copy
	created cloneCreated
	job     jobID
}

// ClonePageCopiedMsg reports one page written, and hands the copy back.
type ClonePageCopiedMsg struct {
	Progress clone.Progress
	copy     *clone.Copy
	job      jobID
}

// CloneFailedMsg reports a clone step that ended short: stopped, refused,
// or out of time. Progress is what it spent; copy, when set, is handed back
// to be closed.
type CloneFailedMsg struct {
	Err      error
	Progress clone.Progress
	copy     *clone.Copy
	created  cloneCreated
	job      jobID
}
