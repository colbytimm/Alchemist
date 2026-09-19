# Alchemist

A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API), inspired by
[harlequin](https://github.com/tconbeer/harlequin): browse databases and containers,
write SQL, and page through results without leaving the terminal.

<!-- TODO: record docs/demo.tape with `vhs docs/demo.tape` and embed docs/demo.gif here -->

It shows the request charge (RU) of every query, runs cross-partition queries by
default, and pages with continuation tokens rather than loading a whole result set.
Result sets export to JSON or CSV, and every query is kept in a searchable history.

> **Status: early development.** Browsing, querying, profiles, history, and export
> work today. Cross-container queries, catalog management, and release builds are
> still to come; the [implementation plan](docs/plan/00-overview.md) tracks them.

## Getting started

There are no release builds yet, so build from source with Go 1.26 or newer:

```sh
git clone https://github.com/colbytimm/Alchemist.git
cd Alchemist
make build
./bin/alchemist --adapter mock   # fixture data, no account needed
```

To run against a real account, start without the flag:

```sh
./bin/alchemist
```

The first run opens the connect screen: name the profile, enter the account endpoint
and key, choose whether to remember the key in the OS keychain, and press enter. The
profile is saved to `config.toml`; the key goes to the keychain or nowhere. Every run
after that connects straight into the catalog, and `alchemist prod` picks a profile by
name.

### Against the local emulator

`make emulator-up` starts the Cosmos DB emulator in Docker, serving HTTP on port 8081.
Add a profile for it and paste the emulator's
[well-known key](https://learn.microsoft.com/azure/cosmos-db/emulator#authentication)
when prompted:

```sh
make emulator-up
./bin/alchemist profile add emulator --endpoint http://localhost:8081
./bin/alchemist emulator
```

## Keys

`tab` moves between the catalog, the editor, and the results. Select a container in
the catalog and queries run against it, or name one in the query itself with
`FROM db.container`. `?` lists these bindings inside the app.

| Key | Where | Action |
|---|---|---|
| `tab` / `shift+tab` | anywhere | next pane / prev pane |
| `e` | anywhere | editor |
| `ctrl+r` | anywhere | run query |
| `ctrl+o` | anywhere | history |
| `?` | anywhere | help |
| `esc` | anywhere | close |
| `q` | anywhere but a text field | quit |
| `↑/k`, `↓/j` | catalog, results | up, down |
| `enter/space` | catalog | expand/collapse |
| `r` | catalog | refresh node |
| `enter` | results | row detail |
| `h/←`, `l/→` | results | scroll left, scroll right |
| `m` | results | fetch more |
| `ctrl+e` | results | export to file |

While the editor has the keyboard, plain letters are text; `ctrl+c` always quits.
Once the editor loses focus it shows the query with keywords, strings, and numbers
colored.

## Exporting results

`ctrl+e` in the results pane asks for a file name and writes the rows fetched so far.
The extension picks the format, and `tab` switches the name between the two:

- `.json` writes the original documents as an indented array, exactly as the account
  returned them.
- `.csv` writes the columns in the order the results pane shows them, with nested
  objects and arrays as compact JSON in their cell.

Export never fetches. If the status bar says `(+more)`, press `m` until it does not,
or export the part you have.

A bare name such as `results.json` lands in the directory Alchemist was started from.
A path works too, relative, absolute, or starting with `~/`, and folders it names that
do not exist yet are created. The prompt shows the full path it will write to as you
type. An existing file is left alone unless the name ends in `!`, as in
`~/exports/orders.csv!`.

## Profiles

Profiles are named connections kept in `config.toml` under `$XDG_CONFIG_HOME/alchemist`
(`~/.config/alchemist` by default). The file holds endpoints, never keys. The connect
screen writes it for you; the `profile` commands do the same from a shell, for scripts
and CI:

```sh
alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify
alchemist profile add prod --endpoint https://myaccount.documents.azure.com:443/ --default
alchemist profile list
alchemist profile set-key prod
alchemist profile remove emulator
```

`profile add` prompts for the account key and stores it in the OS keychain (Keychain
on macOS, Secret Service on Linux, Credential Manager on Windows). `profile set-key`
replaces a key; `profile remove` deletes the profile and its keychain entry together.

The file can also be written by hand:

```toml
default_profile = "emulator"

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # opened in the catalog on start
page_size = 100

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

### Keys on CI and headless machines

The key for profile `<name>` is looked up in this order:

1. The OS keychain (service `alchemist`, account `<name>`).
2. `ALCHEMIST_<NAME>_KEY`: the profile name upper-cased, with dashes as underscores
   (`my-emulator` → `ALCHEMIST_MY_EMULATOR_KEY`).
3. `COSMOS_CONNECTION_STRING`, a whole connection string, for ad-hoc use.
4. The connect screen, which asks for it and offers to store it in the keychain.

A machine with no keychain (a container, a CI runner, a server without a Secret
Service) falls through to the environment. `alchemist profile list` shows which source
each profile resolves to, and never the key itself, so its output is safe to share.

## Query history

Every query that reaches the account is appended to `history.jsonl` under
`$XDG_STATE_HOME/alchemist` (`~/.local/state/alchemist` by default), beside the log
file: the query as typed, the scope it ran in, and its statistics, never a key or an
endpoint. Failed queries are recorded too, with the error, since fixing one is the
usual reason to look back.

`ctrl+o` opens the history, newest first. `/` filters by query text or scope, `enter`
loads the selected query into the editor with its scope restored, and `ctrl+r` loads
it and runs it at once. `alchemist --history=false` records nothing for that session.

The file is one JSON object per line, so `jq . < history.jsonl` reads it. A line a
session never finished writing is skipped, and the file is trimmed to its newest
2,500 entries once it passes 5,000.

## Writing an adapter

The TUI only knows the interfaces in
[`internal/adapter`](internal/adapter/adapter.go): `Adapter` opens a `Connection`,
which serves a lazy `Catalog` tree and runs a `Query` into a `Cursor` of pages. The
in-memory adapter in [`internal/adapter/mock`](internal/adapter/mock/mock.go) is the
smallest complete example, and `internal/adapter/cosmos` is the real one. A new adapter
is registered in `cmd/root.go`; nothing under `internal/tui` may import it, and
`make lint` fails if something does.

## Development

```sh
make all               # fmt-check, lint, spell, test, build
make security          # gosec, govulncheck, gitleaks
make release-snapshot  # every release archive into dist/, nothing published
make help              # list all targets
```

Every tool runs at a version pinned in the `Makefile`, and CI calls the same targets,
so a green `make all` locally means a green quality job.

### CI and releases

| Workflow | Runs on | Does |
|---|---|---|
| `pr.yml` | pull requests to `main` | quality, security, and emulator integration gates; `goreleaser check` and `actionlint` |
| `main.yml` | pushes to `main` | the same gates, then snapshot archives uploaded as build artifacts |
| `release.yml` | tags matching `v*` | the same gates, then a GitHub Release with archives, checksums, and changelog |

`main` is expected to be a protected branch that requires the `pr.yml` checks
(`gates / quality`, `gates / security`, `gates / integration`, `release-check`) to pass
before a merge.

Cutting a release is one step:

```sh
git tag v0.1.0 && git push --tags
```

Design notes, architecture, and the iteration-by-iteration plan live in
[docs/plan](docs/plan/00-overview.md).
