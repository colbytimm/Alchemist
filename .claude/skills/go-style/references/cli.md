# CLI and Cobra

Source: https://cobra.dev/docs/. Repo uses `spf13/cobra` v1.10.2.

## The shape already established here — copy it

`main.go` does one job and owns the only exit:

```go
func main() {
    if err := cmd.NewRootCmd().Execute(); err != nil {
        fmt.Fprintln(os.Stderr, "alchemist:", err)
        os.Exit(1)
    }
}
```

`cmd.NewRootCmd()` is a **constructor, not a package global**. Cobra's docs suggest
registering subcommands in `init()`; **this repo does not** — `init()` cannot return an
error, runs in an order you do not control, and makes the command graph impossible to
build twice in a test. Wire subcommands inside `NewRootCmd`:

```go
func NewRootCmd() *cobra.Command {
    root := &cobra.Command{ ... }
    root.AddCommand(newQueryCmd(), newProfileCmd())
    return root
}
```

Each subcommand gets its own `newXCmd() *cobra.Command` constructor in its own file
under `cmd/`. Unexported unless something outside `cmd` needs it.

## Command fields

```go
&cobra.Command{
    Use:           "query [sql]",        // name + argument signature
    Short:         "Run a single query and print results",  // one line, no period
    Long:          "...",                // full paragraphs, shown by --help
    Example:       "  alchemist query \"SELECT * FROM c\" --db mydb",
    Args:          cobra.MaximumNArgs(1),
    SilenceUsage:  true,
    SilenceErrors: true,
    RunE:          func(cmd *cobra.Command, args []string) error { ... },
}
```

- **`RunE`, never `Run`.** `Run` has nowhere to put an error. The only exception is a
  command that genuinely cannot fail.
- **`SilenceUsage: true`** — a runtime failure should print the error, not 40 lines of
  usage. Usage still prints for flag and argument errors, which is correct.
- **`SilenceErrors: true`** — `main` owns the error output. Without it Cobra prints the
  error *and* `main` prints it again.
- `Short` is a sentence fragment, lowercase after the first word, no trailing period.
- `Use`'s first word is the command name; the rest documents positional args
  (`[optional]`, `<required>`).
- `Hidden` for internal commands, `Deprecated` for ones on the way out — keep a
  deprecated command working for at least one minor release.
- `GroupID` plus `AddGroup` on the root only once there are ~8+ subcommands.
- At most one or two obvious `Aliases`. Ambiguous aliases are worse than none.

## Arguments

Always set `Args`. The default accepts anything and defers the failure to your code.

```go
Args: cobra.NoArgs                                   // flags only
Args: cobra.ExactArgs(1)
Args: cobra.MinimumNArgs(1)
Args: cobra.MaximumNArgs(1)
Args: cobra.MatchAll(cobra.ExactArgs(1), validScope) // compose
```

A custom validator is a `func(cmd *cobra.Command, args []string) error` — return a
lowercase, unpunctuated error like everything else in the repo.

Prefer flags over positional arguments once there is more than one value. Positional
order is invisible in a shell history; `--db mydb --container orders` is not.

## Flags

- `Flags()` is local to the command. `PersistentFlags()` reaches every descendant —
  use it only for genuinely global concerns (`--profile`, `--verbose`), and declare
  those on the root.
- Bind to a variable (`StringVarP`) when several functions need the value; read on
  demand (`cmd.Flags().GetString("db")`) when only `RunE` does. Read-on-demand keeps
  scope tighter and is the default choice.
- Shorthands are one letter and must not collide between sibling commands. `-h` is
  reserved for help.
- Name booleans positively: `--verbose`, not `--no-verbose`. `--verbose=false` already
  works.
- `MarkFlagRequired` for required flags. It returns an `error` (as do `MarkHidden` and
  `MarkDeprecated`), and `errcheck` is on — it only fails when the flag name does not
  exist, so treat a non-nil return as a programming error rather than dropping it into
  `_`. Cobra's built-in group constraints return nothing and are preferred over
  hand-rolled checks in `PreRunE`:

```go
cmd.MarkFlagsRequiredTogether("db", "container")
cmd.MarkFlagsMutuallyExclusive("json", "table")
cmd.MarkFlagsOneRequired("endpoint", "profile")
```

- `MarkHidden` to retire a flag quietly; `MarkDeprecated(name, hint)` when users need
  to be told what replaced it.
- A flag with a non-trivial type implements `pflag.Value` and registers with `Var`.
- Config precedence, when profiles land (see `docs/plan/06-config-profiles.md`):
  **flag > environment > config file > default.** Never invert it.

## Context and cancellation

`Execute()` gives commands a background context. Use `ExecuteContext(ctx)` when the
process needs signal handling, then read `cmd.Context()` inside `RunE` and thread it
into every adapter call:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
if err := cmd.NewRootCmd().ExecuteContext(ctx); err != nil { ... }
```

Never call `context.Background()` inside a `RunE` — that discards the cancellation the
user just asked for with Ctrl-C.

## Output streams

Write through the command, never to `os.Stdout` directly:

```go
fmt.Fprintln(cmd.OutOrStdout(), result)
fmt.Fprintln(cmd.ErrOrStderr(), warning)
```

That is what makes the command testable, and it is why the root command hands
`cmd.InOrStdin()` / `cmd.OutOrStdout()` to `tea.NewProgram`. Errors go up as return
values from `RunE`, not to stderr inside the command.

## Testing commands

Build a fresh command per test — this is the payoff for not using `init()`:

```go
root := cmd.NewRootCmd()
var out bytes.Buffer
root.SetOut(&out)
root.SetArgs([]string{"--version"})

require.NoError(t, root.Execute())
assert.Contains(t, out.String(), app.Version)
```

`SetArgs` on a shared command leaks between tests; a constructor makes that impossible.
`cmd/test/root_test.go` is the working example. Note that `registerAdapters` is guarded
by `sync.Once` precisely so repeated `NewRootCmd()` calls stay idempotent — preserve
that if you add more global registration.

## Version and help

Already wired on the root; do not duplicate:

```go
Version: fmt.Sprintf("%s (built %s)", app.Version, app.BuildDate),
cmd.SetVersionTemplate(fmt.Sprintf("%s {{.Version}}\n", app.Name))
```

`CompletionOptions.DisableDefaultCmd = true` is set deliberately — the default
`completion` command is noise in a single-command TUI binary. Re-enable it (and add
`ValidArgsFunction` for dynamic completion) only when there are subcommands worth
completing.

## Building

```
make build     # go build -ldflags "-X .../app.Version=... -X .../app.BuildDate=..." -o bin/alchemist .
make all       # fmt-check + lint + test + build
```

Version metadata is injected at link time into `app.Version` and `app.BuildDate` —
`VERSION` defaults to `git describe --tags --always --dirty`. Never hardcode a version
string in source; the `app` package's defaults (`"dev"`, `"unknown"`) are the
unset markers.

Build artifacts go to `bin/`, which is gitignored. The binary is built from the module
root (`.`), not from `cmd/` — `main.go` lives at the top level.

## Adding a subcommand — checklist

1. New file `cmd/<name>.go`, `func new<Name>Cmd() *cobra.Command`.
2. `Use`, `Short`, `Long`, `Example`, `Args`, `RunE`. Inherit silence flags from root.
3. Flags on `Flags()` unless genuinely global.
4. Thread `cmd.Context()` into every adapter call.
5. Write through `cmd.OutOrStdout()`; return errors, do not print them.
6. `root.AddCommand(new<Name>Cmd())` in `NewRootCmd` — not `init()`.
7. Test in `cmd/test/` via `SetArgs` + `SetOut`, covering the happy path and one
   failure.
8. Construct concrete adapters here, never in `internal/tui`.
