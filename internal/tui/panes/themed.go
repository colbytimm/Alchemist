package panes

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/colbytimm/alchemist/internal/theme"
)

// The bubbles components keep whatever styles they are handed. Panes hand
// them the active theme's each time they draw rather than once when they
// are built, so a theme switch reaches the next frame with nothing to
// rebuild.

func themedHelp(hints help.Model) help.Model {
	hints.Styles = helpStyles()
	return hints
}

func spinnerView(s spinner.Model) string {
	s.Style = theme.SpinnerStyle()
	return s.View()
}

func inputView(input textinput.Model) string {
	input.PlaceholderStyle = theme.HintStyle()
	input.TextStyle = theme.TextStyle()
	return input.View()
}

// promptedInputView is inputView for an input that shows a prompt before
// what is typed.
func promptedInputView(input textinput.Model) string {
	input.PromptStyle = theme.HintStyle()
	return inputView(input)
}
