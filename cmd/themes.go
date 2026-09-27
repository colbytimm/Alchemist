package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

var _ tui.ThemeCatalog = Themes{}

// Themes serves every theme there is and the one saved in config.toml. The
// picker and alchemist theme use both save through it, so they cannot drift
// apart.
type Themes struct {
	Store  config.Store
	Custom theme.Custom
}

// customThemes is the themes folder beside config.toml. A config directory
// that cannot be located holds no custom themes.
func customThemes() theme.Custom {
	dir, err := config.Dir()
	if err != nil {
		return theme.Custom{}
	}
	dir = filepath.Join(dir, theme.DirName)
	return theme.Custom{Dir: dir, Files: os.DirFS(dir)}
}

// defaultThemes reads config.toml in the config directory, and its themes
// folder.
func defaultThemes() (Themes, error) {
	store, err := config.DefaultStore()
	if err != nil {
		return Themes{}, err
	}
	return Themes{Store: store, Custom: customThemes()}, nil
}

func (t Themes) List() []theme.Entry { return theme.List(t.Custom) }

func (t Themes) Find(name string) (theme.Theme, error) { return theme.Find(name, t.Custom) }

// Save checks the theme loads, then writes it as the theme every launch
// opens in, keeping the rest of config.toml as it is.
func (t Themes) Save(name string) error {
	if _, err := t.Find(name); err != nil {
		return err
	}
	cfg, err := t.Store.Load()
	if err != nil {
		return err
	}
	cfg.Theme = name
	return t.Store.Save(cfg)
}

// Saved is the theme config.toml names, or nothing when it names none or
// cannot be read.
func (t Themes) Saved() string {
	cfg, err := t.Store.Load()
	if err != nil {
		return ""
	}
	return cfg.Theme
}

// themeChoice is the theme a session opens in, and why the saved one was
// passed over, when it was.
type themeChoice struct {
	theme  theme.Theme
	saved  string
	reason error
}

// notice tells the status bar a saved theme did not load, and nothing
// otherwise.
func (c themeChoice) notice() string {
	if c.reason == nil {
		return ""
	}
	return fmt.Sprintf("theme %s not loaded: %s; using %s", c.saved, theme.Problem(c.reason), theme.DefaultName)
}

// selectTheme is --theme, else the saved theme, else alchemist. Only a bad
// --theme is an error: the user typed it for this run. A saved theme that
// does not load falls back for this launch alone, and stays saved.
func (s sessionFlags) selectTheme(l launch, custom theme.Custom) (themeChoice, error) {
	if s.theme != "" {
		found, err := theme.Find(s.theme, custom)
		return themeChoice{theme: found}, err
	}
	if l.theme == "" {
		return themeChoice{theme: theme.Default()}, nil
	}
	found, err := theme.Find(l.theme, custom)
	if err != nil {
		return themeChoice{theme: theme.Default(), saved: l.theme, reason: err}, nil
	}
	return themeChoice{theme: found}, nil
}
