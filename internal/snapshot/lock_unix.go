//go:build !windows

package snapshot

import (
	"errors"
	"os"
	"syscall"
)

// lockFileExclusive takes flock's exclusive lock without waiting; false is
// a lock another open file holds, in this process or another.
func lockFileExclusive(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) // #nosec G115 -- a file descriptor fits an int
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) // #nosec G115 -- a file descriptor fits an int
}
