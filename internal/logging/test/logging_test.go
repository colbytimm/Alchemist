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

func TestOpenWritesToTheStateDirectory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	logger, file, err := logging.Open(log.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	logger.Info("session started", "adapter", "mock")

	dir, err := logging.Dir()
	require.NoError(t, err)
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
			t.Setenv("XDG_STATE_HOME", t.TempDir())

			logger, file, err := logging.Open(tt.level)
			require.NoError(t, err)
			logger.Debug("catalog fetched")
			require.NoError(t, file.Close())

			assert.Equal(t, tt.want, len(readLog(t)) > 0)
		})
	}
}

func readLog(t *testing.T) []byte {
	t.Helper()
	dir, err := logging.Dir()
	require.NoError(t, err)
	contents, err := os.ReadFile(filepath.Join(dir, logging.FileName))
	require.NoError(t, err)
	return contents
}

func TestOpenAppendsToAnExistingLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, message := range []string{"first session", "second session"} {
		logger, file, err := logging.Open(log.InfoLevel)
		require.NoError(t, err)
		logger.Info(message)
		require.NoError(t, file.Close())
	}

	dir, err := logging.Dir()
	require.NoError(t, err)
	contents, err := os.ReadFile(filepath.Join(dir, logging.FileName))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "first session")
	assert.Contains(t, string(contents), "second session")
}
