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
	levels := map[log.Level]bool{log.InfoLevel: false, log.DebugLevel: true}
	for level, wantDebug := range levels {
		t.Setenv("XDG_STATE_HOME", t.TempDir())

		logger, file, err := logging.Open(level)
		require.NoError(t, err)
		logger.Debug("catalog fetched")
		require.NoError(t, file.Close())

		dir, err := logging.Dir()
		require.NoError(t, err)
		contents, err := os.ReadFile(filepath.Join(dir, logging.FileName))
		require.NoError(t, err)
		assert.Equal(t, wantDebug, len(contents) > 0, "level %s", level)
	}
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
