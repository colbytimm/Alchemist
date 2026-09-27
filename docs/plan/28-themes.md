# Iteration 28: Themes

<!-- cspell:words Areeb Avinash Codemos Ferreira Polaire RRGGBB Rayrarpa Reddy Wallacy draculla forbidigo fstest iareebahmad mtech omitempty vscodethemes walcew -->

## Goal

Let the user pick a color theme. Alchemist's palette stays the default, under the name
`alchemist`, and is used unless another theme is selected. Five built-in themes are
adapted from VS Code themes, and users can add as many of their own as they like, one
file each. Every theme is a TOML file with the same schema, so a built-in one is a
starting point for a custom one.

Iteration 23 routed every editor color through named roles in `internal/theme` and
noted that "a later theme switch changes the roles in `theme` and nothing else". This
iteration finishes that job for the rest of the UI, then makes the palette behind the
roles selectable.

## What exists today

Checked against the code on this branch:

- `internal/theme/theme.go` holds seven `lipgloss.AdaptiveColor` package variables
  (gold, copper, verdigris, amethyst, parchment, cinnabar, ash), accessors named after
  them (`Gold()`, `Copper()` and so on), and ten styles built from them at package
  initialization (`TextStyle`, `HintStyle`, `ErrorStyle`, `SuccessStyle`,
  `SelectedStyle`, `SpinnerStyle`, `FocusedBorderStyle`, `BlurredBorderStyle`,
  `LogoStyle`, `TaglineStyle`).
- `internal/theme/syntax.go` holds ten `Syntax*` styles built from the same colors, and
  `DiagnosticError()`, which returns cinnabar.
- Five pane helpers build a style from a palette color instead of a role:
  `panes/editor.go` (`focusedPromptStyle`, a package variable, from `Gold`),
  `panes/review.go` (`warningStyle`, `Gold`), `panes/results.go` (`headerStyle`, `Gold`
  bold), `panes/help.go` (`helpStyles` key style, `Gold`) and `panes/statusbar.go`
  (`chargeStyle`, `Verdigris`).
- `LogoStyle`, `TaglineStyle`, `Logo` and `RawLogo` are used only by
  `internal/theme/test`. Nothing in `cmd` or `internal/tui` draws the logo.
- No pane paints a background color. Every color is a foreground.
- `internal/tui/panes/highlight.go` caches the escape sequences of every syntax role in
  a `palette`, and the drawn frame in a `drawnFrame`, keyed by the color profile, the
  terminal background and the underline setting.
- `internal/config` imports `internal/theme` (for `ParseDiagnosticUnderline`), so
  `theme` can never import `config`.
- `config.Store.Load` refuses unknown keys through `toml.MetaData.Undecoded`.
- No `go:embed` is used anywhere yet.

## Theme file format: TOML

Themes are TOML files. The reasons:

- `config.toml` is already TOML, read with `github.com/BurntSushi/toml` v1.6.0 (in
  `go.mod`). A theme lives next to it in the same directory and reads the same way.
- TOML allows comments. Built-in themes carry their credit and license notice as
  comments, and a user can annotate a custom theme. JSON cannot.
- The decoder reports unknown keys (`MetaData.Undecoded`) and syntax errors with a line
  number (`toml.ParseError`), which is what good error messages need.
- No new dependency. `encoding/json` would also add none, but JSON's only advantage is
  that VS Code themes are JSON, and a VS Code theme is never loaded directly: its
  hundreds of keys are mapped by hand to Alchemist's seventeen roles.

Built-in themes are the same TOML files, embedded with `//go:embed themes/*.toml` in
`internal/theme`. `alchemist theme show <name>` prints any theme's file, so copying a
built-in one is a single command.

## The schema

A theme names colors for roles. It never names lipgloss types, styles, or emphasis.

```toml
[about]
title = "Dracula at Midnight"
author = "Wallacy Santos Ferreira"
source = "https://github.com/walcew/dracula-at-midnight"
license = "MIT"
background = "#1f1f1f"

[colors]
text = "#F8F8F2"
keyword = "#FF79C6"
# ... every role below
```

- **The theme's name is its file name** without `.toml`, as a saved query's name is its
  `.sql` file name. There is no `name` key to disagree with the file.
- **`[about]` is optional** and informational. `title`, `author`, `source` and `license`
  are shown by `alchemist theme list`. `background` records the source theme's editor
  background. Alchemist does not paint it (below), so the docs tell the user to set
  their terminal to it for the intended look.
- **`[colors]` must define every role.** A role's value is either one color for light
  and dark terminals, `"#RRGGBB"`, or a pair, `{ light = "#RRGGBB", dark = "#RRGGBB" }`.
  Both become a `lipgloss.AdaptiveColor`; one color sets `Light` and `Dark` alike.
- **Colors are `#RRGGBB` only.** VS Code colors often carry an alpha channel
  (`#EB396950`), which a terminal cannot draw. Three-digit and named colors are refused
  too, so every theme file reads the same way.
- **Emphasis stays in code.** Keywords stay bold and operators plain, aliases bold,
  parameters and comments italic, selections bold. Bold is what tells a keyword from an
  operator when a theme gives both one color, which three of the five adapted themes
  do.
- **Unknown keys are errors**, in `[about]`, in `[colors]` and at the top level, as in
  `config.toml`.

### Roles

Seventeen roles. The first seven are the UI, the last ten are iteration 23's syntax
classes. Today's alchemy names survive as comments in `alchemist.toml`, the only place
they appear.

| Role | Used for today | Today's accessor | Alchemist color |
|---|---|---|---|
| `text` | body text, form labels, property names in the editor | `TextStyle` | parchment |
| `muted` | hints, blurred borders, the blurred editor prompt | `HintStyle`, `BlurredBorderStyle` | ash |
| `accent` | focused borders, the spinner, the focused editor prompt, help keys, result headers (bold) | `FocusedBorderStyle`, `SpinnerStyle`, `Gold()` in four panes | gold |
| `selected` | the selected row in every list (bold) | `SelectedStyle` | copper |
| `success` | success states, added diff lines, the request charge | `SuccessStyle`, `Verdigris()` in the status bar | verdigris |
| `warning` | the batch review's warning | `Gold()` in `review.go` | gold |
| `error` | errors, removed diff lines, the diagnostic squiggle | `ErrorStyle`, `DiagnosticError` | cinnabar |
| `keyword` | clause and batch keywords (bold) | `SyntaxKeyword` | amethyst |
| `operator` | operator words | `SyntaxOperator` | amethyst |
| `literal` | `true`, `null`, `undefined` | `SyntaxLiteral` | copper |
| `function` | built-in and user-defined functions | `SyntaxFunction` | gold |
| `alias` | aliases (bold) | `SyntaxAlias` | parchment |
| `parameter` | `@name` (italic) | `SyntaxParameter` | copper |
| `string` | strings | `SyntaxString` | verdigris |
| `number` | numbers | `SyntaxNumber` | copper |
| `comment` | comments (italic) | `SyntaxComment` | ash |
| `punctuation` | brackets, commas, operator symbols | `SyntaxPunctuation` | ash |

- `warning` is the one new role. Today it is gold because gold was at hand, not because
  a warning is an accent. A VS Code theme has a warning color of its own
  (`editorWarning.foreground`), and the review's warning should use it. `alchemist`
  sets it to gold, so the default looks exactly as it does today.
- The request charge moves from `Verdigris()` to `success`, which is the same color and
  was already documented as "success states and RU statistics".
- The diagnostic squiggle uses `error`. A separate role would always be set to the same
  color.
- `LogoStyle`, `TaglineStyle`, `Logo` and `RawLogo` are deleted with their tests. Nothing
  draws them, and a role for dead code would be a burden on every theme author.

### The default theme file

`internal/theme/themes/alchemist.toml`, the palette in `theme.go` today, unchanged:

```toml
# Alchemist's default theme. Its colors adapt to light and dark terminals.
# To start your own theme from it:
#   alchemist theme show alchemist > ~/.config/alchemist/themes/mine.toml

[about]
title = "Alchemist"
source = "https://github.com/colbytimm/Alchemist"
license = "MIT"

[colors]
text = { light = "#5C4B37", dark = "#E8DCC8" } # parchment
muted = { light = "#8A8378", dark = "#6B655B" } # ash
accent = { light = "#D4A017", dark = "#F5C542" } # gold
selected = { light = "#B87333", dark = "#D48F52" } # copper
success = { light = "#2E8B84", dark = "#5FD3CE" } # verdigris
warning = { light = "#D4A017", dark = "#F5C542" } # gold
error = { light = "#C0392B", dark = "#E74C3C" } # cinnabar
keyword = { light = "#6C3FA0", dark = "#9B6FD0" } # amethyst
operator = { light = "#6C3FA0", dark = "#9B6FD0" } # amethyst
literal = { light = "#B87333", dark = "#D48F52" } # copper
function = { light = "#D4A017", dark = "#F5C542" } # gold
alias = { light = "#5C4B37", dark = "#E8DCC8" } # parchment
parameter = { light = "#B87333", dark = "#D48F52" } # copper
string = { light = "#2E8B84", dark = "#5FD3CE" } # verdigris
number = { light = "#B87333", dark = "#D48F52" } # copper
comment = { light = "#8A8378", dark = "#6B655B" } # ash
punctuation = { light = "#8A8378", dark = "#6B655B" } # ash
```

The same file is the complete custom-theme example in the docs.

### Light and dark

- `alchemist` keeps its light and dark pairs and still follows the terminal through
  `lipgloss.AdaptiveColor`.
- The five adapted themes come from dark-only VS Code themes (`"type": "dark"` or
  `"uiTheme": "vs-dark"` in their manifests). Each role gets one color for both
  backgrounds. Inventing light variants would present colors as the source theme's that
  it never had.
- The docs say which themes are dark-only. Alchemist does not warn on a light terminal:
  lipgloss's background detection is a guess over SSH and tmux, as iteration 23 found
  for curly underlines.
- A custom theme may give any role a light and dark pair.

### No background

Alchemist paints no background today, and themes do not add one. Painting every cell
would put a background sequence in every styled run, change the highlighter's cached
sequences, and fight the user's terminal transparency and padding. The docs list each
theme's source background (`[about] background`) so the user can set their terminal
to it. A painted background is out of scope.

## Selecting a theme

| Source | Precedence | When the theme is unknown or invalid |
|---|---|---|
| `--theme <name>` | first | refuse to start, and say why |
| `theme = "<name>"` at the top of `config.toml` | second | start with `alchemist`, show a notice, log the reason |
| nothing set | last | `alchemist` |

- **One theme per session, set at the top of `config.toml`.** Not per profile: one
  session holds several accounts (iteration 14), and the panes are shared between
  them. A theme per profile would either change the whole screen on every account
  switch or apply only to the launch account, and neither is what a user would guess.
  An accent color per account (for example red for production) is a separate feature.
- **`--theme` fails loudly.** The user typed it for this run, so a typo stops the run,
  as an unknown profile argument does. The message lists what exists:

  ```
  alchemist: theme "draculla": unknown theme: built-in: alchemist, dracula-at-midnight,
  enchanted-grove-dark, jarvis-hud; custom: mine (in /home/me/.config/alchemist/themes)
  ```

- **A bad `theme` in `config.toml` never stops Alchemist from opening.** A theme is
  cosmetic, and `config.toml` may be shared between machines where a custom theme file
  is missing. The status bar shows the notice for its usual time, for example
  `theme mine not loaded: keyword: "#12345" is not #RRGGBB; using alchemist`, and the
  log file gets the full error. `alchemist theme list` shows it again.
- **`config.Load` does not judge the `theme` value.** It stays a plain string, so no
  theme problem can make `Load` fail, and `profile` subcommands never read theme files.
- **No in-app picker.** Some panes build their styles once, at construction (the help
  bubble's styles, the textarea's, the spinner's). Switching themes live would mean
  rebuilding every pane and invalidating every cache. `alchemist theme list` and
  `alchemist --theme <name> --adapter mock` cover trying themes out. The picker is a
  follow-up, and this iteration keeps it cheap (the highlighter change below).
- **No key is added.**

### Custom themes

- Location: `<config dir>/themes/<name>.toml`, that is
  `~/.config/alchemist/themes/` or `$XDG_CONFIG_HOME/alchemist/themes/`, beside
  `queries/`. `theme.DirName = "themes"`, as `saved.DirName` names `queries`.
- A file name must match `config`'s profile name rule, letters, digits, `-` and `_`,
  plus `.toml`. Other files in the directory are ignored and listed as ignored by
  `theme list`.
- A custom theme may not reuse a built-in name. `themes/arcane.toml` is refused with
  `arcane is a built-in theme: rename the file`. Otherwise a user could not tell which
  one they were looking at, and the built-in one could not be reached.
- The directory is read only at startup, and only the selected file is parsed (plus
  every file for `theme list`). A missing directory means no custom themes.

### The `alchemist theme` command

```
alchemist theme list
alchemist theme show <name>
```

| Command | Effect |
|---|---|
| `list` | every built-in and custom theme: name, title, author, `built-in` or its path, and for a custom theme that does not load, why; the selected one is marked |
| `show` | print the theme's file to standard output, comments included, to copy and edit |

`theme show` prints the embedded bytes for a built-in theme, so the credit and license
comments travel with every copy.

### Validation messages

Every error names the theme and file, then the problem. All wrap
`theme.ErrInvalidTheme` or `theme.ErrUnknownTheme` for `errors.Is`.

| Problem | Message after `theme mine: /home/me/.config/alchemist/themes/mine.toml:` |
|---|---|
| TOML syntax | the decoder's message, with its line: `toml: line 4: expected '=' ...` |
| unknown key | `unknown key "colors.keywords"` |
| missing roles | `missing roles: literal, alias` (every one, in role order) |
| not a color | `keyword: "#12345" is not #RRGGBB` |
| alpha channel | `keyword: "#EB396950" has an alpha channel a terminal cannot draw: use #RRGGBB` |
| half a pair | `keyword: a pair needs both light and dark` |
| built-in name | `arcane is a built-in theme: rename the file` |
| unreadable file | the `os` error |

Unknown name, from `--theme` or `config.toml`: `theme "x": unknown theme: built-in: ...;
custom: ...` as above.

## NO_COLOR and `--ascii`

- **`NO_COLOR`.** lipgloss v1.1.0's renderer takes its profile from termenv's
  `EnvColorProfile`, which returns `Ascii` when `NO_COLOR` is set. Every theme then
  renders without color, and the squiggle is a plain underline, as today. The theme is
  still resolved and still reported when it is bad, so the notice does not appear only
  once colors come back. The configuration docs change "turn off editor colors" to
  "turn off all colors, whatever the theme", which is what lipgloss already does.
- **`--ascii`** changes glyphs, not colors. Any theme works with it, and nothing here
  touches `IconSet`.

## Code design

### `internal/theme`

```go
type Role int

const (
    Text Role = iota
    Muted
    Accent
    Selected
    Success
    Warning
    Error
    Keyword
    Operator
    Literal
    Function
    Alias
    Parameter
    String
    Number
    Comment
    Punctuation
    roleCount
)

func Roles() []Role
func (r Role) String() string // the TOML key: "text", "keyword", ...

type Theme struct {
    name   string
    about  About
    colors [roleCount]lipgloss.AdaptiveColor
}

type About struct {
    Title, Author, Source, License, Background string
}

func (t Theme) Name() string
func (t Theme) About() About
func (t Theme) Color(r Role) lipgloss.AdaptiveColor

func Parse(name string, data []byte) (Theme, error)
func Default() Theme                                  // alchemist
func BuiltinNames() []string
func Find(name string, custom fs.FS) (Theme, error)   // built-in, then custom
func File(name string, custom fs.FS) ([]byte, error)  // for theme show
func List(custom fs.FS) []Entry                        // for theme list

type Entry struct {
    Name    string
    About   About
    BuiltIn bool
    Err     error // why a custom file does not load
}

func Use(t Theme)
func Active() *Theme // identity of the active theme, for caches

var (
    ErrUnknownTheme = errors.New("unknown theme")
    ErrInvalidTheme = errors.New("invalid theme")
)

const DirName = "themes"
```

- **Files.** `role.go` (`Role`, names), `parse.go` (`Parse`, the color value's
  `UnmarshalTOML`), `builtin.go` (the `go:embed`, `Default`, `Find`, `File`, `List`),
  `active.go` (`Use`, the style set) and `themes/*.toml`. `theme.go` keeps `IconSet` and
  loses the palette variables and the palette-named accessors.
- **`[roleCount]` array, not a map or a struct of fields.** A role that is added without
  a name fails to compile (`roleNames` is `[roleCount]string`), `Parse` checks
  completeness by walking `Roles()`, and there is no second list to keep in step.
- **`fs.FS` for custom themes.** `theme` cannot import `config`, and should not read
  the environment. `cmd` passes `os.DirFS(filepath.Join(config.Dir(), theme.DirName))`,
  and tests pass `fstest.MapFS`.
- **The active theme is package state, deliberately.** `active` is an
  `atomic.Pointer[styleSet]`, where `styleSet` holds every style built once from a
  `Theme`. Every existing accessor (`TextStyle()`, `SyntaxKeyword()` and the rest) reads
  it, so none of the roughly 250 call sites in `internal/tui` changes shape. The
  alternative, a `Theme` value passed through `tui.Options` into every pane
  constructor, rewrites all of them for no gain in behavior. This is the repo's second
  deliberate exception to "no mutable package-level state", after the adapter registry,
  and `.claude/skills/go-style/references/api-design.md` says so. The atomic pointer
  makes a read race-free and costs one load per accessor call.
- **`Use` is called once, in `cmd`, before `tui.New`.** Panes that build styles at
  construction then see the chosen theme. It is safe to call later, and tests do, but
  already-built panes keep their styles.
- **The default is parsed from `alchemist.toml`.** A nil `active` means `Default()`.
  `Default` parses the embedded file once (`sync.OnceValue`) and panics if it fails, as
  `regexp.MustCompile` does on a constant. The file is part of the binary, and every
  test in the package fails first if it is broken. There is one source for the default
  palette, not a Go copy and a TOML copy.
- **New role styles for the panes.** `AccentStyle()` (focused editor prompt, help
  keys), `HeadingStyle()` (accent, bold: result headers) and `WarningStyle()`. The
  palette-named accessors (`Gold()`, `Copper()`, `Verdigris()`, `Amethyst()`,
  `Parchment()`, `Cinnabar()`, `Ash()`) are removed, and `DiagnosticError()` returns
  the active theme's `error`.

### `internal/tui`

- `panes/editor.go`: `focusedPromptStyle` and `blurredPromptStyle` stop being package
  variables, which are evaluated before `Use` runs. They become calls to
  `theme.AccentStyle()` and `theme.HintStyle()`.
- `review.go`, `results.go`, `help.go` and `statusbar.go` use `WarningStyle`,
  `HeadingStyle`, `AccentStyle` and `SuccessStyle` in place of the palette accessors.
- `panes/highlight.go`: `palette` and `drawnFrame` also record the active theme's
  identity (`theme.Active()` returns a comparable pointer), so a theme change between
  frames draws fresh colors. It is one pointer comparison per frame. It keeps a later
  picker from inheriting a stale-cache bug, and it keeps tests that switch themes
  honest.
- `tui.Options` gains `Notice string`, shown in the status bar by `Init`, through the
  existing `notify`.
- A `forbidigo` rule in `.golangci.yml` forbids `lipgloss.Color`,
  `lipgloss.AdaptiveColor`, `lipgloss.CompleteColor`,
  `lipgloss.CompleteAdaptiveColor` and `lipgloss.ANSIColor` outside `internal/theme`.
  Every color then goes through a role, so every pane follows the theme.

### `internal/config` and `cmd`

- `config.Config` gains `Theme string \`toml:"theme,omitempty"\``. It round-trips
  through `Save`, so `profile add` never drops it.
- `sessionFlags` gains `theme string`, bound as `--theme`:
  `"color theme: a built-in or a file in the config directory's themes folder (default: theme in config.toml, else alchemist)"`.
- `launch` gains `theme string`, filled from `cfg.Theme` in `profileLaunch`, and in
  `adapterLaunch` from a best-effort `Load` (an unreadable config there leaves it
  empty, as that path already tolerates a missing store).
- `sessionFlags.selectTheme(launch) (theme.Theme, string, error)` returns the theme, a
  notice, and an error only for `--theme`. `run` calls it before opening the log file,
  so a bad `--theme` leaves nothing on disk, calls `theme.Use`, passes the notice in
  `tui.Options.Notice`, and logs the full error with `logger.Warn`.
- `cmd/theme.go`: `newThemeCmd()` with `list` and `show`, added in `NewRootCmd` beside
  `profile` and `snapshot`.

## Built-in themes

| Name | Adapted from | Author | License | Source background |
|---|---|---|---|---|
| `alchemist` | (the default) | Alchemist | MIT | none: adapts |
| `arcane` | Arcane Theme 1.3.2 | Papa Ours | custom, see below | `#000000` |
| `jarvis-hud` | JARVIS HUD | Mohammad Areeb Ahmad | MIT | `#04090e` |
| `dracula-at-midnight` | Dracula At Midnight 3.0.0 | Wallacy Santos Ferreira, after Dracula Theme | MIT | `#1f1f1f` |
| `enchanted-grove-dark` | Enchanted Grove Dark, M Tech Themes 0.14.7 | M Tech | MIT | `#2A3D2B` |
| `ray-dark` | Ray Dark | Rayrarpa | none found, see below | `#491414` |

### Where the palettes came from

vscodethemes.com, the VS Code Marketplace and open-vsx.org are blocked from the
development container. GitHub is not. All five palettes were read from real theme
files, as follows:

| Theme | File read | Commit |
|---|---|---|
| Arcane | `themes/arcane-fixed-color-theme.json` in `github.com/PapaOursPolaire/Arcane-Color-Theme` (the author's repository, version 1.3.2) | `af3635e` |
| JARVIS HUD | `registry/Areeb Ahmad (iareebahmad)/JARVIS HUD (jarvis-hud)/JARVIS HUD.json` in `github.com/AvinashReddy3108/CodemosModern-Themes-Registry`, a mirror of vscodethemes.com's files; no source repository was found | `adea5dc` |
| Dracula At Midnight | `src/themes/DraculaAtMidnight.ts` in `github.com/walcew/dracula-at-midnight` (the author's repository), checked against the built JSON in the mirror above | `55202ae` |
| Enchanted Grove Dark | `themes/Enchanted Grove Dark.json` in `github.com/ChrisMcKee1/mtech-pro-vscode-themes` (the extension's repository, publisher `M-Tech`), identical to the mirror's copy | `997d8a1` |
| Ray Dark | `registry/Rayrarpa/Ray Dark (RayTheme)/Ray Dark.json` in the mirror above; no source repository was found | `adea5dc` |

The Arcane file in the author's repository and the mirror's copy are identical.

### The mapping rule

One rule for every theme, so the mapping is reproducible. For each role, the first
key the theme defines wins. Token keys are TextMate scopes in `tokenColors`, matched as
VS Code matches them (a rule for `keyword` covers `keyword.operator.logical.sql` when
nothing more specific is set).

| Role | VS Code source, first defined |
|---|---|
| `text` | `colors.editor.foreground` |
| `muted` | per theme: the theme's own dim text color, named in the table below |
| `accent` | per theme: its focus or active-tab accent, named below |
| `selected` | per theme: its list or active line-number highlight, named below |
| `success` | `colors.gitDecoration.addedResourceForeground`, then token `markup.inserted.diff`, then `markup.inserted` |
| `warning` | `colors.editorWarning.foreground` |
| `error` | `colors.editorError.foreground` |
| `keyword` | token `keyword.other.sql`, then `keyword` |
| `operator` | token `keyword.operator.logical`, then `keyword.operator`, then `keyword` |
| `literal` | token `constant.language`, then `constant` |
| `function` | token `support.function.aggregate.sql`, then `entity.name.function`, then `meta.function-call.generic` |
| `alias` | a SQL table-name scope where the theme has one, then token `variable` |
| `parameter` | token `variable.parameter` (its color), then `variable` |
| `string` | token `string` |
| `number` | token `constant.numeric`, then `constant` |
| `comment` | token `comment` |
| `punctuation` | token `punctuation`, then `colors.editor.foreground` |

An alpha channel is never flattened: a key whose value has one is skipped for the next
key in the rule.

### The mapped palettes

Values are exactly as they appear in the source files.

| Role | `arcane` | `jarvis-hud` | `dracula-at-midnight` | `enchanted-grove-dark` | `ray-dark` |
|---|---|---|---|---|---|
| `text` | `#C8C0D8` | `#c5ccd6` | `#F8F8F2` | `#FFFFFF` | `#d4d4d4` |
| `muted` | `#A7A8AF` | `#7d8794` | `#9d9d9d` | `#a3c9bb` | `#999999` |
| `accent` | `#EB3969` | `#00d9ff` | `#FF79C6` | `#7A9A42` | `#f06464` |
| `selected` | `#0FF2F2` | `#00e5ff` | `#8BE9FD` | `#7FC76A` | `#ff5959` |
| `success` | `#3DEBAA` | `#5ce6a6` | `#50FA7B` | `#56B56A` | `#81b88b` |
| `warning` | `#6589F2` | `#ffb454` | `#8BE9FD` | `#ebcb8b` | `#cca700` |
| `error` | `#FF293B` | `#ff4d5e` | `#FF5555` | `#BF616A` | `#f48771` |
| `keyword` | `#EB3969` | `#00e5ff` | `#FF79C6` | `#E88787` | `#c678dd` |
| `operator` | `#416EF2` | `#9fa9b7` | `#FF79C6` | `#E88787` | `#56b6c2` |
| `literal` | `#0FF2F2` | `#ff6ec7` | `#FFB86C` | `#B8A3FF` | `#d19a66` |
| `function` | `#0FF2F2` | `#ffc94d` | `#8BE9FD` | `#A3C5F0` | `#61afef` |
| `alias` | `#3DEBAA` | `#dfe4ea` | `#F8F8F2` | `#FFFFFF` | `#e06c75` |
| `parameter` | `#416EF2` | `#8ef2c4` | `#FFB86C` | `#FFB366` | `#e06c75` |
| `string` | `#6589F2` | `#cfe88a` | `#F1FA8C` | `#7AB87A` | `#98c379` |
| `number` | `#0FF2F2` | `#ff8c42` | `#FFB86C` | `#FFB366` | `#d19a66` |
| `comment` | `#2AAB80` | `#59636e` | `#9d9d9d` | `#c4d0ba` | `#7f848e` |
| `punctuation` | `#E84B8B` | `#6b7480` | `#F8F8F2` | `#E8F5E8` | `#d4d4d4` |

The per-theme keys behind `muted`, `accent` and `selected`, and the rule's exceptions:

| Theme | `muted` | `accent` | `selected` | Exceptions |
|---|---|---|---|---|
| `arcane` | token `meta.separator` | `colors.tab.activeBorder` | `colors.editorLineNumber.activeForeground` | `error` is token `token.error-token`: the theme's `editorError.foreground` is `#416EF2`, the same blue as its keywords, and an error would not read as one. `alias` is token `entity.name.function.sql`, which the theme labels "SQL: table and column names". |
| `jarvis-hud` | `colors.descriptionForeground` | `colors.progressBar.background` | `colors.list.activeSelectionForeground` | none |
| `dracula-at-midnight` | `colors.editorLineNumber.foreground` (`misc.comment` in the source) | `colors.focusBorder` | `colors.list.highlightForeground` | none |
| `enchanted-grove-dark` | `colors.disabledForeground` | `colors.tab.activeBorder` | `colors.list.highlightForeground` | none |
| `ray-dark` | `colors.editorCodeLens.foreground` | `colors.editorLineNumber.activeForeground` | `colors.list.activeSelectionForeground` | `focusBorder` (`#4f0000`) and `progressBar.background` (`#600000`) are too dark to read as text on the theme's own background, so `accent` is the active line number |

Each built-in file records its source file, commit, and any exception as a comment, so
the next person can redo the mapping.

### Licenses and credit

| Theme | License found | What it requires | Status |
|---|---|---|---|
| Arcane | custom license in French, `LICENSE.md` | public redistribution, or inclusion in another distributed project, needs the author's permission, the license kept, visible credit as "Arcane Theme by Papa Ours", and the changes stated | **blocked until the author agrees** |
| JARVIS HUD | MIT, (c) 2026 Mohammad Areeb Ahmad, from the mirror's `LICENSE` | keep the copyright and permission notice | ok |
| Dracula At Midnight | MIT, (c) 2016 Dracula Theme, (c) 2025 Wallacy Santos Ferreira | keep the notice | ok |
| Enchanted Grove Dark | MIT, (c) 2017-2024 M Tech | keep the notice | ok |
| Ray Dark | no license file in the mirror, and no source repository found | nothing grants redistribution | **blocked until a license or permission exists** |

- Each MIT theme's `.toml` starts with its copyright line and the MIT permission notice
  as comments, so `theme show` copies carry it.
- A new `THIRD_PARTY_NOTICES.md` at the repo root holds every adapted theme's credit
  and full license text. `.goreleaser.yml` adds it to the archive `files`, beside
  `LICENSE`, since the theme files are embedded in the binary.
- `docs/using/themes.md` credits each author, links the source, and names the license.
- `arcane.toml` and `ray-dark.toml` are committed only once permission or a license is
  in hand (step 1). Until then the built-in list is `alchemist`, `jarvis-hud`,
  `dracula-at-midnight` and `enchanted-grove-dark`, and the docs match what ships.

## Typing budget

Themes do not change the work a keystroke does:

- Styles are still built once, now per `Use` rather than at package initialization.
- An accessor reads one atomic pointer instead of a package variable. That is
  nanoseconds against a budget measured in milliseconds.
- The highlighter's palette is still computed once and cached. It gains one pointer
  comparison per frame for the theme identity.
- No background painting, so no extra escape sequences per run.

`BENCH_FLAGS`, `testdata/bench-baseline.txt` and `cmd/benchmark-gate` do not change.
The benchmarks run under the default theme, and `make bench-gate` must pass against
the existing baseline. The baseline is not refreshed in this iteration. If the gate
fails, the change is wrong, not the baseline.

## Scope

- `internal/theme`: `role.go`, `parse.go`, `builtin.go`, `active.go`, and
  `themes/alchemist.toml`, `themes/jarvis-hud.toml`, `themes/dracula-at-midnight.toml`,
  `themes/enchanted-grove-dark.toml`, with `themes/arcane.toml` and
  `themes/ray-dark.toml` once licensed. `theme.go` and `syntax.go` read the active
  theme. The palette-named accessors and the logo code are removed.
- `internal/theme/test`: `role_test.go`, `parse_test.go`, `builtin_test.go`,
  `active_test.go`; `theme_test.go` loses the palette and logo tests.
- `internal/tui/panes`: `editor.go`, `review.go`, `results.go`, `help.go`,
  `statusbar.go`, `highlight.go`.
- `internal/tui`: `app.go` (`Options.Notice`, shown from `Init`).
- `internal/config`: `config.go` (`Theme`).
- `cmd`: `root.go` (`--theme`, `selectTheme`, `launch.theme`), new `theme.go`.
- `.golangci.yml`: the `forbidigo` rule.
- `.goreleaser.yml`: `THIRD_PARTY_NOTICES.md` in the archives.
- `THIRD_PARTY_NOTICES.md`: new.
- `cspell.config.yaml`: the authors' and themes' names.
- `.claude/skills/go-style/references/api-design.md`: names the active theme as the
  second deliberate exception to "no mutable package-level state".
- Docs, listed in the next section.
- `docs/plan/00-overview.md`: iteration row 28, and the architecture line for `theme/`
  becomes "roles, built-in themes, the active theme, icons".

## Documentation

The user docs change in the same change as the code. Every file below is updated to
describe what ships, no more.

| File | Change |
|---|---|
| `README.md` | Features list gains one line: "Color themes: the default, four built in, and your own." (the count matches what ships). No new screenshot: the workspace screenshot stays the default theme. |
| `docs/README.md` | "Working in the editor" gains `- [Themes](using/themes.md): built-in themes and your own`. |
| `docs/using/themes.md` | New page, below. |
| `docs/using/editor.md` | The highlighting table's "Color" column becomes "Role" (`keyword`, bold; `operator`; and so on), with a sentence linking to [themes](../using/themes.md) for the colors. "Colors adapt to light and dark terminals" moves to themes.md, since only some themes adapt. "Hints are drawn as a curly red underline" becomes "a curly underline in the theme's error color". |
| `docs/reference/cli.md` | The `alchemist` options table gains `--theme <name>`. A new `## alchemist theme` section documents `list` and `show`, in the style of `## alchemist profile`. |
| `docs/reference/configuration.md` | The top-level settings table gains `theme`. The example gains `theme = "dracula-at-midnight"`. The Files table gains `~/.config/alchemist/themes/*.toml`. The `NO_COLOR` row becomes "turn off all colors, whatever the theme". |
| `docs/reference/keys.md` | No change: no key is added, so `internal/tui/test/keys_test.go` has nothing new to check. |

`docs/using/themes.md` holds, in this order:

1. How to select a theme: `--theme`, then `theme` in `config.toml`, with the
   precedence table and what happens to an unknown name in each.
2. The built-in themes: one table (name, based on, author, license, dark-only or
   adaptive, source background), then a screenshot of each,
   `docs/images/theme-<name>.png`, and two for the default, `theme-alchemist-dark.png`
   and `theme-alchemist-light.png`.
3. That Alchemist paints no background, and to set the terminal's background to the
   listed color for the intended look.
4. Writing a custom theme: where the file goes, the name rule, `alchemist theme show
   alchemist > ~/.config/alchemist/themes/mine.toml` as the starting point, the
   complete `alchemist.toml` as the example, a table of every role and where it shows,
   the two value forms, and what `[about]` is for.
5. What happens when a custom theme is wrong: the message table, the notice, and
   `alchemist theme list`.
6. `NO_COLOR` and `--ascii`.
7. Credits: each adapted theme's author, a link to its source, its license, and a link
   to `THIRD_PARTY_NOTICES.md`.

### Docs style

The README and docs were just rewritten in one style. The implementer reads
`README.md`, `docs/using/editor.md` and `docs/reference/configuration.md` first and
matches them:

- Short plain sentences, one idea per sentence, active voice.
- Tables for reference facts: settings, flags, roles, messages, themes.
- Sentence-case headings.
- No em dashes, no bold lead-ins, no marketing words ("beautiful", "stunning",
  "powerful", "seamless").
- Screenshot alt text says what the image shows, as the existing ones do.

## Out of scope

- An in-app theme picker, and changing theme during a session.
- A theme per profile, and an accent color per account.
- Painting a background color.
- Light variants of the adapted themes.
- Emphasis (bold, italic) in theme files.
- Loading VS Code theme JSON directly, or a converter command.
- Themes for glyphs: `IconSet` stays `--ascii`'s concern.
- Colors outside the TUI: `profile` and `snapshot` subcommand output stays plain.

## Relationship to other iterations

- **1, scaffold.** The palette it introduced becomes the `alchemist` theme, unchanged.
  Its logo, never drawn since the shell became the workspace, is removed.
- **6, config and profiles.** Adds one top-level key. Profiles are untouched.
- **14, multiple accounts.** The reason a theme is per session, not per profile.
- **23, syntax highlighting.** Its roles become ten of the seventeen, its "one place to
  change the colors" becomes the theme file, and its typing budget is the guard this
  iteration must not move. Its highlighter cache gains the theme identity.
- **Later iterations that add a role** give it a default taken from an existing role
  when a theme file omits it, and say so in their plan, so existing custom themes keep
  loading. Only roles present at this iteration are required.

## Steps

1. **Licenses.** Open an issue on `PapaOursPolaire/Arcane-Color-Theme` asking
   permission to ship an adapted palette in an MIT project, with the credit its license
   asks for. Ask Rayrarpa the same, or for a license (no repository was found; the
   marketplace listing is the contact). Record each answer in `THIRD_PARTY_NOTICES.md`.
   This step does not block the others, but it gates the two files.
2. **Roles, no visible change.** Add `Role`, `Parse`, `Default` and `alchemist.toml`.
   Build the styles from the active theme, add `AccentStyle`, `HeadingStyle` and
   `WarningStyle`, move the five pane helpers onto roles, turn the editor's prompt
   variables into calls, delete the palette accessors and the logo, and add the
   `forbidigo` rule. A golden render of the workspace, the blurred and focused editor,
   the batch review and the help overlay under `TrueColor` must be byte-identical to
   the render before the change. `make bench-gate` passes.
3. **Selection.** `config.Theme`, `--theme`, `launch.theme`, `selectTheme`,
   `theme.Find` with custom themes from `fs.FS`, `Use`, `Options.Notice`, the
   highlighter's theme identity, and `alchemist theme list` and `show`.
4. **Built-in themes.** `jarvis-hud.toml`, `dracula-at-midnight.toml` and
   `enchanted-grove-dark.toml` from the tables above, each with its notice comment and
   provenance, `THIRD_PARTY_NOTICES.md`, and the `.goreleaser.yml` entry. Add
   `arcane.toml` and `ray-dark.toml` here only if step 1 has cleared them.
5. **Documentation and screenshots**, in the same change as the code. Take the
   screenshots against the emulator after `make emulator-up` and `make emulator-seed`,
   the way the existing `docs/images` ones were taken and at the same size
   (2088 x 1104): the `sales.orders` workspace with a highlighted query that shows every
   syntax class (a keyword, an operator word, a literal, a function, an alias, a
   parameter, a string, a number, a comment), its results, and the status bar's charge.
   One per shipped theme, with the terminal background set to the theme's source
   background, plus `alchemist` on a dark and on a light terminal. Save them as
   `docs/images/theme-<name>.png`. Update every file in the documentation table, add
   the new words to `cspell.config.yaml`, and add row 28 to `docs/plan/00-overview.md`.
6. **Verify.** `make all` (fmt-check, lint, spell, test, build) and `make bench-gate`,
   then the manual checklist.

## Testing

### Unit: `internal/theme/test`

- `TestEveryBuiltinThemeParses`: for every name in `BuiltinNames()`, `Find` returns no
  error. `Parse` refuses a missing role, so this is the guarantee that every built-in
  theme defines every role.
- `TestParseNamesEveryMissingRole`: for each role in `Roles()`, `alchemist.toml` with
  that line removed fails with `ErrInvalidTheme` and a message naming that role. The
  table is generated from `Roles()`, so a new role is covered without editing the test.
- `TestRoleNamesAreUniqueAndLowercase`.
- `TestBuiltinFilesAreAllListed`: every embedded `themes/*.toml` is in
  `BuiltinNames()` and the other way round.
- `TestAlchemistKeepsTodaysPalette`: every role of `Default()` equals the value in the
  roles table above (`#E8DCC8` dark text, and so on), and every pair differs between
  light and dark, replacing `TestPaletteColorsAreAdaptivePairs`.
- `TestAdaptedThemesMatchTheMappingTable`: every role of every adapted theme equals the
  value in this plan's mapping table, one table-driven case per theme and role.
- `Parse` rejections, one case per row of the message table: bad syntax (message
  carries the line), unknown key in each table, 5- and 8-digit colors, a named color, a
  pair missing `dark`, an empty file.
- `Parse` accepts a single color (Light equals Dark) and a pair.
- `Find`: a built-in name; a custom name from `fstest.MapFS`; an unknown name wraps
  `ErrUnknownTheme` and lists both kinds; a custom file named `arcane.toml` (or any
  built-in name) is refused; a name with a space is refused.
- `List`: built-ins first in name order, then custom ones; an invalid custom file is
  listed with its error; a non-`.toml` file is listed as ignored.
- `Use`: after `Use(t)`, `TextStyle`, `SyntaxKeyword` and `DiagnosticError` render
  `t`'s colors; `t.Cleanup` restores `Default()`. Under `termenv.Ascii`, every style
  renders plain text for every built-in theme.
- The existing `syntax_test.go` underline cases pass unchanged.

### Unit: `internal/config/test`

- `theme` loads, and survives `Save` then `Load`.
- `Put` of a profile keeps `Theme`.
- `Load` never fails on a `theme` value, however odd.

### Unit: `cmd/test`

- `--theme` beats `config.toml`, which beats the default.
- `--theme nope` returns an error wrapping `theme.ErrUnknownTheme`, lists the names,
  and creates no log directory (as `resolveLaunch`'s tests check today).
- `theme = "nope"` in the config starts with `alchemist` and a notice naming `nope`.
- A custom theme under a temporary `XDG_CONFIG_HOME` is found by `--theme` and by the
  config.
- `--adapter mock` with a config holding `theme` applies it.
- `alchemist theme list` output names every built-in and a custom theme, and shows a
  broken custom file's reason.
- `alchemist theme show dracula-at-midnight` output parses with `theme.Parse` to the
  built-in theme, and contains its copyright line.
- `theme show nope` fails with the unknown-theme message.

### Unit: `internal/tui/test` and `internal/tui/panes/test`

- With a test theme whose every role is a distinct color, `Use` it and render the
  workspace, the editor focused and blurred, the results header, the help overlay, the
  batch review and the status bar with a charge. Each role's color sequence appears
  where the roles table says it is used. This proves every pane follows the theme.
- The highlighter draws the new colors when the theme changes between two `View` calls
  on the same editor.
- `Options.Notice` shows in the status bar on start.
- `TestThemesDocListsEveryBuiltin` (beside `keys_test.go`, in the same style): every
  built-in name appears in `docs/using/themes.md`, and its screenshot
  `docs/images/theme-<name>.png` exists.

### Benchmarks

`make bench-gate` against the unchanged `testdata/bench-baseline.txt`.

### Integration

None new: no adapter code changes. `test/integration/tui_test.go` keeps passing under
the default theme.

### Manual checklist

- [ ] `alchemist --adapter mock` looks exactly as before on a dark terminal and on a
      light one.
- [ ] `alchemist --adapter mock --theme dracula-at-midnight`: every pane, the editor
      focused and blurred, the results header, the status bar charge, the help overlay
      and a batch review use the theme's colors.
- [ ] Each shipped built-in theme opens with `--theme`, and a query shows every syntax
      class.
- [ ] `theme = "enchanted-grove-dark"` in `config.toml` applies with no flag, and
      `--theme alchemist` overrides it.
- [ ] `alchemist theme show alchemist > ~/.config/alchemist/themes/mine.toml`, change
      `keyword`, run `alchemist --theme mine`: keywords change.
- [ ] Break `mine.toml` (a 5-digit color), set `theme = "mine"`: Alchemist opens in
      `alchemist` colors with the notice, and `alchemist theme list` shows the reason.
- [ ] `alchemist --theme mine` with the broken file: refuses to start with the same
      reason.
- [ ] `alchemist --theme draculla`: refuses, and lists the names.
- [ ] `NO_COLOR=1 alchemist --theme jarvis-hud`: no colors anywhere, plain underline
      under a diagnostic.
- [ ] `--ascii --theme ray-dark` (or any shipped theme): ASCII glyphs, theme colors.
- [ ] 80x24: nothing about themes changes the layout.
- [ ] `make bench-gate` passes.
- [ ] Every screenshot in `docs/using/themes.md` shows the shipped colors.

## Acceptance criteria

- With no theme selected, every screen renders byte-for-byte as it did before this
  iteration.
- `--theme` and `theme` in `config.toml` select a built-in or custom theme, with the
  flag first. No per-profile setting and no key are added.
- Every built-in theme defines every role, enforced by tests that derive the role list
  from the code.
- Every color in `internal/tui` comes from a role, enforced by `forbidigo`.
- A bad custom theme or unknown name in `config.toml` opens Alchemist in `alchemist`
  colors and says why in the status bar, the log and `alchemist theme list`. A bad
  `--theme` refuses to start and says why.
- Under `NO_COLOR` nothing is colored, whatever the theme. `--ascii` changes glyphs
  only.
- No adapted theme ships without a license or permission that allows it, and every
  shipped one is credited in its file, in `THIRD_PARTY_NOTICES.md` and in
  `docs/using/themes.md`.
- `make bench-gate` passes against the unchanged baseline.
- The docs describe the shipped behavior: the built-in list, flags, setting, file
  location and messages match the code. Every link in the changed docs resolves,
  every referenced image exists, and `make spell` passes.
- `make all` passes.

## Open questions

- **Arcane's permission.** Its license requires the author's permission to include
  the palette in a distributed project. If Papa Ours does not answer or declines,
  `arcane` does not ship.
- **Ray Dark's license.** No license file and no source repository were found. Its
  token colors match One Dark Pro's well-known palette (`#c678dd`, `#98c379`,
  `#d19a66`, `#61afef`, `#e06c75`, `#7f848e`), which suggests it was built from that
  theme. Until Rayrarpa grants permission or publishes a license, `ray-dark` does not
  ship.
- **Two palettes come only from a mirror.** JARVIS HUD and Ray Dark were read from
  `AvinashReddy3108/CodemosModern-Themes-Registry`, which mirrors vscodethemes.com. When
  the marketplace is reachable, the implementer should check the values against the
  published `.vsix` files. The JARVIS HUD MIT license is also taken from that mirror.
- **Names.** "Arcane" and "JARVIS" are also names of entertainment properties. The
  built-in names credit the VS Code themes they come from. Whether to keep them or
  rename (for example `arcane` to `papa-ours-arcane`) is the maintainer's call before
  release.
