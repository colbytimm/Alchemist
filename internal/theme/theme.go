// Package theme defines Alchemist's visual identity: the adaptive color
// palette, icon glyphs, shared styles, and the ASCII logo. The theme is the
// only place alchemy flavor lives — components elsewhere use plain names.
//
// Served by accessor rather than var so no caller can mutate what another
// caller sees.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette: alchemy-inspired adaptive colors (light terminal, dark terminal).
var (
	gold      = lipgloss.AdaptiveColor{Light: "#D4A017", Dark: "#F5C542"}
	copper    = lipgloss.AdaptiveColor{Light: "#B87333", Dark: "#D48F52"}
	verdigris = lipgloss.AdaptiveColor{Light: "#2E8B84", Dark: "#5FD3CE"}
	amethyst  = lipgloss.AdaptiveColor{Light: "#6C3FA0", Dark: "#9B6FD0"}
	parchment = lipgloss.AdaptiveColor{Light: "#5C4B37", Dark: "#E8DCC8"}
	cinnabar  = lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#E74C3C"}
	ash       = lipgloss.AdaptiveColor{Light: "#8A8378", Dark: "#6B655B"}
)

// Gold is the primary accent: focused borders, logo, highlights.
func Gold() lipgloss.AdaptiveColor { return gold }

// Copper is the secondary accent: selected rows, numbers.
func Copper() lipgloss.AdaptiveColor { return copper }

// Verdigris marks success states and RU statistics.
func Verdigris() lipgloss.AdaptiveColor { return verdigris }

// Amethyst styles keywords and emphasis.
func Amethyst() lipgloss.AdaptiveColor { return amethyst }

// Parchment is the default body text color.
func Parchment() lipgloss.AdaptiveColor { return parchment }

// Cinnabar marks errors and failure states.
func Cinnabar() lipgloss.AdaptiveColor { return cinnabar }

// Ash is the muted color for borders, hints, and inactive chrome.
func Ash() lipgloss.AdaptiveColor { return ash }

// IconSet groups the glyphs used across panes.
type IconSet struct {
	Database      string
	Container     string
	Expanded      string
	Collapsed     string
	Success       string
	Failure       string
	Separator     string
	SpinnerFrames []string
}

var (
	unicodeIcons = IconSet{
		Database:      "◆",
		Container:     "▪",
		Expanded:      "▾",
		Collapsed:     "▸",
		Success:       "✓",
		Failure:       "✗",
		Separator:     "▪",
		SpinnerFrames: []string{"◐", "◓", "◑", "◒"},
	}
	asciiIcons = IconSet{
		Database:      "*",
		Container:     "-",
		Expanded:      "v",
		Collapsed:     ">",
		Success:       "+",
		Failure:       "x",
		Separator:     "|",
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

var (
	logoStyle     = lipgloss.NewStyle().Foreground(gold).Bold(true)
	taglineStyle  = lipgloss.NewStyle().Foreground(amethyst)
	textStyle     = lipgloss.NewStyle().Foreground(parchment)
	hintStyle     = lipgloss.NewStyle().Foreground(ash)
	errorStyle    = lipgloss.NewStyle().Foreground(cinnabar)
	successStyle  = lipgloss.NewStyle().Foreground(verdigris)
	selectedStyle = lipgloss.NewStyle().Foreground(copper).Bold(true)
	spinnerStyle  = lipgloss.NewStyle().Foreground(gold)

	focusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(gold)
	blurredBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ash)
)

func LogoStyle() lipgloss.Style { return logoStyle }

func TaglineStyle() lipgloss.Style { return taglineStyle }

func TextStyle() lipgloss.Style { return textStyle }

func HintStyle() lipgloss.Style { return hintStyle }

func ErrorStyle() lipgloss.Style { return errorStyle }

func SuccessStyle() lipgloss.Style { return successStyle }

// SelectedStyle marks the row under the cursor.
func SelectedStyle() lipgloss.Style { return selectedStyle }

func SpinnerStyle() lipgloss.Style { return spinnerStyle }

func FocusedBorderStyle() lipgloss.Style { return focusedBorderStyle }

func BlurredBorderStyle() lipgloss.Style { return blurredBorderStyle }

// RawLogo is the unstyled ASCII logo.
const RawLogo = `   _   _    ___ _  _ ___ __  __ ___ ___ _____
  /_\ | |  / __| || | __|  \/  |_ _/ __|_   _|
 / _ \| |_| (__| __ | _|| |\/| || |\__ \ | |
/_/ \_\____\___|_||_|___|_|  |_|___|___/ |_|`

func Logo() string {
	return logoStyle.Render(RawLogo)
}
