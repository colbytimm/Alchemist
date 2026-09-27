package theme_test

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

// withTerminal renders under profile on a dark background for the length of
// the test.
func withTerminal(t *testing.T, profile termenv.Profile) {
	t.Helper()
	previous, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(profile)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(previous)
		lipgloss.SetHasDarkBackground(dark)
	})
}

func TestKeywordsAndOperatorsShareAColorButNotAWeight(t *testing.T) {
	assert.Equal(t, theme.SyntaxKeyword().GetForeground(), theme.SyntaxOperator().GetForeground())
	assert.True(t, theme.SyntaxKeyword().GetBold())
	assert.False(t, theme.SyntaxOperator().GetBold())
}

func TestSequencesOfSplitsAStyleAroundItsText(t *testing.T) {
	withTerminal(t, termenv.TrueColor)
	style := theme.SyntaxString()

	sequences := theme.SequencesOf(style)

	assert.Equal(t, style.Render("'west'"), sequences.Open+"'west'"+sequences.Close)
	assert.NotEmpty(t, sequences.Open)
}

func TestSequencesOfAreEmptyUnderNoColor(t *testing.T) {
	withTerminal(t, termenv.Ascii)

	assert.Equal(t, theme.Sequences{}, theme.SequencesOf(theme.SyntaxKeyword()))
}

func TestDiagnosticUnderlineRender(t *testing.T) {
	tests := []struct {
		name      string
		profile   termenv.Profile
		underline theme.DiagnosticUnderline
		want      string
	}{
		{
			name:      "curly, colored with the palette's red",
			profile:   termenv.TrueColor,
			underline: theme.CurlyUnderline,
			want:      "\x1b[4:3m\x1b[58:2::231:76:60mCONTAIN\x1b[4:0m\x1b[59m",
		},
		{
			name:      "curly, colored from the 256-color table",
			profile:   termenv.ANSI256,
			underline: theme.CurlyUnderline,
			want:      "\x1b[4:3m\x1b[58:5:167mCONTAIN\x1b[4:0m\x1b[59m",
		},
		{
			name:      "plain",
			profile:   termenv.TrueColor,
			underline: theme.PlainUnderline,
			want:      "\x1b[4mCONTAIN\x1b[24m",
		},
		{
			name:      "curly under NO_COLOR is plain and uncolored",
			profile:   termenv.Ascii,
			underline: theme.CurlyUnderline,
			want:      "\x1b[4mCONTAIN\x1b[24m",
		},
		{
			name:      "off",
			profile:   termenv.TrueColor,
			underline: theme.NoUnderline,
			want:      "CONTAIN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTerminal(t, tt.profile)

			require.Equal(t, tt.want, tt.underline.Render("CONTAIN", theme.DiagnosticError()))
		})
	}
}

func TestParseDiagnosticUnderline(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  theme.DiagnosticUnderline
	}{
		{name: "unset is curly", input: "", want: theme.CurlyUnderline},
		{name: "curly", input: "curly", want: theme.CurlyUnderline},
		{name: "underline", input: "underline", want: theme.PlainUnderline},
		{name: "off", input: "off", want: theme.NoUnderline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := theme.ParseDiagnosticUnderline(tt.input)

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			if tt.input != "" {
				require.Equal(t, tt.input, got.String(), "the setting spells itself back")
			}
		})
	}
}

func TestParseDiagnosticUnderlineRefusesAnythingElse(t *testing.T) {
	_, err := theme.ParseDiagnosticUnderline("wavy")

	require.ErrorIs(t, err, theme.ErrUnknownDiagnosticUnderline)
}
