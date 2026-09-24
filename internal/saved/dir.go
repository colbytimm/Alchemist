package saved

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

const tempSuffix = ".tmp"

// Dir is the store on disk, one directory per account under Root. Nothing is
// created until the first save.
type Dir struct {
	Root string
}

func Open(root string) Dir {
	return Dir{Root: root}
}

// AccountPath is where the queries of account are kept.
func (d Dir) AccountPath(account string) string {
	return filepath.Join(d.Root, account)
}

// List never fails because of one file: a file it cannot use is reported in
// Skipped, and the rest are listed. A directory that does not exist is an
// empty listing.
func (d Dir) List(account string) (Listing, error) {
	if err := checkAccount(account); err != nil {
		return Listing{}, err
	}
	files, err := d.queryFiles(account)
	if err != nil {
		return Listing{}, err
	}
	var listing Listing
	taken := map[string]string{}
	for _, file := range files {
		q, err := d.read(account, file, taken)
		if err != nil {
			listing.Skipped = append(listing.Skipped, Skipped{File: filepath.Join(d.AccountPath(account), file), Err: err})
			continue
		}
		taken[strings.ToLower(q.Name)] = file
		listing.Queries = append(listing.Queries, q)
	}
	slices.SortFunc(listing.Queries, func(a, b Query) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return listing, nil
}

func (d Dir) Create(account string, q Query) error {
	if err := checkWrite(account, q); err != nil {
		return err
	}
	_, taken, err := d.find(account, q.Name)
	if err != nil {
		return err
	}
	if taken {
		return existsError(q.Name)
	}
	return d.write(account, q)
}

// Replace keeps the spelling of the file it replaces: changing that is a
// rename.
func (d Dir) Replace(account string, q Query) error {
	if err := checkWrite(account, q); err != nil {
		return err
	}
	existing, found, err := d.find(account, q.Name)
	if err != nil {
		return err
	}
	if found {
		q.Name = existing
	}
	return d.write(account, q)
}

func (d Dir) Rename(account, from, to string) error {
	if err := errors.Join(checkAccount(account), checkName(from), checkName(to)); err != nil {
		return err
	}
	source, err := d.existing(account, from)
	if err != nil {
		return err
	}
	holder, taken, err := d.find(account, to)
	if err != nil {
		return err
	}
	if taken && holder != source {
		return existsError(to)
	}
	if err := os.Rename(d.queryPath(account, source), d.queryPath(account, to)); err != nil {
		return fmt.Errorf("saved: rename %q to %q: %w", source, to, err)
	}
	return nil
}

func (d Dir) Remove(account, name string) error {
	if err := errors.Join(checkAccount(account), checkName(name)); err != nil {
		return err
	}
	existing, err := d.existing(account, name)
	if err != nil {
		return err
	}
	if err := os.Remove(d.queryPath(account, existing)); err != nil {
		return fmt.Errorf("saved: remove %q: %w", existing, err)
	}
	return nil
}

// RemoveAccount deletes the directory of account, and reports how many
// queries it listed.
func (d Dir) RemoveAccount(account string) (removed int, err error) {
	listing, err := d.List(account)
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(d.AccountPath(account)); err != nil {
		return 0, fmt.Errorf("saved: remove %s: %w", d.AccountPath(account), err)
	}
	return len(listing.Queries), nil
}

func checkWrite(account string, q Query) error {
	if err := errors.Join(checkAccount(account), checkName(q.Name)); err != nil {
		return err
	}
	if strings.TrimSpace(q.Text) == "" {
		return errEmptyQuery
	}
	return nil
}

func existsError(name string) error {
	return fmt.Errorf("saved: %q: %w", name, ErrExists)
}

// queryFiles lists the file names in the directory of account that may hold
// a query, in byte order. Subdirectories and dotfiles, the store's temporary
// files among them, are not queries.
func (d Dir) queryFiles(account string) ([]string, error) {
	entries, err := os.ReadDir(d.AccountPath(account))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("saved: read %s: %w", d.AccountPath(account), err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, Extension) {
			continue
		}
		files = append(files, name)
	}
	return files, nil
}

// read loads one file. taken maps the lower-cased names already listed to
// their files, since two names differing only in case are one query.
func (d Dir) read(account, file string, taken map[string]string) (Query, error) {
	name := strings.TrimSuffix(file, Extension)
	if err := checkName(name); err != nil {
		return Query{}, err
	}
	if first, ok := taken[strings.ToLower(name)]; ok {
		return Query{}, fmt.Errorf("saved: same name as %s: %w", first, ErrExists)
	}
	handle, err := os.Open(d.queryPath(account, name)) // #nosec G304 -- account and name are checked against patterns that keep them inside Root
	if err != nil {
		return Query{}, fmt.Errorf("saved: open: %w", err)
	}
	defer func() { _ = handle.Close() }() // a read-only handle has nothing to flush
	info, err := handle.Stat()
	if err != nil {
		return Query{}, fmt.Errorf("saved: stat: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(handle, maxFileSize+1))
	if err != nil {
		return Query{}, fmt.Errorf("saved: read: %w", err)
	}
	q, err := decode(name, data)
	if err != nil {
		return Query{}, err
	}
	q.Saved = info.ModTime()
	return q, nil
}

// find reports the spelling under which name is saved, if it is.
func (d Dir) find(account, name string) (string, bool, error) {
	files, err := d.queryFiles(account)
	if err != nil {
		return "", false, err
	}
	for _, file := range files {
		candidate := strings.TrimSuffix(file, Extension)
		if validName(candidate) && strings.EqualFold(candidate, name) {
			return candidate, true, nil
		}
	}
	return "", false, nil
}

// existing is find for a query that must be there.
func (d Dir) existing(account, name string) (string, error) {
	spelling, found, err := d.find(account, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("saved: %q: %w", name, ErrNotFound)
	}
	return spelling, nil
}

// write replaces the file by renaming a finished one over it, so a crash
// leaves the old file or the new one and never half of either.
func (d Dir) write(account string, q Query) error {
	dir := d.AccountPath(account)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("saved: create %s: %w", dir, err)
	}
	final := d.queryPath(account, q.Name)
	temporary := filepath.Join(dir, "."+q.Name+Extension+tempSuffix)
	if err := os.WriteFile(temporary, encode(q), fileMode); err != nil {
		return fmt.Errorf("saved: write %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, final); err != nil {
		return errors.Join(fmt.Errorf("saved: replace %s: %w", final, err), os.Remove(temporary))
	}
	return nil
}

func (d Dir) queryPath(account, name string) string {
	return filepath.Join(d.AccountPath(account), name+Extension)
}
