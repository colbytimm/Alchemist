package panes

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	infoTitle       = "Info"
	inspectingLabel = "inspecting"
	// propertyGap separates the widest property name from the values.
	propertyGap = 2
	// hintLines is what the hint at the bottom of the frame costs the body.
	hintLines = 1
)

// Info is the metadata overlay for one catalog node. It draws what the tree
// already knows about the node the moment it opens, and the adapter's Details
// once they land. It keeps the last Details per node in a map its value
// copies share, so a node already read reopens without a request and only a
// refresh spends one again.
type Info struct {
	frame    frame
	icons    theme.IconSet
	spinner  spinner.Model
	hints    help.Model
	keys     []key.Binding
	node     adapter.Node
	details  map[string]adapter.Details
	loading  bool
	spinning bool
	failure  string
	offset   int
}

// NewInfo builds the overlay; keys are the bindings its hint line shows.
func NewInfo(icons theme.IconSet, keys []key.Binding) Info {
	hints := help.New()
	hints.Styles = helpStyles()
	return Info{
		frame: frame{title: infoTitle, focused: true},
		icons: icons,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
			spinner.WithStyle(theme.SpinnerStyle()),
		),
		hints:   hints,
		keys:    keys,
		details: map[string]adapter.Details{},
	}
}

func (v Info) SetSize(width, height int) Info {
	v.frame = v.frame.size(width, height)
	v.hints.Width, _ = v.frame.inner()
	return v
}

// Update advances the loading animation and stops it once the request has
// settled, so an idle overlay wakes the program no further.
func (v Info) Update(msg tea.Msg) (Info, tea.Cmd) {
	tick, ok := msg.(spinner.TickMsg)
	if !ok || tick.ID != v.spinner.ID() {
		return v, nil
	}
	if !v.loading {
		v.spinning = false
		return v, nil
	}
	var cmd tea.Cmd
	v.spinner, cmd = v.spinner.Update(tick)
	return v, cmd
}

// Show opens the overlay on node, from the top.
func (v Info) Show(node adapter.Node) Info {
	v.node = node
	v.frame.title = infoTitle + " " + v.icons.Separator + " " + strings.Join(node.Path, ".")
	v.offset = 0
	v.loading = false
	v.failure = ""
	return v
}

func (v Info) Node() adapter.Node {
	return v.node
}

// Loaded reports whether the Details of the node on screen are on hand.
func (v Info) Loaded() bool {
	_, ok := v.details[pathKey(v.node.Path)]
	return ok
}

// StartLoading marks a request for the node on screen as sent. The returned
// command starts the animation, once.
func (v Info) StartLoading() (Info, tea.Cmd) {
	v.loading = true
	v.failure = ""
	if v.spinning {
		return v, nil
	}
	v.spinning = true
	return v, v.spinner.Tick
}

// SetDetails keeps what was read about the node at path. It is shown only
// when that node is the one on screen: an answer for a node the user has
// walked away from waits for them to come back.
func (v Info) SetDetails(path []string, details adapter.Details) Info {
	v.details[pathKey(path)] = details
	if !v.shows(path) {
		return v
	}
	v.loading = false
	v.failure = ""
	v.offset = 0
	return v
}

// Fail shows why the node at path could not be read, under what the tree
// knows about it. A failure for a node no longer on screen is dropped: the
// next opening asks again.
func (v Info) Fail(path []string, err error) Info {
	if !v.shows(path) {
		return v
	}
	v.loading = false
	v.failure = err.Error()
	return v
}

func (v Info) shows(path []string) bool {
	return pathKey(path) == pathKey(v.node.Path)
}

func (v Info) ScrollUp() Info {
	return v.scroll(-1)
}

func (v Info) ScrollDown() Info {
	return v.scroll(1)
}

func (v Info) View() string {
	width, height := v.frame.inner()
	lines := v.lines(width)
	offset := clampScroll(v.offset, len(lines), height-hintLines)
	end := min(offset+height-hintLines, len(lines))
	return v.frame.renderWithHint(lines[offset:end], v.hints.ShortHelpView(v.keys))
}

func (v Info) scroll(delta int) Info {
	width, height := v.frame.inner()
	v.offset = clampScroll(v.offset+delta, len(v.lines(width)), height-hintLines)
	return v
}

// lines renders the body: the adapter's sections once they have arrived,
// and until then what the tree knows, with the request's progress under it.
func (v Info) lines(width int) []string {
	if details, ok := v.details[pathKey(v.node.Path)]; ok && !v.loading && v.failure == "" {
		return sectionLines(details.Sections, width)
	}
	lines := sectionLines(preview(v.node), width)
	lines = append(lines, "")
	switch {
	case v.loading:
		lines = append(lines, theme.HintStyle().Render(v.spinner.View()+" "+inspectingLabel))
	case v.failure != "":
		lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(v.icons.Failure+" "+v.failure, width))...)
	}
	return lines
}

// preview is what the tree already holds about a node, laid out the way the
// adapter will lay out the rest.
func preview(node adapter.Node) []adapter.Section {
	identity := adapter.Section{Title: "Identity"}
	if len(node.Path) > 0 {
		identity.Properties = append(identity.Properties, adapter.Property{Name: "Database", Value: node.Path[0]})
	}
	if len(node.Path) > 1 {
		identity.Properties = append(identity.Properties, adapter.Property{Name: "Container", Value: node.Path[1]})
	}
	sections := []adapter.Section{identity}
	if paths := node.Meta[adapter.MetaPartitionKey]; paths != "" {
		sections = append(sections, adapter.Section{Title: "Partition key", Properties: []adapter.Property{
			{Name: "Paths", Value: strings.ReplaceAll(paths, adapter.PartitionKeyPathSeparator, ", ")},
		}})
	}
	return sections
}

func sectionLines(sections []adapter.Section, width int) []string {
	var lines []string
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, theme.HeadingStyle().Render(indent+section.Title))
		lines = append(lines, propertyLines(section, width)...)
	}
	return lines
}

// propertyLines lays a section's properties out in two columns, or its note
// as a paragraph when it has none.
func propertyLines(section adapter.Section, width int) []string {
	margin := strings.Repeat(indent, 2)
	if len(section.Properties) == 0 {
		return styleAll(theme.TextStyle(), hangLines(margin, wrapText(section.Note, width-lipgloss.Width(margin))))
	}
	nameWidth := widestName(section.Properties) + propertyGap
	var lines []string
	for _, property := range section.Properties {
		label := margin + theme.HintStyle().Render(fit(property.Name, nameWidth))
		value := wrapText(property.Value, width-lipgloss.Width(margin)-nameWidth)
		lines = append(lines, hangLines(label, styleAll(theme.TextStyle(), value))...)
	}
	return lines
}

func widestName(properties []adapter.Property) int {
	width := 0
	for _, property := range properties {
		width = max(width, lipgloss.Width(property.Name))
	}
	return width
}

// hangLines puts label before the first line and indents the rest under it.
func hangLines(label string, lines []string) []string {
	hanging := strings.Repeat(" ", lipgloss.Width(label))
	hung := make([]string, 0, len(lines))
	for i, line := range lines {
		prefix := hanging
		if i == 0 {
			prefix = label
		}
		hung = append(hung, prefix+line)
	}
	return hung
}
