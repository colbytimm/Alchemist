package panes

import (
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	accountsTitle      = "Accounts"
	accountsFilterHint = "filter by name or endpoint"
	noAccountsHint     = "no accounts yet: a to add one"
	// maxAccountColumn stops one long name, host or database from crowding
	// out the state.
	maxAccountColumn = 32
	// glyphWidth is the leading glyph and the space after it.
	glyphWidth = 2
	// defaultHTTPSPort is left off a host, where it says nothing.
	defaultHTTPSPort = ":443"
)

// AccountState is how far a session has got with an account.
type AccountState int

const (
	AccountDisconnected AccountState = iota
	AccountConnecting
	AccountConnected
	AccountFailed
)

// AccountRow is one account as the switcher lists it.
type AccountRow struct {
	Name     string
	Endpoint string
	Database string
	State    AccountState
	Err      error // set when State is AccountFailed
	ReadOnly bool
	// Notice is why the last action on the row was refused.
	Notice string
}

// Accounts is the account switcher. Its value receiver hides shared pointers:
// keep every Accounts it hands back.
type Accounts struct {
	frame    frame
	icons    theme.IconSet
	hints    help.Model
	keys     []key.Binding
	list     filterList[AccountRow]
	current  string
	spinner  spinner.Model
	spinning bool
}

// NewAccounts builds the switcher; keys are the bindings its hint line shows.
func NewAccounts(icons theme.IconSet, keys []key.Binding) Accounts {
	hints := help.New()
	return Accounts{
		frame: frame{title: accountsTitle, focused: true},
		icons: icons,
		hints: hints,
		keys:  keys,
		list:  newFilterList(accountsFilterHint, matchesAccount),
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
		),
	}
}

func (a Accounts) SetSize(width, height int) Accounts {
	a.frame = a.frame.size(width, height)
	width, _ = a.frame.inner()
	a.list = a.list.setWidth(width)
	a.hints.Width = width
	return a
}

// SetRows lists rows, keeping the cursor on the name it was on; current is the account the session is on.
func (a Accounts) SetRows(rows []AccountRow, current string) (Accounts, tea.Cmd) {
	selected, hadSelection := a.list.selected()
	a.list = a.list.setItems(rows)
	a.current = current
	if hadSelection {
		a.list = a.list.placeCursor(named(selected.Name))
	}
	if a.spinning || !anyConnecting(rows) {
		return a, nil
	}
	a.spinning = true
	return a, a.spinner.Tick
}

// Open clears the filter and puts the cursor on the current account.
func (a Accounts) Open() Accounts {
	a.list = a.list.clearFilter().placeCursor(named(a.current))
	return a
}

// Update types into the filter line, or advances the animation of a row that
// is connecting.
func (a Accounts) Update(msg tea.Msg) (Accounts, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		var cmd tea.Cmd
		a.list, cmd = a.list.update(msg)
		return a, cmd
	case spinner.TickMsg:
		return a.animate(msg)
	}
	return a, nil
}

func (a Accounts) animate(tick spinner.TickMsg) (Accounts, tea.Cmd) {
	if !anyConnecting(a.list.items) {
		a.spinning = false
		return a, nil
	}
	var cmd tea.Cmd
	a.spinner, cmd = a.spinner.Update(tick)
	return a, cmd
}

func (a Accounts) StartFilter() Accounts {
	a.list = a.list.startFilter()
	return a
}

// Filtering reports whether typed characters go to the filter line.
func (a Accounts) Filtering() bool {
	return a.list.filtering()
}

func (a Accounts) ClearFilter() Accounts {
	a.list = a.list.clearFilter()
	return a
}

func (a Accounts) CursorUp() Accounts {
	a.list = a.list.moveCursor(-1)
	return a
}

func (a Accounts) CursorDown() Accounts {
	a.list = a.list.moveCursor(1)
	return a
}

func (a Accounts) Selected() (AccountRow, bool) {
	return a.list.selected()
}

func (a Accounts) View() string {
	width, height := a.frame.inner()
	bodyHeight := max(height-chromeLines, 0)
	lines := []string{a.list.filterLine()}
	lines = append(lines, padBody(a.body(width, bodyHeight), bodyHeight)...)
	lines = append(lines, themedHelp(a.hints).ShortHelpView(a.keys))
	return a.frame.render(strings.Join(lines, "\n"))
}

// body is every matching row with its failure or refusal hung under it, cut to the
// window that keeps the cursor's row on screen.
func (a Accounts) body(width, height int) []string {
	matches := a.list.matching()
	switch {
	case len(a.list.items) == 0:
		return []string{theme.HintStyle().Render(noAccountsHint)}
	case len(matches) == 0:
		return []string{theme.HintStyle().Render(noMatchHint)}
	}
	columns := measureAccountColumns(matches)
	var lines []string
	cursorLine := 0
	for i, row := range matches {
		selected := i == a.list.cursor
		if selected {
			cursorLine = len(lines)
		}
		lines = append(lines, a.row(row, columns, width, selected))
		lines = append(lines, messageUnder(row, width)...)
	}
	return window(lines, cursorLine, height)
}

// accountColumns are the widths of the name, host and database columns.
type accountColumns struct {
	name, host, database int
}

func measureAccountColumns(rows []AccountRow) accountColumns {
	var columns accountColumns
	for _, row := range rows {
		columns.name = max(columns.name, lipgloss.Width(row.Name))
		columns.host = max(columns.host, lipgloss.Width(endpointHost(row.Endpoint)))
		columns.database = max(columns.database, lipgloss.Width(row.Database))
	}
	columns.name = min(columns.name, maxAccountColumn)
	columns.host = min(columns.host, maxAccountColumn)
	columns.database = min(columns.database, maxAccountColumn)
	return columns
}

func (a Accounts) row(row AccountRow, columns accountColumns, width int, selected bool) string {
	text := theme.TextStyle()
	if selected {
		text = theme.SelectedStyle()
	}
	cells := strings.Join([]string{
		fit(row.Name, columns.name),
		fit(endpointHost(row.Endpoint), columns.host),
		fit(row.Database, columns.database),
		a.stateLabel(row) + readOnlyLabel(row),
	}, "  ")
	return a.glyph(row) + text.Render(fit(cells, max(width-glyphWidth, 1)))
}

func (a Accounts) glyph(row AccountRow) string {
	switch {
	case row.State == AccountConnecting:
		return spinnerView(a.spinner) + " "
	case row.Name == a.current:
		return theme.SuccessStyle().Render(a.icons.Success) + " "
	case row.State == AccountFailed:
		return theme.ErrorStyle().Render(a.icons.Failure) + " "
	}
	return strings.Repeat(" ", glyphWidth)
}

func readOnlyLabel(row AccountRow) string {
	if !row.ReadOnly {
		return ""
	}
	return " · " + ReadOnlyBadge
}

func (a Accounts) stateLabel(row AccountRow) string {
	switch {
	case row.State == AccountConnecting:
		return "connecting…"
	case row.Name == a.current:
		return "current"
	case row.State == AccountConnected:
		return "connected"
	case row.State == AccountFailed:
		return "failed"
	}
	return "not connected"
}

// messageUnder wraps the reason an account failed, or an action on it was
// refused, under its name, so the part that explains it is never cut off.
func messageUnder(row AccountRow, width int) []string {
	message := row.Notice
	if row.State == AccountFailed && row.Err != nil {
		message = row.Err.Error()
	}
	if message == "" {
		return nil
	}
	hanging := strings.Repeat(" ", glyphWidth)
	lines := wrapText(message, width-glyphWidth)
	hung := make([]string, 0, len(lines))
	for _, line := range lines {
		hung = append(hung, hanging+theme.ErrorStyle().Render(line))
	}
	return hung
}

func matchesAccount(row AccountRow, needle string) bool {
	return strings.Contains(strings.ToLower(row.Name), needle) ||
		strings.Contains(strings.ToLower(row.Endpoint), needle)
}

func named(name string) func(AccountRow) bool {
	return func(row AccountRow) bool { return row.Name == name }
}

func anyConnecting(rows []AccountRow) bool {
	for _, row := range rows {
		if row.State == AccountConnecting {
			return true
		}
	}
	return false
}

// endpointHost is the part of an endpoint worth a column: the host, and its
// port unless that is the one HTTPS implies.
func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return endpoint
	}
	return strings.TrimSuffix(parsed.Host, defaultHTTPSPort)
}
