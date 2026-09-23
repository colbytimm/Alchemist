# Iteration 13 — Query Autocomplete

## Goal

Type in the editor and be offered what can come next: Cosmos SQL keywords and built-in
functions, the databases and containers of the connected account, and the fields of the
container the query is about. Accepting a suggestion writes it into the buffer.

Cosmos has no schema, so "the fields of a container" is the one part of this that is
not a lookup. The plan below is honest about that: fields come from what the session
has already seen, plus a small sample the adapter takes once per container, and the
list is presented as what was *observed*, never as what exists.

## Layout

The list docks to the bottom of the editor pane and the buffer gives up those rows
while it is open:

```
╭─ Editor ─────────────────────────────────────────╮
│┃ SELECT c.id, c.cu                               │
│┃ FROM sales.orders c                             │
│┃                                                 │
│  customerId        field · partition key         │
│  currency          field                         │
│  customer.name     field                         │
│  tab accept   ↑/↓ choose   esc dismiss    3 of 3 │
╰──────────────────────────────────────────────────╯
```

It does not float at the cursor. bubbletea has no layers, and `textarea` exposes the
cursor's line and column but not its viewport offset, so the cursor's row *on screen*
is not knowable without reaching into the widget. Iteration 8 met the same wall when
highlighting the blurred editor. A docked list needs none of it: `SetHeight` shrinks
the textarea by the list's rows, and the textarea keeps its own cursor in view. A
cursor-anchored popup is listed under Out of scope, not forgotten.

At most `maxSuggestions` (6) rows plus the hint line. A pane too short to spare them
(fewer than `minPaneHeight` rows left for the buffer) shows a one-line list: the
selected suggestion and the count.

## What completes where

The context is decided from the tokens before the cursor, never from a regex over the
line.

| Cursor is | Offered | Example |
|---|---|---|
| at the start of a statement or after a complete clause | clause keywords valid next | `SEL` → `SELECT`; after `FROM c ` → `WHERE`, `JOIN`, `ORDER BY`, `GROUP BY`, `OFFSET` |
| after `FROM` or `JOIN … IN`'s source position | databases, then the alias-less container shortcut | `FROM sa` → `sales` |
| after a `,` in a `FROM` container list (iteration 10 union) | databases, as after `FROM` | `FROM sales.orders, sa` → `sales` |
| after `JOIN` | databases, for a cross-container join; nothing for the alias a `JOIN … IN` is about to declare | `JOIN sa` → `sales` |
| after `<database>.` in a source position | that database's containers | `FROM sales.or` → `orders` |
| after `JOIN <db>.<container> [alias] ` | `ON` | |
| after `ON ` or `ON <alias>.<field> = ` of a cross-container join | the two side aliases, then their fields | `ON o.cu` → `customerId` |
| after `<alias>.` anywhere else | fields observed at that path | `c.cu` → `customerId`, `currency` |
| after `<alias>.<path>.` | fields observed under that path | `c.customer.` → `name`, `tier` |
| in an expression position | fields of the root alias, functions, `TRUE`/`FALSE`/`NULL`/`UNDEFINED` | `WHERE STARTS` → `STARTSWITH(` |
| after `ORDER BY <expr> ` | `ASC`, `DESC` | |
| inside a string literal or a `--` comment | nothing | |

Aliases resolve through the parser `internal/query` already has: `FROM sales.orders o`
makes `o` the root alias, a bare `FROM c` makes it `c` against the catalog's scope, and
`JOIN t IN c.tags` makes `t` the element type of the array at `c.tags`. When no alias
can be resolved the field list falls back to the scope container's root fields.

Iteration 10 made aliases plural, and completion follows the planner's reading of
them rather than inventing its own:

- **Join.** `FROM sales.orders o JOIN sales.customers cu ON …` has two root aliases,
  each bound to its own container; `o.` lists fields of `orders` and `cu.` those of
  `customers`. A side with no alias is known by its container name, exactly as
  `query.BuildPlan` has it (`orders.customerId`).
- **Union.** `FROM sales.orders, sales.archive AS c` binds one alias to several
  containers; `c.` lists the union of their fields, each field's `Detail` naming the
  containers it was seen in when it was not seen in all of them.
- A cross-container join projects top-level fields only, so after `SELECT` in such a
  query `o.` offers top-level fields and stops; `ON` and `WHERE` positions still walk
  nested paths.

Completion never offers a shape the planner refuses: no `LEFT`/`OUTER`/`CROSS` before
`JOIN`, no `ORDER BY`/`GROUP BY`/`OFFSET` once a cross-container `JOIN` is in the
query, and no third `JOIN <db>.` source.

Matching is case-insensitive prefix first, then case-insensitive substring, in that
order, so `cid` does not outrank `cu` → `currency`. Keywords are inserted in the case
the user is typing them in (`sel` → `select`, `SEL` → `SELECT`); identifiers are
inserted exactly as observed, because Cosmos property names are case-sensitive.

A field name that is not a plain identifier (`order-id`, `first name`) is inserted in
bracket form, replacing the dot: `c.ord` → `c["order-id"]`.

## Interaction

| Key | Where | Action |
|---|---|---|
| typing | editor | the list opens, narrows, and closes on its own |
| `ctrl+space` | editor | open the list now, even on an empty prefix |
| `tab` | list open | accept the selected suggestion |
| `↑`/`↓` | list open | choose; the buffer's cursor does not move |
| `esc` | list open | dismiss the list; a second `esc` leaves the editor as today |
| `enter` | list open | a newline, as always — it never accepts |

`tab` is the one binding that changes meaning: with the list closed it is still
`NextPane`. The list opens automatically only after an identifier character or a `.`,
never after whitespace, so `tab` immediately after a finished word still moves panes.
`enter` deliberately does not accept: a list that opened by itself must not turn the
key that means "new line" into one that rewrites the line.

`ctrl+space` reaches bubbletea as `ctrl+@`. Some terminals and macOS input-source
switching swallow it; nothing depends on it, since the list opens on its own, and the
binding is documented as best-effort. No function key is bound.

Dismissing with `esc` holds until the token under the cursor changes, so a list the
user did not want does not reopen on the next character of the same word.

## Where suggestions come from

| Kind | Source | Cost | Freshness |
|---|---|---|---|
| Keywords | `query`'s keyword list, extended (see Scope) | none | static |
| Functions | a static table of Cosmos system functions with their signatures | none | static; pinned to a documented date |
| Databases | catalog root nodes the tree has loaded | none | follows the tree, including `r` refresh |
| Containers | catalog children of that database | one `Children` call if the tree has not loaded it yet | follows the tree |
| Fields, observed | raw documents of every page this session fetched for that container, including the leaves of a simulated run (see below) | none | grows as the user queries |
| Fields, partition key | `Node.Meta[MetaPartitionKey]` | none | follows the tree |
| Fields, sampled | `SELECT TOP 20 * FROM c` through the optional adapter interface below | one query per container per session, a few RU | taken once, on the first field completion for that container |

Pages of a simulated run (iteration 10) are synthetic, and the index must take them
apart rather than file them as they look. Their `Columns` are not fields: a union
leads with `_container` and a join prefixes every column with its alias (`o.total`).
Their `Raw` items say where each document came from, and that is what the index reads:

- a union item carries `"_container": "db.container"`: strip it and file the rest
  under the container it names;
- a join item nests each side under its alias (`{"o": {…}, "cu": {…}}`): file each
  half under the container `query.Plan.Leaves` gives for that alias. With a projected
  `SELECT` list the halves hold the projected fields only, which is still true of the
  container, just not complete.

The root model keeps the `query.Plan` of the current run beside its `runID` for this.

Containers of a database the tree has not expanded are fetched through the same
`loadChildren` command the tree uses and land in the tree's cache, so completion and
the catalog never disagree and the work is not done twice.

A query over several containers samples each of them, once each, the first time a
field of that container's alias is completed; a union alias samples every container
it covers.

Sampling spends request units the user did not ask to spend, so it is visible and
switchable: the list's hint line reads `sampling orders…` while it runs, the log
records the charge, and `sample_fields = false` on a profile (or
`--sample-fields=false`) turns it off, leaving observed and partition-key fields only.
A failed sample is logged and never retried in that session; completion carries on
without it.

## Adapter contract

```go
// FieldSampler is optional: a Connection implements it when its backend can
// say cheaply what fields a container's items tend to have.
type FieldSampler interface {
    SampleFields(ctx context.Context, container Node) (FieldSample, error)
}

// FieldSample is what one look at a container found. Paths are dotted from
// the item root; an array's element fields are under "<path>[]".
type FieldSample struct {
    Fields []Field
    Stats  Stats // what the look cost
}

type Field struct {
    Path string // "customer.name", "tags[]", "lines[].sku"
    Kind string // "string", "number", "bool", "object", "array", "null"; "" when it varied
}
```

The TUI asks for `FieldSampler` with a type assertion, as iteration 12 does for
`Inspector`, and an adapter without it simply offers no sampled fields. The mock
adapter implements it over its fixture items so every TUI test can exercise field
completion without a network.

Flattening a document into `Field`s lives in `internal/adapter` beside
`PartitionKeyNodes`, because observed fields (from `Page.Raw`, in the TUI's hands) and
sampled fields (from the adapter) must flatten identically.

## Scope

- `internal/query`
  - `Context(text string, cursor int) Completion` — the token under the cursor, the
    byte range a suggestion replaces, and what kind of thing belongs there (keyword,
    database, container of *db*, field under *alias*+*path*, expression). Built on the
    existing lexer and `parser`, which already track sources and aliases; since
    iteration 10 each `source` also records its `FROM` clause, whether a `JOIN`
    introduced it, and its token range. A field context names every container the
    alias is bound to (one for a join side, several for a union), resolved by the
    same rules as `query.BuildPlan` so the two cannot disagree.
  - Teach the lexer `--` comments. It does not know them today, which already
    colors a query wrongly with an apostrophe in a comment (noted in the iteration 8 PR);
    completion inside comments makes it worth fixing here, and the planner
    (`query.BuildPlan`, which replaced `ParseScope` in iteration 10) gets the fix for
    free.
  - Extend the keyword list with the clause words completion needs and the scope
    parser does not (`GROUP`, `BY`, `OFFSET`, `LIMIT` are present; add `UNDEFINED`,
    `ARRAY`, `ESCAPE`, `UDF`). Check each addition against the rule that list exists
    for: a keyword can never be a bare source alias. Iteration 10 added a second list
    under the same rule, `joinModifiers` (`LEFT`, `INNER`, `OUTER`, …); it stays out
    of highlighting because `LEFT` and `RIGHT` are also functions, and of those words
    completion offers only `INNER`.
- `internal/complete` (new, pure, no TUI imports)
  - `Index`: databases, containers per database, fields per container, fed by plain
    method calls; `Suggest(ctx query.Completion) []Suggestion` ranks and caps.
  - `Suggestion{Text, Insert, Kind, Detail}` — `Insert` differs from `Text` for
    functions (`STARTSWITH(`) and bracket-form fields.
  - The static function table, one entry per function with its signature as `Detail`.
- `internal/adapter` — `FieldSampler`, `FieldSample`, `Field`, and `FlattenFields`.
- `internal/adapter/cosmos` — `SampleFields` over the existing query path, `TOP 20`,
  cross-partition, one page.
- `internal/adapter/mock` — `SampleFields` over the fixtures.
- `internal/tui/panes`
  - `Editor` gains `Cursor() (text string, offset int)` and `Replace(start, end int,
    with string)`. `textarea` has `InsertString` but no public delete, so `Replace` is
    the spike (step 3): either rebuild the value with `SetValue` and walk the cursor
    back with `CursorUp`/`SetCursor`, or feed the widget backspace key messages. Pick
    the one that survives a multi-line buffer and wide runes under test.
  - `Suggestions` — the docked list: rows, selection, hint line, the one-line form.
- `internal/tui`
  - The root model owns the `complete.Index`, feeds it from `CatalogLoadedMsg`,
    `PageLoadedMsg`, and `PageAppendedMsg` (through the current run's `query.Plan`
    when the run was simulated), and recomputes suggestions after every editor
    update. Suggesting is synchronous and in-memory; only the container fetch
    and the sample are commands.
  - `FieldsSampledMsg` and an `OpSampleFields` failure through `ErrMsg`, each carrying
    the container path so a late answer for another container cannot land on this one.
  - Key routing: a `handleSuggestionKey` branch ahead of the editor's own handling
    while the list is open.
  - `KeyMap`: `Complete` (`ctrl+space`), `Accept` (`tab`), both listed under a new
    "Editor" help section; the drift-guard tests cover them.
- `internal/config` — `sample_fields` on `Profile`, default true.
- `README.md` — an "Autocomplete" section, including what sampling costs and how to
  turn it off; the key table.

## Out of scope

- A popup anchored at the cursor. It needs the textarea's viewport offset; revisit if
  `bubbles` exposes it or the editor stops wrapping `textarea`.
- Validating the query or underlining errors. Completion suggests; the service judges.
- Snippets and multi-cursor templates (`SELECT * FROM c WHERE …`).
- User-defined functions and stored procedures, which need a catalog the adapter does
  not serve yet.
- Learning field frequency across sessions or persisting the index. It is rebuilt per
  session from the tree and the pages.
- Completing inside the connect screen, the export prompt, or the history filter.

## Relationship to other iterations

- **10, cross-container.** Landed. `query.BuildPlan` accepts a container list (union,
  one shared alias) and a two-container `JOIN … ON` (one alias per side), and refuses
  every other multi-container shape with `query.ErrUnsupported`. Completion is built
  for both from the start: plural aliases, source positions after `,` and `JOIN`,
  `ON`, and an index fed from the synthetic pages those runs produce. It offers
  nothing the planner would refuse. The sample databases from `make emulator-seed`
  (`sales`, `telemetry`, `hr`) are the manual test bed.
- **11, catalog management.** A container created or deleted through the tree changes
  what completes, with no extra work, because the index is fed from the same messages.
  Deleting a container drops its fields from the index.
- **12, info view.** Both add an optional adapter interface found by type assertion;
  follow whichever lands first for naming and for where the assertion lives.

## Steps

1. `query.Context`, table-tested against the matrix in "What completes where", plus
   the `--` comment fix with its own scope-parser and span tests. A fuzz target: for
   any text and any cursor, the replace range lies inside the text and on rune
   boundaries.
2. `internal/complete`: the index, ranking, the function table. Pure tests.
3. Spike `Editor.Replace`; commit the variant that passes the multi-line and
   wide-rune cases with less code, and record the decision here as iteration 8 did.
4. The `Suggestions` pane and its docking in `Editor.SetSize`.
5. Root-model wiring for keywords, functions, databases, and containers, including
   the on-demand container fetch.
6. Observed fields from pages; partition-key fields from the tree.
7. `FieldSampler` in the adapter package, the mock, then Cosmos; the config switch.
8. README, help section, plan statuses.

Steps 1–6 ship a useful feature with no new adapter surface and no request units
spent; step 7 is separable if sampling needs more thought.

## Testing

**Unit — `query.Context`:** every row of the context table; cursor mid-word (the range
covers the whole word, not just the part before the cursor); cursor in a string, in a
comment, at offset 0, at the end; aliases from `AS`, bare, and `JOIN … IN`; an
unterminated string; multi-byte text before the cursor. For iteration 10's shapes: a
source position after a list comma and after `JOIN`; `ON` after a joined container;
each join alias resolving to its own container, including a side with no alias; a
union alias resolving to every listed container; a `SELECT`-list field context in a
cross-container join marked top-level only.

**Unit — `complete`:** prefix outranks substring; ties keep source order; the cap;
keyword case follows the typed case; a field needing brackets gets them; a deleted
container's fields are gone; fields observed twice appear once.

**Unit — `FlattenFields`:** nested objects, arrays of objects (`lines[].sku`), arrays
of scalars, a field whose kind varies across documents, `null`, empty document.

**TUI, mock adapter:**
- Typing `SEL` lists `SELECT`; `tab` leaves `SELECT` in the buffer and the list closed.
- `FROM ` then `s` lists the mock's databases; `sales.` lists its containers, fetching
  them when the tree had not.
- `c.` lists fields after a query has returned a page, before any sample.
- After a join has run, `o.` and `cu.` list their own container's fields, and neither
  lists `_container`, `o.total`, or any other synthetic column; after a union, `c.`
  lists fields seen in either container and never `_container`.
- In a join, completing `o.` then `cu.` issues one sample per container, two in all.
- After a cross-container `JOIN`, the clause keywords offered exclude `ORDER BY`,
  `GROUP BY`, and `OFFSET`.
- The first field completion for a container issues exactly one sample; a second
  container issues its own; `sample_fields = false` issues none.
- A sample that fails is logged, not retried, and does not disturb typing.
- A late `FieldsSampledMsg` for another container is filed under that container.
- `tab` with the list closed still moves to the next pane; `enter` with it open still
  inserts a newline; `esc` closes the list first and leaves the editor second.
- `q`, `r`, `?` typed into the buffer narrow the list and trigger nothing.
- The list never opens while the editor is blurred, and blurring closes it.
- The editor pane at `minPaneHeight` shows the one-line form.
- Every new binding is grouped exactly once and appears in the help overlay and README.

**Integration (emulator):** `SampleFields` against a seeded container returns the
nested paths of the seed documents and a non-zero request charge.

**Manual checklist:**
- [ ] Against the emulator: complete a database, a container, and a nested field in one
      query, then run it.
- [ ] Against the seeded `sales` database: write the iteration 10 join example using
      completion for both containers, both aliases' fields, and `ON`; run it.
- [ ] Typing at speed in a 200-line buffer stays responsive with the list open.
- [ ] `ctrl+space` behavior noted for Terminal.app, iTerm2, and one Linux terminal.
- [ ] With `sample_fields = false` the log shows no sampling query.

## As built

Decisions the sections above left open, recorded once the code settled them:

- **`Editor.Replace` rebuilds the buffer.** Of the two spike variants, `SetValue` with
  the cursor walked back won: `CursorUp` until `Line()` reaches the target row, then
  `SetCursor` at the rune column. It is a dozen lines, and passes the multi-line and
  wide-rune cases; synthesizing backspace messages would have needed one message per
  rune and a way to move the cursor to the range first. The textarea scrolls to its
  cursor on the next key, as it already did after a history recall.
- **The list shows as many rows as the buffer can spare, up to six.** The buffer keeps
  two rows (`minBufferRows`); the one-line form appears only when not even one list row
  fits above them. At the minimum terminal the editor's five inner rows hold two
  suggestions and the hint line. `Suggest` returns every match ranked; the pane windows
  them around the selection, so the hint's `2 of 14` counts everything that matched.
- **A field's detail is its kind** (`string · partition key`, `object`, `array`), or
  `field` when no item has shown it or items disagree. A union alias appends the
  containers a field was seen in when it was not seen in all of them.
- **Fields are sampled only after `alias.`.** An expression position lists the fields
  already observed as `alias.field`, but does not spend request units until the user
  has asked for a container's fields by name.
- **The `c` shortcut is offered after `FROM` only.** When the catalog has a scope, the
  first source position lists it after the databases as `c · scope · sales.orders`; a
  list comma or `JOIN` lists databases alone.
- **A run or a change of focus closes the list.** `ctrl+r` with the list open runs the
  query and takes the list down, so the `tab` that follows moves panes.
- **`sample_fields` is a `*bool`** so that an unset key means true; `--sample-fields`
  on the root command can only turn it off for a session.
- **The help overlay wraps its columns** onto a second row when the terminal is too
  narrow for all four side by side, which the minimum terminal is.
- **Comments color as hints.** `SpanComment` joins the span kinds, so a `--` comment is
  dimmed in the blurred editor and its apostrophes no longer open a string. `INNER`
  joined the keyword list too: completion offers it, so it has to color and to count
  as a keyword when the context reads back over it.
- **A name the user is inventing is never replaced.** Right after a source path, a
  joined path, `JOIN x`, or a SELECT item, the word under the cursor may be an alias,
  and `tab` on an auto-opened list would have overwritten it with `ORDER BY` or `AS`.
  Those positions offer nothing while a word is being typed; the keywords come back
  after a space. Directly after `JOIN` the word is taken for a database, as the table
  says, which a one-letter `JOIN t IN` alias can still collide with.
- **A number opens no list.** The list auto-opens only at the end of an identifier or
  a dot, so a `tab` after `= 1` moves panes rather than gluing `AND` to the number.
- **Only whole items are observed.** A page is filed as fields of its container only
  when its leaf query is `SELECT [TOP n] * FROM …`; a projection names what the query
  made of the items, not what they hold. A join side is the exception the plan makes,
  minus any column an `AS` renamed. `Plan.LeafItems` undoes the union tag and the
  join pairing in `query`, beside the code that applies them.
- **The tree's failures and loads are respected.** Completion asks for a database's
  containers through `Catalog.LoadPath`, which asks nothing for a node the tree has
  listed, is listing, or has a failure on record for, so a failing listing is not
  retried on every keystroke; the hint says `loading …` only while a request is out.
- **A dismissal ends when the cursor leaves the token**, and covers only the word it
  was typed against, so a new word at an old offset opens again.
- **A refresh keeps the selection.** A sample or a listing landing while the list is
  open re-ranks the rows but stays on the one chosen, when it is still there.
- **Sample state follows the catalog.** A container the tree drops loses its sample
  record with its fields, so a container recreated under the same name is sampled
  afresh and a sample landing after the delete is discarded.

## Acceptance criteria

- Keywords, functions, databases, containers, and fields complete in the positions the
  context table names, and nowhere inside strings or comments.
- In a cross-container query every alias completes the fields of its own container or
  containers, synthetic columns of simulated results never appear as fields, and
  nothing is offered that `query.BuildPlan` would refuse.
- Accepting replaces exactly the token under the cursor, in multi-line buffers and
  after multi-byte text, and leaves the cursor after the inserted text.
- No request is made for keyword, function, or database completion; container
  completion makes at most the one catalog call the tree would have made; field
  completion makes at most one sampling query per container per session, reported in
  the log, and none when switched off.
- `tab`, `enter`, and `esc` behave exactly as before whenever the list is closed.
- `Update` never blocks: a slow container fetch or sample leaves typing, `esc`, and
  `ctrl+c` working.
- `internal/tui` imports `internal/complete` and the adapter interfaces only.
