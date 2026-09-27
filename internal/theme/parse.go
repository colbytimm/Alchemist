package theme

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

var (
	ErrUnknownTheme = errors.New("unknown theme")
	ErrInvalidTheme = errors.New("invalid theme")
)

// Theme is a color for every role, and the credit of where the colors came
// from.
type Theme struct {
	name   string
	about  About
	colors [roleCount]lipgloss.AdaptiveColor
}

// About credits a theme. Background is its source's editor background,
// which only the picker's preview paints.
type About struct {
	Title      string `toml:"title"`
	Author     string `toml:"author"`
	Source     string `toml:"source"`
	License    string `toml:"license"`
	Background string `toml:"background"`
}

func (t Theme) Name() string { return t.name }

func (t Theme) About() About { return t.about }

func (t Theme) Color(r Role) lipgloss.AdaptiveColor { return t.colors[r] }

// Adapts reports whether any role has its own color for light terminals.
func (t Theme) Adapts() bool {
	return slices.ContainsFunc(t.colors[:], func(c lipgloss.AdaptiveColor) bool { return c.Light != c.Dark })
}

// LoadError is why a theme does not load. Path is empty for a theme parsed
// from bytes, and Err is the problem alone, which the picker shows.
type LoadError struct {
	Theme string
	Path  string
	Err   error
}

func (e *LoadError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("theme %s: %v", e.Theme, e.Err)
	}
	return fmt.Sprintf("theme %s: %s: %v", e.Theme, e.Path, e.Err)
}

func (e *LoadError) Unwrap() []error { return []error{ErrInvalidTheme, e.Err} }

// themeFile is the TOML a theme is written in. Colors is read loosely so
// every problem in it can be named by role.
type themeFile struct {
	About  About          `toml:"about"`
	Colors map[string]any `toml:"colors"`
}

var (
	hexColor      = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	hexAlphaColor = regexp.MustCompile(`^#[0-9A-Fa-f]{8}$`)
)

// Parse reads a theme file; name is the file's name without .toml.
func Parse(name string, data []byte) (Theme, error) {
	t, err := parse(data)
	if err != nil {
		return Theme{}, &LoadError{Theme: name, Err: err}
	}
	t.name = name
	return t, nil
}

func parse(data []byte) (Theme, error) {
	var file themeFile
	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return Theme{}, err
	}
	if err := checkUndecoded(meta); err != nil {
		return Theme{}, err
	}
	if err := checkRoleKeys(file.Colors); err != nil {
		return Theme{}, err
	}
	t := Theme{about: file.About}
	for _, role := range Roles() {
		if t.colors[role], err = parseColor(role.String(), file.Colors[role.String()]); err != nil {
			return Theme{}, err
		}
	}
	if background := file.About.Background; background != "" {
		if _, err := parseHex("background", background); err != nil {
			return Theme{}, err
		}
	}
	return t, nil
}

// checkUndecoded refuses a key outside [colors]. The decoder counts the
// keys of a color pair as undecoded too, and checkRoleKeys judges those.
func checkUndecoded(meta toml.MetaData) error {
	for _, key := range meta.Undecoded() {
		if key[0] != "colors" {
			return fmt.Errorf("unknown key %q", key.String())
		}
	}
	return nil
}

// checkRoleKeys refuses a key that names no role, then names every role
// left out, in role order.
func checkRoleKeys(colors map[string]any) error {
	for _, key := range slices.Sorted(maps.Keys(colors)) {
		if !slices.Contains(roleNames[:], key) {
			return fmt.Errorf("unknown key %q", "colors."+key)
		}
	}
	var missing []string
	for _, role := range Roles() {
		if _, ok := colors[role.String()]; !ok {
			missing = append(missing, role.String())
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing roles: %s", strings.Join(missing, ", "))
	}
	return nil
}

// parseColor reads one color for light and dark alike, or a pair.
func parseColor(role string, value any) (lipgloss.AdaptiveColor, error) {
	pair, ok := value.(map[string]any)
	if !ok {
		hex, err := parseHex(role, value)
		return lipgloss.AdaptiveColor{Light: hex, Dark: hex}, err
	}
	for _, key := range slices.Sorted(maps.Keys(pair)) {
		if key != "light" && key != "dark" {
			return lipgloss.AdaptiveColor{}, fmt.Errorf("unknown key %q", "colors."+role+"."+key)
		}
	}
	light, hasLight := pair["light"]
	dark, hasDark := pair["dark"]
	if !hasLight || !hasDark {
		return lipgloss.AdaptiveColor{}, fmt.Errorf("%s: a pair needs both light and dark", role)
	}
	lightHex, err := parseHex(role, light)
	if err != nil {
		return lipgloss.AdaptiveColor{}, err
	}
	darkHex, err := parseHex(role, dark)
	return lipgloss.AdaptiveColor{Light: lightHex, Dark: darkHex}, err
}

func parseHex(role string, value any) (string, error) {
	hex, ok := value.(string)
	switch {
	case !ok:
		return "", fmt.Errorf("%s: %v is not #RRGGBB", role, value)
	case hexAlphaColor.MatchString(hex):
		return "", fmt.Errorf("%s: %q has an alpha channel a terminal cannot draw: use #RRGGBB", role, hex)
	case !hexColor.MatchString(hex):
		return "", fmt.Errorf("%s: %q is not #RRGGBB", role, hex)
	}
	return hex, nil
}
