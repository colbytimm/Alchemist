package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// holder is what a lock file says about who holds it, for the message a
// refusal shows. The lock itself is the operating system's.
type holder struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

// storeLock holds the store's lock file under an exclusive advisory lock
// while a capture, a delete or a prune runs, keeping other processes out,
// cron and a TUI alike, and other captures in this process too. The
// operating system releases it when its process dies, so a lock is never
// left stale and never taken over; the file itself stays.
type storeLock struct {
	file *os.File
}

// takeLock locks the store in dir, or is ErrLocked naming who holds it.
func takeLock(dir string, now time.Time) (*storeLock, error) {
	path := filepath.Join(dir, lockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, pack.FileMode) // #nosec G304 -- the store's own lock file
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	locked, err := lockFileExclusive(file)
	if err != nil {
		_ = file.Close() // the lock failed; the handle holds nothing
		return nil, fmt.Errorf("snapshot: lock %s: %w", path, err)
	}
	if !locked {
		held := heldBy(file)
		_ = file.Close() // it never held the lock
		return nil, fmt.Errorf("snapshot: %s is held by %s: %w", dir, held, ErrLocked)
	}
	if err := sayHolder(file, now); err != nil {
		return nil, errors.Join(err, unlockFile(file), file.Close())
	}
	return &storeLock{file: file}, nil
}

// sayHolder writes this process into the lock file, for a refusal to name.
func sayHolder(file *os.File, now time.Time) error {
	host, _ := os.Hostname() // an unnamed host still locks; the message only reads less well
	data, err := json.Marshal(holder{PID: os.Getpid(), Host: host, Started: now.UTC()})
	if err != nil {
		return fmt.Errorf("snapshot: write lock: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("snapshot: write lock: %w", err)
	}
	if _, err := file.WriteAt(data, 0); err != nil {
		return fmt.Errorf("snapshot: write lock: %w", err)
	}
	return nil
}

// heldBy describes the lock's holder from what it wrote, which the holder
// may not have written yet, or a platform may not let others read.
func heldBy(file *os.File) string {
	data, err := io.ReadAll(io.NewSectionReader(file, 0, 1<<12))
	var h holder
	if err != nil || json.Unmarshal(data, &h) != nil || h.PID == 0 {
		return "another process"
	}
	return fmt.Sprintf("process %d on %s since %s", h.PID, h.Host, h.Started.Format(time.RFC3339))
}

// release unlocks the store. It never removes the file: a process waiting
// on the name could otherwise lock one file while the next creates another.
// A second release does nothing.
func (l *storeLock) release() error {
	if l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	if err := errors.Join(unlockFile(file), file.Close()); err != nil {
		return fmt.Errorf("snapshot: release lock: %w", err)
	}
	return nil
}
