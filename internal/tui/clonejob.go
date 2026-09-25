package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	// cloneStepTimeout bounds one step of a clone, so a request that hangs
	// ends the job instead of wedging it.
	cloneStepTimeout = 2 * time.Minute
	// rateWindow is how many of the latest pages the rate is taken over.
	rateWindow  = 10
	quitWarning = "A clone is running. Quit again to stop it and quit; the target will be incomplete."
)

// cloneRun is the clone holding the job slot: how far it has got, and what
// it has spent. Its copy is held here only between steps; while a step
// runs, the step owns it and hands it back in its message.
type cloneRun struct {
	plan             clone.Plan
	index            int
	databaseCreated  bool
	containerCreated bool
	copy             *clone.Copy
	position         adapter.ScanPosition
	end              panes.CloneEnd
	stopping         bool
	err              error
	// unanswered marks a create that was sent and never answered: what it
	// was creating may exist.
	unanswered  bool
	rows        []panes.CloneContainerRow
	readCharge  float64
	writeCharge float64
	throttles   int
	writers     int
	window      []timedPage
	quitWarned  bool
}

type timedPage struct {
	items    int
	duration time.Duration
}

func newCloneRun(plan clone.Plan) cloneRun {
	run := cloneRun{plan: plan, writers: plan.Job.Writers}
	for _, container := range plan.Containers {
		run.rows = append(run.rows, panes.CloneContainerRow{Name: container.Spec.Name, Estimate: container.Size})
	}
	return run
}

func (r cloneRun) running() bool { return r.end == panes.CloneRunning }

func (r cloneRun) items() bool { return r.plan.Job.Content == clone.DefinitionAndItems }

// startClone takes the job slot and runs the first step. It is reached from
// the review's enter alone, with the target account's name typed.
func (m Model) startClone(plan clone.Plan) (Model, tea.Cmd) {
	source, target := plan.Job.Source, plan.Job.Target
	m.lastJob++
	m.job = job{
		kind:     jobClone,
		id:       m.lastJob,
		accounts: slices.Compact([]string{source.Account, target.Account}),
		target:   writeTarget{account: target.Account, path: target.Path},
	}
	m.cloning = newCloneRun(plan)
	m.clonePrompt = clonePrompt{}
	m.overlay = overlayCloneProgress
	m.logger.Info("clone started", "source", source, "target", target, "content", plan.Job.Content,
		"fidelity", plan.Job.Fidelity, "capacity", plan.Job.Capacity, "writers", plan.Job.Writers)
	return m.stepClone()
}

// stepClone issues the one step that comes next: create what is missing,
// open the copy of a container created earlier, or copy a page.
func (m Model) stepClone() (Model, tea.Cmd) {
	run := m.cloning
	ctx, cancel := context.WithTimeout(context.Background(), cloneStepTimeout)
	m.job.cancel = cancel
	var step tea.Cmd
	switch {
	case !run.containerCreated:
		needsDatabase := run.plan.CreatesDatabase && !run.databaseCreated
		step = createCloneTarget(ctx, cancel, m.job.id, run.plan, run.index, needsDatabase)
	case run.copy == nil:
		step = reopenCloneCopy(ctx, cancel, m.job.id, run.plan, run.index, run.position)
	default:
		m.cloning.copy = nil
		step = copyClonePage(ctx, cancel, m.job.id, run.copy)
	}
	return m.syncClone(), step
}

func createCloneTarget(ctx context.Context, cancel context.CancelFunc, id jobID, plan clone.Plan, i int, needsDatabase bool) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		var created cloneCreated
		if needsDatabase {
			if err := plan.CreateDatabase(ctx); err != nil {
				return CloneFailedMsg{Err: err, job: id}
			}
			created.database = true
		}
		if i == len(plan.Containers) {
			return CloneTargetCreatedMsg{created: created, job: id}
		}
		if err := plan.CreateContainer(ctx, i); err != nil {
			return CloneFailedMsg{Err: err, created: created, job: id}
		}
		created.container = true
		if plan.Job.Content == clone.DefinitionOnly {
			return CloneTargetCreatedMsg{created: created, job: id}
		}
		c, err := plan.Open(ctx, i, "")
		if err != nil {
			return CloneFailedMsg{Err: err, created: created, job: id}
		}
		return CloneTargetCreatedMsg{Copy: c, created: created, job: id}
	}
}

func reopenCloneCopy(ctx context.Context, cancel context.CancelFunc, id jobID, plan clone.Plan, i int, from adapter.ScanPosition) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		c, err := plan.Open(ctx, i, from)
		if err != nil {
			return CloneFailedMsg{Err: err, job: id}
		}
		return CloneTargetCreatedMsg{Copy: c, job: id}
	}
}

func copyClonePage(ctx context.Context, cancel context.CancelFunc, id jobID, c *clone.Copy) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		progress, err := c.CopyPage(ctx)
		if err != nil {
			return CloneFailedMsg{Err: err, Progress: progress, copy: c, job: id}
		}
		return ClonePageCopiedMsg{Progress: progress, copy: c, job: id}
	}
}

// current reports whether a message belongs to the clone holding the slot.
// One for a clone that is over closes the copy it carries.
func (m Model) current(id jobID, c *clone.Copy) bool {
	if m.job.kind == jobClone && m.job.id == id {
		return true
	}
	m.closeCopy(c)
	return false
}

func (m Model) closeCopy(c *clone.Copy) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		m.logger.Error("close clone copy", "error", err)
	}
}

func (m Model) acceptCloneTarget(msg CloneTargetCreatedMsg) (Model, tea.Cmd) {
	if !m.current(msg.job, msg.Copy) {
		return m, nil
	}
	run := &m.cloning
	run.databaseCreated = run.databaseCreated || msg.created.database
	run.containerCreated = true
	run.copy = msg.Copy
	switch {
	case len(run.plan.Containers) == 0:
		return m.completeClone()
	case !run.items():
		return m.finishContainer()
	case run.stopping:
		return m.endClone(context.Canceled)
	}
	return m.stepClone()
}

func (m Model) acceptClonePage(msg ClonePageCopiedMsg) (Model, tea.Cmd) {
	if !m.current(msg.job, msg.copy) {
		return m, nil
	}
	m.logSkips(msg.Progress.Skips)
	m.cloning = m.cloning.tally(msg.Progress)
	m.cloning.copy = msg.copy
	m.cloning.position = msg.copy.Position()
	switch {
	case msg.Progress.Done:
		return m.finishContainer()
	case m.cloning.stopping:
		return m.endClone(context.Canceled)
	}
	return m.stepClone()
}

func (m Model) failClone(msg CloneFailedMsg) (Model, tea.Cmd) {
	if !m.current(msg.job, msg.copy) {
		return m, nil
	}
	m.closeCopy(msg.copy)
	m.cloning = m.cloning.tally(msg.Progress)
	m.cloning.databaseCreated = m.cloning.databaseCreated || msg.created.database
	m.cloning.containerCreated = m.cloning.containerCreated || msg.created.container
	m.cloning.unanswered = !m.cloning.containerCreated && errors.Is(msg.Err, adapter.ErrWriteOutcomeUnknown)
	m.cloning.copy = nil
	return m.endClone(msg.Err)
}

func (m Model) logSkips(skips []clone.Skip) {
	if len(skips) == 0 {
		return
	}
	target := m.cloning.plan.Containers[m.cloning.index].Target()
	for _, skip := range skips {
		m.logger.Warn("clone skipped an item", "target", strings.Join(target, "."), "id", skip.ID, "reason", skip.Reason)
	}
}

// tally adds what a page did to the counters. Only a page that wrote
// something is timed for the rate.
func (r cloneRun) tally(p clone.Progress) cloneRun {
	r.readCharge += p.ReadCharge
	r.writeCharge += p.WriteCharge
	r.throttles += p.Throttles
	if p.Writers > 0 {
		r.writers = p.Writers
	}
	if len(r.rows) == 0 {
		return r
	}
	r.rows = slices.Clone(r.rows)
	row := &r.rows[r.index]
	row.Written += int64(p.Written)
	row.Skipped += int64(p.Skipped)
	row.Charge += p.ReadCharge + p.WriteCharge
	if p.Read > 0 {
		r.window = append(r.window, timedPage{items: p.Written + p.Skipped, duration: p.Duration})
		r.window = r.window[max(len(r.window)-rateWindow, 0):]
	}
	return r
}

// finishContainer moves on to the next container, or ends the clone.
func (m Model) finishContainer() (Model, tea.Cmd) {
	run := &m.cloning
	m.closeCopy(run.copy)
	run.copy = nil
	run.rows = slices.Clone(run.rows)
	run.rows[run.index].Done = true
	if run.index >= len(run.plan.Containers)-1 {
		return m.completeClone()
	}
	run.index++
	run.containerCreated, run.position = false, ""
	if run.stopping {
		return m.endClone(context.Canceled)
	}
	return m.stepClone()
}

// completeClone ends a clone that copied everything, and reloads what it
// created in the target's own tree, on screen or not.
func (m Model) completeClone() (Model, tea.Cmd) {
	run := &m.cloning
	run.end = panes.CloneDone
	m.job.cancel = nil
	target := run.plan.Job.Target
	m.logger.Info("clone finished", "source", run.plan.Job.Source, "target", target,
		"written", run.written(), "skipped", run.skipped(), "read RU", run.readCharge, "write RU", run.writeCharge)
	model, reload := m.syncClone().reloadTarget()
	model, notice := model.notify(run.notice())
	return model, tea.Batch(reload, notice)
}

func (r cloneRun) written() int64 {
	var total int64
	for _, row := range r.rows {
		total += row.Written
	}
	return total
}

func (r cloneRun) skipped() int64 {
	var total int64
	for _, row := range r.rows {
		total += row.Skipped
	}
	return total
}

func (r cloneRun) notice() string {
	target := r.plan.Job.Target.String()
	if !r.items() {
		return "cloned the definition to " + target
	}
	return fmt.Sprintf("cloned %s items to %s (%s RU)", panes.FormatCount(r.written()), target,
		panes.FormatCharge(r.readCharge+r.writeCharge))
}

// reloadTarget reloads the part of the target's tree the clone added to,
// and moves the cursor onto it only when the session is on that account: a
// tree in the background is never rearranged behind the user's back.
func (m Model) reloadTarget() (Model, tea.Cmd) {
	plan := m.cloning.plan
	target := plan.Job.Target
	entry, ok := m.accounts.get(target.Account)
	if !ok || !entry.connected() {
		return m, nil
	}
	var parent []string
	if target.Container() && !plan.CreatesDatabase {
		parent = target.Path[:1]
	}
	pane := entry.pane
	if target.Account == m.accounts.active {
		pane = pane.Select(target.Path)
	}
	pane, fetch, tick := pane.RefreshPath(parent)
	entry.pane = pane
	m.accounts.put(entry)
	if !fetch.Needed {
		return m, tick
	}
	return m, tea.Batch(tick, m.load(entry, fetch))
}

// endClone stops a clone short: by x, by a failure, or for want of time. It
// keeps the slot, so the clone can be resumed or its target deleted.
func (m Model) endClone(err error) (Model, tea.Cmd) {
	run := &m.cloning
	m.closeCopy(run.copy)
	run.copy = nil
	run.end = panes.CloneFailed
	if run.stopping && errors.Is(err, context.Canceled) {
		run.end = panes.CloneStopped
		err = nil
	}
	run.err, run.stopping = err, false
	m.job.cancel = nil
	m.logger.Warn("clone ended short", "source", run.plan.Job.Source, "target", run.plan.Job.Target,
		"container", run.index, "written", run.written(), "error", err)
	return m.syncClone(), nil
}

// showCloneProgress is y while a clone holds the slot: it opens the view
// and starts nothing.
func (m Model) showCloneProgress() (Model, tea.Cmd) {
	m.overlay = overlayCloneProgress
	return m.syncClone(), nil
}

func (m Model) handleCloneProgressKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	run := m.cloning
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		if run.running() {
			m.overlay = overlayNone
			return m, nil
		}
		return m.releaseClone(), nil
	case run.running() && key.Matches(msg, m.keys.StopClone):
		m.cloning.stopping = true
		m.job.stopStep()
		return m.syncClone(), nil
	case !run.running() && run.end != panes.CloneDone && key.Matches(msg, m.keys.ResumeClone):
		return m.resumeClone()
	case run.deletable() && key.Matches(msg, m.keys.DeleteClone):
		return m.openCloneDelete(), nil
	}
	return m, nil
}

// resumeClone picks the clone up where it stopped: at the step that did
// not finish, or after the last page written in full.
func (m Model) resumeClone() (Model, tea.Cmd) {
	m.cloning.end, m.cloning.err, m.cloning.quitWarned, m.cloning.unanswered = panes.CloneRunning, nil, false, false
	m.logger.Info("clone resumed", "target", m.cloning.plan.Job.Target, "container", m.cloning.index, "position", m.cloning.position)
	return m.stepClone()
}

// releaseClone gives the slot up. Whatever the clone created stays.
func (m Model) releaseClone() Model {
	m.job = job{}
	m.cloning = cloneRun{}
	m.overlay = overlayNone
	m.statusBar = m.statusBar.SetJob("")
	return m
}

// abandonClone stops a clone the session is quitting under, and logs what
// an unfinished one leaves behind: nothing is ever deleted on the way out.
func (m Model) abandonClone() Model {
	m.job.stopStep()
	if run := m.cloning; run.end != panes.CloneDone {
		m.logger.Warn("clone left unfinished at quit", "source", run.plan.Job.Source, "target", run.plan.Job.Target,
			"written", run.written(), "skipped", run.skipped(), "read RU", run.readCharge, "write RU", run.writeCharge)
	}
	return m.releaseClone()
}

// warnBeforeQuit shows the running clone, and what quitting would leave,
// before the quit that stops it. It reports whether it did.
func (m Model) warnBeforeQuit() (Model, bool) {
	if !m.job.active() || !m.cloning.running() || m.cloning.quitWarned {
		return m, false
	}
	m.cloning.quitWarned = true
	m.overlay = overlayCloneProgress
	return m.syncClone(), true
}

// deletable reports whether a clone that ended short created what d would
// delete: the container for a container clone, the database for a database
// clone. A database made only to hold a cloned container is left alone.
func (r cloneRun) deletable() bool {
	if r.running() || r.end == panes.CloneDone {
		return false
	}
	if r.plan.Job.Target.Container() {
		return r.containerCreated
	}
	return r.databaseCreated
}

func (m Model) openCloneDelete() Model {
	target := m.cloning.plan.Job.Target
	confirm := panes.NewContainerDelete(m.icons, target.Path)
	op := OpDeleteContainer
	if !target.Container() {
		confirm, op = panes.NewDatabaseDelete(m.icons, target.Path[0]), OpDeleteDatabase
	}
	m.confirm = confirm.SetSize(m.width, m.height)
	m.managing, m.target, m.dialog = op, target.Path, m.dialog+1
	m.overlay = overlayCloneDelete
	return m
}

// handleCloneDeleteKey drives the delete of a partial target. esc goes back
// to the view it came from; the delete is the target account's, whichever
// account the session is on.
func (m Model) handleCloneDeleteKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.confirmUpdate(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyEsc:
		m.overlay = overlayCloneProgress
		return m, nil
	case tea.KeyEnter:
		return m.submitCloneDelete()
	}
	return m.confirmUpdate(msg)
}

func (m Model) submitCloneDelete() (Model, tea.Cmd) {
	if m.confirm.Submitting() || !m.confirm.Confirmed() {
		return m, nil
	}
	target := m.cloning.plan.Job.Target
	entry, _ := m.accounts.get(target.Account)
	admin := entry.permitted().Admin
	if !entry.connected() || admin == nil {
		m.confirm = m.confirm.Fail(fmt.Errorf("%s is not connected", target.Account))
		return m, nil
	}
	m.confirm = m.confirm.StartSubmitting()
	path := target.Path
	change := CatalogChangedMsg{Account: target.Account, Op: m.managing, Target: path}
	if target.Container() {
		change.Parent = path[:1]
		return m, manage(m.dialog, change, func(ctx context.Context) error { return admin.DeleteContainer(ctx, path) })
	}
	return m, manage(m.dialog, change, func(ctx context.Context) error { return admin.DeleteDatabase(ctx, path[0]) })
}

// releaseDeletedClone gives up the slot of a clone whose partial target the
// change deleted: there is nothing left to resume.
func (m Model) releaseDeletedClone(msg CatalogChangedMsg) Model {
	if m.overlay != overlayCloneDelete || msg.dialog != m.dialog {
		return m
	}
	return m.releaseClone()
}

// syncClone redraws the progress view and the status bar field from the
// clone holding the slot.
func (m Model) syncClone() Model {
	if m.job.kind != jobClone {
		return m
	}
	m.cloneProgress = m.cloneProgress.SetStatus(m.cloning.status())
	m.statusBar = m.statusBar.SetJob(m.cloning.label())
	return m
}

// label is the status bar field: both ends, and how far it has got.
func (r cloneRun) label() string {
	switch r.end {
	case panes.CloneDone:
		return "clone done (y)"
	case panes.CloneStopped:
		return "clone stopped (y)"
	case panes.CloneFailed:
		return "clone failed (y)"
	}
	job := r.plan.Job
	label := fmt.Sprintf("clone %s → %s", job.Source, job.Target.Account)
	if total := r.plan.Size(); r.items() && total.Known && total.Items > 0 {
		label += fmt.Sprintf(" %d%%", min(100, int(100*r.done()/total.Items)))
	}
	return label + " (y)"
}

// done counts every item written or skipped, in every container.
func (r cloneRun) done() int64 {
	return r.written() + r.skipped()
}

func (r cloneRun) status() panes.CloneStatus {
	job := r.plan.Job
	status := panes.CloneStatus{
		Source:      job.Source,
		Target:      job.Target,
		Phase:       r.phase(),
		Current:     r.index,
		Items:       r.items(),
		Written:     r.row().Written,
		Skipped:     r.row().Skipped,
		Estimate:    r.row().Estimate,
		Rate:        r.rate(),
		ReadCharge:  r.readCharge,
		WriteCharge: r.writeCharge,
		Projected:   r.projected(),
		Writers:     r.writers,
		MaxWriters:  job.Writers,
		Throttles:   r.throttles,
		End:         r.end,
		Err:         r.err,
		LeftBehind:  r.leftBehind(),
		CanDelete:   r.deletable(),
	}
	if !job.Source.Container() {
		status.Containers = r.rows
	}
	if r.quitWarned && r.running() {
		status.Warning = quitWarning
	}
	return status
}

// row is the container in progress; a database clone of an empty database
// has none.
func (r cloneRun) row() panes.CloneContainerRow {
	if len(r.rows) == 0 {
		return panes.CloneContainerRow{}
	}
	return r.rows[r.index]
}

func (r cloneRun) phase() string {
	switch r.end {
	case panes.CloneDone:
		return "Done."
	case panes.CloneStopped:
		return "Stopped."
	case panes.CloneFailed:
		return "Failed."
	}
	if !r.containerCreated || len(r.plan.Containers) == 0 {
		return "Creating " + r.plan.Job.Target.String()
	}
	target := r.plan.Containers[r.index].Target()
	if r.stopping {
		return "Stopping after the writes in flight…"
	}
	return "Copying items into " + strings.Join(target, ".")
}

func (r cloneRun) rate() float64 {
	var items int
	var elapsed time.Duration
	for _, page := range r.window {
		items += page.items
		elapsed += page.duration
	}
	if elapsed <= 0 {
		return 0
	}
	return float64(items) / elapsed.Seconds()
}

// projected scales what has been spent by what is left, once anything has
// been copied and the size is known.
func (r cloneRun) projected() float64 {
	total := r.plan.Size()
	done := r.done()
	if !total.Known || done == 0 {
		return 0
	}
	return (r.readCharge + r.writeCharge) / float64(done) * float64(total.Items)
}

// leftBehind says what a clone that ended short leaves on the target.
func (r cloneRun) leftBehind() string {
	target := r.plan.Job.Target
	switch {
	case r.running() || r.end == panes.CloneDone:
		return ""
	case r.unanswered:
		return fmt.Sprintf("A create on %s was sent and no answer came back, so what it was creating may exist. "+
			"Close this view and press r in the catalog to see.", target.Account)
	case !r.containerCreated && !r.databaseCreated && r.index == 0:
		return fmt.Sprintf("Nothing was created on %s.", target.Account)
	case target.Container() && !r.containerCreated:
		return fmt.Sprintf("%s/%s was created and holds nothing of the clone.", target.Account, target.Path[0])
	case target.Container():
		row := r.rows[0]
		return fmt.Sprintf("%s holds %s items and is incomplete.", target, panes.OfAbout(row.Written, row.Estimate))
	}
	return fmt.Sprintf("%s holds %d of %d containers, the last of them incomplete.",
		target, r.index+1, len(r.plan.Containers))
}
