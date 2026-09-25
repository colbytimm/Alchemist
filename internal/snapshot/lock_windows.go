//go:build windows

package snapshot

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockedBytes is the region of the lock file LockFileEx locks: its first
// byte is enough, since only the lock matters.
const lockedBytes = 1

// lockFileExclusive takes LockFileEx's exclusive lock without waiting;
// false is a lock another handle holds.
func lockFileExclusive(file *os.File) (bool, error) {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockedBytes, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, lockedBytes, 0, &overlapped)
}
