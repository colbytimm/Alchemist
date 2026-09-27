package emulator

import (
	"os"
	"path/filepath"
	"runtime"
)

// WindowsEmulatorPath is where the native Windows emulator is installed, and
// whether it is. It is detected so its port is not mistaken, never managed.
func WindowsEmulatorPath() (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	path := filepath.Join(os.Getenv("ProgramFiles"), "Azure Cosmos DB Emulator", "Microsoft.Azure.Cosmos.Emulator.exe")
	// #nosec G703 -- the path is only checked for existence, never opened.
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// windowsEmulatorNote is the alternative to offer when the container cannot
// run, empty unless the Windows emulator is installed.
func windowsEmulatorNote() string {
	if _, ok := WindowsEmulatorPath(); !ok {
		return ""
	}
	return "\nThe Windows emulator is installed. To use it instead, start it, then run:\n" +
		"  alchemist profile add " + ProfileName + " --endpoint https://localhost:8081 --insecure-skip-verify --well-known-key"
}
