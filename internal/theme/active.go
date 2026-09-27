package theme

import (
	"sync"
	"sync/atomic"
)

// active is the session's theme, package state on purpose: every accessor
// reads it, so a switch reaches every pane without a theme being threaded
// through each of them. Nil means the default.
var active atomic.Pointer[Styles]

var defaultStyles = sync.OnceValue(func() *Styles { return NewStyles(Default()) })

// Use makes t the theme every accessor draws with, from the next call on.
func Use(t Theme) {
	active.Store(NewStyles(t))
}

// Active is the styles in use. The pointer changes only when Use is called,
// so a cache can tell from it whether what it drew is still current.
func Active() *Styles {
	if s := active.Load(); s != nil {
		return s
	}
	return defaultStyles()
}
