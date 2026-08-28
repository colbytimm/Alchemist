// Package theme defines Alchemist's visual identity: the adaptive color
// palette, icon glyphs, shared styles, and the ASCII logo. The theme is the
// only place alchemy flavor lives — components elsewhere use plain names.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette: alchemy-inspired adaptive colors (light terminal, dark terminal).
var (
	// Gold is the primary accent: focused borders, logo, highlights.
	Gold = lipgloss.AdaptiveColor{Light: "#D4A017", Dark: "#F5C542"}
	// Copper is the secondary accent: selected rows, numbers.
	Copper = lipgloss.AdaptiveColor{Light: "#B87333", Dark: "#D48F52"}
	// Verdigris marks success states and RU statistics.
	Verdigris = lipgloss.AdaptiveColor{Light: "#2E8B84", Dark: "#5FD3CE"}
	// Amethyst styles keywords and emphasis.
	Amethyst = lipgloss.AdaptiveColor{Light: "#6C3FA0", Dark: "#9B6FD0"}
	// Parchment is the default body text color.
	Parchment = lipgloss.AdaptiveColor{Light: "#5C4B37", Dark: "#E8DCC8"}
	// Cinnabar marks errors and failure states.
	Cinnabar = lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#E74C3C"}
	// Ash is the muted color for borders, hints, and inactive chrome.
	Ash = lipgloss.AdaptiveColor{Light: "#8A8378", Dark: "#6B655B"}
)

// IconSet groups the glyphs used across panes.
type IconSet struct {
	Database  string
	Container string
	Expanded  string
	Collapsed string
	Success   string
	Failure   string
	Spinner   string
}

// Icons is the default glyph set (plain Unicode, no Nerd Font required).
var Icons = IconSet{
	Database:  "◆",
	Container: "▪",
	Expanded:  "▾",
	Collapsed: "▸",
	Success:   "✓",
	Failure:   "✗",
	Spinner:   "◌",
}

// ASCIIIcons is a pure-ASCII fallback for limited terminals.
var ASCIIIcons = IconSet{
	Database:  "*",
	Container: "-",
	Expanded:  "v",
	Collapsed: ">",
	Success:   "+",
	Failure:   "x",
	Spinner:   "o",
}

// Shared styles.
var (
	// LogoStyle renders the ASCII logo.
	LogoStyle = lipgloss.NewStyle().Foreground(Gold).Bold(true)
	// TaglineStyle renders the subtitle under the logo.
	TaglineStyle = lipgloss.NewStyle().Foreground(Amethyst)
	// TextStyle is the default body text style.
	TextStyle = lipgloss.NewStyle().Foreground(Parchment)
	// HintStyle renders muted helper text such as key hints.
	HintStyle = lipgloss.NewStyle().Foreground(Ash)
	// ErrorStyle renders error text.
	ErrorStyle = lipgloss.NewStyle().Foreground(Cinnabar)
	// SuccessStyle renders success text and statistics.
	SuccessStyle = lipgloss.NewStyle().Foreground(Verdigris)
	// FocusedBorderStyle is the border for the focused pane.
	FocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Gold)
	// BlurredBorderStyle is the border for unfocused panes.
	BlurredBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Ash)
)

// RawLogo is the unstyled ASCII logo.
const RawLogo = `   _   _    ___ _  _ ___ __  __ ___ ___ _____
  /_\ | |  / __| || | __|  \/  |_ _/ __|_   _|
 / _ \| |_| (__| __ | _|| |\/| || |\__ \ | |
/_/ \_\____\___|_||_|___|_|  |_|___|___/ |_|`

// Logo returns the ASCII logo styled in Gold.
func Logo() string {
	return LogoStyle.Render(RawLogo)
}
