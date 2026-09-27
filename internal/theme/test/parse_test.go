package theme_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

// alchemistFile is the default theme's file, the starting point of every
// custom theme.
func alchemistFile(t *testing.T) string {
	t.Helper()
	data, err := theme.File(theme.DefaultName, theme.Custom{})
	require.NoError(t, err)
	return string(data)
}

// withoutRole drops the line that colors role.
func withoutRole(file string, role theme.Role) string {
	var kept []string
	for _, line := range strings.Split(file, "\n") {
		if !strings.HasPrefix(line, role.String()+" =") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// withColor sets role to value, written as TOML.
func withColor(file string, role theme.Role, value string) string {
	return withoutRole(file, role) + "\n" + role.String() + " = " + value + "\n"
}

// problem is what the picker shows of err: the part after the theme's
// name and file.
func problem(t *testing.T, err error) string {
	t.Helper()
	var loadErr *theme.LoadError
	require.ErrorAs(t, err, &loadErr)
	return loadErr.Err.Error()
}

func TestParseNamesEveryMissingRole(t *testing.T) {
	for _, role := range theme.Roles() {
		t.Run(role.String(), func(t *testing.T) {
			_, err := theme.Parse("mine", []byte(withoutRole(alchemistFile(t), role)))

			require.ErrorIs(t, err, theme.ErrInvalidTheme)
			require.Equal(t, "missing roles: "+role.String(), problem(t, err))
		})
	}
}

func TestParseListsMissingRolesInRoleOrder(t *testing.T) {
	file := withoutRole(withoutRole(alchemistFile(t), theme.Alias), theme.Literal)

	_, err := theme.Parse("mine", []byte(file))

	require.Equal(t, "missing roles: literal, alias", problem(t, err))
}

func TestParseRefuses(t *testing.T) {
	tests := []struct {
		name string
		file func(base string) string
		want string
	}{
		{
			name: "bad syntax, with its line",
			file: func(string) string { return "[colors]\ntext \"#FFFFFF\"\n" },
			want: "toml: line 2",
		},
		{
			name: "an unknown key in colors",
			file: func(base string) string { return base + "keywords = \"#FFFFFF\"\n" },
			want: `unknown key "colors.keywords"`,
		},
		{
			name: "an unknown key in about",
			file: func(base string) string {
				return strings.Replace(base, "[about]\n", "[about]\nversion = \"1\"\n", 1)
			},
			want: `unknown key "about.version"`,
		},
		{
			name: "an unknown key at the top",
			file: func(base string) string { return "name = \"mine\"\n" + base },
			want: `unknown key "name"`,
		},
		{
			name: "a five-digit color",
			file: func(base string) string { return withColor(base, theme.Keyword, `"#12345"`) },
			want: `keyword: "#12345" is not #RRGGBB`,
		},
		{
			name: "an eight-digit color",
			file: func(base string) string { return withColor(base, theme.Keyword, `"#EB396950"`) },
			want: `keyword: "#EB396950" has an alpha channel a terminal cannot draw: use #RRGGBB`,
		},
		{
			name: "a named color",
			file: func(base string) string { return withColor(base, theme.Keyword, `"purple"`) },
			want: `keyword: "purple" is not #RRGGBB`,
		},
		{
			name: "a three-digit color",
			file: func(base string) string { return withColor(base, theme.Keyword, `"#FFF"`) },
			want: `keyword: "#FFF" is not #RRGGBB`,
		},
		{
			name: "a pair missing dark",
			file: func(base string) string { return withColor(base, theme.Keyword, `{ light = "#6C3FA0" }`) },
			want: "keyword: a pair needs both light and dark",
		},
		{
			name: "a background that is not a color",
			file: func(base string) string {
				return strings.Replace(base, "[about]\n", "[about]\nbackground = \"black\"\n", 1)
			},
			want: `background: "black" is not #RRGGBB`,
		},
		{
			name: "an empty file",
			file: func(string) string { return "" },
			want: "missing roles: " + strings.Join(roleNames(), ", "),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := theme.Parse("mine", []byte(tt.file(alchemistFile(t))))

			require.ErrorIs(t, err, theme.ErrInvalidTheme)
			assert.Contains(t, problem(t, err), tt.want)
			assert.True(t, strings.HasPrefix(err.Error(), "theme mine: "), err.Error())
		})
	}
}

func roleNames() []string {
	var names []string
	for _, role := range theme.Roles() {
		names = append(names, role.String())
	}
	return names
}

func TestParseTakesOneColorForBothBackgrounds(t *testing.T) {
	got, err := theme.Parse("mine", []byte(withColor(alchemistFile(t), theme.Keyword, `"#FF79C6"`)))

	require.NoError(t, err)
	assert.Equal(t, "#FF79C6", got.Color(theme.Keyword).Light)
	assert.Equal(t, "#FF79C6", got.Color(theme.Keyword).Dark)
}

func TestParseTakesAPair(t *testing.T) {
	got, err := theme.Parse("mine", []byte(withColor(alchemistFile(t), theme.Keyword, `{ light = "#111111", dark = "#EEEEEE" }`)))

	require.NoError(t, err)
	assert.Equal(t, "#111111", got.Color(theme.Keyword).Light)
	assert.Equal(t, "#EEEEEE", got.Color(theme.Keyword).Dark)
}

func TestParseReadsAbout(t *testing.T) {
	file := strings.Replace(alchemistFile(t), "[about]\n", "[about]\nauthor = \"Ada\"\nbackground = \"#1f1f1f\"\n", 1)

	got, err := theme.Parse("mine", []byte(file))

	require.NoError(t, err)
	assert.Equal(t, "mine", got.Name())
	assert.Equal(t, theme.About{
		Title: "Alchemist", Author: "Ada", Source: "https://github.com/colbytimm/Alchemist",
		License: "MIT", Background: "#1f1f1f",
	}, got.About())
}

// distinctTheme gives every role a color no other role, and no theme built
// with another seed, uses.
func distinctTheme(t *testing.T, name string, seed int) theme.Theme {
	t.Helper()
	var file strings.Builder
	file.WriteString("[colors]\n")
	for _, role := range theme.Roles() {
		fmt.Fprintf(&file, "%s = \"#%02X%02X%02X\"\n", role, seed, int(role)+1, 0x40+seed)
	}
	got, err := theme.Parse(name, []byte(file.String()))
	require.NoError(t, err)
	return got
}
