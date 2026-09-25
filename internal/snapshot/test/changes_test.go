package snapshot_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

func key(t *testing.T, item string, paths ...string) snapshot.Key {
	t.Helper()
	k, err := snapshot.ItemKey(json.RawMessage(item), paths)
	require.NoError(t, err)
	return k
}

func entry(body string, version int) snapshot.Entry {
	return snapshot.Entry{Hash: pack.Sum([]byte(body)), Size: int64(len(body)), Version: snapshot.Fingerprint{byte(version)}, ModifiedUnix: int64(1000 + version)}
}

// randomManifest holds n of a pool of 3n keys, each at one of a few bodies.
func randomManifest(random *rand.Rand, n int) snapshot.Manifest {
	m := snapshot.Manifest{}
	for range n {
		k := snapshot.Key(fmt.Sprintf("k%03d", random.IntN(3*n)))
		m[k] = entry(fmt.Sprintf("body %d", random.IntN(4)), random.IntN(3))
	}
	return m
}

func TestUndoingAnAppliedChangeSetGivesBackTheManifest(t *testing.T) {
	random := rand.New(rand.NewPCG(19, 1))
	for range 200 {
		parent, child := randomManifest(random, 20), randomManifest(random, 20)
		changes := snapshot.Changes(parent, child)

		applied := parent.Clone()
		applied.Apply(changes)
		require.Equal(t, child, applied)
		applied.Undo(changes)
		require.Equal(t, parent, applied)
	}
}

func TestAChangeSetIsInKeyOrder(t *testing.T) {
	random := rand.New(rand.NewPCG(19, 2))
	changes := snapshot.Changes(randomManifest(random, 50), randomManifest(random, 50))

	assert.True(t, slices.IsSortedFunc(changes, func(a, b snapshot.Change) int { return cmp.Compare(a.Key, b.Key) }))
}

func TestComposeIsAssociative(t *testing.T) {
	random := rand.New(rand.NewPCG(19, 3))
	for range 200 {
		m := []snapshot.Manifest{randomManifest(random, 15), randomManifest(random, 15), randomManifest(random, 15), randomManifest(random, 15)}
		a, b, c := snapshot.Changes(m[0], m[1]), snapshot.Changes(m[1], m[2]), snapshot.Changes(m[2], m[3])

		left := snapshot.Compose(snapshot.Compose(a, b), c)
		right := snapshot.Compose(a, snapshot.Compose(b, c))

		require.Equal(t, left, right)
		require.Equal(t, snapshot.Changes(m[0], m[3]), left, "composing is the same as diffing the ends")
	}
}

func TestAChangeRevertedLaterDropsOutOfTheComposition(t *testing.T) {
	k := snapshot.Key("k")
	original, edited := entry("a", 1), entry("b", 2)
	forth := snapshot.ChangeSet{{Key: k, Before: &original, After: &edited}}
	back := snapshot.ChangeSet{{Key: k, Before: &edited, After: &original}}

	assert.Empty(t, snapshot.Compose(forth, back))
}

func TestAnUndefinedKeyValueAndNullAreDifferentKeys(t *testing.T) {
	undefined := key(t, `{"id":"a"}`, "/customerId")
	null := key(t, `{"id":"a","customerId":null}`, "/customerId")

	assert.NotEqual(t, undefined, null)
	identity, err := undefined.Identity()
	require.NoError(t, err)
	assert.Equal(t, snapshot.Identity{PartitionKey: []json.RawMessage{nil}, ID: "a"}, identity)
	identity, err = null.Identity()
	require.NoError(t, err)
	assert.Equal(t, "null", identity.PartitionKeyText())
}

func TestKeysSpellNumbersOneWay(t *testing.T) {
	assert.Equal(t, key(t, `{"id":"a","n":1}`, "/n"), key(t, `{"id":"a","n":1.0}`, "/n"))
	assert.NotEqual(t, key(t, `{"id":"a","n":1}`, "/n"), key(t, `{"id":"a","n":"1"}`, "/n"))
}

func TestAKeyReadsBackAsItsIdentity(t *testing.T) {
	k := key(t, `{"id":"o-1","shipTo":{"region":"east"},"tenant":7}`, "/tenant", "/shipTo/region")

	identity, err := k.Identity()

	require.NoError(t, err)
	assert.Equal(t, "o-1", identity.ID)
	assert.Equal(t, "7/east", identity.PartitionKeyText())
}
