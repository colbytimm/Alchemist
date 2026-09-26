# Alchemist

A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API), inspired by
[harlequin](https://github.com/tconbeer/harlequin): browse databases and containers,
write SQL, and page through results without leaving the terminal.

<!-- TODO: record docs/demo.tape with `vhs docs/demo.tape` and embed docs/demo.gif here -->

It shows the request charge (RU) of every query, runs cross-partition queries by
default, and pages with continuation tokens rather than loading a whole result set.
Result sets export to JSON or CSV, every query is kept in a searchable history, and
the ones worth keeping can be saved under a name.
It also queries across containers, which the service cannot: unions and two-container
joins are [simulated client-side](#querying-across-containers). Writes go through
[transactional batches](#transactions) and [updates by query](#updating-by-query),
reviewed and confirmed before anything is sent.
[Snapshots](#snapshots) of a container, kept on disk, show what changed since.

> **Status: early development.** Browsing, querying, cross-container queries,
> catalog management, transactional batches, updates by query, cloning, snapshots,
> autocomplete, profiles,
> history, saved queries, and export work today.
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
| `ctrl+s` | anywhere, history | save query |
| `ctrl+l` | anywhere | open saved |
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
| `y` | catalog | clone; with a clone under way, show it |
| `s` | catalog, snapshots | take snapshot |
| `v` | catalog | snapshots; with a capture under way, show it |
| `w` | catalog, an update under way | show update/delete job |
| `enter` | results | row detail |
| `h/←`, `l/→` | results | scroll left, scroll right |
| `m` | results | fetch more |
| `ctrl+e` | results | export to file |
| `ctrl+b` | results, row detail | add to batch |
| `ctrl+space` | editor | complete |
| `tab` | editor, list open | accept suggestion |
| `↑/↓`, `esc` | editor, list open | choose, dismiss |
| `enter` | batch review, name typed | commit |
| `↑/↓` | batch review | scroll |
| `enter` | update review, confirmation typed | start |
| `↑/↓` | update review | scroll |
| `esc` | update progress, running | hide |
| `x` | update progress, running | stop |
| `r` | update progress, ended short | resume |
| `esc`, `enter` | update progress, ended | report |
| `esc` | clone progress | hide; close once the clone has ended |
| `x` | clone progress, running | stop |
| `r` | clone progress, ended short | resume |
| `d` | clone progress, ended short | delete the partial target |
| `r` | saved queries | rename |
| `d`, then `y` | saved queries | delete |
| `space` | snapshots | mark |
| `enter` | snapshots | diff |
| `d`, then `enter` | snapshots | delete snapshot |
| `x` | snapshots, capturing | cancel capture |
| `enter` | snapshots, note prompt | take |
| `enter` | diff | fields |
| `tab` | diff | all/added/removed/modified |

While the editor has the keyboard, plain letters are text; `ctrl+c` always quits.

## Highlighting and diagnostics

The editor colors the query as you type it, focused or not:

| What | Examples | Drawn |
|---|---|---|
| Clause keyword | `SELECT`, `FROM`, `WHERE`, `ORDER BY`, `JOIN`, `VALUE` | amethyst, bold |
| Operator word | `AND`, `OR`, `NOT`, `IN`, `LIKE`, `BETWEEN`, `EXISTS` | amethyst |
| Literal | `true`, `null`, `undefined` | copper |
| Function | `STARTSWITH(`, `COUNT(`, `udf.discount(` | gold |
| Alias | the `c` of `FROM c` and of `c.total` | parchment, bold |
| Parameter | `@minTotal` | copper, italic |
| String, number | `'west'`, `1.5e3` | verdigris, copper |
| Comment | `-- note` | ash, italic |
| Punctuation | `( ) , . = < + ??` | ash |

Properties stay plain text, since they are most of any query. A batch is colored with
its own words (`BEGIN BATCH`, `PARTITION`, `UPSERT`, `IF MATCH`, `COMMIT`), and an update
with a query's plus `UPDATE`, `SET` and `UNSET`. Every
color is one of the theme's, and adapts to a light or a dark terminal as the panes do.

What Alchemist can tell is wrong from the text alone gets a red squiggle, and the hint
line at the bottom of the editor says why while the cursor is on it:

| Flagged | Example | Hint |
|---|---|---|
| An unterminated string | `WHERE c.region = "west` | `unterminated string: close it with "` |
| A character Cosmos SQL does not use | `c.total # 5` | `"#" is not part of Cosmos SQL` |
| An unknown function | `CONTAIN(c.name, "A")` | `unknown function CONTAIN: did you mean CONTAINS?` |
| An alias the query never declares | `SELECT o.id FROM c` | `o is not declared: the query reads c` |
| A misspelled clause | `SELECT * FORM c` | `FORM is not a clause: did you mean FROM?` |
| A statement that does not start with `SELECT`, `UPDATE` or `BEGIN BATCH` | `SELEC * FROM c` | `a statement starts with SELECT, UPDATE or BEGIN BATCH` |
| An unbalanced bracket | `WHERE (c.a = 1` | `( is never closed` |
| A batch that does not parse | `BEGIN BATCH sales.orders PARTITION` | the batch parser's message |
| An update that does not parse | `UPDATE sales.orders o SET o.status = "shipped"` | the update parser's message |

"Did you mean" only offers a word within two edits of what you typed. Nothing that
could be right is flagged: not an unknown field (Cosmos has no schema), not an unknown
database or container (the catalog may not be listed yet), and not a query shape the
planner refuses, which `ctrl+r` explains better than a squiggle could. The word you
are typing, and a string you are typing in, are not judged until the cursor leaves
them. Strings and stray characters are flagged as you type; everything else once you
pause. The squiggles are a typing aid, not a validator: a query without one can still
fail on the service, which stays the judge, and its error shows as before.

The squiggle is a curly underline (`SGR 4:3`) in the theme's red, which kitty,
WezTerm, iTerm2, Ghostty, foot, GNOME Terminal and Windows Terminal draw. A terminal
that does not know it draws a plain underline, or none, in the token's own color; the
hint line always says what is wrong in words. Terminals cannot be asked reliably over
SSH or through tmux, so the form is a setting: `diagnostics = "underline"` on the
[profile](#profiles), or `--diagnostics underline` for one session, draws a plain
underline, and `off` flags nothing and stops looking. Under `NO_COLOR` the editor has
no color and every squiggle is a plain underline. tmux passes the curly form through
when told the outer terminal draws it:

```tmux
set -as terminal-overrides ',*:Smulx=\E[4::%p1%dm'
set -as terminal-overrides ',*:Setulc=\E[58::2::%p1%{65536}%/%d::%p1%{256}%/%{255}%&%d::%p1%{255}%&%d%;m'
```

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

**Join.** An inner join of any number of containers, each `ON` one equality between
a field of the container `JOIN` just introduced and a field of any earlier one:

```sql
SELECT o.id, cu.name, p.name AS product
FROM sales.orders o
JOIN sales.customers cu ON o.customerId = cu.id
JOIN sales.products p ON o.sku = p.id
WHERE cu.region = "west"
```

Each `ON` may reach back to the first container, as here, or to the one before it
(`… JOIN telemetry.devices d ON e.deviceId = d.id`), or any mix of the two. The same
container may appear twice under distinct aliases.

Columns come back prefixed with their alias (`o.total`, `cu.name`) unless the select
list renames them (`cu.name AS customer`). The select list is `*` or top-level
`alias.field` items; the `ON` fields may be nested
(`o.customer.id`). A `WHERE` condition is sent to the container it reads, so it must
read one side only. A side with no alias is known by its container name.

Cosmos DB's own `JOIN alias IN c.array` is untouched: it has no `ON` and runs on the
service as it always did.

One side of a join streams and every other side is held in memory. The streamed side
is the first container in written order that no `WHERE` condition filters, or the
`FROM` container when every side is filtered: the side expected to be largest. The
held sides are read one after another and may hold `max_join_rows` rows between them
(10 000 unless the [profile](#profiles) says otherwise). Past that the run stops with
an error naming the side that crossed the line rather than a partial answer; filter
that side, or raise the cap. A merged page holds at most 1 000 rows; a join that fans
out further serves the rest on the next `m` without reading anything new.

When the status bar is too narrow for the per-container breakdown it folds it to a
count, `(3 containers)`, and the full breakdown goes to the log.

Anything else across containers is refused before it runs, never approximated: outer
joins, `ON` with anything but one `=` between the new container and an earlier one,
a `WHERE` condition over more than one side, `JOIN t IN` beside a container join, a
container list mixed with a join, `ORDER BY`/`GROUP BY`/`OFFSET` on a join, and
subqueries over another container.

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

## Transactions

Cosmos DB commits a group of item operations on one container and one logical
partition key as a whole or not at all: a transactional batch. Write one in the editor
and run it with `ctrl+r`:

```sql
BEGIN BATCH sales.orders PARTITION "c01";
  CREATE  {"id": "o900", "customerId": "c01", "status": "open", "total": 45};
  REPLACE "o004" {"id": "o004", "customerId": "c01", "status": "shipped"}
          IF MATCH "\"0800-7f3a\"";
  PATCH   "o007" [{"op": "set", "path": "/status", "value": "cancelled"}];
  DELETE  "o003";
  READ    "o011";
COMMIT
```

- `BEGIN BATCH` always names `<database>.<container>`; the catalog's selection is never
  a batch's target. `PARTITION` takes one value per key path, in order: a hierarchical
  key names every one (`PARTITION "tenant-a", "eu", 42`).
- The operations are `CREATE body`, `UPSERT body`, `REPLACE "id" body`, `DELETE "id"`,
  `READ "id"` and `PATCH "id" [entries] [WHERE "condition"]`. `UPSERT`, `REPLACE`,
  `DELETE` and `PATCH` take `IF MATCH "etag"`; `CREATE` and `READ` refuse it, since the
  service would ignore it. A patch is the service's own array of `{"op", "path",
  "value"}` entries; `move` and a fractional `incr` cannot be sent by the Go SDK.
- Without its `COMMIT` a batch does not parse, so a half-typed one never runs. A buffer
  holds one batch or one query, never two batches: the service cannot commit two
  atomically, and Alchemist will not pretend to.
- The service takes at most 100 operations and 2 MB per batch. Every check that can be
  made before sending is made, and every problem is listed at once: the operation
  count, the size, a body without an `id` or in another partition, a replace whose
  body names another item, a patch of the `id` or the key, two creates of one `id`.

A batch that writes opens a review first, every time, recalled from history or not: the
account, the container, the key, each operation, and a warning for a write with no
`IF MATCH` or two operations on one item. It commits only once the container's name
is typed back exactly and `enter` pressed; `esc` sends nothing and records nothing. A
batch that only reads runs straight away. `ctrl+b` on a row of a `SELECT *` query of
one container, or in its detail, adds a `REPLACE` of that document, conditional on its current ETag, to the batch in the
editor, or starts one when the editor holds a query.

The outcome is a result set, one row per operation, which the detail view and export
treat like any other. It is one of four, and the status bar says which:

- **committed**: every operation applied.
- **rolled back**: an operation failed and nothing was written; the failed one is
  marked, and every other reads `424 Failed Dependency`.
- **not applied**: the request never left, or the service refused it whole (a throttled
  batch included). Nothing was written, and `ctrl+r` tries again.
- **outcome unknown**: the batch was sent and no answer came back, from a timeout, a
  dropped connection or a 5xx. It committed in full or not at all. Alchemist never
  retries a batch, and shows a query over the batch's partition to check before
  running it again.

A batch waits at most 30 seconds for its answer. Until it arrives `ctrl+r`, `ctrl+g`
and `q` wait too; `ctrl+c` still quits. There is no undo.

### Read-only accounts

Writing to anything but a local emulator is a decision made per profile. A profile
with no `read_only` setting is read-only unless its endpoint is `localhost`,
`127.0.0.1` or `::1`; a read-only account refuses every batch that writes and every
update, before an update reads anything, drafts nothing with `ctrl+b`, offers none of the catalog's `n`, `c`, `d` and `t`, and is
never a clone's target, though it is always a valid source. The
status bar and the account switcher say `read-only` beside its name. To allow writes:

```sh
alchemist profile set-read-only prod false
alchemist profile add staging --endpoint https://staging.documents.azure.com:443/ --read-only=false
```

`alchemist --read-only` makes every account of one session read-only, whatever its
profile says; the flag only ever tightens. `--adapter mock` is writable.

## Updating by query

Cosmos DB has no `UPDATE … WHERE`: its SQL only reads. Alchemist does what a careful
script would, and says so: it selects the matching items with a query the service
evaluates, then writes to each one with a patch.

```sql
UPDATE sales.orders o
SET o.status = "archived", o.archivedAt = "2026-01-01"
WHERE o.status = "shipped" AND o.total < 50
```

- The target is always `<database>.<container>`, and the alias (`c` when none is
  written) starts every path: `o.status`, `o.shipTo.region`, `o["order-id"]`,
  `o.lines[0].qty`.
- `SET path = value` sets the field, creating it where it is missing. A value is a
  literal: a string in either quote, a number, `true`, `false`, `null`, or JSON.
  `UNSET path` removes a field; an item that lacks it is left out of that operation.
- `WHERE` is required. `ctrl+r` pressed one clause early must not select a whole
  container, so the every-item form is written out: `WHERE true`. The condition is the
  service's own dialect and is never parsed; it is sent as written, twice.
- Refused, with the reason: an expression or another field on the right of `=` (a
  patch cannot read the item), `+=` and increment (not safe to run twice), `SET o =`,
  array appends and moves, `id`, a partition key path or a system field, two paths
  that overlap, more than 10 operations, a second container anywhere (`FROM`, `JOIN`,
  `WITH`, or a `database.container` inside the `WHERE`), `TOP`, `ORDER BY`, `OFFSET`,
  `LIMIT`, `RETURNING`, and anything after the statement's `;`. Every problem is listed
  at once, and nothing is read.

`ctrl+r` never writes. It is a dry run: it checks the statement, reads which items
match (their identities only, or whole items when an `UNSET` must see which have the
path), and opens a review. The review names the account, the container and its key,
the condition, the exact number of items and when they were read, what the selection
cost, a before → after of a few of them, a rough write cost, and every warning: a
`WHERE` that does not pin the partition key, `WHERE true`, items with no partition key
value, items that already lack an `UNSET` path, and whether a snapshot of the container
exists. It starts only once the container's name is typed back exactly, and for
`WHERE true` the name and the item count (`orders 60`); `esc` writes nothing and
records nothing. A statement recalled from history or a saved query is reviewed again,
every time. More matches than `max_mutation_items` (10,000 unless the profile says
otherwise) is a refusal with nothing written, never a truncated run.

What it means, in plain words:

- **Not atomic.** Each item is its own write. A run that stops, fails or is quit has
  updated some items and not others. There is no rollback and no undo; the report
  lists exactly which.
- **Not isolated.** The targets are the items that matched when the dry run read them.
  Every write carries the `WHERE` as its condition, so an item changed since so that it
  no longer matches is `skipped: changed` and left alone; one deleted since is
  `skipped: gone`. A change to another field survives: a patch touches only its paths.
- **A write is sent once.** A throttled write was not applied and is retried after the
  wait the service asked for, with one writer fewer. A write with no answer is
  `unknown`, never retried, and three in a row stop the job. So do more than 100
  failed items, and a refusal of the very first write, which usually means the service
  does not take the `WHERE` as a patch condition.
- **Running it again is safe.** `SET` to a literal and `UNSET` give the same document
  however often they run. Items already updated often stop matching and are not even
  selected; the rest are written to the same values. That is also how an `unknown`
  item is settled, and how a run stopped in an earlier session is finished.
- Before a large run, take a copy: a [snapshot](#snapshots) of the container, and its
  diff afterwards, or a [clone](#cloning).

The job runs in the background, up to four writes at a time or `writers` on the
profile, and holds the session's one job slot, which a clone and a snapshot use too.
`esc` hides the progress view, the status bar carries `update prod/sales.orders 41% (w)`
on every account, and `w` in the catalog brings it back. `x` stops it after the writes
in flight; `r` resumes with the items not yet attempted. While it runs, `x` in the
switcher refuses its account, `d` refuses its container, a batch into its container
waits, and so do a clone, a snapshot and another update. The first `q` shows it; the
second stops it and quits. Closing the view once the job has ended loads the report
into the results: one row per item with its outcome (`updated`, `skipped: changed`,
`skipped: gone`, `skipped: no partition key`, `failed`, `unknown`, `not attempted`),
the service's status and its charge, and a status bar that splits the charge between
the selection and the writes. Past 10,000 items it shows every item that was not
updated first; the totals always cover every item.

## Cloning

`y` on a container or a database in the catalog copies it under a new name: the
definition alone, or the definition and every item. The copy can land in the account
the session is on or in any other the session knows, connected or not; the prompt
connects a target on its own and leaves the session where it was. `prod` → `emulator`
works with an untouched config, because a real account's profile is read-only until
told otherwise, a clone only reads its source, and the emulator's endpoint is local.

Cosmos DB has no clone a data-plane key can call, so a clone is a client-side copy:
read the definition, create the target, read every item, write every item. That makes
three things true, and the prompt, the review and the progress view say them:

- It spends request units on both sides. Reading spends them on the source, writing on
  the target, and a new provisioned container bills from the moment it exists. The
  cost cannot be known up front; the progress view projects it from the first page on.
  No `COUNT` query is ever spent on an estimate: the item count and size come with the
  definition, labelled "about".
- It is not a snapshot. A source that changes while it is read is copied as it was
  seen, page by page.
- It can take a long time. The session stays usable meanwhile, switching accounts
  included: `esc` hides the progress view, the status bar carries
  `clone prod/sales.orders → emulator 41% (y)` on every account, and `y` in the
  catalog brings the view back from any row.

The prompt offers every account that is not read-only, and says for each read-only one
why it is missing. **Copy** is `definition and items` or `definition only`.
**Definition** is `full`, or `portable`, which leaves out the analytical store TTL, the
conflict resolution policy and the vector and full-text policies and indexes: whatever
depends on a feature the target account may lack. `portable` is the default across
accounts. **Throughput** is `minimum` (400 RU/s manual), `same as source`, or `none`;
the default is `minimum`, so a source at 40,000 RU/s autoscale is never copied at that
price by pressing `enter` twice. A container drawing on its database's throughput keeps
doing so wherever the target database can have some.

`enter` starts nothing: it opens a review that restates the job, what it creates and at
what throughput, and asks for the **target account's** name typed back. Nothing that
exists is ever written into: a target that exists is refused in the form, and again by
the service at create.

| Copied | Not copied |
|---|---|
| partition key paths, kind and version | stored procedures, triggers, user-defined functions |
| indexing policy, unique keys, default TTL | computed properties, change feed and client encryption policies |
| (`full` only) analytical TTL, conflict resolution, vector and full-text policies | `_rid`, `_self`, `_etag`, `_attachments`, `_ts` |
| every item's `id`, `ttl` and fields, byte for byte | change feed history, users, permissions |

Two consequences of dropping `_ts`: every copied item's TTL clock restarts at the
moment it is written, and "last modified" order is the copy's order, not the source's.
An item the target cannot take, one with nothing at a partition key path say, is
skipped, counted, and named in the log; more than 100 in one container stop the clone.

Writes go through up to four at a time, or `writers` on the target's profile (1 to 16).
When the target throttles, every writer waits as long as it asked, and one writer is
dropped for good: a 400 RU/s target settles at one writer within a few pages. `x`
stops the clone after the writes in flight. A clone that stopped, failed or ran out of
retries says exactly what it left behind; `r` resumes after the last page written in
full, and `d` deletes the partial target, through the same typed confirmation as the
catalog's `d`. Nothing is ever deleted automatically. While a clone runs, `x` in the
switcher refuses its two accounts, `d` in the catalog refuses its target, and a batch
into its target waits. The first `q` during a clone shows it, and the second stops it
and quits.

## Snapshots

`s` on a container in the catalog takes a snapshot of it: every item and the
container's definition, kept on disk. `v` lists its snapshots, newest first, and
`enter` shows what changed from the one before, or between the two marked with
`space`: the items added, removed and modified, a field-by-field diff of any
modified item (`enter` on it), and what changed in the definition, such as the
indexing policy, the TTL or the throughput. `s` and `v` on a database take and list
database snapshots, one per container. From a shell, with no TUI:

```sh
alchemist snapshot take prod sales.orders --note "before the migration"
alchemist snapshot diff prod sales.orders          # previous → latest
alchemist snapshot diff prod sales.orders --live   # the latest → now
alchemist snapshot list prod
alchemist snapshot export prod sales.orders latest -o orders.jsonl
alchemist snapshot verify prod sales --deep
```

and from cron, pruning on a line of its own, since nothing prunes by itself:

```
0 6 * * *  alchemist snapshot take prod sales --note nightly \
           && alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30
```

**What a snapshot is.** A snapshot of a live container is every item as the service
returned it during the window the list shows (`started` to `finished`), not a point
in time. No item is torn, and one nobody wrote during the window is exactly as it
was; but there is no consistency between items, and an item created or deleted
during the window may or may not be in it. A write missed that way is caught by the
next snapshot. A restore point consistent across items is the account's continuous
backup, not this.

**What it costs.** The first snapshot reads every item once. After that a snapshot
reads every item's key and version (which is the only way to see deletes), and the
bodies of those that changed; a container nobody touched costs one read of its keys
and under 4 KB of disk. Items are stored once whichever snapshots hold them,
compressed in blocks, so thirty daily snapshots of a container where 1% changes a day
take about 1.4× the disk of one; `v` and `snapshot list` show the store against the
exports it replaces (`30 snapshots · 30.0 GB of items · 460.0 MB on disk · 65× smaller`).
A capture spends request units as fast as the account lets it, and the progress line
shows how many.

**Where it lives.** Under `$XDG_DATA_HOME/alchemist/snapshots`
(`~/.local/share/alchemist/snapshots`), per profile, then database and container;
`snapshot_dir` in `config.toml` or `--snapshot-dir` moves it. Directories are `0700`
and files `0600`. **Snapshots are not encrypted**: they are copies of the account's
data, protected by file permissions and whatever encrypts the disk.

A snapshot only reads, so it works on a read-only account. A capture runs in the
background like a clone: `esc` hides it, the status bar carries
`snapshot prod/sales.orders 41% (v)` on every account, `v` in the catalog brings it
back, and `x` cancels it, keeping nothing. One capture, clone or other background job
runs at a time. A container past `snapshot_max_items` on the profile (5,000,000 when
unset) is refused before anything is read. `ctrl+e` in the list exports a snapshot's
items (`.jsonl` or `.json`), and in a diff, the diff (`.json` with a JSON Patch per
modified item, or `.csv`). `alchemist profile remove` keeps an account's snapshots
and says where; `--purge` deletes them with the profile.

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
alchemist profile set-read-only prod false
alchemist profile remove emulator
```

`profile add` prompts for the account key and stores it in the OS keychain (Keychain
on macOS, Secret Service on Linux, Credential Manager on Windows). `profile set-key`
replaces a key; `profile remove` deletes the profile and its keychain entry together.

The file can also be written by hand:

```toml
default_profile = "emulator"
snapshot_dir = "/mnt/big/alchemist-snapshots"   # snapshots here, not under $XDG_DATA_HOME

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # opened in the catalog on start
page_size = 100
max_join_rows = 5000             # rows a join holds across its held sides; 10000 when unset
sample_fields = false            # autocomplete never queries a container for its fields
read_only = false                # allow writes; unset, only a local endpoint allows them
writers = 8                      # item writes a clone or an update keeps in flight here; 4 when unset
max_mutation_items = 50000       # the most items one update may select; 10000 when unset
snapshot_max_items = 10000000    # the largest container a snapshot takes on; 5000000 when unset
diagnostics = "underline"        # squiggles as curly (default), underline, or off

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
it and runs it at once. `ctrl+s` saves the selected query under a name. `alchemist
--history=false` records nothing for that session.

The file is one JSON object per line, so `jq . < history.jsonl` reads it. A line a
session never finished writing is skipped, and the file is trimmed to its newest
2,500 entries once it passes 5,000.

## Saved queries

`ctrl+s` saves the query in the editor under a name, for the account the session is
on. `ctrl+l` lists that account's saved queries by name, with the same keys as
history: `/` filters by name, text or scope, `enter` loads one into the editor, and
`ctrl+r` loads and runs it. `r` renames the selected query and `d`, then `y`,
deletes it. To update a saved query, load it, edit it, press `ctrl+s` (the name is
filled in) and end the name with `!` to replace it.

Each query is a plain `.sql` file under `queries/<account>/` in the config directory
(`~/.config/alchemist/queries/prod/open orders.sql`), so `ls`, `cat`, `mv` and `rm`
work on them, and a `.sql` file dropped there is listed the next time the overlay
opens. A query saved against the selected container starts with one header line
recording it, and loading it selects that container again:

```sql
-- alchemist: scope=sales/orders
SELECT c.id, c.total FROM c WHERE c.status = "open"
```

A query that names its own containers (`FROM sales.orders c`) is saved without one.
A name is letters, digits, spaces, `.`, `-` and `_`, up to 64 characters.

`alchemist profile remove <name>` keeps the profile's saved queries and snapshots and
says where they are, so a profile removed and added again under the same name finds
them. `--purge` deletes them too.

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

Typing speed is gated. `make bench-gate` runs the typing benchmarks five times each and
holds the median to `testdata/bench-baseline.txt`: allocations and bytes per keystroke
may grow at most 5%, `query.Diagnose` must check a 2,000-line query within 5 ms, and a
2,000-line buffer may cost at most 10× a 200-line one. `make bench-baseline` records a
new baseline, which is committed on its own.

### CI and releases

| Workflow | Runs on | Does |
|---|---|---|
| `pr.yml` | pull requests to `main` | quality, benchmark, security, and emulator integration gates; `goreleaser check` and `actionlint` |
| `main.yml` | pushes to `main` | the same gates, then snapshot archives uploaded as build artifacts |
| `release.yml` | tags matching `v*` | the same gates, then a GitHub Release with archives, checksums, and changelog |

`main` is expected to be a protected branch that requires the `pr.yml` checks
(`gates / quality`, `gates / benchmarks`, `gates / security`, `gates / integration`, `release-check`) to pass
before a merge.

Cutting a release is one step:

```sh
git tag v0.1.0 && git push --tags
```

Design notes, architecture, and the iteration-by-iteration plan live in
[docs/plan](docs/plan/00-overview.md).
