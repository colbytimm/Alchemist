package panes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	updateConsequence = "Items are written one by one. Each write re-checks the WHERE; an item that " +
		"no longer matches is skipped. Stopping leaves the rest unchanged. There is no undo."
	deleteConsequence = "Items are deleted one by one. An item changed since %s is skipped. " +
		"Stopping leaves the rest in place. There is no undo."
	noCharges   = "the backend reported no request units"
	changeWidth = 14
)

// MutationDraft is what a review is opened for: an update or a delete that
// passed every check, the account it would run on, the items it selected there, and the
// warnings the review must show.
type MutationDraft struct {
	Account  string
	KeyPaths []string
	Mutation query.Mutation
	Targets  mutate.Targets
	Warnings []string
	Writers  int
}

// Confirmation is the text the review asks to have typed.
func (d MutationDraft) Confirmation() string {
	return mutate.Confirmation(d.Mutation, len(d.Targets.Items))
}

// MutationReview shows what an update or a delete would do before any of it
// is written, and asks for the confirmation typed back as the one way to
// start it. Like the field it wraps, its value receiver hides shared
// pointers, so a caller must keep every MutationReview it is handed.
type MutationReview struct {
	frame    frame
	hints    help.Model
	keys     []key.Binding
	draft    MutationDraft
	snapshot string
	name     nameField
	offset   int
}

func NewMutationReview(keys []key.Binding) MutationReview {
	hints := help.New()
	hints.Styles = helpStyles()
	return MutationReview{frame: frame{title: "Review update", focused: true}, hints: hints, keys: keys}
}

// Open shows draft with the confirmation empty and the body at its top.
func (r MutationReview) Open(draft MutationDraft) MutationReview {
	r.draft = draft
	r.snapshot = ""
	r.name = newNameField(draft.Confirmation())
	r.offset = 0
	r.frame.title = "Review " + draft.Mutation.Kind.String() + " · " + draft.Account
	return r.SetSize(r.frame.width, r.frame.height)
}

// SetSnapshotLine adds what the review knows of a copy taken before the
// run, as one more warning.
func (r MutationReview) SetSnapshotLine(line string) MutationReview {
	r.snapshot = line
	return r
}

func (r MutationReview) SetSize(width, height int) MutationReview {
	r.frame = r.frame.size(width, height)
	width, _ = r.frame.inner()
	r.name = r.name.setWidth(width)
	r.hints.Width = width
	return r
}

func (r MutationReview) Draft() MutationDraft { return r.draft }

func (r MutationReview) Update(msg tea.KeyMsg) (MutationReview, tea.Cmd) {
	var cmd tea.Cmd
	r.name, cmd = r.name.update(msg)
	return r, cmd
}

// Confirmed reports whether the confirmation was typed back exactly.
func (r MutationReview) Confirmed() bool {
	return r.name.matches()
}

func (r MutationReview) ScrollUp() MutationReview {
	r.offset = max(r.offset-1, 0)
	return r
}

func (r MutationReview) ScrollDown() MutationReview {
	width, _ := r.frame.inner()
	r.offset = clampScroll(r.offset+1, len(r.bodyLines(width)), r.bodyHeight())
	return r
}

func (r MutationReview) View() string {
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

// bodyHeight is what is left for the scrolling part once the header, the
// footer, the two blank lines between them and the hint line are placed.
func (r MutationReview) bodyHeight() int {
	width, height := r.frame.inner()
	fixed := len(r.headerLines(width)) + len(r.footerLines(width)) + 3
	return max(height-fixed, 1)
}

func (r MutationReview) headerLines(width int) []string {
	d := r.draft
	targets := d.Targets
	container := strings.Join(d.Mutation.Target, ".")
	if len(d.KeyPaths) > 0 {
		container += "   key " + strings.Join(d.KeyPaths, ", ")
	}
	lines := reviewField("Account", d.Account, width)
	lines = append(lines, reviewField("Container", container, width)...)
	lines = append(lines, reviewField("Where", d.Mutation.Where, width)...)
	lines = append(lines, reviewField("Items", fmt.Sprintf("%s matched at %s · selection cost %s",
		FormatCount(int64(len(targets.Items))), targets.SelectedAt.Format("15:04:05"), measuredCharge(targets.RequestCharge)), width)...)
	if changes := mutate.Changes(d.Mutation); len(changes) > 0 {
		lines = append(lines, reviewField("Each gets", strings.Join(changes, " · "), width)...)
	}
	return append(lines, reviewField("Writes", r.writesText(), width)...)
}

func (r MutationReview) writesText() string {
	d := r.draft
	perItem := mutate.PlanningCharge(d.Mutation.Kind)
	planned := int64(len(d.Targets.Items) * perItem)
	return fmt.Sprintf("%d at a time · roughly %s RU (%d RU per 1 KB item; larger or heavily indexed items cost more)",
		d.Writers, FormatCount(planned), perItem)
}

// measuredCharge is a measured charge, or what a backend that measures none
// said.
func measuredCharge(charge float64) string {
	if charge == 0 {
		return noCharges
	}
	return FormatCharge(charge) + " RU"
}

// bodyLines are the before and after of the previewed items, how many more
// there are, then every warning.
func (r MutationReview) bodyLines(width int) []string {
	lines := r.previewLines(width)
	if more := len(r.draft.Targets.Items) - len(r.draft.Targets.Preview); more > 0 {
		lines = append(lines, theme.HintStyle().Render(fmt.Sprintf("… %s more", FormatCount(int64(more)))))
	}
	warnings := r.draft.Warnings
	if r.snapshot != "" {
		warnings = append(warnings[:len(warnings):len(warnings)], r.snapshot)
	}
	if len(warnings) > 0 {
		lines = append(lines, "")
	}
	for _, warning := range warnings {
		lines = append(lines, styleAll(warningStyle(), wrapText(warningMark+warning, width))...)
	}
	return lines
}

func (r MutationReview) previewLines(width int) []string {
	if r.draft.Mutation.Kind == query.MutationDelete {
		return r.deletedLines(width)
	}
	var lines []string
	for _, item := range r.draft.Targets.Preview {
		changes, err := mutate.Preview(item, r.draft.Mutation)
		if err != nil {
			continue
		}
		head := itemLabel(item, r.draft.KeyPaths)
		lines = append(lines, theme.TextStyle().Render(ansi.Truncate(head, width, "…")))
		for _, change := range changes {
			lines = append(lines, changeLine(change, width))
		}
	}
	return lines
}

// deletedLines show each previewed item on one line: its id, its key, and
// the rest of it as the service keeps it.
func (r MutationReview) deletedLines(width int) []string {
	var lines []string
	for _, item := range r.draft.Targets.Preview {
		line := "- " + itemLabel(item, r.draft.KeyPaths) + "  " + string(itemBody(item, r.draft.KeyPaths))
		lines = append(lines, theme.ErrorStyle().Render(ansi.Truncate(line, width, "…")))
	}
	return lines
}

// itemBody is item without what its label shows or the service owns.
func itemBody(item json.RawMessage, keyPaths []string) json.RawMessage {
	shown := append(adapter.SystemFields(), "id")
	for _, path := range keyPaths {
		if name := strings.TrimPrefix(path, "/"); !strings.Contains(name, "/") {
			shown = append(shown, name)
		}
	}
	body, err := adapter.WithoutFields(item, shown...)
	if err != nil {
		return item
	}
	return body
}

// itemLabel names an item by its id and its key.
func itemLabel(item json.RawMessage, keyPaths []string) string {
	var head struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(item, &head) // an item the backend served is an object; a label is all this is
	label := head.ID
	if key, err := adapter.PartitionKeyValues(item, keyPaths); err == nil {
		label += "  " + query.PartitionText(key)
	}
	return label
}

func changeLine(change mutate.FieldChange, width int) string {
	var mark, values string
	style := theme.TextStyle()
	switch change.Kind {
	case mutate.Changed:
		mark, values = "~", string(change.Before)+" → "+string(change.After)
	case mutate.Added:
		mark, values, style = "+", string(change.After), theme.SuccessStyle()
	case mutate.Removed:
		mark, values, style = "-", string(change.Before), theme.ErrorStyle()
	case mutate.Unchanged:
		mark, values, style = "=", string(change.After)+" already", theme.HintStyle()
	case mutate.NoParent:
		mark, values, style = "!", "no parent on this item: it is skipped", warningStyle()
	}
	line := "    " + mark + " " + fit(change.Path, changeWidth) + " " + values
	return style.Render(ansi.Truncate(line, width, "…"))
}

func (r MutationReview) footerLines(width int) []string {
	lines := wrapText(r.consequence(), width)
	lines = append(lines, r.prompt())
	return append(styleAll(theme.TextStyle(), lines), r.name.view())
}

func (r MutationReview) consequence() string {
	if r.draft.Mutation.Kind == query.MutationDelete {
		return fmt.Sprintf(deleteConsequence, r.draft.Targets.SelectedAt.Format("15:04:05"))
	}
	return updateConsequence
}

// prompt spells out a confirmation that holds a count, since the count the
// review shows has thousands separators and the one to type has none.
func (r MutationReview) prompt() string {
	d := r.draft
	count := FormatCount(int64(len(d.Targets.Items)))
	switch {
	case d.Mutation.Kind == query.MutationDelete:
		return fmt.Sprintf("Type %s to delete:", d.Confirmation())
	case d.Mutation.EveryItem:
		return fmt.Sprintf("Every item in %s. Type %s to update:", strings.Join(d.Mutation.Target, "."), d.Confirmation())
	}
	return fmt.Sprintf("Type the container name to %s %s items:", d.Mutation.Kind, count)
}
