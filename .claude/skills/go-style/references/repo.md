# Repo conventions and tooling

Module `github.com/colbytimm/alchemist`, Go 1.26.8. A terminal IDE for Azure Cosmos DB.

## Layout

```
main.go                  package main — one job: run cmd, print to stderr, exit 1
app/                     build metadata injected via -ldflags
cmd/                     cobra wiring; concrete adapters constructed and injected here
internal/adapter/        the backend-agnostic contract (Adapter, Connection, Catalog, Cursor)
internal/adapter/mock/   in-memory adapter used by tests and demos
internal/adapter/cosmos/ azcosmos implementation, split by concept
internal/emulator/       drives the emulator container through the docker or podman CLI
internal/sample/         the sample databases alchemist emulator seed creates
internal/query/          query planner and client-side cross-container engine
internal/theme/          lipgloss styles, logo, colour profile
internal/tui/            bubbletea models; interfaces only, no concrete adapter
internal/<pkg>/test/     external test packages (package <pkg>_test)
test/integration/        end-to-end tests against the emulator (make emulator-up)
docs/                    the user manual; README.md links every page
```

**The architectural invariant, enforced by `depguard`:** `internal/tui` may import
`internal/adapter` but never `internal/adapter/cosmos`. Concrete adapters are wired in
`cmd/`. Adding a backend import to the TUI fails lint, and it is failing for a reason.

Adapter registration is explicit in `cmd.registerAdapters`, guarded by a `sync.Once`
so repeated `NewRootCmd()` calls in tests stay idempotent. Do not switch to
blank-import side-effect registration.

Naming follows the memory-noted rule: panes and packages get plain functional names.
The alchemy theme lives in icons and the colour profile only — no `crucible.go`,
no `Cauldron` type.

## Lint config (`.golangci.yml`)

Enabled: `bodyclose`, `depguard`, `errcheck`, `errname`, `errorlint`, `gocognit`,
`gocritic`, `godot`, `gosec`, `govet`, `ineffassign`, `misspell`, `nilnil`, `revive`,
`staticcheck`, `unconvert`, `unparam`, `unused`, `whitespace`.

Things worth knowing:

- `gocognit` fails at cognitive complexity **30**.
- `godot` requires every comment to end with a period.
- `nilnil` bans `return nil, nil`.
- `revive`'s `var-naming` and `unused-parameter` are **disabled**, and `staticcheck`'s
  ST1003 naming check is off by default. Initialism mistakes (`Url`, `appId`) reach
  main unchallenged. Get them right by hand.
- `_test.go` files are exempt from `errcheck`, `gosec`, and `unparam`.
- Formatters: `gofmt` plus `goimports` with `-local github.com/colbytimm/alchemist`,
  which is what produces the three import groups (stdlib / third-party / local).
- `max-issues-per-linter: 0` and `max-same-issues: 0` — nothing is truncated, so a
  clean run means genuinely clean.

## Commands

```
make all                 fmt-check + lint + test + build
make fmt                 gofmt -s -w . && goimports -local
make lint                golangci-lint v2.4.0 (pinned, via go run)
make test                go test ./...
make build               -> bin/alchemist with version ldflags
make emulator-up         alchemist emulator start: run the emulator and wait for it
make test-integration    go test -tags integration ./internal/adapter/cosmos/test/... ./test/integration/...
make emulator-down       alchemist emulator remove --data
make coverage-html       coverage over ./app ./cmd ./internal
make security            gosec + govulncheck + gitleaks
make help                lists targets
```

All tool versions are pinned via `go run tool@version`. Never introduce `@latest`.

## Established patterns to copy

- Error text: `package: operation: detail` — `fmt.Errorf("cosmos: create client: %w", err)`.
- Compile-time interface checks in a `// Compile-time contract checks.` var block.
- Bare receivers when unused: `func (Adapter) Name() string { return Name }`.
- Exported `const Name = "cosmos"` as an adapter's registry key.
- Sentinel errors at package level: `var ErrUnsupported = errors.New("query: ...")`.
- Package doc comments that state the invariant, not the file list.
- `sync.RWMutex` declared directly above the map it guards.

## Uber guide rules this repo does NOT adopt

Do not "fix" code to match these:

- **`_` prefix on unexported globals** (`_defaultPort`). This repo writes `registryMu`,
  `registry`, `registerOnce`.
- **`go.uber.org/atomic`.** Use `sync/atomic`'s typed forms from the stdlib.
- **Avoiding assertion libraries** (this one is Google's, not Uber's): the repo uses
  `stretchr/testify` deliberately. See `testing.md`.
- **Functional options everywhere.** Options structs are fine when the set is small
  and stable, as `cosmos.Settings` is.

Everything else in the Uber guide applies where the higher-precedence guides are silent.

## Upstream sources

- https://google.github.io/styleguide/go/ (guide, decisions, best-practices)
- https://go.dev/doc/effective_go
- https://go.dev/wiki/CodeReviewComments
- https://github.com/uber-go/guide/blob/master/style.md
- https://cobra.dev/docs/ (CLI framework; distilled in `cli.md`)

Fetch one only if this skill genuinely does not cover the question — they are large and
mostly already distilled here.
