// Package saved keeps the queries a user chose to name, one plain .sql file
// per query in a directory per account. The files hold query text and at most
// a scope: never a key, an endpoint, or anything else from a profile.
package saved

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// DirName is the directory of saved queries inside the config directory.
const DirName = "queries"

const Extension = ".sql"

var (
	ErrExists         = errors.New("a saved query already has that name")
	ErrNotFound       = errors.New("saved query not found")
	ErrInvalidName    = errors.New("invalid name")
	ErrInvalidAccount = errors.New("invalid account")
	ErrEmptyQuery     = errors.New("empty query")
)

var (
	// namePattern keeps a name inside its account directory, apart from the
	// store's dotted temporary files, and free of the prompt's ! mark.
	namePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9 ._-]{0,62}[A-Za-z0-9_-])?$`)
	// accountPattern repeats the config's profile name rule, so the store
	// guards its own paths without importing config.
	accountPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type Query struct {
	Name  string
	Text  string
	Scope []string  // empty when the text names its own containers
	Saved time.Time // the file's modification time; never written
}

// Listing is what one account directory held.
type Listing struct {
	Queries []Query // by name, case-insensitively
	Skipped []Skipped
}

// Files counts every query file the directory held, listed or skipped.
func (l Listing) Files() int {
	return len(l.Queries) + len(l.Skipped)
}

// Skipped is a file in an account directory that could not be listed.
type Skipped struct {
	File string
	Err  error
}

// Store keeps the saved queries of every account apart.
type Store interface {
	List(account string) (Listing, error)
	// Create refuses a name already taken, in any case, with ErrExists.
	Create(account string, q Query) error
	// Replace overwrites the query of that name, in any case, or creates it.
	Replace(account string, q Query) error
	// Rename never replaces another query; a case-only change is allowed.
	Rename(account, from, to string) error
	Remove(account, name string) error
}

// Compile-time contract checks.
var (
	_ Store = Dir{}
	_ Store = Unavailable{}
)

func validName(name string) bool {
	return namePattern.MatchString(name)
}

func checkAccount(account string) error {
	if !accountPattern.MatchString(account) {
		return fmt.Errorf("saved: account %q: %w", account, ErrInvalidAccount)
	}
	return nil
}

func checkName(name string) error {
	if !validName(name) {
		return fmt.Errorf("saved: name %q: %w", name, ErrInvalidName)
	}
	return nil
}

// errNoStore is what an Unavailable built without a reason refuses with, so a
// write can never appear to succeed.
var errNoStore = errors.New("no place to keep saved queries")

// Unavailable is the store of a session whose config directory cannot be
// located: it lists nothing and refuses every write with Err.
type Unavailable struct {
	Err error
}

func (Unavailable) List(string) (Listing, error) { return Listing{}, nil }

func (u Unavailable) Create(string, Query) error { return u.reason() }

func (u Unavailable) Replace(string, Query) error { return u.reason() }

func (u Unavailable) Rename(string, string, string) error { return u.reason() }

func (u Unavailable) Remove(string, string) error { return u.reason() }

func (u Unavailable) reason() error {
	if u.Err == nil {
		return errNoStore
	}
	return u.Err
}
