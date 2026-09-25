//go:build !windows

package snapshot

import (
	"errors"
	"syscall"
)

// processAlive sends pid the null signal, which checks for the process
// without touching it. EPERM is a live process owned by someone else.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
