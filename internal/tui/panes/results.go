package panes

import "github.com/colbytimm/alchemist/internal/theme"

const (
	resultsTitle = "Results"
	resultsHint  = "no results yet"
)

// Results is the result table, a hint until iteration 5 gives it rows.
type Results struct {
	frame frame
}

func NewResults() Results {
	return Results{frame: frame{title: resultsTitle}}
}

func (r Results) SetSize(width, height int) Results {
	r.frame = r.frame.size(width, height)
	return r
}

func (r Results) Focus() Results {
	r.frame = r.frame.focus()
	return r
}

func (r Results) Blur() Results {
	r.frame = r.frame.blur()
	return r
}

func (r Results) View() string {
	return r.frame.render(theme.HintStyle().Render(resultsHint))
}
