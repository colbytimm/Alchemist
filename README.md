# Alchemist

A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API), inspired by
[harlequin](https://github.com/tconbeer/harlequin): browse databases and containers,
write SQL, and page through results without leaving the terminal.

<!-- TODO: record docs/demo.tape with `vhs docs/demo.tape` and embed docs/demo.gif here -->

It shows the request charge (RU) of every query, runs cross-partition queries by
default, and pages with continuation tokens rather than loading a whole result set.
Result sets export to JSON or CSV, and every query is kept in a searchable history.
It also queries across containers, which the service cannot: unions and two-container
joins are [simulated client-side](#querying-across-containers).

> **Status: early development.** Browsing, querying, cross-container queries,
> catalog management, autocomplete, profiles, history, and export work today.
> Release builds are still to come; the
> [implementation plan](docs/plan/00-overview.md) tracks them.

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

The first run opens the connect form: name the profile, enter the account endpoint
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
| `ctrl+g` | anywhere | accounts |
| `?` | anywhere | help |
| `esc` | anywhere | close |
| `q` | anywhere but a text field | quit |
| `↑/k`, `↓/j` | catalog, results | up, down |
| `enter/space` | catalog | expand/collapse |
| `r` | catalog | refresh node |
| `n` | catalog | new database |
| `c` | catalog | new container |
| `d` | catalog | delete node |
| `t` | catalog | throughput |
| `i` | catalog | node info |
| `enter` | results | row detail |
| `h/←`, `l/→` | results | scroll left, scroll right |
| `m` | results | fetch more |
| `ctrl+e` | results | export to file |
| `ctrl+space` | editor | complete |
| `tab` | editor, list open | accept suggestion |
| `↑/↓`, `esc` | editor, list open | choose, dismiss |

While the editor has the keyboard, plain letters are text; `ctrl+c` always quits.
Once the editor loses focus it shows the query with keywords, strings, numbers, and
`--` comments colored.

## Autocomplete

Type in the editor and a list docks to the bottom of the pane with what can come
next: clause keywords where a clause may open, Cosmos system functions with their
signatures in an expression, databases after `FROM`, `JOIN`, or a list comma,
containers after `db.`, and fields after `alias.`, nested paths included. Aliases
resolve the way the planner reads them, so in a cross-container query each side
completes its own container's fields, a union alias completes the fields of every
listed container, and `JOIN t IN c.tags` completes `t.` from the array's elements.
Nothing is offered that the planner would refuse: no outer join before `JOIN`, and no
`ORDER BY`, `GROUP BY`, or `OFFSET` once a cross-container join is in the query.

The list opens by itself after an identifier character or a dot, narrows as you type,
and closes on whitespace. `tab` accepts the selected suggestion, `↑`/`↓` choose,
`esc` dismisses it until the next word, and `enter` is always a newline. With the list
closed `tab` moves panes as usual. `ctrl+space` opens the list on demand, even on an
empty prefix; some terminals swallow it, and nothing depends on it. Keywords take the
case you are typing in (`sel` → `select`); field names are inserted exactly as
observed, in bracket form when they are not plain identifiers (`c["order-id"]`).

Cosmos has no schema, so fields are what the session has seen: the partition key from
the catalog, the fields of every page a query returned, and a sample. The first time a
container's fields are completed, Alchemist runs `SELECT TOP 20 * FROM c` against it,
once per container per session, and files what it finds. That spends a few request
units you did not ask for, so the hint line says `sampling orders…` while it runs, the
log records the charge, and it can be turned off with `sample_fields = false` on the
[profile](#profiles) or `--sample-fields=false` for one session. A failed sample is
logged and not retried; completion carries on with what it has.

## Querying across containers

Cosmos DB SQL reads exactly one container per query. Alchemist accepts two shapes
that name more, runs them as one query per container, and merges the pages itself.
The status bar marks such a result `simulated (client-side)`, and its RU figure is the
sum of the underlying queries, broken down per container.

**Union.** List containers after `FROM`. The same query runs against each, and a
leading `_container` column says where every row came from. One alias covers the
whole list:

```sql
SELECT * FROM sales.orders, sales.archive AS c WHERE c.status = "open"
```

**Join.** An inner join of two containers on one equality:

```sql
SELECT o.id, o.total, cu.name
FROM sales.orders AS o
JOIN sales.customers AS cu ON o.customerId = cu.id
WHERE cu.region = "west"
```

Columns come back prefixed with their alias (`o.total`, `cu.name`) unless the select
list renames them (`cu.name AS customer`). The select list is `*` or top-level
`alias.field` items; the `ON` fields may be nested
(`o.customer.id`). A `WHERE` condition is sent to the container it reads, so it must
read one side only. A side with no alias is known by its container name.

Cosmos DB's own `JOIN alias IN c.array` is untouched: it has no `ON` and runs on the
service as it always did.

One side of a join is held in memory: the side a `WHERE` condition filters, or the
joined container when that does not settle it. It may hold `max_join_rows` rows
(10 000 unless the [profile](#profiles) says otherwise). Past that the run stops with
an error rather than a partial answer; filter that side, or raise the cap.

Anything else across containers is refused before it runs, never approximated: outer
joins, more than two containers in a join, `ON` with anything but one `=`,
`ORDER BY`/`GROUP BY`/`OFFSET` on a join, and subqueries over another container.

`make emulator-seed` loads the emulator with `sales`, `telemetry`, and `hr` databases
to try these against. It replaces databases of those names and only runs against
localhost.

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

## Managing the catalog

Cosmos DB has no DDL, so `CREATE` and `DROP` never reach the editor however good it
gets. Four keys in the catalog pane do that work instead:

- `n` creates a database, with optional shared throughput its containers draw on.
- `c` creates a container in the database under the cursor, taking a partition key of
  up to three comma-separated paths (`/tenantId, /customerId` is hierarchical).
- `d` deletes the database or container under the cursor.
- `t` reads the provisioned throughput of the database or container under the cursor
  and replaces it, manual or autoscale. A container that draws on its database's
  throughput is changed on the database, and the dialog says so rather than sending a
  request Cosmos would refuse.

Nothing is destroyed by a single keystroke: a delete asks for the name back, character
for character, the way the portal does. Cosmos has no undo and no recycle bin.

Minimum RU/s, autoscale step size, and which modes an account may use vary by account
type, so Alchemist sends the request and shows the service's own answer rather than
guessing the rules. A refused create leaves the dialog open with that answer under the
fields, so a rejected name is one edit away from a retry.

Renaming is absent because Cosmos cannot rename a database or a container — the ID is
the resource identity. A backend that cannot manage its catalog, or a session that must
not, simply does not offer these keys, and they disappear from the help overlay too.

## Inspecting a node

`i` on a database or container opens a read-only overlay with everything the account
serves about it: identity and last-modified time, partition key kind and paths,
throughput mode with its floor and whether a change is still scaling, document count
and storage size, physical partition ranges, time to live, the indexing policy, and
unique key, conflict resolution, vector and full-text policies. A figure the account
declines to serve — a container drawing on its database's throughput has no offer of
its own — keeps its heading with a line saying why. The overlay scrolls, `r` reads the
node again, and `esc` or `i` closes it. A node already read reopens without a request.

## Profiles

Profiles are named connections kept in `config.toml` under `$XDG_CONFIG_HOME/alchemist`
(`~/.config/alchemist` by default). The file holds endpoints, never keys. The connect
form writes it for you; the `profile` commands do the same from a shell, for scripts
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
max_join_rows = 5000             # cross-container joins; 10000 when unset
sample_fields = false            # autocomplete never queries a container for its fields

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

### Accounts

A session is on one account at a time, the one named on the command line or the
default profile: its databases fill the catalog and its name leads the status bar.
`ctrl+g` opens the account switcher, which lists every profile, from anywhere,
the editor included:

| Key | Action |
|---|---|
| `enter` | switch to the selected account, connecting it first if it is not connected |
| `a` | add account: the connect form, empty |
| `x` | disconnect the selected account |
| `/` | filter by name or endpoint |
| `esc` | clear the filter, or close |

An account connects the first time it is switched to, and a switch that cannot connect
leaves the session where it was, with the reason under the account's row. One whose
key is nowhere to be found opens the connect form instead. An account you leave stays
connected, with its tree and selected container kept, so switching back is instant.
`x` closes its connection and forgets its tree; the profile and its key stay where
they are.

The editor is shared by every account, so the same query can be run on two in turn.
The results pane's title names the account its rows came from, which is not always
the one the session is on now. A query always runs on the account the session is on:
`FROM staging.sales.orders` naming another account is refused with a message saying so.

### Keys on CI and headless machines

The key for profile `<name>` is looked up in this order:

1. The OS keychain (service `alchemist`, account `<name>`).
2. `ALCHEMIST_<NAME>_KEY`: the profile name upper-cased, with dashes as underscores
   (`my-emulator` → `ALCHEMIST_MY_EMULATOR_KEY`).
3. `COSMOS_CONNECTION_STRING`, a whole connection string, for ad-hoc use. It serves
   only a profile whose endpoint is the account the string names, so with several
   accounts in one session a string for one can never connect another.
4. The connect form, which asks for it and offers to store it in the keychain.

A machine with no keychain (a container, a CI runner, a server without a Secret
Service) falls through to the environment. `alchemist profile list` shows which source
each profile resolves to, and never the key itself, so its output is safe to share.

## Query history

Every query that reaches the account is appended to `history.jsonl` under
`$XDG_STATE_HOME/alchemist` (`~/.local/state/alchemist` by default), beside the log
file: the query as typed, the scope it ran in, and its statistics, never a key or an
endpoint. Failed queries are recorded too, with the error, since fixing one is the
usual reason to look back.

`ctrl+o` opens the history of the account the session is on, newest first; switch
accounts to see another's. `/` filters by query text or scope, `enter`
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
