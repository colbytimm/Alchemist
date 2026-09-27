package emulator_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/emulator"
)

var errStarting = errors.New("503 Service Unavailable: pgcosmos extension is still starting")

const statusLog = "2026-09-27 18:24:55: PostgreSQL=OK, Gateway=OK, Explorer=OK\n"

// probeFailing fails its first n calls with errStarting, then succeeds.
func probeFailing(n int) (emulator.Probe, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		if calls <= n {
			return errStarting
		}
		return nil
	}, &calls
}

func quickManager(exec *fakeExec, probe emulator.Probe, progress *bytes.Buffer) emulator.Manager {
	m := emulator.NewManager(docker(exec), probe, progress)
	m.PollInterval = time.Millisecond
	m.ProgressInterval = time.Millisecond
	return m
}

func TestWaitReturnsOnceTheProbeAnswers(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 8081, "sha256:aaa")).on("logs", reply{out: statusLog})
	probe, calls := probeFailing(2)
	var progress bytes.Buffer

	err := quickManager(exec, probe, &progress).Wait(context.Background(), "http://localhost:8081", time.Minute)

	require.NoError(t, err)
	assert.Equal(t, 3, *calls)
	assert.Contains(t, progress.String(), "waiting for the emulator (0:00): PostgreSQL=OK, Gateway=OK")
}

func TestWaitTimesOutNamingWhatToDo(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 8081, "sha256:aaa")).on("logs", reply{out: statusLog})
	probe, _ := probeFailing(1 << 30)

	err := quickManager(exec, probe, &bytes.Buffer{}).Wait(context.Background(), "http://localhost:8081", 20*time.Millisecond)

	var notReady *emulator.NotReadyError
	require.ErrorAs(t, err, &notReady)
	for _, want := range []string{
		"emulator: no answer from http://localhost:8081 after",
		"last answer: " + errStarting.Error(),
		"last status: PostgreSQL=OK, Gateway=OK",
		"alchemist emulator start --recreate",
		"alchemist emulator remove --data && alchemist emulator start",
	} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestWaitFailsAtOnceWhenTheContainerExits(t *testing.T) {
	exec := newFakeExec().on("container inspect",
		inspected("running", true, 8081, "sha256:aaa"),
		inspected("exited", true, 8081, "sha256:aaa"))
	probe, calls := probeFailing(1 << 30)

	err := quickManager(exec, probe, &bytes.Buffer{}).Wait(context.Background(), "http://localhost:8081", time.Minute)

	var exited *emulator.ExitedError
	require.ErrorAs(t, err, &exited)
	assert.Equal(t, "emulator: the container exited (code 1): see alchemist emulator logs", err.Error())
	assert.Equal(t, 2, *calls)
}

func TestProbeWithinGivesUpOnAProbeThatIgnoresItsDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stuck := emulator.Probe(func(context.Context) error {
		<-release
		return nil
	})

	err := stuck.Within(context.Background(), 10*time.Millisecond)

	require.EqualError(t, err, "emulator: no answer within 10ms")
}

func TestWaitStopsWhenCancelled(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 8081, "sha256:aaa"))
	probe, _ := probeFailing(1 << 30)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := quickManager(exec, probe, &bytes.Buffer{}).Wait(ctx, "http://localhost:8081", time.Minute)

	require.ErrorIs(t, err, context.Canceled)
}
