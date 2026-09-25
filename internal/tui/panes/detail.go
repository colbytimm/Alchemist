package panes

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	detailTitle  = "Document"
	detailIndent = "  "
)

// Detail is the raw-document overlay for the row under the results cursor.
type Detail struct {
	frame  frame
	lines  []string
	offset int
}

func NewDetail() Detail {
	return Detail{frame: frame{title: detailTitle, focused: true}}
}

func (d Detail) SetSize(width, height int) Detail {
	d.frame = d.frame.size(width, height)
	return d
}

// SetDocument shows raw pretty-printed, or verbatim when it is not JSON the
// standard library can indent.
func (d Detail) SetDocument(raw json.RawMessage) Detail {
	d.lines = strings.Split(indentJSON(raw), "\n")
	d.offset = 0
	return d
}

func (d Detail) ScrollUp() Detail {
	return d.scroll(-1)
}

func (d Detail) ScrollDown() Detail {
	return d.scroll(1)
}

func (d Detail) View() string {
	_, height := d.frame.inner()
	end := min(d.offset+height, len(d.lines))
	return d.frame.render(theme.TextStyle().Render(strings.Join(d.lines[d.offset:end], "\n")))
}

func (d Detail) scroll(delta int) Detail {
	_, height := d.frame.inner()
	d.offset = clampScroll(d.offset+delta, len(d.lines), height)
	return d
}

func indentJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", detailIndent); err != nil {
		return string(raw)
	}
	return buf.String()
}
