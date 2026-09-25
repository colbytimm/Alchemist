package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// maxRecordedBatch is how much of a batch's text history keeps. A longer
// one is cut at an operation boundary, which drops its COMMIT, so recalling
// it can never run it by reflex.
const maxRecordedBatch = 64 * 1024

var (
	errReadOnly       = errors.New("read-only")
	errNoBatchSupport = errors.New("this adapter has no transactions")
	errBatchInFlight  = errors.New("a batch is committing: wait for its outcome")
	errNotADocument   = errors.New("only a whole item of one container, from SELECT *, can be written back")
)

// readOnlyError says how to lift the refusal as well as what it was.
func readOnlyError(account string) error {
	return fmt.Errorf("%s is %w, so nothing was sent. To allow writes on this account:\n  alchemist profile set-read-only %s false",
		account, errReadOnly, account)
}

// batchRefusalError is a batch that failed a check made before sending, with
// every problem found.
type batchRefusalError struct {
	problems []string
}

func (e *batchRefusalError) Error() string {
	return "Batch refused, nothing was sent:\n  " + strings.Join(e.problems, "\n  ")
}

// batchRequest is a batch between ctrl+r and its review: the account it
// belongs to from the moment the key was pressed, and the text it came from.
type batchRequest struct {
	account string
	text    string
	batch   adapter.Batch
}

// pendingBatch is a batch waiting for the tree to list its target's
// database, since only the container's node knows its key paths.
type pendingBatch struct {
	request  batchRequest
	database []string
	waiting  bool
}

// startBatch validates the batch in the editor and opens its review. Nothing
// is sent from here: the one way to send a batch that writes is the
// review's enter, with the container's name typed.
func (m Model) startBatch() (Model, tea.Cmd) {
	m = m.closeSuggestions()
	m.pendingBatch = pendingBatch{}
	entry, connected := m.activeConnection()
	if !connected {
		m = m.beginRun(m.accounts.active)
		return m.showFailure(m.whyNoConnection(), runFailed)
	}
	request := batchRequest{account: entry.account.Name, text: m.editor.Value()}
	b, err := query.ParseBatch(request.text)
	if err != nil {
		return m.refuseBatch(request, fmt.Errorf("the batch does not parse, so nothing was sent: %w", err))
	}
	request.batch = b
	if m.job.writesTo(request.account, b.Scope) {
		return m.refuseBatch(request, fmt.Errorf("a %s is writing %s: the batch waits for it (y)", m.job.kind, strings.Join(b.Scope, ".")))
	}
	if _, err := m.batcher(entry, b); err != nil {
		return m.refuseBatch(request, err)
	}
	return m.checkBatch(request)
}

// batcher hands out the Batcher that runs b on entry's account. It is the
// one place a batch reaches a backend from, and so where read-only is
// enforced: a batch that only reads is the one a read-only account may run.
func (m Model) batcher(entry accountEntry, b adapter.Batch) (adapter.Batcher, error) {
	if entry.management.Batcher == nil {
		return nil, errNoBatchSupport
	}
	if entry.account.ReadOnly && writes(b) {
		return nil, readOnlyError(entry.account.Name)
	}
	return entry.management.Batcher, nil
}

func writes(b adapter.Batch) bool {
	return slices.ContainsFunc(b.Operations, func(op adapter.Operation) bool { return op.Kind.Writes() })
}

// checkBatch validates request against its container's key paths, asking
// the tree for them first when it has not listed the container's database.
func (m Model) checkBatch(request batchRequest) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(request.account)
	if !ok || !entry.connected() {
		return m.refuseBatch(request, errRunAbandoned)
	}
	scope := request.batch.Scope
	if node, ok := entry.pane.Node(scope); ok {
		return m.reviewBatch(request, keyPaths(node))
	}
	database := scope[:1]
	if entry.pane.Listed(database) {
		return m.refuseBatch(request, noSuchContainer(scope))
	}
	pane, fetch, tick := entry.pane.LoadPath(database)
	entry.pane = pane
	m.accounts.put(entry)
	if !fetch.Needed && !pane.Loading(database) {
		return m.refuseBatch(request, noSuchContainer(scope))
	}
	m.pendingBatch = pendingBatch{request: request, database: database, waiting: true}
	if !fetch.Needed {
		return m, tick
	}
	return m, tea.Batch(tick, m.load(entry, fetch))
}

func noSuchContainer(scope []string) error {
	return fmt.Errorf("%s: no such container (r in the catalog reloads it)", strings.Join(scope, "."))
}

func keyPaths(container adapter.Node) []string {
	joined := container.Meta[adapter.MetaPartitionKey]
	if joined == "" {
		return nil
	}
	return strings.Split(joined, adapter.PartitionKeyPathSeparator)
}

// resumeBatch validates the batch that waited for parent's listing in
// account, now that it has landed.
func (m Model) resumeBatch(account string, parent []string) (Model, tea.Cmd) {
	wait := m.pendingBatch
	if !wait.waiting || wait.request.account != account || !slices.Equal(wait.database, parent) {
		return m, nil
	}
	m.pendingBatch = pendingBatch{}
	return m.checkBatch(wait.request)
}

// failPendingBatch refuses the batch that waited for a listing that failed.
func (m Model) failPendingBatch(msg ErrMsg) (Model, tea.Cmd) {
	wait := m.pendingBatch
	if !wait.waiting || wait.request.account != msg.Account || !slices.Equal(wait.database, msg.Path) {
		return m, nil
	}
	m.pendingBatch = pendingBatch{}
	return m.refuseBatch(wait.request, msg.Err)
}

// reviewBatch refuses a batch that fails a check, runs one that only reads,
// and opens the review for any other.
func (m Model) reviewBatch(request batchRequest, paths []string) (Model, tea.Cmd) {
	check := query.CheckBatch(request.batch, paths)
	if len(check.Problems) > 0 {
		return m.refuseBatch(request, &batchRefusalError{problems: check.Problems})
	}
	draft := panes.BatchDraft{Account: request.account, KeyPaths: paths, Batch: request.batch, Check: check}
	m.batchText = request.text
	if !writes(request.batch) {
		return m.commitBatch(draft)
	}
	m.review = m.review.Open(draft)
	m.overlay = overlayBatchReview
	return m, nil
}

// refuseBatch reports a batch that never reached the adapter, and records
// it, as every refusal is.
func (m Model) refuseBatch(request batchRequest, err error) (Model, tea.Cmd) {
	m = m.beginRun(request.account)
	model, cmd := m.showFailure(err, runFailed)
	model.historyEntry = newBatchEntry(request.account, request.text, request.batch.Scope)
	return model, tea.Batch(cmd, model.recordFailure(err))
}

func newBatchEntry(account, text string, target []string) history.Entry {
	entry := newEntry(account, target, query.BatchPrefix(text, maxRecordedBatch))
	entry.Kind = history.KindBatch
	return entry
}

// handleReviewKey drives the review. Typed characters are the name field's,
// which is why q quits nothing here and ctrl+g opens no switcher.
func (m Model) handleReviewKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.reviewUpdate(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyEsc:
		m.overlay = overlayNone
		return m, nil
	case tea.KeyEnter:
		if !m.review.Confirmed() {
			return m, nil
		}
		return m.commitBatch(m.review.Draft())
	case tea.KeyUp:
		m.review = m.review.ScrollUp()
		return m, nil
	case tea.KeyDown:
		m.review = m.review.ScrollDown()
		return m, nil
	}
	return m.reviewUpdate(msg)
}

func (m Model) reviewUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.review, cmd = m.review.Update(msg)
	return m, cmd
}

// commitBatch sends the batch the review was opened for, once. It is
// reached only from the review's enter with the name typed, or for a batch
// that writes nothing.
func (m Model) commitBatch(draft panes.BatchDraft) (Model, tea.Cmd) {
	m.overlay = overlayNone
	request := batchRequest{account: draft.Account, text: m.batchText, batch: draft.Batch}
	entry, ok := m.accounts.get(draft.Account)
	if !ok || !entry.connected() {
		return m.refuseBatch(request, errRunAbandoned)
	}
	batcher, err := m.batcher(entry, draft.Batch)
	if err != nil {
		return m.refuseBatch(request, err)
	}
	m = m.beginRun(draft.Account)
	m.state = runCommitting
	m.batchState = panes.BatchCommitting
	m.committing = draft
	m.historyEntry = newBatchEntry(draft.Account, m.batchText, draft.Batch.Scope)
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, executeBatch(batcher, draft.Account, draft.Batch))
}

// blockWhileCommitting refuses what would leave a batch's outcome unseen or
// start a second one: a run, a switch of account, a quit short of ctrl+c.
// Nothing cancels the batch itself.
func (m Model) blockWhileCommitting(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m, nil, false
	case key.Matches(msg, m.keys.Quit) && m.typesIntoEditor(msg):
		return m, nil, false
	case key.Matches(msg, m.keys.Run, m.keys.Rerun, m.keys.Accounts, m.keys.Quit),
		msg.Type == tea.KeyEsc && m.overlay == overlayNone:
		model, cmd := m.notify(errBatchInFlight.Error())
		return model, cmd, true
	}
	return m, nil, false
}

// typesIntoEditor reports whether msg is text for the editor, where q is a
// letter rather than a quit.
func (m Model) typesIntoEditor(msg tea.KeyMsg) bool {
	return typesIntoBuffer(msg) && m.overlay == overlayNone && m.focus == focusEditor
}

// finishBatch lays the report out as the result set, whatever the outcome:
// a rollback is a result, not an error.
func (m Model) finishBatch(msg BatchDoneMsg) (Model, tea.Cmd) {
	m.logger.Info("batch answered", "account", msg.Account, "committed", msg.Result.Committed)
	page := query.BatchReport(msg.Batch, msg.Result)
	m.stats = page.Stats
	m.state = runLoaded
	m.batchState = panes.BatchRolledBack
	if msg.Result.Committed {
		m.batchState = panes.BatchCommitted
	}
	m.results = m.results.Load(page).SetSource(msg.Account).SetOutcome(m.batchState.String()).
		SetBanner(query.BatchSummary(msg.Batch, msg.Result), !msg.Result.Committed)
	if failed, ok := query.FailedOperation(msg.Result); ok && !msg.Result.Committed {
		m.results = m.results.MarkFailedRow(failed)
	}
	entry := m.historyEntry
	entry.OK = msg.Result.Committed
	entry.Rows = len(msg.Batch.Operations)
	entry.RequestCharge = page.Stats.RequestCharge
	entry.ElapsedMillis = page.Stats.Elapsed.Milliseconds()
	if !entry.OK {
		entry.Error = rollbackText(msg.Batch, msg.Result)
	}
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.record(entry))
}

func rollbackText(b adapter.Batch, r adapter.BatchResult) string {
	failed, ok := query.FailedOperation(r)
	if !ok || failed >= len(b.Operations) {
		return "rolled back"
	}
	return fmt.Sprintf("rolled back: operation %d %s: %s", failed+1, query.OperationName(b.Operations[failed]), r.Results[failed].Status)
}

// failBatch reports a batch with no answer. One that may have applied says
// so, and nothing more: it is neither a failure nor a success.
func (m Model) failBatch(msg BatchFailedMsg) (Model, tea.Cmd) {
	m.logger.Error("batch not answered", "account", msg.Account, "error", msg.Err)
	m.state = runFailed
	if errors.Is(msg.Err, adapter.ErrWriteOutcomeUnknown) {
		return m.reportUnknownOutcome(msg)
	}
	m.results = m.results.SetSource(msg.Account).SetOutcome("not applied").
		SetBanner("Not applied: "+msg.Err.Error()+retryHint(msg.Err), true)
	m.batchState = panes.BatchNone
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.recordFailure(fmt.Errorf("not applied: %w", msg.Err)))
}

// retryHint passes on the wait a throttled batch was told to make. The
// wait is the user's: a batch is never retried for them.
func retryHint(err error) string {
	var throttled *adapter.ThrottledError
	if !errors.As(err, &throttled) || throttled.RetryAfter <= 0 {
		return ""
	}
	return fmt.Sprintf(" (retry after %s)", throttled.RetryAfter)
}

func (m Model) reportUnknownOutcome(msg BatchFailedMsg) (Model, tea.Cmd) {
	m.batchState = panes.BatchUnknown
	m.results = m.results.SetSource(msg.Account).SetOutcome(m.batchState.String()).
		SetBanner(unknownOutcomeText(msg, m.committing.KeyPaths), false)
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.recordFailure(fmt.Errorf("outcome unknown: %w", msg.Err)))
}

// unknownOutcomeText is what to do next, rather than a verdict: the query it
// ends with lists the one partition the batch could have written.
func unknownOutcomeText(msg BatchFailedMsg, paths []string) string {
	reason := msg.Err.Error()
	if errors.Is(msg.Err, context.DeadlineExceeded) {
		reason = fmt.Sprintf("timed out after %s", batchTimeout)
	}
	return fmt.Sprintf("Outcome unknown. The batch was sent and no answer came back (%s).\n"+
		"It committed in full or not at all. Alchemist did not retry it and will not.\n\n"+
		"Check before running it again:\n  %s", reason, query.PartitionCheckQuery(msg.Batch, paths))
}

// draftReplace appends a REPLACE of the row under the cursor to the batch in
// the editor, or starts a batch with it when the editor holds none. It
// writes text and stops: nothing is sent.
func (m Model) draftReplace() (Model, tea.Cmd) {
	entry, ok := m.activeConnection()
	drafter := entry.permitted().Drafter
	if !ok || drafter == nil {
		return m, nil
	}
	item, ok := m.results.SelectedDocument()
	if !ok || !m.showsWholeItems(entry) {
		return m.notify(errNotADocument.Error())
	}
	text, err := m.appendReplace(entry, drafter, item)
	if err != nil {
		return m.notify("not added: " + err.Error())
	}
	m.editor = m.editor.SetValue(text)
	return m.notify("added a REPLACE to the batch in the editor")
}

// showsWholeItems reports whether the rows on screen are items as stored in
// one container of entry's account: a projection written back would erase
// every field it left out.
func (m Model) showsWholeItems(entry accountEntry) bool {
	return m.batchState == panes.BatchNone && !m.plan.Simulated() && len(m.plan.Leaves) == 1 &&
		m.plan.Leaves[0].WholeItems() && m.runAccount == entry.account.Name
}

func (m Model) appendReplace(entry accountEntry, drafter adapter.ItemDrafter, item []byte) (string, error) {
	scope := m.plan.Scope()
	node, ok := entry.pane.Node(scope)
	if !ok {
		return "", fmt.Errorf("the catalog has not listed %s: open %s in it first", strings.Join(scope, "."), scope[0])
	}
	values, err := adapter.PartitionKeyValues(item, keyPaths(node))
	if err != nil {
		return "", err
	}
	op, err := drafter.DraftReplace(item)
	if err != nil {
		return "", err
	}
	text := m.editor.Value()
	if !query.IsBatch(text) {
		return query.FormatBatch(adapter.Batch{Scope: scope, PartitionKey: values, Operations: []adapter.Operation{op}}), nil
	}
	current, err := query.ParseBatch(text)
	if err != nil {
		return "", fmt.Errorf("the batch in the editor does not parse: %w", err)
	}
	if !slices.Equal(current.Scope, scope) || !query.SamePartition(current.PartitionKey, values) {
		return "", fmt.Errorf("the batch in the editor writes to %s, partition %s; this row is in %s, partition %s",
			strings.Join(current.Scope, "."), query.PartitionText(current.PartitionKey), strings.Join(scope, "."), query.PartitionText(values))
	}
	return query.AppendOperation(text, op)
}
