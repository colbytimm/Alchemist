//go:build windows

package pack

// syncDir does nothing: Windows cannot open a directory to flush it, and
// makes a completed rename durable on its own.
func syncDir(string) error { return nil }
