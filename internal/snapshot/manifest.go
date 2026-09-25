package snapshot

import (
	"bufio"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

const (
	manifestKind = "alchemist-manifest"
	changesKind  = "alchemist-changes"
	trailerSize  = 8 + sha256.Size
)

// Fingerprint is the first 8 bytes of the SHA-256 of an item's version
// string, whatever the backend's format for it.
type Fingerprint [8]byte

func fingerprint(version string) Fingerprint {
	sum := sha256.Sum256([]byte(version))
	var f Fingerprint
	copy(f[:], sum[:])
	return f
}

// Entry is what a snapshot records of one item: the hash and length of its
// canonical body, and its version and modified time, which are kept beside
// the body and never in it. Entries compare with ==.
type Entry struct {
	Hash    pack.Hash
	Size    int64
	Version Fingerprint
	// ModifiedUnix is in seconds, and zero when the backend gave none.
	ModifiedUnix int64
}

// Modified is the item's modified time, zero when unknown.
func (e Entry) Modified() time.Time {
	if e.ModifiedUnix == 0 {
		return time.Time{}
	}
	return time.Unix(e.ModifiedUnix, 0).UTC()
}

// Manifest is a snapshot's contents: every item's key and entry.
type Manifest map[Key]Entry

func (m Manifest) Clone() Manifest { return maps.Clone(m) }

// SortedKeys lists m's keys in byte order.
func (m Manifest) SortedKeys() []Key {
	return slices.Sorted(maps.Keys(m))
}

func (m Manifest) LogicalBytes() int64 {
	var total int64
	for _, entry := range m {
		total += entry.Size
	}
	return total
}

func appendEntry(encoded []byte, e Entry) []byte {
	encoded = append(encoded, e.Hash[:]...)
	encoded = append(encoded, e.Version[:]...)
	encoded = binary.AppendUvarint(encoded, uint64(e.Size)) // #nosec G115 -- a body's length is never negative
	return binary.AppendVarint(encoded, e.ModifiedUnix)
}

func appendKey(encoded []byte, k Key) []byte {
	encoded = binary.AppendUvarint(encoded, uint64(len(k)))
	return append(encoded, k...)
}

func writeManifest(path string, m Manifest) error {
	return writeStream(path, manifestKind, len(m), func(emit func([]byte) error) error {
		for _, k := range m.SortedKeys() {
			if err := emit(appendEntry(appendKey(nil, k), m[k])); err != nil {
				return err
			}
		}
		return nil
	})
}

// writeStream writes a file of kind: its first line, one deflate stream of
// records, then the record count and the SHA-256 of the raw stream.
func writeStream(path, kind string, count int, records func(emit func([]byte) error) error) error {
	temp, err := pack.CreateTemp(path)
	if err != nil {
		return err
	}
	buffered := bufio.NewWriter(temp)
	if err := encodeStream(buffered, kind, count, records); err != nil {
		pack.Discard(temp)
		return err
	}
	if err := buffered.Flush(); err != nil {
		pack.Discard(temp)
		return fmt.Errorf("snapshot: write %s: %w", path, err)
	}
	return pack.Commit(temp, path)
}

func encodeStream(out io.Writer, kind string, count int, records func(emit func([]byte) error) error) error {
	if _, err := out.Write(pack.Header(kind)); err != nil {
		return fmt.Errorf("snapshot: write %s: %w", kind, err)
	}
	compressor, err := flate.NewWriter(out, flate.DefaultCompression)
	if err != nil {
		return fmt.Errorf("snapshot: write %s: %w", kind, err)
	}
	sum := sha256.New()
	raw := io.MultiWriter(compressor, sum)
	err = records(func(record []byte) error {
		_, err := raw.Write(record)
		return err
	})
	if err = errors.Join(err, compressor.Close()); err != nil {
		return fmt.Errorf("snapshot: write %s: %w", kind, err)
	}
	trailer := binary.LittleEndian.AppendUint64(nil, uint64(count)) // #nosec G115 -- a count is never negative
	if _, err := out.Write(sum.Sum(trailer)); err != nil {
		return fmt.Errorf("snapshot: write %s: %w", kind, err)
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	m := Manifest{}
	err := readStream(path, manifestKind, func(r *streamReader) error {
		k, err := r.key()
		if err != nil {
			return err
		}
		e, err := r.entry()
		if err != nil {
			return err
		}
		if _, dup := m[k]; dup {
			return fmt.Errorf("key listed twice: %w", ErrCorrupt)
		}
		m[k] = e
		return nil
	})
	return m, err
}

// streamReader decodes records from the raw stream while hashing it.
type streamReader struct {
	in    *bufio.Reader
	sum   hash.Hash
	count uint64
}

func (r *streamReader) ReadByte() (byte, error) {
	b, err := r.in.ReadByte()
	if err == nil {
		r.sum.Write([]byte{b})
	}
	return b, err
}

func (r *streamReader) read(n int) ([]byte, error) {
	data := make([]byte, n)
	if _, err := io.ReadFull(r.in, data); err != nil {
		return nil, fmt.Errorf("record cut short: %w", ErrCorrupt)
	}
	r.sum.Write(data)
	return data, nil
}

func (r *streamReader) key() (Key, error) {
	length, err := binary.ReadUvarint(r)
	if err != nil || length > maxKeyComponent*4 {
		return "", fmt.Errorf("key length unreadable: %w", ErrCorrupt)
	}
	data, err := r.read(int(length))
	return Key(data), err
}

func (r *streamReader) entry() (Entry, error) {
	fixed, err := r.read(len(pack.Hash{}) + len(Fingerprint{}))
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	copy(e.Hash[:], fixed)
	copy(e.Version[:], fixed[len(e.Hash):])
	size, err := binary.ReadUvarint(r)
	if err != nil || size > 1<<40 {
		return Entry{}, fmt.Errorf("entry size unreadable: %w", ErrCorrupt)
	}
	e.Size = int64(size)
	if e.ModifiedUnix, err = binary.ReadVarint(r); err != nil {
		return Entry{}, fmt.Errorf("entry modified time unreadable: %w", ErrCorrupt)
	}
	return e, nil
}

// readStream checks a file of kind and hands each record to decode, then
// checks the trailer against what was decoded.
func readStream(path, kind string, decode func(*streamReader) error) error {
	file, err := os.Open(path) // #nosec G304 -- a file inside the snapshot store
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	if err := decodeStream(file, kind, decode); err != nil {
		return fmt.Errorf("snapshot: %s: %w", path, err)
	}
	return nil
}

func decodeStream(file *os.File, kind string, decode func(*streamReader) error) error {
	start, err := pack.ReadHeader(file, kind)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	body := info.Size() - int64(start) - trailerSize
	if body < 0 {
		return fmt.Errorf("no trailer: %w", ErrCorrupt)
	}
	trailer := make([]byte, trailerSize)
	if _, err := file.ReadAt(trailer, int64(start)+body); err != nil {
		return fmt.Errorf("trailer unreadable: %w", ErrCorrupt)
	}
	decompressor := flate.NewReader(io.NewSectionReader(file, int64(start), body))
	r := &streamReader{in: bufio.NewReader(decompressor), sum: sha256.New()}
	for {
		_, err := r.in.Peek(1)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("stream does not decompress: %w: %w", ErrCorrupt, err)
		}
		if err := decode(r); err != nil {
			return err
		}
		r.count++
	}
	if r.count != binary.LittleEndian.Uint64(trailer) || !bytes.Equal(r.sum.Sum(nil), trailer[8:]) {
		return fmt.Errorf("records disagree with the trailer: %w", ErrCorrupt)
	}
	return nil
}
