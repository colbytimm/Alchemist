package pack

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

type Body struct {
	Hash Hash
	Data []byte
}

// Walk decompresses every block of the pack called name in dir and hands
// fn each body, rehashed, in the order they were written.
func Walk(dir, name string, fn func(Body) error) error {
	return walkBlocks(dir, name, func(offset uint32, b block) error {
		for _, data := range b.bodies {
			if err := fn(Body{Hash: Sum(data), Data: data}); err != nil {
				return err
			}
		}
		return nil
	})
}

func walkBlocks(dir, name string, fn func(offset uint32, b block) error) error {
	file, offset, err := openPack(dir, name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	for {
		b, next, err := readBlock(file, int64(offset))
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("pack: %s at %d: %w", name, offset, err)
		}
		start := uint32(offset) // #nosec G115 -- a pack rolls long before 4 GB
		if err := fn(start, b); err != nil {
			return err
		}
		offset = int(next)
	}
}

// openPack opens a pack and reads past its first line.
func openPack(dir, name string) (*os.File, int, error) {
	file, err := os.Open(filepath.Join(dir, name+PackExt)) // #nosec G304 -- a pack inside the snapshot store
	if err != nil {
		return nil, 0, fmt.Errorf("pack: %w", err)
	}
	start, err := ReadHeader(file, packKind)
	if err != nil {
		_ = file.Close() // read-only, and already failing
		return nil, 0, fmt.Errorf("pack: %s: %w", name, err)
	}
	return file, start, nil
}

// indexContent rebuilds the index of a pack by reading every body in it.
func indexContent(dir, name string) (index, error) {
	var entries []indexEntry
	err := walkBlocks(dir, name, func(offset uint32, b block) error {
		for i, data := range b.bodies {
			entries = append(entries, indexEntry{prefix: prefixOf(Sum(data)), at: location{offset: offset, position: uint16(i)}}) // #nosec G115 -- a block holds at most maxBlockItems
		}
		return nil
	})
	if err != nil {
		return index{}, err
	}
	return newIndex(entries), nil
}

// RebuildIndex replaces the index of the pack called name with one read
// from the pack itself.
func RebuildIndex(dir, name string) error {
	x, err := indexContent(dir, name)
	if err != nil {
		return err
	}
	return WriteFile(filepath.Join(dir, name+IndexExt), x.encode())
}

// Check verifies the pack called name and its index: the first lines, every
// block's CRC, and that every index entry points into a block. deep also
// decompresses every block and checks that the index lists every body it
// holds under its own hash.
func Check(dir, name string, depth Depth) error {
	x, err := readIndex(filepath.Join(dir, name+IndexExt))
	if err != nil {
		return err
	}
	counts, bodies, err := blockCounts(dir, name, depth)
	if err != nil {
		return err
	}
	for _, entry := range x.entries {
		count, ok := counts[entry.at.offset]
		if !ok || uint32(entry.at.position) >= count {
			return fmt.Errorf("pack: %s: index names a body at %d/%d the pack does not hold: %w", name, entry.at.offset, entry.at.position, ErrCorrupt)
		}
	}
	for _, body := range bodies {
		if !listedAt(x, body) {
			return fmt.Errorf("pack: %s: the body at %d/%d is not indexed under its hash: %w", name, body.at.offset, body.at.position, ErrCorrupt)
		}
	}
	return nil
}

// Depth is how much of a pack Check reads.
type Depth int

const (
	// Shallow reads every block's bytes and checks their CRC.
	Shallow Depth = iota
	// Deep also decompresses every block and rehashes every body.
	Deep
)

type hashedBody struct {
	hash Hash
	at   location
}

func blockCounts(dir, name string, depth Depth) (map[uint32]uint32, []hashedBody, error) {
	counts := map[uint32]uint32{}
	if depth == Shallow {
		return counts, nil, walkCompressed(dir, name, func(offset uint32, h blockHeader) { counts[offset] = h.count })
	}
	var bodies []hashedBody
	err := walkBlocks(dir, name, func(offset uint32, b block) error {
		counts[offset] = uint32(len(b.bodies)) // #nosec G115 -- a block holds at most maxBlockItems
		for i, data := range b.bodies {
			bodies = append(bodies, hashedBody{hash: Sum(data), at: location{offset: offset, position: uint16(i)}}) // #nosec G115 -- as above
		}
		return nil
	})
	return counts, bodies, err
}

func walkCompressed(dir, name string, fn func(offset uint32, h blockHeader)) error {
	file, start, err := openPack(dir, name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	for offset := int64(start); ; {
		h, compressed, err := readCompressed(file, offset)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("pack: %s at %d: %w", name, offset, err)
		}
		if crc32.ChecksumIEEE(compressed) != h.crc {
			return fmt.Errorf("pack: %s at %d: block CRC mismatch: %w", name, offset, ErrCorrupt)
		}
		fn(uint32(offset), h) // #nosec G115 -- a pack rolls long before 4 GB
		offset += blockHeaderSize + int64(h.compressed)
	}
}

func listedAt(x index, body hashedBody) bool {
	for _, at := range x.find(body.hash) {
		if at == body.at {
			return true
		}
	}
	return false
}
