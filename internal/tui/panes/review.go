package panes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	reviewTitle       = "Review batch"
	reviewConsequence = "All of it commits or none of it does. There is no undo."
	reviewPrompt      = "Type the container name to commit:"
	warningMark       = "! "
	// reviewLabelWidth lines the header values up under one another.
	reviewLabelWidth = 15
)

// operationOrder is the order the header counts operations in.
var operationOrder = []adapter.OperationKind{
	adapter.OperationCreate, adapter.OperationUpsert, adapter.OperationReplace,
	adapter.OperationPatch, adapter.OperationDelete, adapter.OperationRead,
}

// BatchDraft is what a review is opened for: a batch that passed every check
// made before sending, the account it would run on, and the key paths of its
// container.
type BatchDraft struct {
	Account  string
	KeyPaths []string
	Batch    adapter.Batch
	Check    query.BatchCheck
}

// BatchReview shows a batch before it is sent, and asks for its container's
// name typed back as the one way to send it. Like the field it wraps, its
// value receiver hides shared pointers, so a caller must keep every
// BatchReview it is handed.
type BatchReview struct {
	frame  frame
	hints  help.Model
	keys   []key.Binding
	draft  BatchDraft
	name   nameField
	offset int
}

func NewBatchReview(keys []key.Binding) BatchReview {
	hints := help.New()
	hints.Styles = helpStyles()
	return BatchReview{frame: frame{title: reviewTitle, focused: true}, hints: hints, keys: keys}
}

// Open shows draft with the name field empty and the list at its top.
func (r BatchReview) Open(draft BatchDraft) BatchReview {
	r.draft = draft
	r.name = newNameField(draft.Batch.Scope[len(draft.Batch.Scope)-1])
	r.offset = 0
	return r.SetSize(r.frame.width, r.frame.height)
}

func (r BatchReview) SetSize(width, height int) BatchReview {
	r.frame = r.frame.size(width, height)
	width, _ = r.frame.inner()
	r.name = r.name.setWidth(width)
	r.hints.Width = width
	return r
}

func (r BatchReview) Draft() BatchDraft {
	return r.draft
}

func (r BatchReview) Update(msg tea.KeyMsg) (BatchReview, tea.Cmd) {
	var cmd tea.Cmd
	r.name, cmd = r.name.update(msg)
	return r, cmd
}

// Confirmed reports whether the container's name has been typed back
// exactly.
func (r BatchReview) Confirmed() bool {
	return r.name.matches()
}

func (r BatchReview) ScrollUp() BatchReview {
	r.offset = max(r.offset-1, 0)
	return r
}

func (r BatchReview) ScrollDown() BatchReview {
	width, _ := r.frame.inner()
	r.offset = clampScroll(r.offset+1, len(r.bodyLines(width)), r.bodyHeight())
	return r
}

func (r BatchReview) View() string {
	width, _ := r.frame.inner()
	body := r.bodyLines(width)
	height := r.bodyHeight()
	offset := clampScroll(r.offset, len(body), height)
	visible := make([]string, height)
	copy(visible, body[offset:min(offset+height, len(body))])

	lines := append(r.headerLines(width), "")
	lines = append(lines, visible...)
	lines = append(lines, "")
	lines = append(lines, r.footerLines(width)...)
	return r.frame.renderWithHint(lines, r.hints.ShortHelpView(r.keys))
}

// bodyHeight is what is left for the scrolling list once the header, the
// footer, the two blank lines between them and the hint line are placed.
func (r BatchReview) bodyHeight() int {
	width, height := r.frame.inner()
	fixed := len(r.headerLines(width)) + len(r.footerLines(width)) + 3
	return max(height-fixed, 1)
}

func (r BatchReview) headerLines(width int) []string {
	b := r.draft.Batch
	lines := reviewField("Account", r.draft.Account, width)
	lines = append(lines, reviewField("Container", strings.Join(b.Scope, "."), width)...)
	lines = append(lines, reviewField("Partition key", partitionLabel(r.draft.KeyPaths, b.PartitionKey), width)...)
	return append(lines, reviewField("Operations", r.operationsLabel(), width)...)
}

func reviewField(label, value string, width int) []string {
	wrapped := wrapText(value, max(width-reviewLabelWidth, 1))
	lines := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		prefix := strings.Repeat(" ", reviewLabelWidth)
		if i == 0 {
			prefix = fit(label, reviewLabelWidth)
		}
		lines = append(lines, headerStyle().Render(prefix)+theme.TextStyle().Render(line))
	}
	return lines
}

func partitionLabel(paths []string, key adapter.PartitionKey) string {
	pairs := make([]string, 0, len(key))
	for i, value := range key {
		path := "?"
		if i < len(paths) {
			path = paths[i]
		}
		pairs = append(pairs, path+" = "+string(value))
	}
	return strings.Join(pairs, ", ")
}

func (r BatchReview) operationsLabel() string {
	counts := map[adapter.OperationKind]int{}
	for _, op := range r.draft.Batch.Operations {
		counts[op.Kind]++
	}
	var parts []string
	for _, kind := range operationOrder {
		if counts[kind] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
		}
	}
	return fmt.Sprintf("%d (%s) · %s of %s", len(r.draft.Batch.Operations), strings.Join(parts, ", "),
		query.FormatSize(r.draft.Check.Bytes), query.FormatSize(query.MaxBatchBytes))
}

// bodyLines are the operations, one to a line, then every warning.
func (r BatchReview) bodyLines(width int) []string {
	var lines []string
	for i, op := range r.draft.Batch.Operations {
		lines = append(lines, theme.TextStyle().Render(ansi.Truncate(operationLine(i, op), width, "…")))
	}
	if len(r.draft.Check.Warnings) > 0 {
		lines = append(lines, "")
	}
	for _, warning := range r.draft.Check.Warnings {
		wrapped := wrapText(warningMark+warning, width)
		lines = append(lines, styleAll(warningStyle(), wrapped)...)
	}
	return lines
}

func operationLine(i int, op adapter.Operation) string {
	line := fmt.Sprintf("%3d  %-8s %s", i+1, strings.ToUpper(string(op.Kind)), query.ItemID(op))
	if op.Kind == adapter.OperationPatch {
		line += "  " + patchSummary(op.Body)
	}
	if op.IfMatch != "" {
		line += "  if match " + op.IfMatch
	}
	return line
}

// patchSummary names what each patch entry does and where: "set /status".
func patchSummary(body json.RawMessage) string {
	var entries []struct {
		Op   string `json:"op"`
		Path string `json:"path"`
	}
	if json.Unmarshal(body, &entries) != nil {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entry.Op+" "+entry.Path)
	}
	return strings.Join(parts, ", ")
}

func (r BatchReview) footerLines(width int) []string {
	lines := wrapText(reviewConsequence, width)
	lines = append(lines, reviewPrompt)
	return append(styleAll(theme.TextStyle(), lines), r.name.view())
}

func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Gold())
}
