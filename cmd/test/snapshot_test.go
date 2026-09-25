package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

// addMockProfile adds a profile served by the mock adapter, with a key, so
// take can connect it.
func (h harness) addMockProfile(t *testing.T, name string) {
	t.Helper()
	_, err := h.run("key\n", "profile", "add", name, "--adapter", mock.Name, "--endpoint", "https://mock")
	require.NoError(t, err)
}

func snapshotsDir(t *testing.T) string {
	t.Helper()
	root, err := snapshot.DefaultRoot()
	require.NoError(t, err)
	return root
}

func TestSnapshotTakePrintsOneSummaryLinePerContainer(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")

	first, err := h.run("", "snapshot", "take", "m", "sales.orders", "--note", "nightly")
	require.NoError(t, err)
	second, err := h.run("", "snapshot", "take", "m", "sales.orders")
	require.NoError(t, err)
	database, err := h.run("", "snapshot", "take", "m", "sales")
	require.NoError(t, err)

	assert.Regexp(t, `^sales\.orders \d{8}T\d{6}Z: 0 items, first snapshot, [\d.]+ RU, .+ new\n$`, first)
	assert.Contains(t, second, "+0 −0 ~0")
	assert.Equal(t, 2, strings.Count(database, "\n"), "one line for each container of sales")
	assert.Contains(t, database, "sales.customers ")
}

func TestSnapshotTakeRefusesAnUnknownProfileAndContainer(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")

	_, err := h.run("", "snapshot", "take", "nobody", "sales.orders")
	require.Error(t, err)
	_, err = h.run("", "snapshot", "take", "m", "sales.nothing")
	require.Error(t, err)
}

func TestSnapshotScopeArguments(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	_, err := h.run("", "snapshot", "take", "m", "sales.orders")
	require.NoError(t, err)
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "db and container as one argument", args: []string{"snapshot", "list", "m", "sales.orders"}},
		{name: "db and container by flag", args: []string{"snapshot", "list", "m", "--database", "sales", "--container", "orders"}},
		{name: "a container flag alone", args: []string{"snapshot", "list", "m", "--container", "orders"}, wantErr: true},
		{name: "a diff of a database", args: []string{"snapshot", "diff", "m", "sales"}, wantErr: true},
		{name: "an export with no file", args: []string{"snapshot", "export", "m", "sales.orders"}, wantErr: true},
		{name: "a prune with no keep-last", args: []string{"snapshot", "prune", "m", "sales"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := h.run("", tt.args...)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, out, "m/sales.orders")
		})
	}
}

func TestSnapshotDiffResolvesReferences(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	for range 3 {
		_, err := h.run("", "snapshot", "take", "m", "sales.orders")
		require.NoError(t, err)
	}
	listed := listJSON(t, h, "m")
	require.Len(t, listed, 1)
	ids := listed[0].Snapshots

	byDefault, err := h.run("", "snapshot", "diff", "m", "sales.orders")
	require.NoError(t, err)
	byPrefix, err := h.run("", "snapshot", "diff", "m", "sales.orders", ids[0].ID, "latest")
	require.NoError(t, err)
	_, err = h.run("", "snapshot", "diff", "m", "sales.orders", "1999")
	require.ErrorIs(t, err, snapshot.ErrNoSnapshot)

	assert.Contains(t, byDefault, ids[1].ID+" → "+ids[2].ID+": +0 −0 ~0")
	assert.Contains(t, byPrefix, ids[0].ID+" → "+ids[2].ID)
	assert.Contains(t, byDefault, "definition unchanged")
}

type listedJSON struct {
	Account   string            `json:"account"`
	Profile   bool              `json:"profile"`
	Snapshots []snapshot.Record `json:"snapshots"`
}

func listJSON(t *testing.T, h harness, args ...string) []listedJSON {
	t.Helper()
	out, err := h.run("", append([]string{"snapshot", "list", "--json"}, args...)...)
	require.NoError(t, err)
	var listed []listedJSON
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	return listed
}

func TestSnapshotDiffLiveTakesASnapshotAndKeepsIt(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	_, err := h.run("", "snapshot", "take", "m", "sales.orders")
	require.NoError(t, err)

	out, err := h.run("", "snapshot", "diff", "m", "sales.orders", "--live", "-o", filepath.Join(t.TempDir(), "diff.json"))

	require.NoError(t, err)
	assert.Contains(t, out, "+0 −0 ~0")
	assert.Len(t, listJSON(t, h, "m")[0].Snapshots, 2)
}

func TestSnapshotExportDeletePruneAndVerify(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	for range 3 {
		_, err := h.run("", "snapshot", "take", "m", "sales.orders")
		require.NoError(t, err)
	}
	path := filepath.Join(t.TempDir(), "items.jsonl")

	exported, err := h.run("", "snapshot", "export", "m", "sales.orders", "latest", "-o", path)
	require.NoError(t, err)
	assert.Contains(t, exported, "wrote 0 items")
	_, err = os.Stat(path)
	require.NoError(t, err)

	dry, err := h.run("", "snapshot", "prune", "m", "sales", "--keep-last", "1", "--dry-run")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(dry, "would delete m/sales.orders "))
	assert.Len(t, listJSON(t, h, "m")[0].Snapshots, 3, "a dry run deletes nothing")

	deleted, err := h.run("", "snapshot", "delete", "m", "sales.orders", "previous")
	require.NoError(t, err)
	assert.Contains(t, deleted, "deleted m/sales.orders ")
	_, err = h.run("", "snapshot", "prune", "m", "sales", "--keep-last", "1")
	require.NoError(t, err)
	assert.Len(t, listJSON(t, h, "m")[0].Snapshots, 1)

	verified, err := h.run("", "snapshot", "verify", "m", "sales", "--deep")
	require.NoError(t, err)
	assert.Contains(t, verified, "m/sales.orders: ok, 1 snapshots")
}

func TestSnapshotListMarksAnAccountWithNoProfile(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	_, err := h.run("", "snapshot", "take", "m", "sales.orders")
	require.NoError(t, err)
	_, err = h.run("", "profile", "remove", "m")
	require.NoError(t, err)

	out, err := h.run("", "snapshot", "list")

	require.NoError(t, err)
	assert.Contains(t, out, "m/sales.orders (no profile): 1 snapshot")
	assert.False(t, listJSON(t, h)[0].Profile)
}

func TestSnapshotDirMovesTheStore(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	elsewhere := t.TempDir()

	_, err := h.run("", "--snapshot-dir", elsewhere, "snapshot", "take", "m", "sales.orders")

	require.NoError(t, err)
	stores, err := snapshot.Stores(elsewhere)
	require.NoError(t, err)
	assert.Len(t, stores, 1)
	stores, err = snapshot.Stores(snapshotsDir(t))
	require.NoError(t, err)
	assert.Empty(t, stores)
}

func TestProfileRemoveReportsSnapshotsAndPurgeRemovesThem(t *testing.T) {
	h := newHarness(t)
	h.addMockProfile(t, "m")
	h.addMockProfile(t, "n")
	for _, profile := range []string{"m", "n"} {
		_, err := h.run("", "snapshot", "take", profile, "sales.orders")
		require.NoError(t, err)
	}

	kept, err := h.run("", "profile", "remove", "m")
	require.NoError(t, err)
	purged, err := h.run("", "profile", "remove", "n", "--purge")
	require.NoError(t, err)

	assert.Contains(t, kept, "kept 1 snapshot (")
	assert.Contains(t, kept, filepath.Join(snapshotsDir(t), "m"))
	assert.Contains(t, kept, "alchemist profile remove m --purge")
	assert.Equal(t, "removed profile n and its 1 snapshot\n", purged)
	_, err = os.Stat(filepath.Join(snapshotsDir(t), "n"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}
