package cmd_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/emulator"
	"github.com/colbytimm/alchemist/internal/tui"
)

// addLocalMockProfile adds a profile the emulator commands accept: a local
// endpoint and the well-known key, served by the mock adapter.
func (h harness) addLocalMockProfile(t *testing.T, name string) {
	t.Helper()
	_, err := h.run("", "profile", "add", name, "--adapter", mock.Name,
		"--endpoint", "http://localhost:8081", "--well-known-key")
	require.NoError(t, err)
}

func TestBareEmulatorWithoutAProfileSaysHowToMakeOne(t *testing.T) {
	err := newHarness(t).launch("emulator")

	require.Error(t, err)
	assert.NotErrorIs(t, err, tea.ErrProgramKilled, "the TUI must not start")
	assert.Contains(t, err.Error(), "no emulator profile: run alchemist emulator start")
}

func TestBareEmulatorOpensTheEmulatorProfile(t *testing.T) {
	h := newHarness(t)
	h.addLocalMockProfile(t, "emulator")

	err := h.launch("emulator", "--read-only", "--ascii")

	require.ErrorIs(t, err, tea.ErrProgramKilled)
}

func TestEmulatorSeedReportsEveryContainer(t *testing.T) {
	h := newHarness(t)
	h.addLocalMockProfile(t, "local")

	out, err := h.run("", "emulator", "seed", "--profile", "local", "--replace")

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 9)
	assert.Contains(t, lines, "sales.orders: 60 items, partitioned on /customerId")
}

// The mock adapter's own fixture holds sales and telemetry.
func TestEmulatorSeedWithoutReplaceRefusesExistingDatabases(t *testing.T) {
	h := newHarness(t)
	h.addLocalMockProfile(t, "local")

	out, err := h.run("", "emulator", "seed", "--profile", "local")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales and telemetry exist: pass --replace to drop and recreate them")
	assert.Empty(t, out)
}

// withoutContainerRuntimes leaves PATH with neither docker nor podman.
func withoutContainerRuntimes(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv(emulator.RuntimeEnvVar, "")
}

func TestEmulatorStartWithoutARuntimeSaysWhatToInstall(t *testing.T) {
	h := newHarness(t)
	withoutContainerRuntimes(t)

	_, err := h.run("", "emulator", "start")

	require.ErrorIs(t, err, emulator.ErrNoRuntime)
	assert.Contains(t, err.Error(), "install Docker Desktop")
	assert.Contains(t, err.Error(), "Podman")
}

func TestEmulatorStatusReportsWhatItCanWithoutARuntime(t *testing.T) {
	h := newHarness(t)
	withoutContainerRuntimes(t)
	h.addLocalMockProfile(t, "emulator")

	out, err := h.run("", "emulator", "status")

	require.NoError(t, err)
	assert.Contains(t, out, "found neither docker nor podman on PATH")
	assert.Contains(t, out, "http://localhost:8081: ready")
	assert.Contains(t, out, "emulator (key: well-known)")
}

func TestEmulatorStatusWithoutAProfile(t *testing.T) {
	h := newHarness(t)
	withoutContainerRuntimes(t)

	out, err := h.run("", "emulator", "status")

	require.NoError(t, err)
	assert.Contains(t, out, "none: run alchemist emulator start")
}

func TestEmulatorRuntimeCommandsRefuseAnUnknownRuntime(t *testing.T) {
	for _, command := range []string{"start", "stop", "status", "logs", "remove"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(t)

			_, err := h.run("", "emulator", command, "--runtime", "nerdctl")

			if command == "status" {
				require.NoError(t, err, "status reports the refusal on its runtime line")
				return
			}
			require.ErrorIs(t, err, emulator.ErrUnknownRuntime)
		})
	}
}

func TestEmulatorSeedRefusesARemoteProfileBeforeConnecting(t *testing.T) {
	h := newHarness(t)
	_, err := h.run("", "profile", "add", "remote", "--endpoint", "https://myaccount.documents.azure.com:443/")
	require.NoError(t, err)

	_, err = h.run("", "emulator", "seed", "--profile", "remote")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only runs against an emulator on this machine")
	assert.NotErrorIs(t, err, tui.ErrCredentialsNeeded, "a remote profile is refused before its key is looked for")
}
