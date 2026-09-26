//go:build !windows

package pack

import (
	"errors"
	"fmt"
	"os"
)

// syncDir makes a rename in dir durable.
func syncDir(dir string) error {
	handle, err := os.Open(dir) // #nosec G304 -- a directory inside the snapshot store
	if err != nil {
		return fmt.Errorf("pack: %w", err)
	}
	if err := errors.Join(handle.Sync(), handle.Close()); err != nil {
		return fmt.Errorf("pack: sync %s: %w", dir, err)
	}
	return nil
}
