package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// jobKind is what a background job does; the zero value is no job at all.
// Every kind is minutes of request units against an account behind a view
// that can be hidden, and a session runs one of them at a time.
type jobKind int

const (
	jobNone jobKind = iota
	jobClone
	jobCapture
)

func (k jobKind) String() string {
	switch k {
	case jobClone:
		return "clone"
	case jobCapture:
		return "snapshot"
	}
	return "job"
}

// reopenKey is the catalog key that shows a job of kind k.
func (k jobKind) reopenKey() string {
	if k == jobCapture {
		return "v"
	}
	return "y"
}

// stopVerb is what the job's view calls ending it.
func (k jobKind) stopVerb() string {
	if k == jobCapture {
		return "cancel"
	}
	return "stop"
}

// jobID identifies one job. Every message its steps send carries it, so a
// step of a job that is over is recognized and dropped.
type jobID int

// job is the session's one background job. It is held from the
// confirmation that starts it until its ended view is closed, so one that
// stopped short and can still be resumed keeps every other job out.
type job struct {
	kind jobKind
	id   jobID
	// accounts are the ones the job holds connections to, which the
	// switcher refuses to disconnect.
	accounts []string
	target   writeTarget
	cancel   context.CancelFunc
}

// writeTarget is where a job writes: a container, or a whole database. The
// zero value is a job that only reads.
type writeTarget struct {
	account string
	path    []string
}

func (j job) active() bool { return j.kind != jobNone }

// writes reports the account and the path the job writes at or under; ok
// is false for a job that only reads.
func (j job) writes() (account string, path []string, ok bool) {
	if j.target.account == "" {
		return "", nil, false
	}
	return j.target.account, j.target.path, true
}

// writesTo reports whether the job writes to path on account: path is its
// target or under it, or holds it, as a database holds the container a job
// fills.
func (j job) writesTo(account string, path []string) bool {
	target, prefix, ok := j.writes()
	return ok && target == account && (within(prefix, path) || within(path, prefix))
}

func (j job) uses(account string) bool {
	return j.active() && slices.Contains(j.accounts, account)
}

func (j job) stopStep() {
	if j.cancel != nil {
		j.cancel()
	}
}

func (j job) usingText(account string) string {
	return fmt.Sprintf("a %s is using %s: %s it first (%s in the catalog)", j.kind, account, j.kind.stopVerb(), j.kind.reopenKey())
}

func (j job) writingText(path []string) string {
	return fmt.Sprintf("a %s is writing %s: %s it first (%s in the catalog)", j.kind, strings.Join(path, "."), j.kind.stopVerb(), j.kind.reopenKey())
}

// waitText refuses another job while this one runs: "a snapshot is
// running: clones wait for it (v)".
func (j job) waitText(others string) string {
	return fmt.Sprintf("a %s is running: %s wait for it (%s)", j.kind, others, j.kind.reopenKey())
}

// warnBeforeQuit shows the running job, and what quitting would leave,
// before the quit that stops it. It reports whether it did, with what the
// view it opened needs to load.
func (m Model) warnBeforeQuit() (Model, tea.Cmd, bool) {
	switch {
	case m.job.kind == jobClone && m.cloning.running() && !m.cloning.quitWarned:
		m.cloning.quitWarned = true
		m.overlay = overlayCloneProgress
		return m.syncClone(), nil, true
	case m.job.kind == jobCapture && !m.capturing.quitWarned:
		m.capturing.quitWarned = true
		m.capturing.status.Warning = captureQuitWarning
		model, load := m.showCapture()
		return model, load, true
	}
	return m, nil, false
}
