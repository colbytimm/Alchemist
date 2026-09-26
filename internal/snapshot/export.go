package snapshot

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// ExtJSONLines names the one-body-per-line format of a contents export.
const ExtJSONLines = ".jsonl"

// exportDirMode matches internal/export: a folder made to hold an export
// is the user's, not the store's.
const exportDirMode = 0o750

// undefinedValue stands for a partition key value an item does not have,
// in an export: no key value can be an object, so it is never ambiguous.
var undefinedValue = json.RawMessage(`{}`)

// Existing is what an export does about a file already at its path.
type Existing int

const (
	// RefuseExisting fails with export.ErrFileExists, as export.WriteFile
	// does.
	RefuseExisting Existing = iota
	// ReplaceExisting swaps the file for the export once it is whole.
	ReplaceExisting
)

// WriteItems writes what snapshot id held to path, streamed from the packs
// in key order: .jsonl is one canonical body per line, .json an array.
func (s *Store) WriteItems(path, id string, existing Existing) error {
	var write func(w *bufio.Writer, bodies func(func([]byte) error) error) error
	switch strings.ToLower(filepath.Ext(path)) {
	case ExtJSONLines:
		write = writeLines
	case export.ExtJSON:
		write = writeArray
	default:
		return fmt.Errorf("snapshot: %s: %w", path, export.ErrUnknownFormat)
	}
	contents, err := s.Contents(id)
	if err != nil {
		return err
	}
	return createExport(path, existing, func(w *bufio.Writer) error {
		return write(w, func(emit func([]byte) error) error {
			for _, key := range contents.SortedKeys() {
				body, err := s.Body(contents[key].Hash)
				if err != nil {
					return err
				}
				if err := emit(body); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func writeLines(w *bufio.Writer, bodies func(func([]byte) error) error) error {
	return bodies(func(body []byte) error {
		_, err := w.Write(append(body, '\n'))
		return err
	})
}

func writeArray(w *bufio.Writer, bodies func(func([]byte) error) error) error {
	written := 0
	err := bodies(func(body []byte) error {
		separator := ",\n"
		if written == 0 {
			separator = "[\n"
		}
		written++
		if _, err := w.WriteString(separator); err != nil {
			return err
		}
		_, err := w.Write(body)
		return err
	})
	if err != nil {
		return err
	}
	closing := "\n]\n"
	if written == 0 {
		closing = "[]\n"
	}
	_, err = w.WriteString(closing)
	return err
}

// createExport writes path through write, and leaves nothing of what it
// wrote when write fails: a partial export is worse than none.
func createExport(path string, existing Existing, write func(*bufio.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), exportDirMode); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if existing == ReplaceExisting {
		return replaceExport(path, write)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, pack.FileMode) // #nosec G304 -- the path the user asked to export to
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", export.ErrFileExists, path)
	}
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	buffered := bufio.NewWriter(file)
	err = write(buffered)
	if err == nil {
		err = buffered.Flush()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		_ = os.Remove(path) // the export failed; a partial file would read as a whole one
		return fmt.Errorf("snapshot: export %s: %w", path, err)
	}
	return nil
}

// replaceExport writes beside path and renames over it, so the file it
// replaces survives an export that fails.
func replaceExport(path string, write func(*bufio.Writer) error) error {
	temp, err := pack.CreateTemp(path)
	if err != nil {
		return err
	}
	buffered := bufio.NewWriter(temp)
	err = write(buffered)
	if err == nil {
		err = buffered.Flush()
	}
	if err != nil {
		pack.Discard(temp)
		return fmt.Errorf("snapshot: export %s: %w", path, err)
	}
	return pack.Commit(temp, path)
}

// diffDocument is a diff as .json exports it.
type diffDocument struct {
	From       string             `json:"from"`
	To         string             `json:"to"`
	Summary    diffSummary        `json:"summary"`
	Definition exportedDefinition `json:"definition"`
	Changes    []exportedChange   `json:"changes"`
}

type diffSummary struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Modified int `json:"modified"`
}

type exportedDefinition struct {
	Summary string   `json:"summary"`
	Changes []string `json:"changes,omitempty"`
}

type exportedChange struct {
	Change       string            `json:"change"`
	PartitionKey []json.RawMessage `json:"partitionKey"`
	ID           string            `json:"id"`
	Patch        []FieldChange     `json:"patch,omitempty"`
	Body         json.RawMessage   `json:"body,omitempty"`
}

// WriteDiff writes d to path by its extension: .json holds every change,
// with a JSON Patch for a modified item and the body of an added or
// removed one; .csv is one row per item.
func (s *Store) WriteDiff(path string, d Diff, existing Existing) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case export.ExtJSON:
		return createExport(path, existing, func(w *bufio.Writer) error { return s.writeDiffJSON(w, d) })
	case export.ExtCSV:
		return createExport(path, existing, func(w *bufio.Writer) error { return s.writeDiffCSV(w, d) })
	}
	return fmt.Errorf("snapshot: %s: %w", path, export.ErrUnknownFormat)
}

func (s *Store) writeDiffJSON(w io.Writer, d Diff) error {
	document := diffDocument{
		From:       d.From.ID,
		To:         d.To.ID,
		Summary:    diffSummary{Added: d.Count(Added), Removed: d.Count(Removed), Modified: d.Count(Modified)},
		Definition: exportedDefinition{Summary: d.Definition.Summary(), Changes: d.Definition.Changes},
		Changes:    make([]exportedChange, 0, len(d.Items)),
	}
	for _, item := range d.Items {
		change, err := s.exportedChange(item)
		if err != nil {
			return err
		}
		document.Changes = append(document.Changes, change)
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

func (s *Store) exportedChange(item ItemChange) (exportedChange, error) {
	change := exportedChange{Change: item.Kind.String(), PartitionKey: exportedKey(item.Identity), ID: item.Identity.ID}
	before, after, err := s.ItemBodies(item)
	if err != nil {
		return exportedChange{}, err
	}
	switch item.Kind {
	case Added:
		change.Body = after
	case Removed:
		change.Body = before
	default:
		if change.Patch, err = Structural(before, after); err != nil {
			return exportedChange{}, err
		}
	}
	return change, nil
}

func exportedKey(identity Identity) []json.RawMessage {
	values := make([]json.RawMessage, len(identity.PartitionKey))
	for i, value := range identity.PartitionKey {
		values[i] = value
		if value == nil {
			values[i] = undefinedValue
		}
	}
	return values
}

var diffColumns = []string{"change", "partition_key", "id", "fields", "modified_before", "modified_after"}

func (s *Store) writeDiffCSV(w io.Writer, d Diff) error {
	out := csv.NewWriter(w)
	if err := out.Write(diffColumns); err != nil {
		return err
	}
	for _, item := range d.Items {
		fields, err := s.ChangedFields(item)
		if err != nil {
			return err
		}
		row := []string{item.Kind.String(), item.Identity.PartitionKeyText(), item.Identity.ID, strings.Join(fields, "; "),
			modifiedText(item.Before), modifiedText(item.After)}
		if err := out.Write(row); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// ChangedFields names the fields a modified item changed; an added or a
// removed one names none.
func (s *Store) ChangedFields(item ItemChange) ([]string, error) {
	if item.Kind != Modified {
		return nil, nil
	}
	before, after, err := s.ItemBodies(item)
	if err != nil {
		return nil, err
	}
	changes, err := Structural(before, after)
	if err != nil {
		return nil, err
	}
	return FieldNames(changes), nil
}

func modifiedText(e *Entry) string {
	if e == nil || e.ModifiedUnix == 0 {
		return ""
	}
	return e.Modified().Format(time.RFC3339)
}
