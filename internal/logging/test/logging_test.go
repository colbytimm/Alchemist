package logging_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/logging"
)

func TestDirFollowsXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	dir, err := logging.Dir()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(state, "alchemist"), dir)
}

func TestDirFallsBackToTheHomeDirectory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir, err := logging.Dir()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "state", "alchemist"), dir)
}

func TestOpenWritesToTheDirectoryItIsGiven(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "alchemist")

	logger, file, err := logging.Open(dir, log.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	logger.Info("session started", "adapter", "mock")

	contents, err := os.ReadFile(filepath.Join(dir, logging.FileName))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "session started")
	assert.Contains(t, string(contents), "adapter=mock")
}

func TestOpenKeepsDebugRecordsOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name  string
		level log.Level
		want  bool
	}{
		{name: "info drops debug records", level: log.InfoLevel, want: false},
		{name: "debug keeps them", level: log.DebugLevel, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			logger, file, err := logging.Open(dir, tt.level)
			require.NoError(t, err)
			logger.Debug("catalog fetched")
			require.NoError(t, file.Close())

			assert.Equal(t, tt.want, len(readLog(t, dir)) > 0)
		})
	}
}

func readLog(t *testing.T, dir string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(dir, logging.FileName))
	require.NoError(t, err)
	return contents
}

func TestOpenAppendsToAnExistingLog(t *testing.T) {
	dir := t.TempDir()

	for _, message := range []string{"first session", "second session"} {
		logger, file, err := logging.Open(dir, log.InfoLevel)
		require.NoError(t, err)
		logger.Info(message)
		require.NoError(t, file.Close())
	}

	contents := string(readLog(t, dir))
	assert.Contains(t, contents, "first session")
	assert.Contains(t, contents, "second session")
}
