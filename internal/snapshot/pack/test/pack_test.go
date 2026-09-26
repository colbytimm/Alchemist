package pack_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

const prefix = "20260919T060000Z"

// document is a body shaped like a small order, distinct for each n.
func document(n int) []byte {
	return fmt.Appendf(nil, `{"customerId":"c%03d","id":"o-%06d","lines":[{"sku":"sku-%d","qty":%d}],"status":"open"}`, n%97, n, n%13, n%5)
}

// writePack writes bodies into a fresh directory as one writer would.
func writePack(t testing.TB, bodies ...[]byte) string {
	t.Helper()
	dir := t.TempDir()
	writer, err := pack.NewWriter(dir, prefix)
	require.NoError(t, err)
	for _, body := range bodies {
		h := pack.Sum(body)
		if writer.Has(h) {
			continue
		}
		require.NoError(t, writer.Add(h, body))
	}
	require.NoError(t, writer.Close())
	return dir
}

func openSet(t *testing.T, dir string) *pack.Set {
	t.Helper()
	set, err := pack.OpenSet(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, set.Close()) })
	return set
}

func readAll(t *testing.T, set *pack.Set, bodies [][]byte) {
	t.Helper()
	for _, body := range bodies {
		got, err := set.Read(pack.Sum(body))
		require.NoError(t, err)
		require.Equal(t, body, got)
	}
}

func TestBodiesReadBackAsWritten(t *testing.T) {
	large := bytes.Repeat([]byte(`{"a":"0123456789abcdef"}`), 2*pack.BlockTarget/24)
	tests := []struct {
		name   string
		bodies [][]byte
	}{
		{name: "none"},
		{name: "one", bodies: [][]byte{document(1)}},
		{name: "ten thousand", bodies: documents(10000)},
		{name: "one larger than a block", bodies: [][]byte{document(1), large, document(2)}},
		{name: "an empty body", bodies: [][]byte{{}, document(3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writePack(t, tt.bodies...)
			set := openSet(t, dir)

			readAll(t, set, tt.bodies)
			for _, name := range set.Names() {
				require.NoError(t, pack.Check(dir, name, pack.Deep))
			}
		})
	}
}

func documents(n int) [][]byte {
	bodies := make([][]byte, n)
	for i := range bodies {
		bodies[i] = document(i)
	}
	return bodies
}

func TestABodyNeverWrittenIsNotFound(t *testing.T) {
	set := openSet(t, writePack(t, document(1)))

	_, err := set.Read(pack.Sum(document(2)))

	require.ErrorIs(t, err, pack.ErrNotFound)
	assert.False(t, set.Has(pack.Sum(document(2))))
	assert.True(t, set.Has(pack.Sum(document(1))))
}

func TestARebuiltIndexFindsWhatTheWrittenOneDid(t *testing.T) {
	bodies := documents(3000)
	dir := writePack(t, bodies...)
	name := onlyPack(t, dir)
	written, err := os.ReadFile(filepath.Join(dir, name+pack.IndexExt))
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(dir, name+pack.IndexExt)))
	readAll(t, openSet(t, dir), bodies)
	require.NoError(t, pack.RebuildIndex(dir, name))

	rebuilt, err := os.ReadFile(filepath.Join(dir, name+pack.IndexExt))
	require.NoError(t, err)
	assert.Equal(t, written, rebuilt)
}

func onlyPack(t *testing.T, dir string) string {
	t.Helper()
	names, err := pack.Names(dir)
	require.NoError(t, err)
	require.Len(t, names, 1)
	return names[0]
}

func TestAFlippedByteFailsTheCRC(t *testing.T) {
	dir := writePack(t, documents(50)...)
	name := onlyPack(t, dir)
	path := filepath.Join(dir, name+pack.PackExt)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-10] ^= 0xff
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = openSet(t, dir).Read(pack.Sum(document(7)))

	require.ErrorIs(t, err, pack.ErrCorrupt)
	require.ErrorIs(t, pack.Check(dir, name, pack.Shallow), pack.ErrCorrupt)
}

// TestABodySwappedForAnotherFailsTheRehash builds a pack whose index was
// written for different bodies: the CRCs hold, so only rehashing can tell.
func TestABodySwappedForAnotherFailsTheRehash(t *testing.T) {
	dir := writePack(t, document(1))
	name := onlyPack(t, dir)
	other := writePack(t, document(2))
	swapped, err := os.ReadFile(filepath.Join(other, onlyPack(t, other)+pack.PackExt))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+pack.PackExt), swapped, 0o600))

	_, err = openSet(t, dir).Read(pack.Sum(document(1)))

	require.ErrorIs(t, err, pack.ErrCorrupt)
	require.NoError(t, pack.Check(dir, name, pack.Shallow), "the CRCs still hold")
	require.ErrorIs(t, pack.Check(dir, name, pack.Deep), pack.ErrCorrupt)
}

func TestAnotherVersionIsAnUnknownFormat(t *testing.T) {
	dir := writePack(t, document(1))
	name := onlyPack(t, dir)
	path := filepath.Join(dir, name+pack.PackExt)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bytes.Replace(data, []byte("alchemist-pack 1\n"), []byte("alchemist-pack 2\n"), 1), 0o600))

	require.ErrorIs(t, pack.Check(dir, name, pack.Shallow), pack.ErrUnknownFormat)
}

func TestCollectKeepsWhatIsLiveAndReclaimsTheRest(t *testing.T) {
	tests := []struct {
		name          string
		live          int
		wantRemoved   int
		wantRewritten int
	}{
		{name: "nothing live deletes the pack", live: 0, wantRemoved: 1},
		{name: "under half live is rewritten", live: 40, wantRewritten: 1},
		{name: "half live stays", live: 50},
		{name: "all live stays", live: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodies := documents(100)
			dir := writePack(t, bodies...)
			live := map[pack.Hash]bool{}
			for _, body := range bodies[:tt.live] {
				live[pack.Sum(body)] = true
			}

			done, err := pack.Collect(dir, "gc", live)

			require.NoError(t, err)
			assert.Equal(t, tt.wantRemoved, done.Removed)
			assert.Equal(t, tt.wantRewritten, done.Rewritten)
			readAll(t, openSet(t, dir), bodies[:tt.live])
		})
	}
}

func TestCollectMergesTheSmallestPastSixtyFourPacks(t *testing.T) {
	dir := t.TempDir()
	var bodies [][]byte
	for i := range 70 {
		writer, err := pack.NewWriter(dir, fmt.Sprintf("p%02d", i))
		require.NoError(t, err)
		body := document(i)
		require.NoError(t, writer.Add(pack.Sum(body), body))
		require.NoError(t, writer.Close())
		bodies = append(bodies, body)
	}
	live := map[pack.Hash]bool{}
	for _, body := range bodies {
		live[pack.Sum(body)] = true
	}

	_, err := pack.Collect(dir, "gc", live)

	require.NoError(t, err)
	names, err := pack.Names(dir)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(names), 64)
	readAll(t, openSet(t, dir), bodies)
}

func TestASecondWriterWithTheSamePrefixNeverReplacesAPack(t *testing.T) {
	dir := writePack(t, document(1))
	writer, err := pack.NewWriter(dir, prefix)
	require.NoError(t, err)
	require.NoError(t, writer.Add(pack.Sum(document(2)), document(2)))
	require.NoError(t, writer.Close())

	readAll(t, openSet(t, dir), [][]byte{document(1), document(2)})
}

func FuzzThePackReaderRefusesWhatItCannotRead(f *testing.F) {
	dir := f.TempDir()
	writer, err := pack.NewWriter(dir, prefix)
	require.NoError(f, err)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range 40 {
		body := document(random.IntN(1000) + i*1000)
		require.NoError(f, writer.Add(pack.Sum(body), body))
	}
	require.NoError(f, writer.Close())
	name := writer.Published()[0]
	valid, err := os.ReadFile(filepath.Join(dir, name+pack.PackExt))
	require.NoError(f, err)
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add([]byte("alchemist-pack 1\n\xff\xff\xff\xff\x00\x00\x00\x10\x01\x00\x00\x00\x01\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzed := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(fuzzed, name+pack.PackExt), data, 0o600))
		require.NoError(t, os.Link(filepath.Join(dir, name+pack.IndexExt), filepath.Join(fuzzed, name+pack.IndexExt)))

		err := pack.Check(fuzzed, name, pack.Deep)

		if err != nil {
			assertRefused(t, err)
		}
		require.NotPanics(t, func() { _ = pack.Walk(fuzzed, name, func(pack.Body) error { return nil }) })
	})
}

func FuzzTheIndexReaderRefusesWhatItCannotRead(f *testing.F) {
	dir := writePack(f, documents(30)...)
	names, err := pack.Names(dir)
	require.NoError(f, err)
	name := names[0]
	valid, err := os.ReadFile(filepath.Join(dir, name+pack.IndexExt))
	require.NoError(f, err)
	f.Add(valid)
	f.Add([]byte("alchemist-pack-index 1\n\xff\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzed := t.TempDir()
		require.NoError(t, os.Link(filepath.Join(dir, name+pack.PackExt), filepath.Join(fuzzed, name+pack.PackExt)))
		require.NoError(t, os.WriteFile(filepath.Join(fuzzed, name+pack.IndexExt), data, 0o600))

		set, err := pack.OpenSet(fuzzed)
		if err != nil {
			assertRefused(t, err)
			return
		}
		defer set.Close()
		for _, body := range documents(30) {
			if _, err := set.Read(pack.Sum(body)); err != nil && !errors.Is(err, pack.ErrNotFound) {
				assertRefused(t, err)
			}
		}
	})
}

func assertRefused(t *testing.T, err error) {
	t.Helper()
	assert.True(t, errors.Is(err, pack.ErrCorrupt) || errors.Is(err, pack.ErrUnknownFormat), "refused with %v", err)
}

func TestRemoveDurablyRemovesAndToleratesWhatIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	require.NoError(t, pack.RemoveDurably(path))
	require.NoError(t, pack.RemoveDurably(path), "a second removal finds nothing and is no error")

	assert.NoFileExists(t, path)
}
