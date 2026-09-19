// Package export writes the pages a query has already fetched to a file. It
// never fetches: a result set with pages still to come exports as the part
// that is on screen.
package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var (
	ErrUnknownFormat = errors.New("export: unknown format")
	ErrFileExists    = errors.New("export: file exists")
)

// The file extensions that name a format.
const (
	ExtJSON = ".json"
	ExtCSV  = ".csv"
)

const (
	fileMode   = 0o600
	jsonIndent = "  "
)

type encoder func(out *bytes.Buffer, pages []adapter.Page) error

// WriteFile exports pages in the format path's extension names, and fails
// with ErrFileExists rather than replace a file.
func WriteFile(path string, pages []adapter.Page) error {
	return writeFile(path, pages, os.O_EXCL)
}

func OverwriteFile(path string, pages []adapter.Page) error {
	return writeFile(path, pages, os.O_TRUNC)
}

// writeFile encodes before it opens path, so a result set that cannot be
// encoded never truncates the file it was meant to replace.
func writeFile(path string, pages []adapter.Page, onExisting int) error {
	encode, err := encoderFor(path)
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err := encode(&encoded, pages); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|onExisting, fileMode) // #nosec G304 -- the path is the one the user typed to export to
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrFileExists, path)
	}
	if err != nil {
		return fmt.Errorf("export: %w", err) // a *fs.PathError names the operation and the path itself
	}
	_, writeErr := file.Write(encoded.Bytes())
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

func encoderFor(path string) (encoder, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ExtJSON:
		return JSON, nil
	case ExtCSV:
		return CSV, nil
	default:
		return nil, ErrUnknownFormat
	}
}

// JSON writes the original documents as one indented array.
func JSON(out *bytes.Buffer, pages []adapter.Page) error {
	out.WriteString("[")
	count := 0
	for _, page := range pages {
		for _, document := range page.Raw {
			if count > 0 {
				out.WriteString(",")
			}
			out.WriteString("\n" + jsonIndent)
			if err := json.Indent(out, document, jsonIndent, jsonIndent); err != nil {
				return fmt.Errorf("export: document %d: %w", count+1, err)
			}
			count++
		}
	}
	if count > 0 {
		out.WriteString("\n")
	}
	out.WriteString("]\n")
	return nil
}

// CSV writes a header and the rendered cells. Adapters only ever append
// columns, so the widest page's header serves every row, and a row fetched
// before a column existed is padded out to it.
func CSV(out *bytes.Buffer, pages []adapter.Page) error {
	columns := widestColumns(pages)
	if len(columns) == 0 {
		return nil
	}
	records := [][]string{columns}
	for _, page := range pages {
		for _, row := range page.Rows {
			records = append(records, padded(row, len(columns)))
		}
	}
	if err := csv.NewWriter(out).WriteAll(records); err != nil {
		return fmt.Errorf("export: write csv: %w", err)
	}
	return nil
}

func widestColumns(pages []adapter.Page) []string {
	var columns []string
	for _, page := range pages {
		if len(page.Columns) > len(columns) {
			columns = page.Columns
		}
	}
	return columns
}

func padded(row []string, width int) []string {
	if len(row) >= width {
		return row
	}
	return append(row[:len(row):len(row)], make([]string, width-len(row))...)
}
