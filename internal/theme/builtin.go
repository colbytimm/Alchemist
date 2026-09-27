package theme

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// DirName is the folder of custom themes inside the config directory.
const DirName = "themes"

// DefaultName is the theme used when none is chosen.
const DefaultName = "alchemist"

const fileExt = ".toml"

//go:embed themes/*.toml
var builtinFiles embed.FS

// namePattern is config's profile name rule: a theme's name is its file's.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// defaultTheme panics on a broken alchemist.toml, as regexp.MustCompile does
// on a broken constant: the file is part of the binary, and every test in
// the package fails first if it does not parse.
var defaultTheme = sync.OnceValue(func() Theme {
	t, err := parseBuiltin(DefaultName)
	if err != nil {
		panic(err)
	}
	return t
})

func Default() Theme { return defaultTheme() }

func BuiltinNames() []string {
	// The embed pattern does not compile without the folder, so reading it
	// cannot fail.
	entries, _ := builtinFiles.ReadDir(DirName)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), fileExt))
	}
	return names
}

// Custom is where custom themes are read from: Files is the folder, and Dir
// its path, for messages. The zero value holds no themes.
type Custom struct {
	Dir   string
	Files fs.FS
}

func (c Custom) path(name string) string {
	return filepath.Join(c.Dir, name+fileExt)
}

// Find loads a built-in theme, or else a custom one.
func Find(name string, custom Custom) (Theme, error) {
	if slices.Contains(BuiltinNames(), name) {
		return parseBuiltin(name)
	}
	data, err := customFile(name, custom)
	if err != nil {
		return Theme{}, err
	}
	t, err := Parse(name, data)
	return t, withPath(err, custom.path(name))
}

// File is a theme's file as written, comments included, to copy and edit.
func File(name string, custom Custom) ([]byte, error) {
	if slices.Contains(BuiltinNames(), name) {
		return builtinFiles.ReadFile(path.Join(DirName, name+fileExt))
	}
	return customFile(name, custom)
}

func parseBuiltin(name string) (Theme, error) {
	data, err := builtinFiles.ReadFile(path.Join(DirName, name+fileExt))
	if err != nil {
		return Theme{}, err
	}
	return Parse(name, data)
}

func customFile(name string, custom Custom) ([]byte, error) {
	if !namePattern.MatchString(name) || custom.Files == nil {
		return nil, unknownTheme(name, custom)
	}
	data, err := fs.ReadFile(custom.Files, name+fileExt)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, unknownTheme(name, custom)
	}
	if err != nil {
		return nil, &LoadError{Theme: name, Path: custom.path(name), Err: err}
	}
	return data, nil
}

func withPath(err error, path string) error {
	var loadErr *LoadError
	if errors.As(err, &loadErr) {
		loadErr.Path = path
	}
	return err
}

// unknownTheme lists every theme there is, so a typo can be put right from
// the message alone.
func unknownTheme(name string, custom Custom) error {
	available := "built-in: " + strings.Join(BuiltinNames(), ", ")
	var customNames []string
	for _, entry := range List(custom) {
		if !entry.BuiltIn && !entry.Ignored {
			customNames = append(customNames, entry.Name)
		}
	}
	switch {
	case len(customNames) > 0:
		available += fmt.Sprintf("; custom: %s (in %s)", strings.Join(customNames, ", "), custom.Dir)
	case custom.Dir != "":
		available += "; no custom themes in " + custom.Dir
	}
	return fmt.Errorf("theme %q: %w: %s", name, ErrUnknownTheme, available)
}

// Entry is one theme as theme list and the picker show it. Err is why a
// custom file does not load; an ignored file is not a theme at all.
type Entry struct {
	Name    string
	About   About
	BuiltIn bool
	Path    string
	Err     error
	Ignored bool
}

// List is every built-in theme, then every file in the custom folder, each
// in name order. A missing folder holds no custom themes.
func List(custom Custom) []Entry {
	var entries []Entry
	for _, name := range BuiltinNames() {
		t, err := parseBuiltin(name)
		entries = append(entries, Entry{Name: name, About: t.About(), BuiltIn: true, Err: err})
	}
	if custom.Files == nil {
		return entries
	}
	files, err := fs.ReadDir(custom.Files, ".")
	if err != nil {
		return entries
	}
	for _, file := range files {
		if !file.IsDir() {
			entries = append(entries, customEntry(file.Name(), custom))
		}
	}
	return entries
}

var errNotAThemeFile = errors.New("not a theme file: a theme is <name>.toml, named with letters, digits, - and _")

func customEntry(fileName string, custom Custom) Entry {
	name, isTOML := strings.CutSuffix(fileName, fileExt)
	path := filepath.Join(custom.Dir, fileName)
	switch {
	case !isTOML || !namePattern.MatchString(name):
		return Entry{Name: fileName, Path: path, Err: errNotAThemeFile, Ignored: true}
	case slices.Contains(BuiltinNames(), name):
		err := &LoadError{Theme: name, Path: path, Err: fmt.Errorf("%s is a built-in theme: rename the file", name)}
		return Entry{Name: name, Path: path, Err: err}
	}
	t, err := Find(name, custom)
	return Entry{Name: name, About: t.About(), Path: path, Err: err}
}
