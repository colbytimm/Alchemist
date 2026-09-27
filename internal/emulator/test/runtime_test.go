package emulator_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/emulator"
)

// onPath makes PATH a directory holding an executable for each name, and
// nothing else.
func onPath(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binaries are shell-style executables")
	}
	dir := t.TempDir()
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755))
	}
	t.Setenv("PATH", dir)
	t.Setenv(emulator.RuntimeEnvVar, "")
}

func TestDetectRuntimeOrder(t *testing.T) {
	tests := []struct {
		name   string
		onPath []string
		env    string
		flag   string
		want   string
	}{
		{name: "docker when both are installed", onPath: []string{"docker", "podman"}, want: "docker"},
		{name: "podman when docker is not", onPath: []string{"podman"}, want: "podman"},
		{name: "the environment beats the default", onPath: []string{"docker", "podman"}, env: "podman", want: "podman"},
		{name: "the flag beats the environment", onPath: []string{"docker", "podman"}, env: "podman", flag: "docker", want: "docker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			onPath(t, tt.onPath...)
			t.Setenv(emulator.RuntimeEnvVar, tt.env)

			got, err := emulator.DetectRuntime(tt.flag)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestDetectRuntimeWithNeitherInstalled(t *testing.T) {
	onPath(t)

	_, err := emulator.DetectRuntime("")

	require.ErrorIs(t, err, emulator.ErrNoRuntime)
	assert.Contains(t, err.Error(), "install Docker Desktop")
	assert.Contains(t, err.Error(), "Podman")
}

func TestDetectRuntimeNamesAMissingChoice(t *testing.T) {
	onPath(t, "docker")

	_, err := emulator.DetectRuntime("podman")

	require.EqualError(t, err, "emulator: podman is not on PATH")
}

func TestDetectRuntimeRefusesAnyOtherRuntime(t *testing.T) {
	onPath(t, "docker", "nerdctl")

	_, err := emulator.DetectRuntime("nerdctl")

	require.ErrorIs(t, err, emulator.ErrUnknownRuntime)
}

func TestCheckExplainsARuntimeThatIsNotRunning(t *testing.T) {
	tests := []struct {
		runtime string
		advice  string
	}{
		{runtime: "docker", advice: "docker is installed but not running: start Docker Desktop or the docker service, then try again"},
		{runtime: "podman", advice: "podman cannot reach its machine: run podman machine start, then try again"},
	}
	for _, tt := range tests {
		t.Run(tt.runtime, func(t *testing.T) {
			exec := newFakeExec().on("info", failure("Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\nmore"))

			err := emulator.Runtime{Name: tt.runtime, Exec: exec}.Check(context.Background())

			var down *emulator.RuntimeDownError
			require.ErrorAs(t, err, &down)
			assert.Equal(t, "emulator: "+tt.advice+": Cannot connect to the Docker daemon at unix:///var/run/docker.sock.", err.Error())
		})
	}
}

func TestRunPublishesOnLoopbackWithTheDataVolume(t *testing.T) {
	tests := []struct {
		port    int
		publish string
	}{
		{port: 8081, publish: "127.0.0.1:8081:8081"},
		{port: 9081, publish: "127.0.0.1:9081:8081"},
	}
	for _, tt := range tests {
		t.Run(tt.publish, func(t *testing.T) {
			exec := newFakeExec()

			require.NoError(t, docker(exec).Run(context.Background(), tt.port))

			assert.Equal(t, [][]string{{
				"run", "--detach",
				"--name", "alchemist-cosmos-emulator",
				"--label", "dev.alchemist.emulator=1",
				"--publish", tt.publish,
				"--volume", "alchemist-cosmos-emulator-data:/data",
				"--env", "ENABLE_TELEMETRY=false",
				"--env", "ENABLE_EXPLORER=false",
				"mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview",
			}}, exec.calls)
		})
	}
}

func TestInspect(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  emulator.Container
	}{
		{name: "absent", reply: noSuchContainer, want: emulator.Container{State: emulator.StateAbsent}},
		{
			name:  "created",
			reply: inspected("created", true, 8081, "sha256:aaa"),
			want:  emulator.Container{State: "created", ImageID: "aaa", HostPort: 8081, Managed: true},
		},
		{
			name:  "running",
			reply: inspected("running", true, 9081, "sha256:aaa"),
			want:  emulator.Container{State: "running", ImageID: "aaa", HostPort: 9081, Managed: true},
		},
		{
			name:  "exited",
			reply: inspected("exited", true, 8081, "sha256:aaa"),
			want:  emulator.Container{State: "exited", ExitCode: 1, ImageID: "aaa", HostPort: 8081, Managed: true},
		},
		{
			name:  "without the label",
			reply: inspected("running", false, 8081, "sha256:aaa"),
			want:  emulator.Container{State: "running", ImageID: "aaa", HostPort: 8081},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := docker(newFakeExec().on("container inspect", tt.reply)).Inspect(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInspectPassesOnOtherFailures(t *testing.T) {
	_, err := docker(newFakeExec().on("container inspect", failure("permission denied"))).Inspect(context.Background())

	require.ErrorContains(t, err, "permission denied")
}

func TestParseStatusLine(t *testing.T) {
	log := "starting\n" +
		"2026-09-27 18:24:50: PostgreSQL=FAIL, Gateway=FAIL, Explorer=OK\n" +
		"2026-09-27 18:24:55: PostgreSQL=OK, Gateway=FAIL, Explorer=OK\n" +
		"some other line\n"

	got, ok := emulator.ParseStatusLine(log)

	require.True(t, ok)
	assert.Equal(t, "PostgreSQL=OK, Gateway=FAIL", got)
}

func TestParseStatusLineWithoutOne(t *testing.T) {
	_, ok := emulator.ParseStatusLine("starting\n")

	assert.False(t, ok)
}
