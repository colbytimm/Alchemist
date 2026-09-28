package emulator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NotReadyError is an emulator that never answered the probe.
type NotReadyError struct {
	Endpoint   string
	Waited     time.Duration
	LastAnswer string
	// LastStatus is the log's last status line, empty when it had none.
	LastStatus string
}

func (e *NotReadyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "emulator: no answer from %s after %s\n", e.Endpoint, e.Waited.Round(time.Second))
	fmt.Fprintf(&b, "last answer: %s\n", e.LastAnswer)
	if e.LastStatus != "" {
		fmt.Fprintf(&b, "last status: %s\n", e.LastStatus)
	}
	b.WriteString("The emulator can stay like this after Docker restarts. Recreate the container, keeping your data:\n" +
		"  alchemist emulator start --recreate\n" +
		"If it is still not ready, start again with no data:\n" +
		"  alchemist emulator remove --data && alchemist emulator start")
	return b.String()
}

// ExitedError is a container that stopped while it was waited on.
type ExitedError struct {
	Code int
}

func (e *ExitedError) Error() string {
	return fmt.Sprintf("emulator: the container exited (code %d): see alchemist emulator logs", e.Code)
}

// Wait polls the probe until it answers, the container stops, or timeout
// passes. Readiness is the probe's answer alone: the container's state and
// the log's status line only explain a wait that fails.
func (m Manager) Wait(ctx context.Context, endpoint string, timeout time.Duration) error {
	started := time.Now()
	deadline := started.Add(timeout)
	lastProgress := started
	var lastStatus string
	for {
		answer := m.Probe.Within(ctx, m.attemptBudget(deadline))
		if answer == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c, err := m.Runtime.Inspect(ctx)
		if err == nil && c.Stopped() {
			return &ExitedError{Code: c.ExitCode}
		}
		if line, ok := m.Runtime.StatusLine(ctx); ok {
			lastStatus = line
		}
		if time.Until(deadline) <= m.PollInterval {
			return &NotReadyError{Endpoint: endpoint, Waited: timeout, LastAnswer: answer.Error(), LastStatus: lastStatus}
		}
		if time.Since(lastProgress) >= m.ProgressInterval {
			m.showProgress(time.Since(started), lastStatus)
			lastProgress = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.PollInterval):
		}
	}
}

// attemptBudget keeps an attempt inside the wait's deadline, but never shorter
// than a poll, so a late timer cannot leave it no time at all.
func (m Manager) attemptBudget(deadline time.Time) time.Duration {
	return max(min(m.AttemptTimeout, time.Until(deadline)), m.PollInterval)
}

// Within makes one attempt and gives up on it after timeout, even when the
// probe does not: the Cosmos SDK reads the account's properties under a
// deadline of its own, about a minute against a container that accepts
// connections and never answers. The abandoned attempt ends on that deadline.
func (p Probe) Within(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	answer := make(chan error, 1)
	go func() { answer <- p(ctx) }()
	select {
	case err := <-answer:
		return err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("emulator: no answer within %s", timeout)
		}
		return ctx.Err()
	}
}

func (m Manager) showProgress(waited time.Duration, status string) {
	line := fmt.Sprintf("waiting for the emulator (%d:%02d)", int(waited.Minutes()), int(waited.Seconds())%60)
	if status != "" {
		line += ": " + status
	}
	// Progress is for the person watching; a terminal that cannot take it
	// changes nothing about the wait.
	_, _ = fmt.Fprintln(m.Progress, line)
}
