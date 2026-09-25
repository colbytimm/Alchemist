package pack

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Set reads the bodies of every pack published in one directory. It is not
// safe for concurrent use: it keeps the last block it decompressed, since
// bodies are mostly read in the order they were written.
type Set struct {
	dir    string
	packs  []indexedPack
	files  map[string]*os.File
	cached cachedBlock
}

type indexedPack struct {
	name  string
	index index
}

type cachedBlock struct {
	name   string
	offset uint32
	block  block
	valid  bool
}

// OpenSet loads the index of every pack in dir. A pack whose index is
// missing, which a crash between publishing the two can leave, is indexed
// from its content instead.
func OpenSet(dir string) (*Set, error) {
	names, err := Names(dir)
	if err != nil {
		return nil, err
	}
	set := &Set{dir: dir, files: map[string]*os.File{}}
	for _, name := range names {
		x, err := readIndex(filepath.Join(dir, name+IndexExt))
		if errors.Is(err, fs.ErrNotExist) {
			x, err = indexContent(dir, name)
		}
		if err != nil {
			return nil, err
		}
		set.packs = append(set.packs, indexedPack{name: name, index: x})
	}
	return set, nil
}

func (s *Set) Names() []string {
	names := make([]string, 0, len(s.packs))
	for _, p := range s.packs {
		names = append(names, p.name)
	}
	return names
}

func (s *Set) Has(h Hash) bool {
	for _, p := range s.packs {
		if len(p.index.find(h)) > 0 {
			return true
		}
	}
	return false
}

// Read returns the body stored under h, having checked that it still hashes
// to h: an index keeps only a prefix, and a disk can rot.
func (s *Set) Read(h Hash) ([]byte, error) {
	listed := false
	for _, p := range s.packs {
		for _, at := range p.index.find(h) {
			listed = true
			body, err := s.body(p.name, at)
			if err != nil {
				return nil, err
			}
			if Sum(body) == h {
				return body, nil
			}
		}
	}
	if listed {
		return nil, fmt.Errorf("pack: %s: no body hashes to %s: %w", s.dir, h, ErrCorrupt)
	}
	return nil, fmt.Errorf("pack: %s: %s: %w", s.dir, h, ErrNotFound)
}

func (s *Set) Close() error {
	var errs []error
	for _, file := range s.files {
		errs = append(errs, file.Close())
	}
	s.files = map[string]*os.File{}
	return errors.Join(errs...)
}

func (s *Set) body(name string, at location) ([]byte, error) {
	b, err := s.block(name, at.offset)
	if err != nil {
		return nil, err
	}
	if int(at.position) >= len(b.bodies) {
		return nil, fmt.Errorf("pack: %s: position %d of a block of %d: %w", name, at.position, len(b.bodies), ErrCorrupt)
	}
	return b.bodies[at.position], nil
}

func (s *Set) block(name string, offset uint32) (block, error) {
	if c := s.cached; c.valid && c.name == name && c.offset == offset {
		return c.block, nil
	}
	file, err := s.open(name)
	if err != nil {
		return block{}, err
	}
	b, _, err := readBlock(file, int64(offset))
	if errors.Is(err, io.EOF) {
		err = fmt.Errorf("no block at the end of the pack: %w", ErrCorrupt)
	}
	if err != nil {
		return block{}, fmt.Errorf("pack: %s at %d: %w", name, offset, err)
	}
	s.cached = cachedBlock{name: name, offset: offset, block: b, valid: true}
	return b, nil
}

func (s *Set) open(name string) (*os.File, error) {
	if file, ok := s.files[name]; ok {
		return file, nil
	}
	file, err := os.Open(filepath.Join(s.dir, name+PackExt)) // #nosec G304 -- a pack inside the snapshot store
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	s.files[name] = file
	return file, nil
}

// readBlock reads the block at offset of file and reports where the next
// one starts.
func readBlock(file *os.File, offset int64) (block, int64, error) {
	h, compressed, err := readCompressed(file, offset)
	if err != nil {
		return block{}, 0, err
	}
	b, err := decodeBlock(h, compressed)
	return b, offset + blockHeaderSize + int64(h.compressed), err
}

func readCompressed(file *os.File, offset int64) (blockHeader, []byte, error) {
	info, err := file.Stat()
	if err != nil {
		return blockHeader{}, nil, fmt.Errorf("pack: %w", err)
	}
	if offset == info.Size() {
		return blockHeader{}, nil, io.EOF
	}
	header := make([]byte, blockHeaderSize)
	if _, err := file.ReadAt(header, offset); err != nil {
		return blockHeader{}, nil, fmt.Errorf("block header at %d unreadable: %w", offset, ErrCorrupt)
	}
	h, err := parseBlockHeader(header, info.Size()-offset)
	if err != nil {
		return blockHeader{}, nil, err
	}
	compressed := make([]byte, h.compressed)
	if _, err := file.ReadAt(compressed, offset+blockHeaderSize); err != nil {
		return blockHeader{}, nil, fmt.Errorf("pack: %w", err)
	}
	return h, compressed, nil
}
