package theme_test

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/theme"
)

// roleStyles draws every role through the style that shows it.
var roleStyles = map[theme.Role]func(*theme.Styles) lipgloss.Style{
	theme.Text:        (*theme.Styles).TextStyle,
	theme.Muted:       (*theme.Styles).HintStyle,
	theme.Accent:      (*theme.Styles).AccentStyle,
	theme.Selected:    (*theme.Styles).SelectedStyle,
	theme.Success:     (*theme.Styles).SuccessStyle,
	theme.Warning:     (*theme.Styles).WarningStyle,
	theme.Error:       (*theme.Styles).ErrorStyle,
	theme.Keyword:     (*theme.Styles).SyntaxKeyword,
	theme.Operator:    (*theme.Styles).SyntaxOperator,
	theme.Literal:     (*theme.Styles).SyntaxLiteral,
	theme.Function:    (*theme.Styles).SyntaxFunction,
	theme.Alias:       (*theme.Styles).SyntaxAlias,
	theme.Parameter:   (*theme.Styles).SyntaxParameter,
	theme.String:      (*theme.Styles).SyntaxString,
	theme.Number:      (*theme.Styles).SyntaxNumber,
	theme.Comment:     (*theme.Styles).SyntaxComment,
	theme.Punctuation: (*theme.Styles).SyntaxPunctuation,
}

func TestEveryRoleHasAStyle(t *testing.T) {
	assert.Len(t, roleStyles, len(theme.Roles()))
}

func TestStylesOfTwoThemesDifferInEveryRole(t *testing.T) {
	withTerminal(t, termenv.TrueColor)
	a := theme.NewStyles(distinctTheme(t, "a", 0x10))
	b := theme.NewStyles(distinctTheme(t, "b", 0x20))

	for role, style := range roleStyles {
		assert.NotEqual(t, style(a).Render("x"), style(b).Render("x"), role.String())
	}
}

func TestNewStylesLeavesTheActiveStylesAlone(t *testing.T) {
	before := theme.Active()

	theme.NewStyles(distinctTheme(t, "a", 0x10))

	assert.Same(t, before, theme.Active())
}

func TestEmphasisStaysInCode(t *testing.T) {
	styles := theme.NewStyles(distinctTheme(t, "a", 0x10))

	assert.True(t, styles.SyntaxKeyword().GetBold())
	assert.False(t, styles.SyntaxOperator().GetBold())
	assert.True(t, styles.SyntaxAlias().GetBold())
	assert.True(t, styles.SyntaxParameter().GetItalic())
	assert.True(t, styles.SyntaxComment().GetItalic())
	assert.True(t, styles.SelectedStyle().GetBold())
	assert.True(t, styles.HeadingStyle().GetBold())
}

func TestTheSquiggleIsTheErrorColor(t *testing.T) {
	a := distinctTheme(t, "a", 0x10)

	assert.Equal(t, a.Color(theme.Error), theme.NewStyles(a).DiagnosticError())
}
