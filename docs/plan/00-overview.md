# Alchemist — Plan Overview

Alchemist is a terminal IDE for Azure Cosmos DB (NoSQL API), written in Go, modeled on
[harlequin](https://github.com/tconbeer/harlequin) — a keyboard-driven TUI with a data
catalog, a SQL editor, and a results viewer. The core application is **adapter-agnostic**:
all data access flows through a small set of interfaces so additional database adapters
can be added without touching the TUI.

## Vision

- One binary, one screen: browse databases and containers, write Cosmos SQL, run it,
  page through results — without leaving the terminal.
- Cosmos-aware where it matters: request charge (RU) per query, **cross-partition
  queries by default** (native in azcosmos v1.x — no hand-rolled REST like the old
  CLI needed), continuation-token paging.
- Goes where the service can't: **cross-container queries** (UNION scans and simulated
  equi-joins) executed client-side, since Cosmos SQL cannot join across containers —
  see [10-cross-container.md](10-cross-container.md).
- Extensible by design: the Cosmos adapter is the *first* adapter, not a special case.

## Harlequin feature mapping

| Harlequin | Alchemist | Iteration |
|---|---|---|
| Data catalog tree | Catalog pane: databases → containers, lazy-loaded | 4 |
| Query editor | Editor pane: multiline Cosmos SQL | 5 |
| Results viewer | Results pane: pageable table with fetch-more | 5 |
| Query history | History overlay, JSONL-backed | 7 |
| Data exporter | JSON / CSV export | 8 |
| DDL via SQL (Cosmos has none) | Catalog management: create/delete, throughput | 11 |
| Adapter plugins | Go interfaces + registry | 2, 3 |
| Profiles / config | TOML profiles + OS keychain secrets | 6 |
| Help overlay | Keybinding help (`?`) | 4 |
| — | Info overlay: resource metadata (`i`) | 12 |

The alchemy identity lives in the **theme only** — the color palette and icon glyphs
defined in `internal/theme` (see [01-scaffold.md](01-scaffold.md)). Components, panes,
packages, and keybindings use plain, self-explanatory names.

## Architecture

```
main.go                       # thin entry
cmd/                          # cobra: root launches TUI; version; profile subcommands
internal/
  adapter/                    # CORE INTERFACES ONLY — no cosmos imports
    adapter.go registry.go
    mock/                     # in-memory adapter used by all TUI tests
    cosmos/                   # azcosmos v1.5.0 implementation
  query/                      # "db.container" scope tokenizer/rewriter
  tui/
    app.go                    # root model: layout, focus, routing only
    keys.go messages.go
    panes/                    # catalog.go, editor.go, results.go, history.go,
                              # statusbar.go, help.go
  theme/                      # adaptive alchemy palette + ASCII logo
  config/                     # TOML profiles + keyring/env secret resolution
  history/                    # JSONL query history
  export/                     # JSON/CSV export
  logging/                    # log to file while the TUI owns the terminal
docs/plan/                    # these documents
test/integration/             # emulator docker-compose + smoke tests
```

**Dependency rule:** `internal/tui` imports `internal/adapter` (and `internal/adapter/mock`
in tests) — never `internal/adapter/cosmos`. Enforced by a `depguard` rule in
`.golangci.yml`. Only `cmd/` wires a concrete adapter into the TUI (constructor
injection; no package-global clients anywhere).

### Core adapter interfaces (sketch)

```go
package adapter

// Adapter creates connections for one backend kind (e.g. "cosmos").
type Adapter interface {
    Name() string
    Connect(ctx context.Context, settings map[string]string) (Connection, error)
}

// Connection is an authenticated session against one account.
type Connection interface {
    Catalog() Catalog
    Query(ctx context.Context, q Query) (Cursor, error)
    Ping(ctx context.Context) error
    Close() error
}

type Query struct {
    Text     string   // adapter-native SQL, already scope-stripped
    Scope    []string // e.g. ["mydb", "orders"]
    PageSize int32
}

// Catalog is a lazy tree: databases → containers → metadata leaves.
type Catalog interface {
    Root(ctx context.Context) ([]Node, error)
    Children(ctx context.Context, n Node) ([]Node, error)
}

type Node struct {
    Kind        string            // "database" | "container" | "field"
    Name        string
    Path        []string
    Meta        map[string]string // e.g. "partitionKey": "/pk"
    HasChildren bool
}

// Cursor streams result pages; the TUI wraps NextPage in a tea.Cmd.
type Cursor interface {
    NextPage(ctx context.Context) (Page, error)
    HasMore() bool
    Close() error
}

type Page struct {
    Columns []string          // stable union of item keys, first-page order locked
    Rows    [][]string        // pre-rendered cells
    Raw     []json.RawMessage // original items, for export / detail view
    Stats   Stats
}

type Stats struct {
    RequestCharge float64 // RU; 0 where the backend has no such concept
    Elapsed       time.Duration
    RowCount      int
    More          bool
}
```

Everything is context-first. Adapters return errors — they never `log.Fatal` or `panic`.
The TUI converts errors into messages rendered in the Assay pane.

## Iterations

| # | Document | Delivers | Depends on |
|---|---|---|---|
| 1 | [01-scaffold.md](01-scaffold.md) | Module, Makefile, lint, theme, runnable shell | — |
| 2 | [02-adapter-core.md](02-adapter-core.md) | Adapter interfaces, registry, mock adapter, scope parser | 1 |
| 3 | [03-cosmos-adapter.md](03-cosmos-adapter.md) | azcosmos v1.5.0 adapter + emulator integration tests | 2 |
| 4 | [04-tui-shell.md](04-tui-shell.md) | Three-pane layout, focus, keymap, catalog tree | 2 |
| 5 | [05-editor-results.md](05-editor-results.md) | Editor, run-query flow, pageable results, status bar | 4 |
| 6 | [06-config-profiles.md](06-config-profiles.md) | TOML profiles, keychain secrets, profile CLI | 3, 5 |
| 7 | [07-history.md](07-history.md) | Query history store + history overlay | 5 |
| 8 | [08-export-polish.md](08-export-polish.md) | JSON/CSV export, syntax highlighting, README | 5 |
| 9 | [09-ci-release.md](09-ci-release.md) | PR/main workflows, goreleaser release pipeline | 1 (grows with 3) |
| 10 | [10-cross-container.md](10-cross-container.md) | Client-side cross-container queries (union scan, simulated join) | 5 |
| 11 | [11-catalog-management.md](11-catalog-management.md) | Create/delete databases and containers, throughput editing | 3, 4 |
| 12 | [12-info-view.md](12-info-view.md) | Info overlay: per-resource metadata for a database or container | 3, 4 |

Iterations 3 and 4 are parallelizable — both depend only on the interfaces from 2.

Query capability split, explicitly:

- **Cross-partition** (one container, all partitions): handled by the **Cosmos
  adapter** via the v1.5.0 SDK — iteration 3.
- **Cross-container** (several containers): impossible in Cosmos SQL and the SDK, so
  it is **simulated client-side** in the adapter-agnostic query engine — iteration 10.

## Definition of done (applies to every iteration)

- [x] `make all` (fmt-check, lint, test, build) passes with zero warnings.
- [x] New exported identifiers have single-sentence doc comments ending in a period (`godot`).
- [x] Unit tests live in a `test/` subfolder of the package under test, using
      `testify` `require`/`assert`; external dependencies are mocked behind the
      adapter interfaces — TUI tests never touch the network.
- [x] Integration tests (where applicable) are behind `//go:build integration` and run
      against the Cosmos DB emulator via `make test-integration`.
- [x] The manual verification checklist in the iteration document has been walked through.
- [x] No secrets in code, config, or fixtures (the emulator's well-known key is the only
      exception and is annotated as such for gitleaks).

## Testing strategy

- **Unit**: pure logic (scope parser, row flattening, exporters, config resolution) and
  `tea.Model` `Update` functions driven with the mock adapter and synthetic messages.
- **Integration**: the Cosmos adapter against the emulator
  (`https://localhost:8081`, well-known key). On Apple Silicon use the Linux
  `vnext-preview` emulator image; details in [03-cosmos-adapter.md](03-cosmos-adapter.md).
- **Manual**: each iteration ends with a short scripted walkthrough in a real terminal.
