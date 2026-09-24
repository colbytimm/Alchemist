package saved_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/saved"
)

const account = "prod"

func openStore(t *testing.T) saved.Dir {
	t.Helper()
	return saved.Open(filepath.Join(t.TempDir(), saved.DirName))
}

func openOrders() saved.Query {
	return saved.Query{
		Name:  "open orders",
		Text:  "SELECT c.id, c.total\nFROM c\nWHERE c.status = \"open\"",
		Scope: []string{"sales", "orders"},
	}
}

// writeFile puts a file in the account directory by hand, the way a user
// editing it outside the application would.
func writeFile(t *testing.T, store saved.Dir, file, content string) string {
	t.Helper()
	dir := store.AccountPath(account)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func list(t *testing.T, store saved.Store) saved.Listing {
	t.Helper()
	listing, err := store.List(account)
	require.NoError(t, err)
	return listing
}

func names(listing saved.Listing) []string {
	var all []string
	for _, q := range listing.Queries {
		all = append(all, q.Name)
	}
	return all
}

// only lists the single query a test saved, less its modification time.
func only(t *testing.T, store saved.Store) saved.Query {
	t.Helper()
	listing := list(t, store)
	require.Len(t, listing.Queries, 1)
	q := listing.Queries[0]
	assert.False(t, q.Saved.IsZero(), "the modification time is the saved time")
	q.Saved = time.Time{}
	return q
}

func TestNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "words and a space", input: "open orders", valid: true},
		{name: "every allowed character", input: "a-b_c.d 9", valid: true},
		{name: "one character", input: "a", valid: true},
		{name: "ends with underscore", input: "late_", valid: true},
		{name: "sixty-four characters", input: strings.Repeat("a", 64), valid: true},
		{name: "empty", input: ""},
		{name: "sixty-five characters", input: strings.Repeat("a", 65)},
		{name: "leading dot", input: ".hidden"},
		{name: "leading space", input: " orders"},
		{name: "trailing space", input: "orders "},
		{name: "trailing dot", input: "orders."},
		{name: "slash", input: "a/b"},
		{name: "backslash", input: `a\b`},
		{name: "parent directory", input: ".."},
		{name: "overwrite mark", input: "orders!"},
		{name: "non-ASCII", input: "orders€"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := saved.Query{Name: tt.input, Text: "SELECT * FROM c"}
			err := openStore(t).Create(account, q)
			if tt.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, saved.ErrInvalidName)
		})
	}
}

func TestAQueryRoundTripsWithItsScope(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	assert.Equal(t, openOrders(), only(t, store))
}

func TestAQueryRoundTripsWithoutAScope(t *testing.T) {
	store := openStore(t)
	q := saved.Query{Name: "every customer", Text: "SELECT * FROM sales.customers c"}
	require.NoError(t, store.Create(account, q))

	assert.Equal(t, q, only(t, store))
}

func TestTheFileIsTheHeaderThenTheTextAsTyped(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	data, err := os.ReadFile(filepath.Join(store.AccountPath(account), "open orders.sql"))
	require.NoError(t, err)
	assert.Equal(t, "-- alchemist: scope=sales/orders\n"+openOrders().Text+"\n", string(data))
}

func TestAScopeSegmentMayHoldADot(t *testing.T) {
	store := openStore(t)
	q := saved.Query{Name: "dotted", Text: "SELECT * FROM c", Scope: []string{"sales.eu", "orders.2026"}}
	require.NoError(t, store.Create(account, q))

	assert.Equal(t, q, only(t, store))
}

func TestReadingAFile(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantText  string
		wantScope []string
	}{
		{
			name:     "no header is no scope",
			content:  "SELECT * FROM c\n",
			wantText: "SELECT * FROM c",
		},
		{
			name:     "a comment of the text's own is kept",
			content:  "-- the open ones\nSELECT * FROM c",
			wantText: "-- the open ones\nSELECT * FROM c",
		},
		{
			name:     "an unknown header key is dropped",
			content:  "-- alchemist: theme=dark\nSELECT * FROM c",
			wantText: "SELECT * FROM c",
		},
		{
			name:     "a scope with an empty segment is none",
			content:  "-- alchemist: scope=sales//orders\nSELECT * FROM c",
			wantText: "SELECT * FROM c",
		},
		{
			name:     "a malformed header line is dropped",
			content:  "-- alchemist: nonsense\nSELECT * FROM c",
			wantText: "SELECT * FROM c",
		},
		{
			name:     "CRLF line endings read as LF",
			content:  "-- alchemist: scope=sales/orders\r\nSELECT *\r\nFROM c\r\n",
			wantText: "SELECT *\nFROM c", wantScope: []string{"sales", "orders"},
		},
		{
			name:     "trailing blank lines are trimmed",
			content:  "SELECT *\nFROM c\n\n  \n\n",
			wantText: "SELECT *\nFROM c",
		},
		{
			name:     "leading blank lines are kept",
			content:  "\nSELECT * FROM c",
			wantText: "\nSELECT * FROM c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openStore(t)
			writeFile(t, store, "q.sql", tt.content)

			got := only(t, store)
			assert.Equal(t, tt.wantText, got.Text)
			assert.Equal(t, tt.wantScope, got.Scope)
		})
	}
}

func TestFilesTheStoreCannotUseAreSkippedAndTheRestListed(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
	}{
		{name: "a name that breaks the rules", file: "bad name?.sql", content: "SELECT 1"},
		{name: "an empty body", file: "empty.sql", content: ""},
		{name: "a whitespace-only body", file: "blank.sql", content: " \n\t\n"},
		{name: "a header and nothing else", file: "header.sql", content: "-- alchemist: scope=a/b\n"},
		{name: "text that is not UTF-8", file: "binary.sql", content: "SELECT \xff\xfe"},
		{name: "a file over the size limit", file: "huge.sql", content: strings.Repeat("x", 1<<20+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openStore(t)
			require.NoError(t, store.Create(account, openOrders()))
			path := writeFile(t, store, tt.file, tt.content)

			listing := list(t, store)
			assert.Equal(t, []string{"open orders"}, names(listing))
			require.Len(t, listing.Skipped, 1)
			assert.Equal(t, path, listing.Skipped[0].File)
			assert.Error(t, listing.Skipped[0].Err)
		})
	}
}

func TestFilesThatAreNotQueriesAreIgnoredSilently(t *testing.T) {
	store := openStore(t)
	writeFile(t, store, ".hidden.sql", "SELECT 1")
	writeFile(t, store, ".open orders.sql.tmp", "SELECT 1")
	writeFile(t, store, "notes.txt", "SELECT 1")
	require.NoError(t, os.Mkdir(filepath.Join(store.AccountPath(account), "folder.sql"), 0o700))

	assert.Equal(t, saved.Listing{}, list(t, store))
}

func TestOfTwoNamesDifferingInCaseTheFirstInByteOrderIsListed(t *testing.T) {
	store := openStore(t)
	writeFile(t, store, "Orders.sql", "SELECT 1")
	path := writeFile(t, store, "orders.sql", "SELECT 2")

	listing := list(t, store)
	require.Len(t, listing.Queries, 1)
	assert.Equal(t, "SELECT 1", listing.Queries[0].Text)
	require.Len(t, listing.Skipped, 1)
	assert.Equal(t, path, listing.Skipped[0].File)
}

func TestAFileThatCannotBeReadIsSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	store := openStore(t)
	path := writeFile(t, store, "locked.sql", "SELECT 1")
	require.NoError(t, os.Chmod(path, 0o000))

	listing := list(t, store)
	assert.Empty(t, listing.Queries)
	require.Len(t, listing.Skipped, 1)
}

func TestTheListIsSortedByNameIgnoringCase(t *testing.T) {
	store := openStore(t)
	for _, name := range []string{"late-shipments", "Every customer", "open orders", "archive"} {
		require.NoError(t, store.Create(account, saved.Query{Name: name, Text: "SELECT 1"}))
	}

	assert.Equal(t, []string{"archive", "Every customer", "late-shipments", "open orders"}, names(list(t, store)))
}

func TestListingAnAccountWithNothingSavedCreatesNothing(t *testing.T) {
	store := openStore(t)

	assert.Equal(t, saved.Listing{}, list(t, store))
	assert.NoDirExists(t, store.Root)
}

func TestCreateMakesPrivateDirectoriesAndFiles(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	dir, err := os.Stat(store.AccountPath(account))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
	file, err := os.Stat(filepath.Join(store.AccountPath(account), "open orders.sql"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), file.Mode().Perm())
}

func TestCreateRefusesATakenNameInAnyCase(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	err := store.Create(account, saved.Query{Name: "Open Orders", Text: "SELECT 2"})

	require.ErrorIs(t, err, saved.ErrExists)
	assert.Equal(t, openOrders(), only(t, store))
}

func TestCreateRefusesAnEmptyQuery(t *testing.T) {
	err := openStore(t).Create(account, saved.Query{Name: "blank", Text: " \n "})

	require.ErrorIs(t, err, saved.ErrEmptyQuery)
}

func TestReplaceOverwritesAndKeepsTheExistingSpelling(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	require.NoError(t, store.Replace(account, saved.Query{Name: "OPEN ORDERS", Text: "SELECT 2"}))

	assert.Equal(t, saved.Query{Name: "open orders", Text: "SELECT 2"}, only(t, store))
}

func TestReplaceCreatesANewName(t *testing.T) {
	store := openStore(t)

	require.NoError(t, store.Replace(account, openOrders()))

	assert.Equal(t, openOrders(), only(t, store))
}

func TestNoTemporaryFileRemainsAfterAWrite(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))
	require.NoError(t, store.Replace(account, openOrders()))

	entries, err := os.ReadDir(store.AccountPath(account))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "open orders.sql", entries[0].Name())
}

func TestRenameMovesTheQuery(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	require.NoError(t, store.Rename(account, "open orders", "pending"))

	want := openOrders()
	want.Name = "pending"
	assert.Equal(t, want, only(t, store))
}

func TestRenameAllowsACaseOnlyChange(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	require.NoError(t, store.Rename(account, "open orders", "Open Orders"))

	assert.Equal(t, []string{"Open Orders"}, names(list(t, store)))
}

func TestRenameRefusesATakenName(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))
	require.NoError(t, store.Create(account, saved.Query{Name: "pending", Text: "SELECT 2"}))

	err := store.Rename(account, "open orders", "PENDING")

	require.ErrorIs(t, err, saved.ErrExists)
	assert.Equal(t, []string{"open orders", "pending"}, names(list(t, store)))
}

func TestRenameRefusesTheOverwriteMark(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	require.ErrorIs(t, store.Rename(account, "open orders", "pending!"), saved.ErrInvalidName)
}

func TestRenameAndRemoveOfAMissingQueryAreNotFound(t *testing.T) {
	store := openStore(t)

	require.ErrorIs(t, store.Rename(account, "absent", "present"), saved.ErrNotFound)
	require.ErrorIs(t, store.Remove(account, "absent"), saved.ErrNotFound)
}

func TestRemoveDeletesTheQueryInAnyCase(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create(account, openOrders()))

	require.NoError(t, store.Remove(account, "OPEN ORDERS"))

	assert.Empty(t, list(t, store).Queries)
}

func TestAnAccountThatCouldEscapeTheRootIsRefusedByEveryMethod(t *testing.T) {
	parent := t.TempDir()
	store := saved.Open(filepath.Join(parent, saved.DirName))
	outside := "../x"
	q := openOrders()

	_, err := store.List(outside)
	require.ErrorIs(t, err, saved.ErrInvalidAccount)
	require.ErrorIs(t, store.Create(outside, q), saved.ErrInvalidAccount)
	require.ErrorIs(t, store.Replace(outside, q), saved.ErrInvalidAccount)
	require.ErrorIs(t, store.Rename(outside, q.Name, "other"), saved.ErrInvalidAccount)
	require.ErrorIs(t, store.Remove(outside, q.Name), saved.ErrInvalidAccount)
	_, err = store.RemoveAccount(outside)
	require.ErrorIs(t, err, saved.ErrInvalidAccount)

	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing was created beside the root")
}

func TestAccountsKeepTheirQueriesApart(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create("prod", openOrders()))
	staging := saved.Query{Name: "open orders", Text: "SELECT 2"}
	require.NoError(t, store.Create("staging", staging))

	prod, err := store.List("prod")
	require.NoError(t, err)
	other, err := store.List("staging")
	require.NoError(t, err)

	require.Len(t, prod.Queries, 1)
	require.Len(t, other.Queries, 1)
	assert.Equal(t, openOrders().Text, prod.Queries[0].Text)
	assert.Equal(t, staging.Text, other.Queries[0].Text)
}

func TestRemoveAccountRemovesOnlyThatAccountAndCountsItsQueries(t *testing.T) {
	store := openStore(t)
	require.NoError(t, store.Create("prod", openOrders()))
	require.NoError(t, store.Create("prod", saved.Query{Name: "pending", Text: "SELECT 2"}))
	require.NoError(t, store.Create("staging", openOrders()))

	removed, err := store.RemoveAccount("prod")

	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.NoDirExists(t, store.AccountPath("prod"))
	assert.DirExists(t, store.AccountPath("staging"))
}

func TestRemoveAccountWithNothingSavedRemovesNothing(t *testing.T) {
	removed, err := openStore(t).RemoveAccount("prod")

	require.NoError(t, err)
	assert.Zero(t, removed)
}

func TestUnavailableListsNothingAndRefusesEveryWrite(t *testing.T) {
	reason := errors.New("no config directory")
	store := saved.Unavailable{Err: reason}

	listing, err := store.List(account)
	require.NoError(t, err)
	assert.Equal(t, saved.Listing{}, listing)
	require.ErrorIs(t, store.Create(account, openOrders()), reason)
	require.ErrorIs(t, store.Replace(account, openOrders()), reason)
	require.ErrorIs(t, store.Rename(account, "a", "b"), reason)
	require.ErrorIs(t, store.Remove(account, "a"), reason)
}

func TestUnavailableWithoutAReasonStillRefuses(t *testing.T) {
	assert.Error(t, saved.Unavailable{}.Create(account, openOrders()))
}

func FuzzAListedFileSurvivesBeingSavedAgain(f *testing.F) {
	for _, seed := range []string{
		"SELECT * FROM c",
		"-- alchemist: scope=sales/orders\nSELECT *\nFROM c\n",
		"-- alchemist: scope=\n-- alchemist: x\n\n-- alchemist: scope=a/b\nSELECT 1",
		"\r\n\r\nSELECT 1\r\n\r\n",
		"-- alchemist:",
		"\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		store := saved.Open(t.TempDir())
		writeFile(t, store, "q.sql", content)
		listing := list(t, store)
		if len(listing.Queries) == 0 {
			return
		}
		first := listing.Queries[0]

		require.NoError(t, store.Replace(account, first))

		again := list(t, store)
		require.Len(t, again.Queries, 1)
		assert.Equal(t, first.Text, again.Queries[0].Text)
		assert.Equal(t, first.Scope, again.Queries[0].Scope)
	})
}
