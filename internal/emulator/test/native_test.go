package emulator_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/emulator"
)

// installWindowsEmulator puts an empty emulator executable where the Windows
// installer would, under a ProgramFiles of the test's own.
func installWindowsEmulator(t *testing.T) string {
	t.Helper()
	programFiles := t.TempDir()
	t.Setenv("ProgramFiles", programFiles)
	path := filepath.Join(programFiles, "Azure Cosmos DB Emulator", "Microsoft.Azure.Cosmos.Emulator.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	return path
}

func TestWindowsEmulatorPathIsNeverFoundOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this machine is Windows")
	}
	installWindowsEmulator(t)

	_, ok := emulator.WindowsEmulatorPath()

	assert.False(t, ok)
}

func TestWindowsEmulatorPathFollowsProgramFiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the Windows emulator installs only on Windows")
	}
	want := installWindowsEmulator(t)

	got, ok := emulator.WindowsEmulatorPath()

	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestWindowsEmulatorPathWithoutTheInstall(t *testing.T) {
	t.Setenv("ProgramFiles", t.TempDir())

	_, ok := emulator.WindowsEmulatorPath()

	assert.False(t, ok)
}
