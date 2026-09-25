package pack

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
)

// FormatVersion is the one version of every file kind this build writes and
// reads.
const FormatVersion = 1

// maxHeaderLine is longer than any kind's first line, so a file of garbage
// is refused after a few bytes rather than read to its first newline.
const maxHeaderLine = 64

// Header is the first line of a file of kind: "<kind> <version>\n".
func Header(kind string) []byte {
	return []byte(kind + " " + strconv.Itoa(FormatVersion) + "\n")
}

// ReadHeader consumes the first line of r and checks that it names kind at
// FormatVersion. Another version of kind is ErrUnknownFormat; anything else
// is ErrCorrupt.
func ReadHeader(r io.Reader, kind string) (int, error) {
	line := make([]byte, 0, maxHeaderLine)
	one := make([]byte, 1)
	for len(line) < maxHeaderLine {
		if _, err := io.ReadFull(r, one); err != nil {
			return len(line), fmt.Errorf("%s header cut short: %w", kind, ErrCorrupt)
		}
		line = append(line, one[0])
		if one[0] == '\n' {
			return len(line), checkHeader(line, kind)
		}
	}
	return len(line), fmt.Errorf("no %s header: %w", kind, ErrCorrupt)
}

func checkHeader(line []byte, kind string) error {
	name, version, found := bytes.Cut(bytes.TrimSuffix(line, []byte("\n")), []byte(" "))
	if !found || string(name) != kind {
		return fmt.Errorf("not a %s file: %w", kind, ErrCorrupt)
	}
	if string(version) != strconv.Itoa(FormatVersion) {
		return fmt.Errorf("%s version %q: %w", kind, version, ErrUnknownFormat)
	}
	return nil
}
