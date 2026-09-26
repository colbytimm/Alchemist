package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// selectionStepTimeout bounds one page of a selection.
const selectionStepTimeout = 2 * time.Minute

var (
	errNoEditSupport      = errors.New("this adapter cannot update items")
	errSelectionCancelled = errors.New("selection cancelled: nothing was written")
)

// mutationRefusalError is an update that failed a check made before
// anything was read, with every problem found.
type mutationRefusalError struct {
	problems []string
}

func (e *mutationRefusalError) Error() string {
	return "Update refused, nothing was read:\n  " + strings.Join(e.problems, "\n  ")
}

// mutationRequest is an update between ctrl+r and its review: the account
// it belongs to from the moment the key was pressed, and the text it came
// from.
type mutationRequest struct {
	account  string
	text     string
	mutation query.Mutation
}

// pendingMutation is an update waiting for the tree to list its target's
// database, since only the container's node knows its key paths.
type pendingMutation struct {
	request  mutationRequest
	database []string
	waiting  bool
}

// mutationSelecting is an update whose targets are being read: what its
// review will need once they are in.
type mutationSelecting struct {
	request  mutationRequest
	keyPaths []string
	check    query.MutationCheck
}

// startMutation is ctrl+r on an update. It never writes: it checks the
// statement, reads which items it would change, and opens the review. The
// one way to write is the review's enter, with the confirmation typed.
func (m Model) startMutation() (Model, tea.Cmd) {
	m = m.closeSuggestions()
	m.pendingMutation = pendingMutation{}
	entry, connected := m.activeConnection()
	if !connected {
		m = m.beginRun(m.accounts.active)
		return m.showFailure(m.whyNoConnection(), runFailed)
	}
	request := mutationRequest{account: entry.account.Name, text: m.editor.Value()}
	if err := m.checkNoAccountTarget(request.text); err != nil {
		return m.refuseMutation(request, err)
	}
	mutation, err := query.ParseMutation(request.text)
	if err != nil {
		return m.refuseMutation(request, fmt.Errorf("the update does not parse, so nothing was read: %w", err))
	}
	request.mutation = mutation
	if refusal := m.jobRefusal(); refusal != nil {
		return m.refuseMutation(request, refusal)
	}
	if _, err := m.itemEditor(entry); err != nil {
		return m.refuseMutation(request, err)
	}
	return m.checkMutation(request)
}

// checkNoAccountTarget refuses a target whose first part names an account
// of this session, for the reason checkNoAccountNamed refuses a source.
func (m Model) checkNoAccountTarget(text string) error {
	target := query.MutationTarget(text)
	if len(target) >= accountQualifiedParts && m.accounts.known(target[0]) {
		return fmt.Errorf("UPDATE %s: %w", strings.Join(target, "."), errAccountInQuery)
	}
	return nil
}

// jobRefusal is why no update may start now: another job holds the slot.
// It is asked before anything is read, so a refused update spends nothing.
func (m Model) jobRefusal() error {
	if !m.job.active() {
		return nil
	}
	if m.job.kind == jobMutation {
		account, path, _ := m.job.writes()
		return fmt.Errorf("%s is running on %s/%s: %s in the catalog shows it",
			m.job.named(), account, strings.Join(path, "."), m.job.kind.reopenKey())
	}
	return errors.New(m.job.waitText("updates"))
}

// itemEditor hands out the ItemEditor that writes on entry's account. It
// is the one place an update reaches a backend's writes from, and so where
// read-only is enforced: before the selection, which a read-only account
// could never act on, and again as the job starts.
func (m Model) itemEditor(entry accountEntry) (adapter.ItemEditor, error) {
	if entry.management.Editor == nil || entry.management.Scanner == nil {
		return nil, errNoEditSupport
	}
	if entry.account.ReadOnly {
		return nil, readOnlyError(entry.account.Name)
	}
	return entry.management.Editor, nil
}

// checkMutation finds the target's key paths, asking the tree for them
// first when it has not listed the target's database.
func (m Model) checkMutation(request mutationRequest) (Model, tea.Cmd) {
	entry, ok := m.accounts.get(request.account)
	if !ok || !entry.connected() {
		return m.refuseMutation(request, errRunAbandoned)
	}
	target := request.mutation.Target
	m, node, found, load := m.lookupContainer(entry, target)
	switch found {
	case containerMissing:
		return m.refuseMutation(request, noSuchContainer(target))
	case containerLoading:
		m.pendingMutation = pendingMutation{request: request, database: target[:1], waiting: true}
		return m, load
	}
	return m.selectTargets(request, keyPaths(node))
}

// resumePendingMutation checks the update that waited for parent's
// listing in account, now that it has landed.
func (m Model) resumePendingMutation(account string, parent []string) (Model, tea.Cmd) {
	wait := m.pendingMutation
	if !wait.waiting || wait.request.account != account || !slices.Equal(wait.database, parent) {
		return m, nil
	}
	m.pendingMutation = pendingMutation{}
	return m.checkMutation(wait.request)
}

// failPendingMutation refuses the update that waited for a listing that
// failed.
func (m Model) failPendingMutation(msg ErrMsg) (Model, tea.Cmd) {
	wait := m.pendingMutation
	if !wait.waiting || wait.request.account != msg.Account || !slices.Equal(wait.database, msg.Path) {
		return m, nil
	}
	m.pendingMutation = pendingMutation{}
	return m.refuseMutation(wait.request, msg.Err)
}

// selectTargets checks the statement against its container and starts
// reading the items it matches. The selection only reads: esc cancels it,
// and a newer run supersedes it.
func (m Model) selectTargets(request mutationRequest, paths []string) (Model, tea.Cmd) {
	check := query.CheckMutation(request.mutation, paths)
	if len(check.Problems) > 0 {
		return m.refuseMutation(request, &mutationRefusalError{problems: check.Problems})
	}
	entry, _ := m.accounts.get(request.account)
	m = m.beginRun(request.account)
	m.state = runSelecting
	m.mutationBadge = panes.MutationBadge{Text: "selecting…"}
	m.selecting = mutationSelecting{request: request, keyPaths: paths, check: check}
	m.historyEntry = newMutationEntry(request)
	ctx, cancel := context.WithTimeout(context.Background(), selectionStepTimeout)
	m.cancel = cancel
	m.logger.Info("update selecting", "account", request.account, "target", request.mutation.Target)
	scanner, limit := entry.management.Scanner, entry.account.MaxMutationItems
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, openSelection(ctx, cancel, model.run, request, scanner, paths, limit))
}

func openSelection(ctx context.Context, cancel context.CancelFunc, run runID, request mutationRequest,
	scanner adapter.ItemScanner, paths []string, limit int,
) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		selection, err := mutate.Select(ctx, scanner, request.mutation, paths, limit)
		if err != nil {
			return TargetsSelectedMsg{Account: request.account, Err: err, run: run}
		}
		return readTargets(ctx, run, request.account, selection)
	}
}

func nextTargets(ctx context.Context, cancel context.CancelFunc, run runID, account string, selection *mutate.Selection) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		return readTargets(ctx, run, account, selection)
	}
}

func readTargets(ctx context.Context, run runID, account string, selection *mutate.Selection) tea.Msg {
	progress, err := selection.Next(ctx)
	if err != nil || progress.Done {
		return TargetsSelectedMsg{Account: account, Targets: selection.Targets(), Err: err, selection: selection, run: run}
	}
	return TargetPageMsg{Account: account, Progress: progress, selection: selection, run: run}
}

// acceptTargetPage shows how far the selection has got and reads on. A
// page of a run that was replaced closes its selection.
func (m Model) acceptTargetPage(msg TargetPageMsg) (Model, tea.Cmd) {
	if msg.run != m.run || m.state != runSelecting {
		m.closeSelection(msg.selection)
		return m, nil
	}
	m.stats = adapter.Stats{RequestCharge: msg.Progress.RequestCharge, RowCount: msg.Progress.Matched}
	ctx, cancel := context.WithTimeout(context.Background(), selectionStepTimeout)
	m.cancel = cancel
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, nextTargets(ctx, cancel, m.run, msg.Account, msg.selection))
}

func (m Model) closeSelection(selection *mutate.Selection) {
	if selection == nil {
		return
	}
	if err := selection.Close(); err != nil {
		m.logger.Error("close an update's selection", "error", err)
	}
}

// acceptTargets opens the review of what the selection found, or says why
// there is nothing to review.
func (m Model) acceptTargets(msg TargetsSelectedMsg) (Model, tea.Cmd) {
	m.closeSelection(msg.selection)
	if msg.run != m.run || m.state != runSelecting {
		return m, nil
	}
	m = m.cancelInFlight()
	selecting := m.selecting
	targets := msg.Targets
	m.stats = adapter.Stats{RequestCharge: targets.RequestCharge, RowCount: len(targets.Items)}
	switch {
	case errors.Is(msg.Err, mutate.ErrTooManyTargets):
		return m.refuseSelection(fmt.Errorf("%w. Narrow the WHERE, or raise max_mutation_items on the %s profile. "+
			"Nothing was written; the selection cost %s RU", msg.Err, msg.Account, panes.FormatCharge(targets.RequestCharge)))
	case msg.Err != nil:
		return m.refuseSelection(msg.Err)
	case len(targets.Items) == 0:
		return m.reportNoMatch(targets)
	}
	m.state, m.mutationBadge = runIdle, panes.MutationBadge{}
	entry, _ := m.accounts.get(msg.Account)
	draft := panes.MutationDraft{
		Account:  msg.Account,
		KeyPaths: selecting.keyPaths,
		Mutation: selecting.request.mutation,
		Targets:  targets,
		Warnings: append(slices.Clone(selecting.check.Warnings), selectionWarnings(selecting.request.mutation, targets)...),
		Writers:  writersFor(entry.account),
	}
	m.mutationReview = m.mutationReview.Open(draft).SetSize(m.width, m.height)
	m.overlay = overlayMutationReview
	m.logger.Info("update selected", "account", msg.Account, "target", draft.Mutation.Target,
		"items", len(targets.Items), "RU", targets.RequestCharge)
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, m.snapshotLine(msg.Account, draft.Mutation.Target))
}

// selectionWarnings are what the selection found worth a second look.
func selectionWarnings(m query.Mutation, targets mutate.Targets) []string {
	var warnings []string
	if keyless := targets.Keyless(); keyless > 0 {
		warnings = append(warnings, fmt.Sprintf("%s have no partition key value: they cannot be addressed and are skipped.",
			countItems(keyless)))
	}
	if targets.WholeItems {
		warnings = append(warnings, "An UNSET made the selection read whole items, to see which have each path: "+
			"its charge is a full read of the matching items.")
	}
	if targets.Unaffected > 0 {
		paths := make([]string, 0, len(m.Removals))
		for _, path := range m.Removals {
			paths = append(paths, path.Pointer())
		}
		warnings = append(warnings, fmt.Sprintf("%s already lack %s and are left out.",
			countItems(targets.Unaffected), strings.Join(paths, ", ")))
	}
	return warnings
}

func countItems(n int) string {
	if n == 1 {
		return "1 item"
	}
	return panes.FormatCount(int64(n)) + " items"
}

// snapshotLine reads what the review can say of a copy taken before the
// run: the newest snapshot of the target, or that there is none. Without
// a snapshot store, it names the clone.
func (m Model) snapshotLine(account string, target []string) tea.Cmd {
	run, root := m.run, m.snapshotRoot
	container := strings.Join(target, ".")
	if root == "" {
		line := fmt.Sprintf("Before a large run, take a copy: esc, then y in the catalog clones %s.", container)
		return func() tea.Msg { return snapshotLineMsg{line: line, run: run} }
	}
	loc := snapshot.Location{Root: root, Account: account, Database: target[0], Container: target[1]}
	return func() tea.Msg {
		record, err := snapshot.Newest(loc)
		switch {
		case errors.Is(err, snapshot.ErrNoSnapshot):
			return snapshotLineMsg{line: fmt.Sprintf("No snapshot of %s. esc, then s in the catalog takes one.", container), run: run}
		case err != nil:
			return snapshotLineMsg{line: "The snapshot store could not be read: " + err.Error(), run: run}
		}
		age := time.Since(record.Finished).Round(time.Minute)
		return snapshotLineMsg{line: fmt.Sprintf("The newest snapshot of %s was taken %s ago (%s).",
			container, age, record.Finished.Local().Format("2006-01-02 15:04")), run: run}
	}
}

func (m Model) fileSnapshotLine(msg snapshotLineMsg) Model {
	if msg.run != m.run || m.overlay != overlayMutationReview {
		return m
	}
	m.mutationReview = m.mutationReview.SetSnapshotLine(msg.line)
	return m
}

// reportNoMatch is a selection that found nothing: a result, recorded as a
// run that wrote no item.
func (m Model) reportNoMatch(targets mutate.Targets) (Model, tea.Cmd) {
	mutation := m.selecting.request.mutation
	text := fmt.Sprintf("No item in %s matches. Nothing to %s.", strings.Join(mutation.Target, "."), mutation.Kind)
	if targets.Unaffected > 0 {
		text = fmt.Sprintf("%s match and none needs a change. Nothing to %s.", countItems(targets.Unaffected), mutation.Kind)
	}
	m.state = runLoaded
	m.mutationBadge = panes.MutationBadge{Text: mutation.Kind.Applied()}
	m.results = m.results.Load(adapter.Page{}).SetOutcome(mutation.Kind.Applied()).SetBanner(text, false)
	entry := m.historyEntry
	entry.OK, entry.RequestCharge = true, targets.RequestCharge
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, model.record(entry))
}

// refuseSelection reports a selection that ended without targets to
// review, and records it.
func (m Model) refuseSelection(err error) (Model, tea.Cmd) {
	m.mutationBadge = panes.MutationBadge{}
	model, cmd := m.showFailure(err, runFailed)
	return model, tea.Batch(cmd, model.recordFailure(err))
}

// cancelSelection is esc while a selection reads. It only ever read, so
// nothing needs undoing; the page in flight is dropped as it arrives.
func (m Model) cancelSelection() (Model, tea.Cmd) {
	m = m.cancelInFlight()
	m.run++
	m.mutationBadge = panes.MutationBadge{}
	return m.showFailure(errSelectionCancelled, runFailed)
}

// refuseMutation reports an update that never reached the adapter, and
// records it, as every refusal is.
func (m Model) refuseMutation(request mutationRequest, err error) (Model, tea.Cmd) {
	m = m.beginRun(request.account)
	model, cmd := m.showFailure(err, runFailed)
	model.historyEntry = newMutationEntry(request)
	return model, tea.Batch(cmd, model.recordFailure(err))
}

// newMutationEntry records an update against the target it names, which is
// no scope for what runs next; one that did not parse has none.
func newMutationEntry(request mutationRequest) history.Entry {
	entry := newEntry(request.account, request.mutation.Target, request.text)
	entry.Kind = history.KindUpdate
	return entry
}

// handleMutationReviewKey drives the review. Typed characters are the
// confirmation's, which is why q quits nothing here and ctrl+g opens no
// switcher.
func (m Model) handleMutationReviewKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.mutationReviewUpdate(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyEsc:
		m.overlay = overlayNone
		return m, nil
	case tea.KeyEnter:
		if !m.mutationReview.Confirmed() {
			return m, nil
		}
		return m.confirmMutation(m.mutationReview.Draft())
	case tea.KeyUp:
		m.mutationReview = m.mutationReview.ScrollUp()
		return m, nil
	case tea.KeyDown:
		m.mutationReview = m.mutationReview.ScrollDown()
		return m, nil
	}
	return m.mutationReviewUpdate(msg)
}

func (m Model) mutationReviewUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.mutationReview, cmd = m.mutationReview.Update(msg)
	return m, cmd
}

// confirmMutation starts the job the review was opened for. It is reached
// from the review's enter alone, with the confirmation typed, and asks
// again what the dry run asked: the account may have gone read-only, or
// another job may have started, since.
func (m Model) confirmMutation(draft panes.MutationDraft) (Model, tea.Cmd) {
	m.overlay = overlayNone
	request := m.selecting.request
	entry, ok := m.accounts.get(draft.Account)
	if !ok || !entry.connected() {
		return m.refuseMutation(request, errRunAbandoned)
	}
	if refusal := m.jobRefusal(); refusal != nil {
		return m.refuseMutation(request, refusal)
	}
	editor, err := m.itemEditor(entry)
	if err != nil {
		return m.refuseMutation(request, err)
	}
	return m.startMutationJob(draft, request.text, editor)
}
