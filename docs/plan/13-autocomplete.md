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
| after `<database>.` in a source position | that database's containers | `FROM sales.or` → `orders` |
| after `<alias>.` anywhere else | fields observed at that path | `c.cu` → `customerId`, `currency` |
| after `<alias>.<path>.` | fields observed under that path | `c.customer.` → `name`, `tier` |
| in an expression position | fields of the root alias, functions, `TRUE`/`FALSE`/`NULL`/`UNDEFINED` | `WHERE STARTS` → `STARTSWITH(` |
| after `ORDER BY <expr> ` | `ASC`, `DESC` | |
| inside a string literal or a `--` comment | nothing | |

Aliases resolve through the parser `internal/query` already has: `FROM sales.orders o`
makes `o` the root alias, a bare `FROM c` makes it `c` against the catalog's scope, and
`JOIN t IN c.tags` makes `t` the element type of the array at `c.tags`. When no alias
can be resolved the field list falls back to the scope container's root fields.

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
| Fields, observed | columns and raw documents of every page this session fetched for that container | none | grows as the user queries |
| Fields, partition key | `Node.Meta[MetaPartitionKey]` | none | follows the tree |
| Fields, sampled | `SELECT TOP 20 * FROM c` through the optional adapter interface below | one query per container per session, a few RU | taken once, on the first field completion for that container |

Containers of a database the tree has not expanded are fetched through the same
`loadChildren` command the tree uses and land in the tree's cache, so completion and
the catalog never disagree and the work is not done twice.

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
    existing lexer and `parser`, which already track sources and aliases.
  - Teach the lexer `--` comments. It does not know them today, which already
    colors a query wrongly with an apostrophe in a comment (noted in the iteration 8 PR);
    completion inside comments makes it worth fixing here, and scope parsing gets the
    fix for free.
  - Extend the keyword list with the clause words completion needs and the scope
    parser does not (`GROUP`, `BY`, `OFFSET`, `LIMIT` are present; add `UNDEFINED`,
    `ARRAY`, `ESCAPE`, `UDF`). Check each addition against the rule that list exists
    for: a keyword can never be a bare source alias.
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
    `PageLoadedMsg`, and `PageAppendedMsg`, and recomputes suggestions after every
    editor update. Suggesting is synchronous and in-memory; only the container fetch
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

- **10, cross-container.** Several `db.container` sources in one query each get their
  own alias and their own field list; `Context` already returns the alias, so nothing
  here assumes a single container. Until 10 lands the scope parser still rejects such
  a query at run time, and completion does not try to get ahead of that.
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
unterminated string; multi-byte text before the cursor.

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
- [ ] Typing at speed in a 200-line buffer stays responsive with the list open.
- [ ] `ctrl+space` behavior noted for Terminal.app, iTerm2, and one Linux terminal.
- [ ] With `sample_fields = false` the log shows no sampling query.

## Acceptance criteria

- Keywords, functions, databases, containers, and fields complete in the positions the
  context table names, and nowhere inside strings or comments.
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
