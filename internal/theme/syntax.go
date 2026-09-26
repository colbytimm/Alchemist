package theme

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The editor's token classes. Keywords are bold where operators are not, the
// one thing telling the two amethyst classes apart.
var (
	syntaxKeywordStyle     = lipgloss.NewStyle().Foreground(amethyst).Bold(true)
	syntaxOperatorStyle    = lipgloss.NewStyle().Foreground(amethyst)
	syntaxLiteralStyle     = lipgloss.NewStyle().Foreground(copper)
	syntaxFunctionStyle    = lipgloss.NewStyle().Foreground(gold)
	syntaxAliasStyle       = lipgloss.NewStyle().Foreground(parchment).Bold(true)
	syntaxParameterStyle   = lipgloss.NewStyle().Foreground(copper).Italic(true)
	syntaxStringStyle      = lipgloss.NewStyle().Foreground(verdigris)
	syntaxNumberStyle      = lipgloss.NewStyle().Foreground(copper)
	syntaxCommentStyle     = lipgloss.NewStyle().Foreground(ash).Italic(true)
	syntaxPunctuationStyle = lipgloss.NewStyle().Foreground(ash)
)

func SyntaxKeyword() lipgloss.Style { return syntaxKeywordStyle }

func SyntaxOperator() lipgloss.Style { return syntaxOperatorStyle }

func SyntaxLiteral() lipgloss.Style { return syntaxLiteralStyle }

func SyntaxFunction() lipgloss.Style { return syntaxFunctionStyle }

func SyntaxAlias() lipgloss.Style { return syntaxAliasStyle }

func SyntaxParameter() lipgloss.Style { return syntaxParameterStyle }

func SyntaxString() lipgloss.Style { return syntaxStringStyle }

func SyntaxNumber() lipgloss.Style { return syntaxNumberStyle }

func SyntaxComment() lipgloss.Style { return syntaxCommentStyle }

func SyntaxPunctuation() lipgloss.Style { return syntaxPunctuationStyle }

// DiagnosticError is the color of the squiggle under a flagged range.
func DiagnosticError() lipgloss.AdaptiveColor { return cinnabar }

// Sequences are the escape codes a style writes before and after its text,
// so many runs can be painted without rendering each through lipgloss.
type Sequences struct {
	Open  string
	Close string
}

// SequencesOf holds for styles that only color and emphasize: one that pads,
// aligns or sizes its text writes more than a prefix and a suffix.
func SequencesOf(style lipgloss.Style) Sequences {
	const marker = "x"
	rendered := style.Render(marker)
	i := strings.Index(rendered, marker)
	return Sequences{Open: rendered[:i], Close: rendered[i+len(marker):]}
}

// DiagnosticUnderline is how a diagnostic's range is drawn: a setting rather
// than a detection, since whether a terminal draws a curly underline cannot
// be told reliably over SSH or through tmux.
type DiagnosticUnderline int

const (
	CurlyUnderline DiagnosticUnderline = iota
	PlainUnderline
	NoUnderline
)

var diagnosticUnderlineNames = []string{"curly", "underline", "off"}

var ErrUnknownDiagnosticUnderline = errors.New("unknown diagnostics setting")

// ParseDiagnosticUnderline reads the diagnostics setting; empty is the
// default, curly.
func ParseDiagnosticUnderline(name string) (DiagnosticUnderline, error) {
	if name == "" {
		return CurlyUnderline, nil
	}
	for i, known := range diagnosticUnderlineNames {
		if name == known {
			return DiagnosticUnderline(i), nil
		}
	}
	return 0, fmt.Errorf("theme: %q, want one of %s: %w",
		name, strings.Join(diagnosticUnderlineNames, ", "), ErrUnknownDiagnosticUnderline)
}

func (u DiagnosticUnderline) String() string {
	return diagnosticUnderlineNames[u]
}

// Render underlines text in color. A terminal that does not know the curly
// form (SGR 4:3) draws a plain underline or none, in the text's own color,
// and under NO_COLOR every form is a plain underline.
func (u DiagnosticUnderline) Render(text string, color lipgloss.AdaptiveColor) string {
	switch {
	case u == NoUnderline:
		return text
	case u == PlainUnderline || lipgloss.ColorProfile() == termenv.Ascii:
		return "\x1b[4m" + text + "\x1b[24m"
	}
	return "\x1b[4:3m\x1b[" + underlineColor(color) + "m" + text + "\x1b[4:0m\x1b[59m"
}

// underlineColor is SGR 58 for color, in the terminal's profile.
func underlineColor(color lipgloss.AdaptiveColor) string {
	hex := color.Light
	if lipgloss.HasDarkBackground() {
		hex = color.Dark
	}
	if lipgloss.ColorProfile() != termenv.TrueColor {
		if indexed, ok := termenv.ANSI256.Color(hex).(termenv.ANSI256Color); ok {
			return "58:5:" + strconv.Itoa(int(indexed))
		}
	}
	rgb, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		return "59"
	}
	return fmt.Sprintf("58:2::%d:%d:%d", rgb>>16, rgb>>8&0xff, rgb&0xff)
}
