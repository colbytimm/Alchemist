// Package history keeps the log of every query the TUI has run, so a past
// query — a failed one most of all — can be recalled and run again. The log
// holds query text, scope, and statistics only: never a key, an endpoint, or
// anything else from a profile.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FileName is the log written inside the state directory, one JSON entry
// per line.
const FileName = "history.jsonl"

// A log longer than MaxEntries is cut back to its newest KeepEntries when
// it is opened.
const (
	MaxEntries  = 5000
	KeepEntries = 2500
)

// maxErrorLength bounds the error recorded with a failed query: enough to
// say what went wrong, not a whole service response.
const maxErrorLength = 200

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Kinds of entry beside a query, whose Kind is empty.
const (
	KindBatch  = "batch"
	KindUpdate = "update"
	KindDelete = "delete"
)

// Entry is one recorded run.
type Entry struct {
	Time          time.Time `json:"ts"`
	Kind          string    `json:"kind,omitempty"`
	Profile       string    `json:"profile"`
	Scope         []string  `json:"scope"`
	Query         string    `json:"query"`
	OK            bool      `json:"ok"`
	Rows          int       `json:"rows"`
	RequestCharge float64   `json:"ru"`
	ElapsedMillis int64     `json:"elapsed_ms"`
	Error         string    `json:"error,omitempty"`
}

// NamesTarget reports whether e's Scope is the container it wrote to, not
// a scope a query ran under.
func (e Entry) NamesTarget() bool {
	return e.Kind == KindBatch || e.Kind == KindUpdate || e.Kind == KindDelete
}

// Store records runs and serves them back.
type Store interface {
	Append(entry Entry) error
	// Recent returns at most n entries of account, newest first.
	Recent(account string, n int) ([]Entry, error)
}

// Compile-time contract checks.
var (
	_ Store = File{}
	_ Store = Discard{}
)

// File is the log on disk. Every Append is one write to the end of the
// file, so a session that dies mid-write costs at most that line: Recent
// skips a line it cannot parse rather than failing on it.
type File struct {
	path string
}

// Open readies the log in dir. A location that cannot be written is
// refused here rather than at the first query.
func Open(dir string) (File, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return File{}, fmt.Errorf("history: create %s: %w", dir, err)
	}
	f := File{path: filepath.Join(dir, FileName)}
	if err := f.touch(); err != nil {
		return File{}, err
	}
	if err := f.trim(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) Append(entry Entry) error {
	entry.Error = truncate(entry.Error, maxErrorLength)
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("history: encode entry: %w", err)
	}
	file, err := f.openForAppend()
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("history: append to %s: %w", f.path, err)
	}
	return nil
}

// Recent parses the last n well-formed lines recorded for account, newest
// first. The file is read whole: Open holds it to a few thousand lines, so
// there is no older data worth seeking past.
func (f File) Recent(account string, n int) ([]Entry, error) {
	lines, err := f.lines()
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for i := len(lines) - 1; i >= 0 && len(entries) < n; i-- {
		var entry Entry
		if json.Unmarshal(lines[i], &entry) != nil || entry.Profile != account {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// touch creates the file, or proves an existing one can be appended to.
func (f File) touch() error {
	file, err := f.openForAppend()
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("history: close %s: %w", f.path, err)
	}
	return nil
}

func (f File) openForAppend() (*os.File, error) {
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode) // #nosec G304 -- constant file name under the directory handed to Open
	if err != nil {
		return nil, fmt.Errorf("history: open %s: %w", f.path, err)
	}
	return file, nil
}

// trim writes the lines it keeps beside the log and renames them over it,
// so a crash mid-trim leaves the log whole.
func (f File) trim() error {
	lines, err := f.lines()
	if err != nil {
		return err
	}
	if len(lines) <= MaxEntries {
		return nil
	}
	kept := bytes.Join(lines[len(lines)-KeepEntries:], []byte("\n"))
	replacement := f.path + ".tmp"
	if err := os.WriteFile(replacement, append(kept, '\n'), fileMode); err != nil {
		return fmt.Errorf("history: write %s: %w", replacement, err)
	}
	if err := os.Rename(replacement, f.path); err != nil {
		return fmt.Errorf("history: replace %s: %w", f.path, err)
	}
	return nil
}

func (f File) lines() ([][]byte, error) {
	data, err := os.ReadFile(f.path) // #nosec G304 -- constant file name under the directory handed to Open
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: read %s: %w", f.path, err)
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil, nil
	}
	return bytes.Split(data, []byte("\n")), nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// Discard is the store of a session that keeps no history: one run with
// --history=false, or one whose state directory cannot be written.
type Discard struct{}

func (Discard) Append(Entry) error { return nil }

func (Discard) Recent(string, int) ([]Entry, error) { return nil, nil }
