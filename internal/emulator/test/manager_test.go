package emulator_test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/emulator"
)

var imagePresent = reply{out: "sha256:aaa\n"}

func manager(exec *fakeExec) emulator.Manager {
	return emulator.NewManager(docker(exec), nil, io.Discard)
}

// freePort is a port nothing listens on, as far as this machine knows.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func TestStartRunsAnAbsentContainerFromThePresentImage(t *testing.T) {
	exec := newFakeExec().on("container inspect", noSuchContainer).on("image ls", imagePresent)
	port := freePort(t)

	got, err := manager(exec).Start(context.Background(), emulator.StartOptions{Port: port})

	require.NoError(t, err)
	assert.Equal(t, port, got)
	assert.Equal(t, []string{"container inspect", "image ls", "run"}, exec.verbs())
}

func TestStartPullsAMissingImage(t *testing.T) {
	exec := newFakeExec().on("container inspect", noSuchContainer).on("image ls", reply{})

	_, err := manager(exec).Start(context.Background(), emulator.StartOptions{Port: freePort(t)})

	require.NoError(t, err)
	assert.Equal(t, []string{"container inspect", "image ls", "pull", "run"}, exec.verbs())
}

func TestStartStartsAStoppedContainer(t *testing.T) {
	for _, state := range []string{"created", "exited"} {
		t.Run(state, func(t *testing.T) {
			exec := newFakeExec().on("container inspect", inspected(state, true, 8081, "sha256:aaa"))

			got, err := manager(exec).Start(context.Background(), emulator.StartOptions{})

			require.NoError(t, err)
			assert.Equal(t, 8081, got)
			assert.Equal(t, []string{"container inspect", "start"}, exec.verbs())
		})
	}
}

func TestStartLeavesARunningContainerRunning(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 9081, "sha256:aaa"))

	got, err := manager(exec).Start(context.Background(), emulator.StartOptions{})

	require.NoError(t, err)
	assert.Equal(t, 9081, got, "the published port is kept when none is asked for")
	assert.Equal(t, []string{"container inspect"}, exec.verbs())
}

func TestStartRecreateKeepsTheVolume(t *testing.T) {
	exec := newFakeExec().
		on("container inspect", inspected("running", true, 8081, "sha256:aaa")).
		on("image ls", imagePresent)
	port := freePort(t)

	_, err := manager(exec).Start(context.Background(), emulator.StartOptions{Port: port, Recreate: true})

	require.NoError(t, err)
	assert.Equal(t, []string{"container inspect", "rm", "image ls", "run"}, exec.verbs())
	assert.Equal(t, []string{"rm", "--force", emulator.ContainerName}, exec.calls[1])
}

func TestStartPullRecreatesOnlyForANewImage(t *testing.T) {
	tests := []struct {
		name      string
		pulledID  string
		wantVerbs []string
	}{
		{name: "same image", pulledID: "sha256:aaa\n", wantVerbs: []string{"container inspect", "pull", "image ls"}},
		{name: "new image", pulledID: "sha256:bbb\n", wantVerbs: []string{"container inspect", "pull", "image ls", "rm", "image ls", "run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := freePort(t)
			exec := newFakeExec().
				on("container inspect", inspected("running", true, port, "sha256:aaa")).
				on("image ls", reply{out: tt.pulledID})

			_, err := manager(exec).Start(context.Background(), emulator.StartOptions{Port: port, Pull: true})

			require.NoError(t, err)
			assert.Equal(t, tt.wantVerbs, exec.verbs())
		})
	}
}

func TestStartRefusesToMoveAPortWithoutRecreate(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 8081, "sha256:aaa"))

	_, err := manager(exec).Start(context.Background(), emulator.StartOptions{Port: 9081})

	require.EqualError(t, err, "emulator: the container publishes port 8081: pass --recreate to move it to 9081")
	assert.Equal(t, []string{"container inspect"}, exec.verbs())
}

func TestStartRefusesAContainerItDidNotCreate(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("exited", false, 8081, "sha256:aaa"))

	_, err := manager(exec).Start(context.Background(), emulator.StartOptions{Recreate: true})

	require.ErrorIs(t, err, emulator.ErrNotManaged)
	assert.Contains(t, err.Error(), "remove it with docker rm -f alchemist-cosmos-emulator, then run start again")
	assert.Equal(t, []string{"container inspect"}, exec.verbs())
}

func TestStartRefusesATakenPortBeforeRunning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	exec := newFakeExec().on("container inspect", noSuchContainer).on("image ls", reply{})

	_, err = manager(exec).Start(context.Background(), emulator.StartOptions{Port: listener.Addr().(*net.TCPAddr).Port})

	require.ErrorIs(t, err, emulator.ErrPortInUse)
	assert.Contains(t, err.Error(), "pass --port to use another")
	assert.NotContains(t, exec.verbs(), "run")
	assert.NotContains(t, exec.verbs(), "pull")
}

func TestStopLeavesTheData(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", true, 8081, "sha256:aaa"))

	_, err := manager(exec).Stop(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"container inspect", "stop"}, exec.verbs())
}

func TestStopRefusesAContainerItDidNotCreate(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", false, 8081, "sha256:aaa"))

	_, err := manager(exec).Stop(context.Background())

	require.ErrorIs(t, err, emulator.ErrNotManaged)
	assert.Equal(t, []string{"container inspect"}, exec.verbs())
}

func TestRemoveDeletesOnlyWhatItIsAskedTo(t *testing.T) {
	tests := []struct {
		name      string
		options   emulator.RemoveOptions
		wantVerbs []string
	}{
		{name: "the container", wantVerbs: []string{"container inspect", "rm"}},
		{
			name:      "and its data",
			options:   emulator.RemoveOptions{Data: true},
			wantVerbs: []string{"container inspect", "rm", "volume ls", "volume rm"},
		},
		{
			name:      "and the image",
			options:   emulator.RemoveOptions{Image: true},
			wantVerbs: []string{"container inspect", "rm", "image ls", "image rm"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := newFakeExec().
				on("container inspect", inspected("running", true, 8081, "sha256:aaa")).
				on("volume ls", reply{out: "other\n" + emulator.VolumeName + "\n"}).
				on("image ls", imagePresent)

			_, err := manager(exec).Remove(context.Background(), tt.options)

			require.NoError(t, err)
			assert.Equal(t, tt.wantVerbs, exec.verbs())
		})
	}
}

func TestRemoveRefusesAContainerItDidNotCreate(t *testing.T) {
	exec := newFakeExec().on("container inspect", inspected("running", false, 8081, "sha256:aaa"))

	_, err := manager(exec).Remove(context.Background(), emulator.RemoveOptions{Data: true, Image: true})

	require.ErrorIs(t, err, emulator.ErrNotManaged)
	assert.Equal(t, []string{"container inspect"}, exec.verbs())
}

func TestRemoveWithNothingThereRemovesNothing(t *testing.T) {
	exec := newFakeExec().on("container inspect", noSuchContainer).on("volume ls", reply{}).on("image ls", reply{})

	removed, err := manager(exec).Remove(context.Background(), emulator.RemoveOptions{Data: true, Image: true})

	require.NoError(t, err)
	assert.Equal(t, emulator.Removed{}, removed)
	assert.Equal(t, []string{"container inspect", "volume ls", "image ls"}, exec.verbs())
}
