package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// BlockTarget is how many uncompressed bytes a block collects before it
	// is compressed: enough similar documents for deflate to find what they
	// share. Similar JSON measured 6.3× smaller at this size.
	BlockTarget = 256 << 10
	packRoll    = 64 << 20
	seqDigits   = 4
)

// Writer appends new bodies to packs of its own in dir, named
// <prefix>-0001, <prefix>-0002…, rolling to the next at 64 MB. A pack is
// written under a temporary name and published, with its index, when it
// rolls or the writer is closed; nothing a Writer has not published is
// visible to a Set.
type Writer struct {
	dir    string
	prefix string
	next   int

	file    *os.File
	name    string
	size    int64
	entries []indexEntry

	pending       [][]byte
	pendingHashes []Hash
	pendingRaw    int

	added     map[Hash]bool
	stored    int64
	published []string
}

func NewWriter(dir, prefix string) (*Writer, error) {
	last, err := lastSequence(dir, prefix)
	if err != nil {
		return nil, err
	}
	return &Writer{dir: dir, prefix: prefix, next: last + 1, added: map[Hash]bool{}}, nil
}

// lastSequence is the highest number a published pack of prefix has, so a
// second writer with the prefix of an abandoned one never replaces its packs.
func lastSequence(dir, prefix string) (int, error) {
	names, err := Names(dir)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, name := range names {
		rest, ok := strings.CutPrefix(name, prefix+"-")
		if !ok {
			continue
		}
		if seq, err := strconv.Atoi(rest); err == nil {
			last = max(last, seq)
		}
	}
	return last, nil
}

func (w *Writer) Has(h Hash) bool { return w.added[h] }

// Add appends body under h, which the caller has checked is not stored
// already.
func (w *Writer) Add(h Hash, body []byte) error {
	if len(w.pending) > 0 && w.pendingRaw+len(body) > BlockTarget {
		if err := w.flush(); err != nil {
			return err
		}
	}
	w.pending = append(w.pending, bytes.Clone(body))
	w.pendingHashes = append(w.pendingHashes, h)
	w.pendingRaw += len(body)
	w.added[h] = true
	if len(w.pending) == maxBlockItems {
		return w.flush()
	}
	return nil
}

// Stored is how many bytes this writer has written to disk so far; a block
// still collecting bodies counts once it is compressed.
func (w *Writer) Stored() int64 { return w.stored }

func (w *Writer) Published() []string { return w.published }

// Close publishes what the writer still holds.
func (w *Writer) Close() error {
	if err := w.flush(); err != nil {
		return err
	}
	return w.publish()
}

// Abort removes the pack being written. Packs already published stay: a
// later capture may reuse their bodies, and garbage collection reclaims
// them otherwise.
func (w *Writer) Abort() {
	if w.file != nil {
		Discard(w.file)
		w.file = nil
	}
	w.pending, w.pendingHashes, w.pendingRaw = nil, nil, 0
}

func (w *Writer) flush() error {
	if len(w.pending) == 0 {
		return nil
	}
	if w.file == nil {
		if err := w.start(); err != nil {
			return err
		}
	}
	encoded, err := encodeBlock(w.pending)
	if err != nil {
		return err
	}
	offset := uint32(w.size) // #nosec G115 -- a pack rolls long before 4 GB
	if _, err := w.file.Write(encoded); err != nil {
		return fmt.Errorf("pack: write %s: %w", w.name, err)
	}
	for i, h := range w.pendingHashes {
		w.entries = append(w.entries, indexEntry{prefix: prefixOf(h), at: location{offset: offset, position: uint16(i)}}) // #nosec G115 -- a block holds at most maxBlockItems
	}
	w.size += int64(len(encoded))
	w.stored += int64(len(encoded))
	w.pending, w.pendingHashes, w.pendingRaw = nil, nil, 0
	if w.size >= packRoll {
		return w.publish()
	}
	return nil
}

func (w *Writer) start() error {
	w.name = fmt.Sprintf("%s-%0*d", w.prefix, seqDigits, w.next)
	w.next++
	file, err := CreateTemp(filepath.Join(w.dir, w.name+PackExt))
	if err != nil {
		return err
	}
	header := Header(packKind)
	if _, err := file.Write(header); err != nil {
		Discard(file)
		return fmt.Errorf("pack: write %s: %w", w.name, err)
	}
	w.file, w.size, w.entries = file, int64(len(header)), nil
	return nil
}

// publish commits the pack before its index: an index naming a pack that
// is not there would let a capture reuse a body that is gone.
func (w *Writer) publish() error {
	if w.file == nil {
		return nil
	}
	file := w.file
	w.file = nil
	if err := Commit(file, filepath.Join(w.dir, w.name+PackExt)); err != nil {
		return err
	}
	encoded := newIndex(w.entries).encode()
	if err := WriteFile(filepath.Join(w.dir, w.name+IndexExt), encoded); err != nil {
		return err
	}
	w.stored += int64(len(encoded))
	w.published = append(w.published, w.name)
	return nil
}

// Names lists the published packs in dir, without their extension, in
// name order. A missing dir holds none.
func Names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), PackExt); ok {
			names = append(names, name)
		}
	}
	return names, nil
}
