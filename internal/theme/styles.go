package theme

import "github.com/charmbracelet/lipgloss"

// Styles is every style Alchemist draws with, built from one theme. It is
// the only place styles are made from colors, so the workspace and the
// picker's preview of any theme cannot disagree.
type Styles struct {
	theme         Theme
	text          lipgloss.Style
	hint          lipgloss.Style
	err           lipgloss.Style
	success       lipgloss.Style
	selected      lipgloss.Style
	spinner       lipgloss.Style
	accent        lipgloss.Style
	heading       lipgloss.Style
	warning       lipgloss.Style
	focusedBorder lipgloss.Style
	blurredBorder lipgloss.Style
	syntax        [roleCount]lipgloss.Style
}

// NewStyles leaves the active styles alone. Emphasis is not the theme's to
// choose: a keyword is told from an operator of the same color by weight.
func NewStyles(t Theme) *Styles {
	fg := func(r Role) lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Color(r)) }
	border := func(r Role) lipgloss.Style {
		return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Color(r))
	}
	s := &Styles{
		theme:         t,
		text:          fg(Text),
		hint:          fg(Muted),
		err:           fg(Error),
		success:       fg(Success),
		selected:      fg(Selected).Bold(true),
		spinner:       fg(Accent),
		accent:        fg(Accent),
		heading:       fg(Accent).Bold(true),
		warning:       fg(Warning),
		focusedBorder: border(Accent),
		blurredBorder: border(Muted),
	}
	for _, r := range []Role{Operator, Literal, Function, String, Number, Punctuation} {
		s.syntax[r] = fg(r)
	}
	s.syntax[Keyword] = fg(Keyword).Bold(true)
	s.syntax[Alias] = fg(Alias).Bold(true)
	s.syntax[Parameter] = fg(Parameter).Italic(true)
	s.syntax[Comment] = fg(Comment).Italic(true)
	return s
}

func (s *Styles) Theme() Theme { return s.theme }

func (s *Styles) TextStyle() lipgloss.Style { return s.text }

func (s *Styles) HintStyle() lipgloss.Style { return s.hint }

func (s *Styles) ErrorStyle() lipgloss.Style { return s.err }

func (s *Styles) SuccessStyle() lipgloss.Style { return s.success }

func (s *Styles) SelectedStyle() lipgloss.Style { return s.selected }

func (s *Styles) SpinnerStyle() lipgloss.Style { return s.spinner }

// AccentStyle marks what has the keyboard: the focused editor's prompt and
// the keys in help.
func (s *Styles) AccentStyle() lipgloss.Style { return s.accent }

func (s *Styles) HeadingStyle() lipgloss.Style { return s.heading }

func (s *Styles) WarningStyle() lipgloss.Style { return s.warning }

func (s *Styles) FocusedBorderStyle() lipgloss.Style { return s.focusedBorder }

func (s *Styles) BlurredBorderStyle() lipgloss.Style { return s.blurredBorder }

func (s *Styles) SyntaxKeyword() lipgloss.Style { return s.syntax[Keyword] }

func (s *Styles) SyntaxOperator() lipgloss.Style { return s.syntax[Operator] }

func (s *Styles) SyntaxLiteral() lipgloss.Style { return s.syntax[Literal] }

func (s *Styles) SyntaxFunction() lipgloss.Style { return s.syntax[Function] }

func (s *Styles) SyntaxAlias() lipgloss.Style { return s.syntax[Alias] }

func (s *Styles) SyntaxParameter() lipgloss.Style { return s.syntax[Parameter] }

func (s *Styles) SyntaxString() lipgloss.Style { return s.syntax[String] }

func (s *Styles) SyntaxNumber() lipgloss.Style { return s.syntax[Number] }

func (s *Styles) SyntaxComment() lipgloss.Style { return s.syntax[Comment] }

func (s *Styles) SyntaxPunctuation() lipgloss.Style { return s.syntax[Punctuation] }

// BackgroundStyle paints the source theme's editor background, which only
// the picker's preview does; false when the theme names none.
func (s *Styles) BackgroundStyle() (lipgloss.Style, bool) {
	background := s.theme.About().Background
	if background == "" {
		return lipgloss.Style{}, false
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(background)), true
}

// DiagnosticError is the color of the squiggle under a flagged range.
func (s *Styles) DiagnosticError() lipgloss.AdaptiveColor { return s.theme.Color(Error) }
