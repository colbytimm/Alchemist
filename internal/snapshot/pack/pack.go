// Package pack keeps content-addressed bodies in append-only files of
// deflate-compressed blocks, each pack beside a sorted index of what it holds.
// The layout of both files is specified in internal/snapshot's package
// comment.
package pack

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

var (
	ErrCorrupt       = errors.New("corrupt")
	ErrUnknownFormat = errors.New("unknown format")
	ErrNotFound      = errors.New("blob not found")
)

const (
	PackExt  = ".pack"
	IndexExt = ".idx"
	TempExt  = ".tmp"
)

const (
	packKind          = "alchemist-pack"
	indexKind         = "alchemist-pack-index"
	codecDeflate byte = 1
	// blockHeaderSize is the compressed length, the raw length, the item
	// count, the codec and the CRC.
	blockHeaderSize = 4 + 4 + 4 + 1 + 4
	// maxBlockRaw bounds what a reader will decompress for one block: a
	// block is ~256 KB unless one body alone is larger, and no backend
	// stores an item near this size.
	maxBlockRaw = 64 << 20
	// maxBlockItems is what the index's 16-bit position can address.
	maxBlockItems = 1 << 16
)

type Hash [sha256.Size]byte

func Sum(body []byte) Hash { return sha256.Sum256(body) }

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

func ParseHash(text string) (Hash, error) {
	var h Hash
	decoded, err := hex.DecodeString(text)
	if err != nil || len(decoded) != len(h) {
		return Hash{}, fmt.Errorf("pack: hash %q: %w", text, ErrCorrupt)
	}
	copy(h[:], decoded)
	return h, nil
}

// block is one decompressed block: its bodies in the order they were added.
type block struct {
	bodies [][]byte
}

// encodeBlock lays bodies out as the pack stores them: the header, then the
// compressed lengths and bodies.
func encodeBlock(bodies [][]byte) ([]byte, error) {
	var raw bytes.Buffer
	var length [binary.MaxVarintLen64]byte
	for _, body := range bodies {
		raw.Write(length[:binary.PutUvarint(length[:], uint64(len(body)))])
	}
	for _, body := range bodies {
		raw.Write(body)
	}
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		return nil, fmt.Errorf("pack: compress: %w", err)
	}
	if _, err := writer.Write(raw.Bytes()); err != nil {
		return nil, fmt.Errorf("pack: compress: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("pack: compress: %w", err)
	}
	encoded := make([]byte, blockHeaderSize, blockHeaderSize+compressed.Len())
	binary.LittleEndian.PutUint32(encoded[0:], uint32(compressed.Len())) // #nosec G115 -- a block is far below 4 GB
	binary.LittleEndian.PutUint32(encoded[4:], uint32(raw.Len()))        // #nosec G115 -- as above
	binary.LittleEndian.PutUint32(encoded[8:], uint32(len(bodies)))      // #nosec G115 -- at most maxBlockItems
	encoded[12] = codecDeflate
	binary.LittleEndian.PutUint32(encoded[13:], crc32.ChecksumIEEE(compressed.Bytes()))
	return append(encoded, compressed.Bytes()...), nil
}

// blockHeader is a block's fixed-size prefix, checked against the room left
// in its file before anything is allocated for it.
type blockHeader struct {
	compressed uint32
	raw        uint32
	count      uint32
	codec      byte
	crc        uint32
}

func parseBlockHeader(data []byte, room int64) (blockHeader, error) {
	if len(data) < blockHeaderSize {
		return blockHeader{}, fmt.Errorf("block header cut short: %w", ErrCorrupt)
	}
	h := blockHeader{
		compressed: binary.LittleEndian.Uint32(data[0:]),
		raw:        binary.LittleEndian.Uint32(data[4:]),
		count:      binary.LittleEndian.Uint32(data[8:]),
		codec:      data[12],
		crc:        binary.LittleEndian.Uint32(data[13:]),
	}
	switch {
	case h.codec != codecDeflate:
		return blockHeader{}, fmt.Errorf("block codec %d: %w", h.codec, ErrUnknownFormat)
	case int64(h.compressed) > room-blockHeaderSize:
		return blockHeader{}, fmt.Errorf("block of %d bytes runs past the end of the file: %w", h.compressed, ErrCorrupt)
	case h.raw > maxBlockRaw:
		return blockHeader{}, fmt.Errorf("block claims %d raw bytes: %w", h.raw, ErrCorrupt)
	case h.count > h.raw || h.count > maxBlockItems:
		return blockHeader{}, fmt.Errorf("block claims %d items in %d bytes: %w", h.count, h.raw, ErrCorrupt)
	}
	return h, nil
}

// decodeBlock checks the CRC of compressed and returns its bodies.
func decodeBlock(h blockHeader, compressed []byte) (block, error) {
	if crc32.ChecksumIEEE(compressed) != h.crc {
		return block{}, fmt.Errorf("block CRC mismatch: %w", ErrCorrupt)
	}
	reader := flate.NewReader(bytes.NewReader(compressed))
	raw, err := io.ReadAll(io.LimitReader(reader, int64(h.raw)+1))
	if err != nil {
		return block{}, fmt.Errorf("block does not decompress: %w: %w", ErrCorrupt, err)
	}
	if len(raw) != int(h.raw) {
		return block{}, fmt.Errorf("block decompresses to %d bytes, not %d: %w", len(raw), h.raw, ErrCorrupt)
	}
	return splitBodies(raw, int(h.count))
}

func splitBodies(raw []byte, count int) (block, error) {
	lengths := make([]uint64, count)
	rest := raw
	var total uint64
	for i := range lengths {
		length, n := binary.Uvarint(rest)
		if n <= 0 || length > uint64(len(raw)) {
			return block{}, fmt.Errorf("block body length %d unreadable: %w", i, ErrCorrupt)
		}
		lengths[i] = length
		total += length
		rest = rest[n:]
	}
	if total != uint64(len(rest)) {
		return block{}, fmt.Errorf("block bodies hold %d bytes, not %d: %w", len(rest), total, ErrCorrupt)
	}
	b := block{bodies: make([][]byte, count)}
	for i, length := range lengths {
		b.bodies[i] = rest[:length:length]
		rest = rest[length:]
	}
	return b, nil
}
