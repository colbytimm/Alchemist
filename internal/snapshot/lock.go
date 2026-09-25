package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// abandonedLockAge is how old a lock file with nothing readable in it must
// be before it is taken for one whose writer died between creating it and
// filling it in.
const abandonedLockAge = time.Minute

// holder is what a lock file says about who holds it.
type holder struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

// storeLock is the store's lock file, present while a capture, a delete or
// a prune runs. It keeps other processes out, cron and a TUI alike.
type storeLock struct {
	path string
}

// takeLock creates the lock, taking over one whose process is gone from
// this host. A lock held by a live process, or by another host, is
// ErrLocked.
func takeLock(dir string, now time.Time) (*storeLock, error) {
	path := filepath.Join(dir, lockFile)
	for range 2 {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, pack.FileMode) // #nosec G304 -- the store's own lock file
		if err == nil {
			return fillLock(file, now)
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("snapshot: %w", err)
		}
		held, err := heldBy(path, now)
		if err != nil {
			return nil, err
		}
		if held != "" {
			return nil, fmt.Errorf("snapshot: %s is held by %s: %w", dir, held, ErrLocked)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("snapshot: take over %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("snapshot: %s: %w", dir, ErrLocked)
}

func fillLock(file *os.File, now time.Time) (*storeLock, error) {
	host, _ := os.Hostname() // an unnamed host still locks; only takeover needs the name
	data, err := json.Marshal(holder{PID: os.Getpid(), Host: host, Started: now.UTC()})
	if err == nil {
		_, err = file.Write(data)
	}
	if err = errors.Join(err, file.Close()); err != nil {
		_ = os.Remove(file.Name()) // a lock that could not be written is no lock
		return nil, fmt.Errorf("snapshot: write lock: %w", err)
	}
	return &storeLock{path: file.Name()}, nil
}

// heldBy describes the live holder of the lock at path, and is empty when
// it may be taken over.
func heldBy(path string, now time.Time) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	var h holder
	if readJSON(path, &h) != nil || h.PID == 0 {
		if now.Sub(info.ModTime()) > abandonedLockAge {
			return "", nil
		}
		return "a process that is starting", nil
	}
	host, _ := os.Hostname() // no name matches no lock, so none is taken over
	if h.Host == host && host != "" && !processAlive(h.PID) {
		return "", nil
	}
	return fmt.Sprintf("process %d on %s since %s", h.PID, h.Host, h.Started.Format(time.RFC3339)), nil
}

func (l *storeLock) release() error {
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("snapshot: release lock: %w", err)
	}
	return nil
}
