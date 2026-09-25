//go:build windows

package snapshot

import "os"

// processAlive opens a handle to pid, which Windows refuses for a process
// that has exited.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release() // only the lookup mattered
	return true
}
