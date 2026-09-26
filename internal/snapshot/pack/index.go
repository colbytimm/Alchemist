package pack

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"os"
	"slices"
)

const (
	prefixSize     = 16
	indexEntrySize = prefixSize + 4 + 2
	firstBytes     = 256
)

type prefix [prefixSize]byte

func prefixOf(h Hash) prefix {
	var p prefix
	copy(p[:], h[:prefixSize])
	return p
}

// location is where one body is: the block starting at offset in its pack,
// and the body's position in that block.
type location struct {
	offset   uint32
	position uint16
}

type indexEntry struct {
	prefix prefix
	at     location
}

// index is one pack's table from hash prefix to location, sorted by
// prefix, with git's first-byte table: byFirstByte[b] counts the entries whose
// first byte is at most b.
type index struct {
	byFirstByte [firstBytes]uint32
	entries     []indexEntry
}

func newIndex(entries []indexEntry) index {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, compareEntries)
	x := index{entries: sorted}
	for _, entry := range sorted {
		x.byFirstByte[entry.prefix[0]]++
	}
	for b := 1; b < firstBytes; b++ {
		x.byFirstByte[b] += x.byFirstByte[b-1]
	}
	return x
}

// find returns every location filed under h's prefix; more than one is a
// prefix collision, which the reader settles by rehashing.
func (x index) find(h Hash) []location {
	p := prefixOf(h)
	var low uint32
	if p[0] > 0 {
		low = x.byFirstByte[p[0]-1]
	}
	candidates := x.entries[low:x.byFirstByte[p[0]]]
	i, _ := slices.BinarySearchFunc(candidates, p, func(e indexEntry, p prefix) int { return bytes.Compare(e.prefix[:], p[:]) })
	var found []location
	for ; i < len(candidates) && candidates[i].prefix == p; i++ {
		found = append(found, candidates[i].at)
	}
	return found
}

func compareEntries(a, b indexEntry) int {
	if order := bytes.Compare(a.prefix[:], b.prefix[:]); order != 0 {
		return order
	}
	if a.at.offset != b.at.offset {
		return cmp.Compare(a.at.offset, b.at.offset)
	}
	return cmp.Compare(a.at.position, b.at.position)
}

func (x index) encode() []byte {
	encoded := Header(indexKind)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(x.entries))) // #nosec G115 -- a 64 MB pack holds far fewer
	for _, count := range x.byFirstByte {
		encoded = binary.LittleEndian.AppendUint32(encoded, count)
	}
	for _, entry := range x.entries {
		encoded = append(encoded, entry.prefix[:]...)
		encoded = binary.LittleEndian.AppendUint32(encoded, entry.at.offset)
		encoded = binary.LittleEndian.AppendUint16(encoded, entry.at.position)
	}
	return encoded
}

func readIndex(path string) (index, error) {
	info, err := os.Stat(path)
	if err != nil {
		return index{}, fmt.Errorf("pack: %w", err)
	}
	if info.Size() > int64(len(Header(indexKind)))+4+4*firstBytes+indexEntrySize*maxIndexEntries {
		return index{}, fmt.Errorf("pack: %s is %d bytes: %w", path, info.Size(), ErrCorrupt)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- a file inside the snapshot store
	if err != nil {
		return index{}, fmt.Errorf("pack: %w", err)
	}
	x, err := parseIndex(data)
	if err != nil {
		return index{}, fmt.Errorf("pack: %s: %w", path, err)
	}
	return x, nil
}

// maxIndexEntries bounds the size of an index file a reader will load: a
// pack rolls at 64 MB, and no body is stored in less than one byte.
const maxIndexEntries = packRoll

func parseIndex(data []byte) (index, error) {
	reader := bytes.NewReader(data)
	if _, err := ReadHeader(reader, indexKind); err != nil {
		return index{}, err
	}
	rest := data[len(data)-reader.Len():]
	if len(rest) < 4+4*firstBytes {
		return index{}, fmt.Errorf("index cut short: %w", ErrCorrupt)
	}
	count := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	if uint64(len(rest)) != 4*firstBytes+uint64(count)*indexEntrySize {
		return index{}, fmt.Errorf("index of %d entries is %d bytes: %w", count, len(rest), ErrCorrupt)
	}
	var x index
	for b := range x.byFirstByte {
		x.byFirstByte[b] = binary.LittleEndian.Uint32(rest[4*b:])
	}
	rest = rest[4*firstBytes:]
	x.entries = make([]indexEntry, count)
	for i := range x.entries {
		copy(x.entries[i].prefix[:], rest[:prefixSize])
		x.entries[i].at.offset = binary.LittleEndian.Uint32(rest[prefixSize:])
		x.entries[i].at.position = binary.LittleEndian.Uint16(rest[prefixSize+4:])
		rest = rest[indexEntrySize:]
	}
	if err := x.check(); err != nil {
		return index{}, err
	}
	return x, nil
}

// check refuses an index whose first-byte table disagrees with its
// entries, since find trusts the table to slice the entries.
func (x index) check() error {
	want := newIndex(x.entries)
	if !slices.EqualFunc(want.entries, x.entries, func(a, b indexEntry) bool { return a == b }) || want.byFirstByte != x.byFirstByte {
		return fmt.Errorf("index is not sorted or its first-byte table disagrees: %w", ErrCorrupt)
	}
	return nil
}
