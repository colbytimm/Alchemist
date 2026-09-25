package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// Sentinels, each wrapped with the store and, where there is one, the file.
var (
	ErrUnknownFormat = pack.ErrUnknownFormat
	ErrCorrupt       = pack.ErrCorrupt
	ErrLocked        = errors.New("store is locked")
	ErrNoSnapshot    = errors.New("no such snapshot")
	ErrTooManyItems  = errors.New("too many items")
)

const (
	formatFile = "FORMAT"
	formatKind = "alchemist-snapshot-store"
	storeFile  = "store.json"
	lockFile   = "lock"

	recordsDir   = "records"
	changesDir   = "changes"
	manifestsDir = "manifests"
	packsDir     = "packs"
	groupsDir    = "groups"

	jsonExt      = ".json"
	changesExt   = ".changes"
	manifestExt  = ".manifest"
	rangeDivider = ".."

	segmentHashDigits = 8
	documentFormat    = 1
)

// unsafeSegment is anything a database or container name may hold that a
// directory name should not.
var unsafeSegment = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// DefaultRoot is where snapshots live unless configured otherwise:
// $XDG_DATA_HOME/alchemist/snapshots, or ~/.local/share/alchemist/snapshots.
// They are user data, not state a reinstall may lose.
func DefaultRoot() (string, error) {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return filepath.Join(base, "alchemist", "snapshots"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("snapshot: locate home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "alchemist", "snapshots"), nil
}

// Segment is a database or container name as a directory: readable, and
// distinct from every other name on a case-insensitive filesystem, since
// the hash of the exact name follows a ~. A group directory has no ~, so
// no name can collide with it.
func Segment(name string) string {
	sum := sha256.Sum256([]byte(name))
	return unsafeSegment.ReplaceAllString(name, "_") + "~" + hex.EncodeToString(sum[:])[:segmentHashDigits]
}

// Location names one container's store. The account is a profile name,
// which config guarantees is safe as a path segment.
type Location struct {
	Root      string
	Account   string
	Database  string
	Container string
}

func (l Location) Dir() string {
	return filepath.Join(l.DatabaseDir(), Segment(l.Container))
}

// DatabaseDir holds the stores of the database's containers and its groups.
func (l Location) DatabaseDir() string {
	return filepath.Join(l.AccountDir(), Segment(l.Database))
}

func (l Location) AccountDir() string {
	return filepath.Join(l.Root, l.Account)
}

func (l Location) String() string {
	if l.Container == "" {
		return l.Account + "/" + l.Database
	}
	return l.Account + "/" + l.Database + "." + l.Container
}

func (l Location) Path() []string {
	return []string{l.Database, l.Container}
}

// storeMeta is store.json: the real names behind the directory's segments.
type storeMeta struct {
	Format    int    `json:"format"`
	Account   string `json:"account"`
	Database  string `json:"database"`
	Container string `json:"container"`
}

// Stores lists every container store under root, from each one's
// store.json, including those of accounts no profile names any more.
func Stores(root string) ([]Location, error) {
	metas, err := filepath.Glob(filepath.Join(root, "*", "*~*", "*~*", storeFile))
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	locations := make([]Location, 0, len(metas))
	for _, path := range metas {
		var meta storeMeta
		if err := readJSON(path, &meta); err != nil {
			return nil, err
		}
		locations = append(locations, Location{Root: root, Account: meta.Account, Database: meta.Database, Container: meta.Container})
	}
	return locations, nil
}

// RemoveAccount deletes every snapshot of account. It is what a purged
// profile asks for: copies of an account's data go with the account.
func RemoveAccount(root, account string) error {
	if err := os.RemoveAll(Location{Root: root, Account: account}.AccountDir()); err != nil {
		return fmt.Errorf("snapshot: remove %s: %w", account, err)
	}
	return nil
}

func ensureDir(path string) error {
	if err := os.MkdirAll(path, pack.DirMode); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	return nil
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("snapshot: %w", err)
	}
	return true, nil
}

// changesName is the change set leading from parent to child.
func changesName(parent, child string) string {
	return parent + rangeDivider + child + changesExt
}

func trimExt(name, ext string) (string, bool) {
	return strings.CutSuffix(name, ext)
}
