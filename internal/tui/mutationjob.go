package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/tui/panes"
	"github.com/colbytimm/alchemist/internal/writers"
)

const (
	// mutationStepTimeout bounds one chunk of writes, each write on its
	// own deadline within it.
	mutationStepTimeout = 2 * time.Minute
	// reportPageSize is how many rows of a report the results pane is
	// handed at a time.
	reportPageSize = 100
)

// mutationRun is the update holding the job slot: what it was confirmed
// for, and how far it has got. Its job is held here only between steps;
// while a step runs, the step owns it and hands it back in its message.
type mutationRun struct {
	draft    panes.MutationDraft
	job      *mutate.Job
	progress mutate.Progress
	end      panes.MutationEnd
	stopping bool
	err      error
	// inFlight are the ids the step under way was handed, whose outcome a
	// quit leaves unknown.
	inFlight   []string
	window     []timedPage
	quitWarned bool
	entry      history.Entry
}

func (r mutationRun) running() bool { return r.end == panes.MutationRunning }

// startMutationJob takes the job slot and writes the first target alone:
// the probe. The job holds the connection of the account the review named,
// whichever account the session moves to.
func (m Model) startMutationJob(draft panes.MutationDraft, text string, editor adapter.ItemEditor) (Model, tea.Cmd) {
	mutation := draft.Mutation
	m.lastJob++
	m.job = job{
		kind:     jobMutation,
		id:       m.lastJob,
		accounts: []string{draft.Account},
		target:   writeTarget{account: draft.Account, path: mutation.Target},
		noun:     mutation.Kind.String(),
	}
	pool := writers.NewPool(draft.Writers, writers.SystemClock{})
	m.mutating = mutationRun{
		draft: draft,
		job:   mutate.NewJob(mutation, draft.Targets, editor, pool),
		entry: newMutationEntry(mutationRequest{account: draft.Account, text: text, mutation: mutation}),
	}
	m.overlay = overlayMutationProgress
	m.logger.Info("update started", "account", draft.Account, "target", mutation.Target,
		"items", len(draft.Targets.Items), "writers", draft.Writers)
	m = m.withJobKeys()
	return m.stepMutation()
}

// stepMutation hands the job to the next chunk's command, if the account
// may still be written: a profile read afresh by the switcher can turn it
// read-only between two chunks, and the job then ends short, resumable.
func (m Model) stepMutation() (Model, tea.Cmd) {
	if err := m.mutationWritable(); err != nil {
		return m.endMutation(panes.MutationFailed, err)
	}
	run := &m.mutating
	j := run.job
	run.job = nil
	run.inFlight = j.Pending()
	ctx, cancel := context.WithTimeout(context.Background(), mutationStepTimeout)
	m.job.cancel = cancel
	return m.syncMutation(), applyChunk(ctx, cancel, m.job.id, run.draft.Account, j)
}

func applyChunk(ctx context.Context, cancel context.CancelFunc, id jobID, account string, j *mutate.Job) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		progress, err := j.ApplyChunk(ctx)
		switch {
		case err != nil:
			return MutationFailedMsg{Account: account, Progress: progress, Err: err, mutation: j, job: id}
		case progress.Done:
			return MutationFinishedMsg{Account: account, Progress: progress, mutation: j, job: id}
		}
		return MutationChunkAppliedMsg{Account: account, Progress: progress, mutation: j, job: id}
	}
}

// mutationWritable asks the one gate again whether the job's account may
// be written.
func (m Model) mutationWritable() error {
	entry, ok := m.accounts.get(m.mutating.draft.Account)
	if !ok || !entry.connected() {
		return errRunAbandoned
	}
	_, err := m.itemEditor(entry)
	return err
}

// currentMutation reports whether a message belongs to the update holding
// the slot, and takes its job back if so.
func (m Model) currentMutation(id jobID, j *mutate.Job, progress mutate.Progress) (Model, bool) {
	if m.job.kind != jobMutation || m.job.id != id {
		return m, false
	}
	m.job.cancel = nil
	m.mutating = m.mutating.tally(progress)
	m.mutating.job = j
	m.mutating.inFlight = nil
	return m, true
}

func (m Model) acceptMutationChunk(msg MutationChunkAppliedMsg) (Model, tea.Cmd) {
	m, ok := m.currentMutation(msg.job, msg.mutation, msg.Progress)
	if !ok {
		return m, nil
	}
	if m.mutating.stopping {
		return m.endMutation(panes.MutationStopped, nil)
	}
	return m.stepMutation()
}

func (m Model) finishMutationJob(msg MutationFinishedMsg) (Model, tea.Cmd) {
	m, ok := m.currentMutation(msg.job, msg.mutation, msg.Progress)
	if !ok {
		return m, nil
	}
	return m.endMutation(panes.MutationDone, nil)
}

// failMutationJob ends a job whose step ended short. A stop is not a
// failure; neither is a step the user's stop cancelled.
func (m Model) failMutationJob(msg MutationFailedMsg) (Model, tea.Cmd) {
	m, ok := m.currentMutation(msg.job, msg.mutation, msg.Progress)
	if !ok {
		return m, nil
	}
	if m.mutating.stopping && errors.Is(msg.Err, context.Canceled) {
		return m.endMutation(panes.MutationStopped, nil)
	}
	return m.endMutation(panes.MutationFailed, msg.Err)
}

// tally keeps the job's totals, and times a step that wrote something for
// the rate.
func (r mutationRun) tally(p mutate.Progress) mutationRun {
	r.progress = p
	if p.Items > 0 {
		r.window = append(r.window, timedPage{items: p.Items, duration: p.Duration})
		r.window = r.window[max(len(r.window)-rateWindow, 0):]
	}
	return r
}

// endMutation stops the job at a step boundary. It keeps the slot, so a job
// that ended short can be resumed, until its view is closed.
func (m Model) endMutation(end panes.MutationEnd, err error) (Model, tea.Cmd) {
	run := &m.mutating
	run.end, run.err, run.stopping = end, err, false
	m.job.cancel = nil
	c := run.progress.Counts
	m.logger.Info("update ended", "account", run.draft.Account, "target", run.draft.Mutation.Target, "end", end,
		"updated", c.Applied, "changed", c.Changed, "gone", c.Gone, "no key", c.NoKey, "failed", c.Failed,
		"unknown", c.Unknown, "not attempted", c.NotAttempted, "RU", run.progress.WriteCharge, "error", err)
	return m.syncMutation(), nil
}

// showMutationProgress is w while an update holds the slot: it opens the
// view and starts nothing.
func (m Model) showMutationProgress() (Model, tea.Cmd) {
	m.overlay = overlayMutationProgress
	return m.syncMutation(), nil
}

func (m Model) handleMutationProgressKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	run := m.mutating
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case run.running() && key.Matches(msg, m.keys.HideClone):
		m.mutating.quitWarned = false
		m.overlay = overlayNone
		return m.syncMutation(), nil
	case run.running() && key.Matches(msg, m.keys.StopClone):
		return m.stopMutation(), nil
	case !run.running() && run.end != panes.MutationDone && key.Matches(msg, m.keys.ResumeClone):
		return m.resumeMutationJob()
	case !run.running() && key.Matches(msg, m.keys.ShowReport):
		return m.closeMutation()
	}
	return m, nil
}

// stopMutation starts no new write: the writes in flight finish, each on
// its own deadline, since cancelling a sent write is how an unknown
// outcome is made.
func (m Model) stopMutation() Model {
	m.mutating.stopping = true
	m.job.stopStep()
	return m.syncMutation()
}

// resumeMutationJob carries on with the targets that have no outcome.
func (m Model) resumeMutationJob() (Model, tea.Cmd) {
	if err := m.mutationWritable(); err != nil {
		return m.endMutation(panes.MutationFailed, err)
	}
	run := &m.mutating
	run.end, run.err, run.quitWarned = panes.MutationRunning, nil, false
	run.job.Resume()
	m.logger.Info("update resumed", "account", run.draft.Account, "target", run.draft.Mutation.Target,
		"not attempted", run.progress.Counts.NotAttempted)
	return m.stepMutation()
}

// closeMutation gives the slot up, records the run, and loads its report
// into the results, replacing what is there, as any run does.
func (m Model) closeMutation() (Model, tea.Cmd) {
	run := m.mutating
	cursor := mutate.NewReportCursor(run.job, reportPageSize)
	for _, row := range cursor.Omitted() {
		m.logger.Info("update outcome past the report's rows", "account", run.draft.Account,
			"target", run.draft.Mutation.Target, "id", row.ID, "partition key", row.PartitionKey, "outcome", row.Outcome)
	}
	record := m.record(run.finishedEntry(cursor.Summary()))
	m = m.releaseMutation().beginRun(run.draft.Account)
	page, err := cursor.NextPage(context.Background())
	if err != nil {
		model, cmd := m.showFailure(err, runFailed)
		return model, tea.Batch(cmd, record)
	}
	summary := cursor.Summary()
	failed := summary.Counts.Failed > 0 || summary.Counts.Unknown > 0
	outcome := run.outcome()
	m.pageCursor, m.stats, m.state = cursor, page.Stats, runLoaded
	m.mutationBadge = panes.MutationBadge{Text: outcome, Failed: failed || run.end == panes.MutationFailed}
	m.results = m.results.Load(page).SetSource(run.draft.Account).SetOutcome(outcome).SetBanner(cursor.Banner(), failed)
	if row, ok := cursor.FirstProblem(); ok && failed && row < len(page.Rows) {
		m.results = m.results.MarkFailedRow(row)
	}
	model, cmd := m.syncStatusBar()
	return model, tea.Batch(cmd, record)
}

// outcome is the report's badge: how the job ended.
func (r mutationRun) outcome() string {
	switch r.end {
	case panes.MutationStopped:
		return "stopped"
	case panes.MutationFailed:
		return "failed"
	}
	return r.draft.Mutation.Kind.Applied()
}

// finishedEntry is the run as history keeps it: the items written, what
// selecting and writing them cost, and what kept it from being clean.
func (r mutationRun) finishedEntry(summary mutate.Summary) history.Entry {
	entry := r.entry
	c := summary.Counts
	entry.Rows = c.Applied
	entry.RequestCharge = summary.SelectionCharge + summary.WriteCharge
	entry.ElapsedMillis = summary.Elapsed.Milliseconds()
	entry.OK = summary.Clean()
	var problems []string
	if c.NotAttempted > 0 {
		problems = append(problems, fmt.Sprintf("stopped after %d of %d", summary.Total-c.NotAttempted, summary.Total))
	}
	if c.Failed > 0 || c.Unknown > 0 {
		problems = append(problems, fmt.Sprintf("%d failed, %d unknown", c.Failed, c.Unknown))
	}
	if r.err != nil {
		problems = append(problems, r.err.Error())
	}
	entry.Error = strings.Join(problems, "; ")
	return entry
}

func (m Model) releaseMutation() Model {
	m.job = job{}
	m.mutating = mutationRun{}
	m.overlay = overlayNone
	m.statusBar = m.statusBar.SetJob("")
	return m.withJobKeys()
}

// abandonMutation stops an update the session is quitting under, and
// records it before the session ends. The writes in flight run to their
// own deadline or the process's end; their outcome is unknown.
func (m Model) abandonMutation() Model {
	m.job.stopStep()
	run := m.mutating
	summary := mutate.Summary{
		Kind: run.draft.Mutation.Kind, Container: run.draft.Mutation.Target,
		Counts: run.progress.Counts, Total: len(run.draft.Targets.Items),
		SelectionCharge: run.draft.Targets.RequestCharge, WriteCharge: run.progress.WriteCharge,
	}
	if run.progress.Total == 0 {
		summary.Counts.NotAttempted = summary.Total
	}
	entry := run.finishedEntry(summary)
	if run.running() {
		entry.OK = false
		entry.Error = strings.TrimPrefix(entry.Error+"; quit while running", "; ")
	}
	if err := m.history.Append(entry); err != nil {
		m.logger.Warn("update not recorded", "error", err)
	}
	c := run.progress.Counts
	m.logger.Warn("update left unfinished at quit", "account", run.draft.Account, "target", run.draft.Mutation.Target,
		"updated", c.Applied, "failed", c.Failed, "unknown", c.Unknown, "not attempted", c.NotAttempted,
		"in flight, outcome unknown", run.inFlight)
	return m.releaseMutation()
}

func (m Model) syncMutation() Model {
	if m.job.kind != jobMutation {
		return m
	}
	m.mutationProgress = m.mutationProgress.SetStatus(m.mutating.status())
	m.statusBar = m.statusBar.SetJob(m.mutating.label())
	return m
}

// label is the status bar field: the job and how far it has got, and once
// it ends, how it ended, until its view is closed.
func (r mutationRun) label() string {
	kind := r.draft.Mutation.Kind.String()
	switch r.end {
	case panes.MutationDone:
		return kind + " done (w)"
	case panes.MutationStopped:
		return kind + " stopped (w)"
	case panes.MutationFailed:
		return kind + " failed (w)"
	}
	done, total := r.attempted(), len(r.draft.Targets.Items)
	return fmt.Sprintf("%s %s/%s %d%% (w)", kind, r.draft.Account, strings.Join(r.draft.Mutation.Target, "."), min(99, 100*done/max(total, 1)))
}

// attempted counts the targets with an outcome, none before the first
// step has reported.
func (r mutationRun) attempted() int {
	if r.progress.Total == 0 {
		return 0
	}
	return r.progress.Total - r.progress.Counts.NotAttempted
}

func (r mutationRun) status() panes.MutationStatus {
	status := panes.MutationStatus{
		Account:    r.draft.Account,
		Container:  r.draft.Mutation.Target,
		Kind:       r.draft.Mutation.Kind,
		Progress:   r.progress,
		Rate:       r.rate(),
		Projected:  r.projected(),
		MaxWriters: r.draft.Writers,
		Stopping:   r.stopping,
		End:        r.end,
		Err:        r.err,
	}
	if status.Progress.Total == 0 {
		status.Progress.Total = len(r.draft.Targets.Items)
		status.Progress.Counts.NotAttempted = status.Progress.Total
		status.Progress.Writers = r.draft.Writers
	}
	if r.quitWarned && r.running() {
		applied := r.draft.Mutation.Kind.Applied()
		status.Warning = fmt.Sprintf("The %s is running. Quit again to stop it and quit; items already %s stay %s.",
			r.draft.Mutation.Kind, applied, applied)
	}
	return status
}

func (r mutationRun) rate() float64 {
	var items int
	var elapsed time.Duration
	for _, step := range r.window {
		items += step.items
		elapsed += step.duration
	}
	if elapsed <= 0 {
		return 0
	}
	return float64(items) / elapsed.Seconds()
}

// projected scales what the writes have cost by what is left to write.
func (r mutationRun) projected() float64 {
	attempted := r.progress.Counts.Attempted()
	if attempted == 0 || r.progress.WriteCharge == 0 {
		return 0
	}
	return r.progress.WriteCharge / float64(attempted) * float64(attempted+r.progress.Counts.NotAttempted)
}
