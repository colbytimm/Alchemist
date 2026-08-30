package panes

import "github.com/colbytimm/alchemist/internal/theme"

const (
	resultsTitle = "Results"
	resultsHint  = "no results yet"
)

// Results is the result table. It renders a hint until iteration 5 gives it
// rows to show.
type Results struct {
	width   int
	height  int
	focused bool
}

func NewResults() Results {
	return Results{}
}

func (r Results) SetSize(width, height int) Results {
	r.width, r.height = width, height
	return r
}

func (r Results) Focus() Results {
	r.focused = true
	return r
}

func (r Results) Blur() Results {
	r.focused = false
	return r
}

func (r Results) View() string {
	return frame{title: resultsTitle, width: r.width, height: r.height, focused: r.focused}.
		render(theme.HintStyle().Render(resultsHint))
}
