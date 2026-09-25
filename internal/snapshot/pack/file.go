package pack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirMode  = 0o700
	FileMode = 0o600
)

// CreateTemp opens path's temporary sibling for writing, replacing any a
// crash left there.
func CreateTemp(path string) (*os.File, error) {
	file, err := os.OpenFile(path+TempExt, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FileMode) // #nosec G304 -- a path inside the snapshot store
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	return file, nil
}

// Commit makes what was written to temp durable and then visible at path.
// The sync comes first: a rename that outlives its data is corruption.
func Commit(temp *os.File, path string) error {
	syncErr := temp.Sync()
	if err := errors.Join(syncErr, temp.Close()); err != nil {
		_ = os.Remove(temp.Name()) // the write already failed; the leftover is only debris
		return fmt.Errorf("pack: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("pack: %w", err)
	}
	return syncDir(filepath.Dir(path))
}

// Discard closes and removes a temporary file that will not be committed.
func Discard(temp *os.File) {
	_ = temp.Close()           // the file is being thrown away
	_ = os.Remove(temp.Name()) // and whatever is left is debris the store's Open removes
}

// WriteFile writes data to path through a temporary file, so a reader sees
// the old file or the new one and never part of either.
func WriteFile(path string, data []byte) error {
	temp, err := CreateTemp(path)
	if err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		Discard(temp)
		return fmt.Errorf("pack: %w", err)
	}
	return Commit(temp, path)
}
