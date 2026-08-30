// Package logging writes diagnostics to a file. While the TUI owns the
// terminal anything written to stdout or stderr would corrupt the screen, so
// there is deliberately no console writer.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
)

// FileName is the log file written inside the state directory.
const FileName = "alchemist.log"

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Dir returns the state directory holding the log file: $XDG_STATE_HOME
// when set, otherwise ~/.local/state.
func Dir() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "alchemist"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("logging: locate home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "alchemist"), nil
}

// Open creates the state directory and returns a logger appending to its log
// file, together with the closer for that file.
func Open(level log.Level) (*log.Logger, io.Closer, error) {
	dir, err := Dir()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, nil, fmt.Errorf("logging: create %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode) // #nosec G304 -- constant file name under a directory this package owns
	if err != nil {
		return nil, nil, fmt.Errorf("logging: open %s: %w", path, err)
	}
	logger := log.NewWithOptions(file, log.Options{Level: level, ReportTimestamp: true})
	return logger, file, nil
}
