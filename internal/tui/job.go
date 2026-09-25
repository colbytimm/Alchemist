package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// jobKind is what a background job does; the zero value is no job at all.
// Every kind is minutes of request units against an account behind a view
// that can be hidden, and a session runs one of them at a time.
type jobKind int

const (
	jobNone jobKind = iota
	jobClone
)

func (k jobKind) String() string {
	if k == jobClone {
		return "clone"
	}
	return "job"
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
	// cancel stops the step in flight.
	cancel context.CancelFunc
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

// usingText refuses to disconnect an account the job holds.
func (j job) usingText(account string) string {
	return fmt.Sprintf("a %s is using %s: stop it first (y in the catalog)", j.kind, account)
}

// writingText refuses a change to what the job writes.
func (j job) writingText(path []string) string {
	return fmt.Sprintf("a %s is writing %s: stop it first (y in the catalog)", j.kind, strings.Join(path, "."))
}
