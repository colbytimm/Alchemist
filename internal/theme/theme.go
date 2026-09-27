// Package theme holds Alchemist's look: the color themes, the styles built
// from the one in use, and the icon glyphs. Components elsewhere ask for a
// style by role and never name a color.
package theme

import "github.com/charmbracelet/lipgloss"

// IconSet groups the glyphs used across panes.
type IconSet struct {
	Database  string
	Container string
	Expanded  string
	Collapsed string
	Left      string
	Right     string
	Success   string
	Failure   string
	Separator string
	BarFull   string
	BarEmpty  string
	// Marked is a row picked out of a list; Removed signs what a diff
	// lost, beside + and ~.
	Marked        string
	Removed       string
	SpinnerFrames []string
}

var (
	unicodeIcons = IconSet{
		Database:      "◆",
		Container:     "▪",
		Expanded:      "▾",
		Collapsed:     "▸",
		Left:          "◂",
		Right:         "▸",
		Success:       "✓",
		Failure:       "✗",
		Separator:     "▪",
		BarFull:       "█",
		BarEmpty:      "░",
		Marked:        "●",
		Removed:       "−",
		SpinnerFrames: []string{"◐", "◓", "◑", "◒"},
	}
	asciiIcons = IconSet{
		Database:      "*",
		Container:     "-",
		Expanded:      "v",
		Collapsed:     ">",
		Left:          "<",
		Right:         ">",
		Success:       "+",
		Failure:       "x",
		Separator:     "|",
		BarFull:       "#",
		BarEmpty:      "-",
		Marked:        "*",
		Removed:       "-",
		SpinnerFrames: []string{"|", "/", "-", `\`},
	}
)

// Icons returns the default glyph set (no Nerd Font required).
func Icons() IconSet { return unicodeIcons.clone() }

// ASCIIIcons returns a pure-ASCII fallback for limited terminals.
func ASCIIIcons() IconSet { return asciiIcons.clone() }

func (s IconSet) clone() IconSet {
	s.SpinnerFrames = append([]string(nil), s.SpinnerFrames...)
	return s
}

func TextStyle() lipgloss.Style { return Active().TextStyle() }

func HintStyle() lipgloss.Style { return Active().HintStyle() }

func ErrorStyle() lipgloss.Style { return Active().ErrorStyle() }

func SuccessStyle() lipgloss.Style { return Active().SuccessStyle() }

func SelectedStyle() lipgloss.Style { return Active().SelectedStyle() }

func SpinnerStyle() lipgloss.Style { return Active().SpinnerStyle() }

// AccentStyle marks what has the keyboard: the focused editor's prompt and
// the keys in help.
func AccentStyle() lipgloss.Style { return Active().AccentStyle() }

func HeadingStyle() lipgloss.Style { return Active().HeadingStyle() }

func WarningStyle() lipgloss.Style { return Active().WarningStyle() }

func FocusedBorderStyle() lipgloss.Style { return Active().FocusedBorderStyle() }

func BlurredBorderStyle() lipgloss.Style { return Active().BlurredBorderStyle() }
