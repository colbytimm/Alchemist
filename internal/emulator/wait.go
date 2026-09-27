package emulator

import (
	"context"
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
	lastProgress := started
	var lastStatus string
	for {
		answer := m.attempt(ctx)
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
		waited := time.Since(started)
		if waited >= timeout {
			return &NotReadyError{Endpoint: endpoint, Waited: timeout, LastAnswer: answer.Error(), LastStatus: lastStatus}
		}
		if time.Since(lastProgress) >= m.ProgressInterval {
			m.showProgress(waited, lastStatus)
			lastProgress = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.PollInterval):
		}
	}
}

func (m Manager) attempt(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, m.AttemptTimeout)
	defer cancel()
	return m.Probe(ctx)
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
