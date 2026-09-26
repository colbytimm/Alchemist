package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	// captureStepTimeout bounds one page of a capture, so a request that
	// hangs fails the capture instead of wedging it.
	captureStepTimeout = 2 * time.Minute
	captureQuitWarning = "A snapshot is running. Quit again to cancel it and quit; nothing will be kept."
)

// capturer is a container's capture or a database's, a step at a time.
type capturer interface {
	next(ctx context.Context) (captureStep, error)
	abort() error
}

// captureStep is where a capture has got: the container in progress of a
// database snapshot, and that container's progress.
type captureStep struct {
	container string
	progress  snapshot.Progress
	done      bool
}

type containerCapture struct {
	capture *snapshot.Capture
}

func (c containerCapture) next(ctx context.Context) (captureStep, error) {
	progress, err := c.capture.Next(ctx)
	return captureStep{progress: progress, done: progress.Done}, err
}

func (c containerCapture) abort() error { return c.capture.Abort() }

type databaseCapture struct {
	capture *snapshot.GroupCapture
}

func (c databaseCapture) next(ctx context.Context) (captureStep, error) {
	progress, err := c.capture.Next(ctx)
	return captureStep{container: progress.Container, progress: progress.Capture, done: progress.Done}, err
}

func (c databaseCapture) abort() error { return c.capture.Abort() }

// captureRun is the capture holding the job slot, and once it has ended,
// its outcome until its overlay has been opened. Its capturer is held here
// only between steps; while a step runs, the step owns it.
type captureRun struct {
	account    string
	loc        snapshot.Location
	capturer   capturer
	status     panes.CaptureStatus
	throttles  int
	seen       bool
	quitWarned bool
}

func (r captureRun) started() bool { return r.account != "" }

func (r captureRun) running() bool { return r.started() && r.status.End == panes.CaptureRunning }

// reopens reports whether v should show this capture rather than the
// node under the cursor: it is running, or ended and not yet seen.
func (r captureRun) reopens() bool {
	return r.running() || r.started() && !r.seen && r.status.End != panes.CaptureCancelled
}

// promptCapture is s in the catalog: the snapshots overlay of the node
// under the cursor, asking for a note. Another job running refuses it.
func (m Model) promptCapture() (Model, tea.Cmd) {
	if m.jobRunning() {
		return m.notify(m.job.waitText("snapshots"))
	}
	model, cmd := m.openSnapshots()
	if model.overlay != overlaySnapshots {
		return model, cmd
	}
	model.snapshotsPane = model.snapshotsPane.PromptNote()
	return model, cmd
}

// startCapture takes the job slot for a snapshot of the store on screen,
// and begins it. The capture holds its account's connection and name from
// here on, whichever account the session moves to.
func (m Model) startCapture(note string) (Model, tea.Cmd) {
	browsing := m.browsing
	entry, ok := m.accounts.get(browsing.account)
	if !ok || !entry.connected() || entry.management.Scanner == nil {
		m.snapshotsPane = m.snapshotsPane.SetNotice(fmt.Sprintf("%s is not connected", browsing.account))
		return m, nil
	}
	m, retired := m.retireEndedJob()
	m.lastJob++
	m.job = job{kind: jobCapture, id: m.lastJob, accounts: []string{browsing.account}}
	m.capturing = captureRun{account: browsing.account, loc: browsing.loc, seen: true}
	ctx, cancel := context.WithTimeout(context.Background(), captureStepTimeout)
	m.job.cancel = cancel
	options := snapshot.CaptureOptions{Note: note, MaxItems: entry.account.SnapshotMaxItems}
	m.logger.Info("snapshot started", "account", browsing.account, "store", browsing.loc)
	return m.syncCapture(), tea.Batch(retired, beginCapture(ctx, cancel, m.job.id, entry, browsing.loc, options))
}

// beginCapture opens the store and takes its lock, finding a database's
// containers in the catalog first.
func beginCapture(ctx context.Context, cancel context.CancelFunc, id jobID, entry accountEntry, loc snapshot.Location, options snapshot.CaptureOptions) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		sources, err := captureSources(ctx, entry, loc)
		if err != nil {
			return SnapshotProgressMsg{Err: err, job: id}
		}
		if loc.Container == "" {
			group := snapshot.GroupSource{Containers: sources, Throughput: entry.management.Throughput}
			capture, err := snapshot.BeginGroup(loc, group, options)
			if err != nil {
				return SnapshotProgressMsg{Err: err, job: id}
			}
			return SnapshotProgressMsg{capturer: databaseCapture{capture: capture}, job: id}
		}
		store, err := snapshot.Open(loc)
		if err != nil {
			return SnapshotProgressMsg{Err: err, job: id}
		}
		capture, err := store.Begin(sources[0], options)
		if err != nil {
			return SnapshotProgressMsg{Err: err, job: id}
		}
		return SnapshotProgressMsg{capturer: containerCapture{capture: capture}, job: id}
	}
}

// captureSources reads the database's containers from the catalog: all of
// them for a database snapshot, or the one named.
func captureSources(ctx context.Context, entry accountEntry, loc snapshot.Location) ([]snapshot.Source, error) {
	database := adapter.Node{Kind: adapter.NodeDatabase, Name: loc.Database, Path: []string{loc.Database}}
	nodes, err := entry.catalog.Children(ctx, database)
	if err != nil {
		return nil, err
	}
	var sources []snapshot.Source
	for _, node := range nodes {
		if node.Kind != adapter.NodeContainer || loc.Container != "" && node.Name != loc.Container {
			continue
		}
		sources = append(sources, snapshot.Source{
			Container:     node.Path,
			PartitionKeys: strings.Split(node.Meta[adapter.MetaPartitionKey], adapter.PartitionKeyPathSeparator),
			Items:         entry.management.Scanner,
			Definitions:   entry.management.Definitions,
			Throughput:    entry.management.Throughput,
		})
	}
	if len(sources) == 0 && loc.Container != "" {
		return nil, fmt.Errorf("%s has no container %s", loc.Database, loc.Container)
	}
	return sources, nil
}

// stepCapture runs one step. A step cancelled under it, by x or by a quit,
// aborts its capture itself, releasing the store's lock even when the
// session has gone and its message is never read.
func stepCapture(ctx context.Context, cancel context.CancelFunc, id jobID, c capturer) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		step, err := c.next(ctx)
		if errors.Is(ctx.Err(), context.Canceled) {
			err = errors.Join(err, c.abort())
		}
		return SnapshotProgressMsg{Step: step, Err: err, capturer: c, job: id}
	}
}

// acceptCaptureStep takes a step's capturer back and issues the next step,
// waits out a throttle, or ends the capture. A step of a capture that is
// over has its capturer aborted, which releases the store's lock.
func (m Model) acceptCaptureStep(msg SnapshotProgressMsg) (Model, tea.Cmd) {
	if m.job.kind != jobCapture || m.job.id != msg.job {
		m.dropCapturer(msg.capturer, msg.Step)
		return m, nil
	}
	m.job.cancel = nil
	run := &m.capturing
	run.capturer = msg.capturer
	var throttled *adapter.ThrottledError
	switch {
	case errors.As(msg.Err, &throttled) && run.throttles < snapshot.MaxThrottles:
		run.throttles++
		m.logger.Warn("snapshot page throttled", "store", run.loc, "wait", throttled.RetryAfter, "in a row", run.throttles)
		return m.syncCapture(), waitForRetry(m.job.id, throttled.Wait())
	case msg.Err != nil:
		return m.endCapture(panes.CaptureFailed, msg.Err)
	}
	run.throttles = 0
	run.status.Container, run.status.Progress = msg.Step.container, msg.Step.progress
	if msg.Step.done {
		return m.endCapture(panes.CaptureDone, nil)
	}
	return m.nextCaptureStep()
}

// dropCapturer ends the capturer of a step that came back after its
// capture was cancelled or abandoned.
func (m Model) dropCapturer(c capturer, step captureStep) {
	if c == nil {
		return
	}
	if step.done {
		m.logger.Warn("a snapshot finished as it was cancelled, and was kept")
	}
	if err := c.abort(); err != nil {
		m.logger.Error("release a cancelled snapshot", "error", err)
	}
}

func waitForRetry(id jobID, wait time.Duration) tea.Cmd {
	return tea.Tick(wait, func(time.Time) tea.Msg { return snapshotRetryMsg{job: id} })
}

func (m Model) retryCaptureStep(msg snapshotRetryMsg) (Model, tea.Cmd) {
	if m.job.kind != jobCapture || m.job.id != msg.job || m.capturing.capturer == nil {
		return m, nil
	}
	return m.nextCaptureStep()
}

func (m Model) nextCaptureStep() (Model, tea.Cmd) {
	c := m.capturing.capturer
	m.capturing.capturer = nil
	ctx, cancel := context.WithTimeout(context.Background(), captureStepTimeout)
	m.job.cancel = cancel
	return m.syncCapture(), stepCapture(ctx, cancel, m.job.id, c)
}

// endCapture gives up the slot. A failed capture is aborted, so nothing it
// wrote is listed; a finished one has published and released its lock.
func (m Model) endCapture(end panes.CaptureEnd, err error) (Model, tea.Cmd) {
	run := &m.capturing
	if end != panes.CaptureDone {
		m.abortHeld()
	}
	run.capturer = nil
	run.status.End, run.status.Err, run.status.Warning = end, err, ""
	run.seen = m.showingCapture()
	m.job = job{}
	switch end {
	case panes.CaptureDone:
		m.logger.Info("snapshot taken", "store", run.loc, "items", run.status.Progress.Items, "RU", run.status.Progress.RequestCharge)
	case panes.CaptureFailed:
		m.logger.Error("snapshot failed", "store", run.loc, "error", err)
	}
	m = m.syncCapture()
	if m.showingCapture() {
		return m.reloadSnapshots()
	}
	return m, nil
}

func (m Model) abortHeld() {
	if c := m.capturing.capturer; c != nil {
		if err := c.abort(); err != nil {
			m.logger.Error("abort a snapshot", "store", m.capturing.loc, "error", err)
		}
	}
}

// cancelCapture is x in the overlay: the step in flight is cancelled, and
// its capturer aborted when it comes back; nothing is published.
func (m Model) cancelCapture() (Model, tea.Cmd) {
	m.job.stopStep()
	m.logger.Info("snapshot cancelled", "store", m.capturing.loc)
	return m.endCapture(panes.CaptureCancelled, nil)
}

// abandonCapture cancels a capture the session is quitting under.
func (m Model) abandonCapture() Model {
	m.job.stopStep()
	m.abortHeld()
	m.logger.Warn("snapshot cancelled at quit: nothing was kept", "store", m.capturing.loc)
	m.capturing = captureRun{}
	m.job = job{}
	return m.syncCapture()
}

// showCapture is v while a capture runs, or has ended unseen: its overlay,
// on whichever account it runs.
func (m Model) showCapture() (Model, tea.Cmd) {
	run := m.capturing
	m.capturing.seen = true
	return m.openSnapshotsOf(run.account, run.loc)
}

// showingCapture reports whether the overlay on screen is the capture's.
func (m Model) showingCapture() bool {
	return m.overlay == overlaySnapshots && m.capturing.started() &&
		m.browsing.account == m.capturing.account && m.browsing.loc == m.capturing.loc
}

// syncCapture puts the capture in its overlay's top row, and its field in
// the status bar, which belongs to the job and not to an account.
func (m Model) syncCapture() Model {
	m.statusBar = m.statusBar.SetJob(m.capturing.label())
	if m.browsing.account == m.capturing.account && m.browsing.loc == m.capturing.loc && m.capturing.started() {
		status := m.capturing.status
		m.snapshotsPane = m.snapshotsPane.SetCapture(&status)
	} else {
		m.snapshotsPane = m.snapshotsPane.SetCapture(nil)
	}
	return m
}

// label is the status bar field: where the capture is, and how far it has
// got; once it ends, how it ended, until its overlay has been opened.
func (r captureRun) label() string {
	if !r.started() || r.seen && !r.running() {
		return ""
	}
	switch r.status.End {
	case panes.CaptureDone:
		return "snapshot done (v)"
	case panes.CaptureFailed:
		return "snapshot failed (v)"
	case panes.CaptureCancelled:
		return ""
	}
	label := "snapshot " + r.loc.String()
	p := r.status.Progress
	if p.Expected > 0 && (p.Phase == snapshot.PhaseScan || p.Phase == snapshot.PhaseSweep) {
		label += fmt.Sprintf(" %d%%", min(99, int(100*p.PhaseItems/p.Expected)))
	}
	return label + " (v)"
}
