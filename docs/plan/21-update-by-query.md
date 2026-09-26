# Iteration 21 — Bulk Update by Query

## Goal

Change many items with one statement:

```
UPDATE sales.orders o
SET o.status = "archived", o.archivedAt = "2026-01-01"
WHERE o.status = "shipped" AND o.total < 50
```

Cosmos DB for NoSQL has no `UPDATE … WHERE`. Its SQL is read-only, and the only
server-side write logic is a stored procedure, which the Go SDK has no client for
(iteration 12 records this). So Alchemist does what a careful script would do, and says
so: **select the matching items with a query, then write to each one.** The selection
is an ordinary single-container read that the service evaluates. The writes are one
partial document update (patch) per item, each carrying the statement's `WHERE` as a
server-side condition, so an item that stopped matching since it was selected is left
alone.

This iteration introduces the engine that iteration 22 (delete by query) reuses:
statement detection, target selection, the dry run and its review, the job, the report.
**22 depends on 21**, not the reverse. Update goes first on purpose. The shared part is
the same either way, the part that differs (assignments turned into patch operations)
is pure and small, and the first feature through a new write engine should be the one
whose worst case is a wrong field value, not a missing document.

The second goal is iteration 17's and outranks the first: nothing is written by
accident, against the wrong account, or without the user having seen how many items it
touches. This plan adds no second safety model. It reuses 17's: a statement in the
editor, `ctrl+r`, a review overlay, a typed confirmation, `read_only`, the
unknown-outcome rule, a report that is an `adapter.Page`. It reuses 18's job model for
the part that takes minutes.

What was verified, and where:

| Fact | Source |
|---|---|
| Patch operations: add, set, replace, remove, incr, move. `set` creates the path when it is absent and replaces it when present; `replace` and `remove` fail on an absent path | Microsoft Learn, *Partial document update* (page dated 2025-07-21) |
| At most 10 operations in one patch | same page, "Supported modes" |
| A patch may carry a filter predicate (`from c where c.taskNum = 3`); the operation fails when the predicate does not hold | same page, "Conditional update" |
| Paths are JSON Pointers; `~` and `/` in a property name are written `~0` and `~1` | same page |
| `ContainerClient.PatchItem(ctx, partitionKey, itemId, ops, *ItemOptions)`; the SDK checks neither the count nor the paths, so the 10-operation limit and the `/id` and key-path rules are the service's to enforce and Alchemist's to refuse first | `cosmos_container.go`, azcosmos v1.5.0 |
| `PatchOperations` has `AppendAdd`, `AppendSet`, `AppendReplace`, `AppendRemove`, `AppendIncrement(path, int64)` and `SetCondition`; there is no `move` | `cosmos_patch_operations.go` |
| A patch value is an `any` field tagged to be left out of the JSON when empty: a nil value is dropped from the payload, so `AppendSet(path, nil)` sends a `set` with no value. A `json.RawMessage("null")` is a non-nil interface and is sent as `null` | same file, `patchOperation` |
| The condition is formatted into the payload with `%s`, unescaped | same file, `PatchOperations.MarshalJSON`; iteration 17 found the same |
| `ItemOptions.IfMatchEtag` is sent as `If-Match` on `PatchItem`, `ReplaceItem` and `DeleteItem` | `cosmos_item_request_options.go`, `toHeaders` |
| `ItemResponse` carries `RequestCharge` and `ETag` | `cosmos_response.go` |
| Delete items by partition key is in public preview, needs an account capability (`DeleteAllItemsByPartitionKey`), and is listed for the .NET, Java and Python SDKs only, "support for other SDKs is planned" | Microsoft Learn, *Delete items by partition key value* (page dated 2025-12-05) |
| azcosmos v1.5.0 has no such operation: the only trace is the unused header constant `cosmosHeaderIsPartitionKeyDeletePending` | `cosmos_http_constants.go`; `ContainerClient` has no method for it |
| The Linux emulator lists Patch as supported and request units as not implemented | iteration 17's table |

Not verified, and treated accordingly: which status a failed predicate returns (the
adapter maps whatever the service sends for a precondition to one sentinel, and the
integration test pins it), and whether every `WHERE` the query engine accepts is also
accepted as a patch predicate (see "The first write is a probe").

## The model

**An update is a statement in the editor**, for the reasons 17 gives for a batch: the
buffer is visible, editable, recallable, and names its own target.

`ctrl+r` on such a statement never writes. It runs the **dry run**: parse, check,
select the targets, open the review. The review shows what will happen and asks for
the container name. `enter` there starts the **job**, which writes in the background
the way a clone copies. When it ends, the **report** is a result set.

```
ctrl+r → parse → check → select targets (reads only) → review
       → typed confirmation → job (writes, chunk by chunk) → report
```

### Grammar

Parsed by `internal/query` with the lexer it has. A value is found the way 17 finds a
body: `{`/`}` and `[`/`]` matched over tokens, the byte range checked with
`json.Valid`.

```
update     = "UPDATE" target [ [ "AS" ] alias ]
             ( "SET" assignment { "," assignment } [ "UNSET" path { "," path } ]
             | "UNSET" path { "," path } )
             "WHERE" condition [ ";" ]
target     = identifier "." identifier
assignment = path "=" value
path       = alias { "." identifier | "[" string "]" | "[" integer "]" }
value      = string | number | "TRUE" | "FALSE" | "NULL" | object | array
condition  = every token up to the end of the statement
```

- Keywords are case-insensitive. A string value takes either quote, as in a query, and
  is sent as a JSON string. Objects and arrays are JSON as written.
- The alias defaults to `c`, as `aliasOrDefault` has it for a query. Every path starts
  with it: `o.status`, `o.shipTo.region`, `o["order-id"]`, `o.lines[0].qty`.
- A path becomes a JSON Pointer (`/shipTo/region`, `/order-id`, `/lines/0/qty`), with
  `~` and `/` escaped as `~0` and `~1`.
- `SET path = value` is the patch operation `set`: the path's last step is created when
  absent and replaced when present. Its parent is never created: a `set` of
  `/ship/region` on an item with no `ship` is refused with 400 (verified on the
  emulator; see "Implementation notes" for how such items are handled). It is not `replace`, which would fail every item that lacks
  the field, and not `add`, which inserts into an array instead of overwriting.
- `UNSET path` is `remove`. The service fails a `remove` of an absent path, and one
  failed operation fails the item's whole patch. So an item that lacks the path gets a
  patch without that operation, and an item left with no operation at all is not a
  target. See "Selecting the targets".
- **`WHERE` is mandatory.** A statement without one does not parse:
  `UPDATE needs a WHERE. To change every item, write WHERE true`. This is 17's
  "a half-typed statement is inert" applied here: `ctrl+r` pressed one clause early
  must not select a whole container. `WHERE true`, exactly that, is the every-item
  form, and the review treats it differently (below).
- The condition is **never parsed**. It is the service's dialect, the service
  evaluates it twice (selection, then as each patch's predicate), and Alchemist sends
  the same bytes both times. It ends at the statement's end; a `;` inside a string is
  the lexer's problem and already solved.
- A buffer is an update when its first token is `UPDATE`. No Cosmos SQL query starts
  with it, so nothing that runs today changes meaning. `query.IsMutation(text)` answers
  it; iteration 22 adds `DELETE FROM` to the same function.

### Refused

Syntax problems are a `*query.MutationSyntaxError{Line, Column, Message}`. Shapes that
parse as SQL somewhere but are not built are `query.ErrMutationUnsupported` ("not
supported in an UPDATE or DELETE"), wrapped with the shape. `query.ErrUnsupported` is
not reused: its text is about running something "server-side, one container at a
time", which is the wrong advice here.

| Shape | Refused as |
|---|---|
| no `WHERE` | the message above |
| `SET o.total = o.total * 1.1`, `= o.other`, `= UPPER(o.x)`, `= @p` | `SET takes a literal JSON value: a patch cannot read another field` |
| `SET o.n += 1`, `INCREMENT` | `increment is not built: it is the one patch operation that is not safe to run twice` |
| `SET o = {…}` | `SET needs a field: replacing whole items is a REPLACE in a BEGIN BATCH` |
| `SET o.tags[-]`, a negative index, `move` | `appending to and moving within arrays is not built` |
| a path that does not start with the alias | `o2.status: paths start with the target's alias o` |
| target `orders`, or `FROM c` style with no database | `name the target as database.container: a write never depends on the catalog cursor` |
| target of three parts whose first is a known account | iteration 14's `errAccountInQuery` text |
| `UPDATE a.b, a.c`, `UPDATE … FROM`, `UPDATE … JOIN`, `WITH … UPDATE` | `an UPDATE has one target container` |
| a `database.container` source inside the `WHERE` | `the WHERE reads another container: choosing targets with a join is not built. Run the join as a query and update WHERE o.id IN (…)` |
| `TOP`, `ORDER BY`, `OFFSET`, `LIMIT`, `RETURNING` | `<keyword> in an UPDATE` |
| text after the `;` | `a buffer holds one statement` |

A subquery over the target's own arrays (`EXISTS(SELECT VALUE t FROM t IN o.tags WHERE
…)`) is not a second container: its source starts with the statement's alias. The
scanner in `scope.go` already tells the two apart for `JOIN t IN c.tags`.

### Checks

`query.CheckMutation(m, keyPaths)` runs after the parse and needs the container's key
paths, fetched as 17 fetches them (`Node.Meta[adapter.MetaPartitionKey]`, through
`loadChildren` when the tree has not loaded the database). Every problem is listed, not
the first, in 17's refusal format.

| Check | Refusal |
|---|---|
| the container is in the catalog | `sales.invoices: no such container (r in the catalog reloads it)` |
| 1 to 10 operations (`query.MaxPatchOperations`) | `a patch takes at most 10 operations; this statement has 12. Two statements are two writes per item` |
| no path is `/id` | `cannot change id: that is a delete and a create` |
| no path is a partition key path, a parent of one, or a child of one | `cannot change the partition key /customerId: that is a delete and a create` |
| no path names a field the backend owns (`adapter.IsSystemField`) | `cannot set _etag: the service owns it` |
| no path appears twice, and none is a prefix of another | `o.shipTo and o.shipTo.region overlap` |

A statement is never split into two patches to get past 10 operations: two patches are
two writes that can succeed and fail separately, per item, and the review would have to
explain a state between them.

### Selecting the targets

The dry run reads the matching items' identities, all of them, before the review
opens: `id`, the values at the partition key paths, and the version. This is a
**filtered identity scan**, iteration 18's `ItemScanner` with 19's `ScanIdentity`
projection and one new request field, `ScanRequest.Filter` (see "Adapter contract").

Decisions:

- **All targets up front, not a count.** The review has to say "412 items" and mean
  the 412 it is about to write. `SELECT VALUE COUNT(1)` costs RU, returns a number that
  is already stale, and is followed by a second read of the same index range for the
  identities anyway. One read, kept.
- **Up front, not streamed page by page into writes.** A write changes what the
  `WHERE` matches while a continuation token is walking it. Freezing the list first
  makes the job's definition simple enough to say out loud: *the items that matched at
  14:02:11*. It also makes stop and resume exact, and the confirmation honest.
- **Identities, not bodies.** About 150 bytes an item. Ten thousand targets are 1.5 MB
  and, at iteration 19's planning figure of 0.015 RU per key, 150 RU.
- **A cap.** `max_mutation_items` per profile, default 10 000, delivered as
  `Account.MaxMutationItems`. The selection stops at the cap plus one and the run is
  refused with `mutate.ErrTooManyTargets`: `more than 10,000 items match. Narrow the
  WHERE, or raise max_mutation_items on the prod profile`. Nothing was written; the RU
  the selection spent is shown. It is the `max_join_rows` pattern: never a silent
  truncation, because "updated the first 10 000" is a partial run nobody asked for.
- **A statement with `UNSET` reads whole items instead.** Which items have the path
  decides which get the `remove`, and only the body says. Each page is reduced to
  identities plus one bit per `UNSET` path as it arrives, so memory is the same; the
  RU is a full read of the matching items, and the review says so.
- **A preview read.** One more page, `ScanWholeItems`, `PageSize` 3, same filter, for
  the before → after block. Items it returns that are not in the target list are not
  shown.
- The selection is a run like a query: `selecting…` in the status bar, `esc` cancels
  it (it is a read; cancelling is safe), a newer `ctrl+r` supersedes it by `runID`.
- An item with no value at a key path (`adapter.ErrNoPartitionKey`), or an integer key
  past 2^53, cannot be addressed through this SDK (18 records why). It is kept in the
  list as `skipped: no partition key`, counted in the review, and never written.
- Zero targets is a result, not a review: `No item in sales.orders matches. Nothing to
  update.` It is recorded, with its RU.

## Semantics, stated honestly

The README section and the review's fixed lines say these in plain words.

- **Not atomic.** Each item is its own write. A run that stops, fails or is quit has
  updated some items and not others. There is no rollback and no undo. The report lists
  exactly which.
- **Not isolated.** The target list is the items that matched when the dry run read
  them. An item that starts matching afterwards is not touched; run the statement
  again. An item that stopped matching is protected by the condition, next point.
- **Each write re-asserts the `WHERE`.** The patch carries `FROM o WHERE <condition>`
  as its predicate, in the statement's own words and alias, so the service checks it
  atomically with the write. An item changed since selection so that it no longer
  matches is `skipped: changed` and not written. An item changed in some *other* field
  still matches and is updated, with the other change intact: a patch touches only its
  paths. That is why the guard is the predicate and not an ETag `If-Match`. An ETag
  would skip every item anyone touched for any reason, which throws away the one thing
  a patch is better at than a replace. Iteration 22 makes the opposite choice for the
  opposite reason.
- An item deleted since selection is `skipped: gone`.
- **A write is sent once.** azcore's retry policy is turned off for these calls, as 17
  turns it off for a batch. A throttled write (429) is known not to have been applied
  and is retried by the engine under 18's rules. A write with no answer is `unknown`:
  it is not retried, the item is marked, and the job goes on. Three unknowns in a row
  end the job, because the network is gone.
- **Running the statement again is safe**, and is the resume story across sessions.
  `SET` to a literal and `UNSET` give the same document however many times they run;
  that is why increment is refused. Items already updated often no longer match (the
  example's `status` is no longer `shipped`) and are not even selected; the rest are
  patched again to the same values, at the cost of their RU. An `unknown` item is
  settled by the rerun either way.
- **Stop** (`x`) starts no new write and lets the ones in flight finish. A write's
  context is never cancelled by the model, because cancelling a sent write is how an
  unknown outcome is made (17).
- **Before a large run, take a copy.** Iteration 19's snapshot, then its diff
  afterwards, is the intended pairing; iteration 18's clone is the other. The review
  says which exists.

### Single writes, not transactional batches

17's `Batcher` could carry up to 100 patches of one logical partition atomically, in
one round trip. It is not used, and the reason is semantics, not effort:

- A batch is all or nothing. One `skipped: changed` item rolls back the other 99, and
  the only recovery is to send those 99 again one by one. Skipping per item is the
  feature; a batch turns it into a failure mode.
- Targets would have to be grouped by partition key value. A container keyed on
  something nearly unique (`/deviceId`, `/id`) yields batches of one and gains nothing;
  a container with a hot key hits the 2 MB and 5 s limits instead.
- 17's contract is "never retried, a 429 refuses the whole batch". A throttled batch of
  100 is 100 items of backoff bookkeeping.
- A batch saves round trips, not RU. 18's bounded writer pool already takes latency out
  of the path.

The report therefore has one row per item with its own outcome. Grouping by partition
behind `ItemEditor` stays possible later for the case where every item applies; it
would not change the engine or the report.

### The first write is a probe

The job writes its first target alone, before the pool starts. A refusal there that is
not about that item — the service does not accept this `WHERE` as a patch predicate, a
path is invalid, the key cannot write — ends the job with one item attempted and the
service's words on screen. Without it, a systematic refusal would fail ten thousand
writes at full speed. After the probe, more than `mutate.MaxFailures` (100) failed
items end the job, as 18's `MaxSkipped` does: something systematic is wrong.

A `WHERE` the service accepts for a query but not as a predicate is refused this way in
v1, verbatim, with `a few items can be patched in a BEGIN BATCH with IF MATCH`. There
is no second guard mode to fall back to: two guards would mean two meanings of
"skipped".

## Layout and interaction

| Key | Where | Action |
|---|---|---|
| `ctrl+r` | anywhere, buffer is an update | dry run; then the review, or the refusal |
| `esc` | while `selecting…` | cancel the selection |
| typing | review | the confirmation field |
| `↑`/`↓` | review | scroll |
| `enter` | review | start — only once the typed text matches |
| `esc` | review | close; nothing was written, nothing is recorded |
| `esc` | progress view, running | hide it; the job keeps running |
| `x` | progress view, running | stop after the writes in flight |
| `r` | progress view, ended short | resume with the targets not yet attempted |
| `esc`, `enter` | progress view, ended | close it and load the report into the results |
| `w` | catalog, any row or none, any account, while an update or delete job exists | reopen the progress view |

`w` (`KeyMap.ShowMutation`, help "show update/delete job") is the one new binding, and
it is the pattern 18 (`y`) and 19 (`v`) set for their jobs: a catalog rune that means
"show the job" while one exists. It is disabled, and so absent from help, while none
does. It is free in `internal/tui/keys.go` (`tab`, `shift+tab`, `e`, `r`, `m`, `q`,
`?`, `/`, `esc`, `enter`, `space`, `h`/`j`/`k`/`l`, arrows, `ctrl+r`, `ctrl+o`,
`ctrl+e`, `ctrl+c`), in the README, and in everything plans 11–20 reserve: `n`, `c`,
`d`, `t`, `i`, `y`, `s`, `v` in the catalog, `a` and `x` in the switcher, `ctrl+g`,
`ctrl+space`, `ctrl+s`, `ctrl+l`, `ctrl+b`. No global key and no function key is
added. `x`, `r` and `esc` in the progress view mean what they mean in 18's. While a
job exists `ctrl+r` on another update or delete is refused with `an update is running
on prod/sales.orders: w in the catalog shows it`; queries run as usual.

### Review

```
┌─ Review update ─────────────────────────────────────────────────────┐
│  Account     prod                                                   │
│  Container   sales.orders                     key /customerId       │
│  Where       o.status = "shipped" AND o.total < 50                  │
│  Items       412 matched at 14:02:11 · selection cost 6.18 RU       │
│  Each gets   set /status "archived" · set /archivedAt "2026-01-01"  │
│  Writes      4 at a time · roughly 4,100 RU (10 RU per 1 KB item;   │
│              larger or heavily indexed items cost more)             │
│                                                                     │
│  o017  c02   ~ /status      "shipped" → "archived"                  │
│              + /archivedAt  "2026-01-01"                            │
│  o034  c04   ~ /status      "shipped" → "archived"                  │
│              + /archivedAt  "2026-01-01"                            │
│  … 410 more                                                         │
│                                                                     │
│  ! The WHERE does not pin /customerId: the selection read every     │
│    partition.                                                       │
│  ! No snapshot of sales.orders today. esc, then s in the catalog    │
│    takes one.                                                       │
│                                                                     │
│  Items are written one by one. Each write re-checks the WHERE; an   │
│  item that no longer matches is skipped. Stopping leaves the rest   │
│  unchanged. There is no undo.                                       │
│  Type the container name to update 412 items:                       │
│  > orders                                                           │
│                                                                     │
│  enter update   ↑/↓ scroll   esc cancel                             │
└─────────────────────────────────────────────────────────────────────┘
```

- **Confirmation.** The container name, typed exactly: 17's rule, 17's widget
  (`panes.Confirm`). For `WHERE true` the text is the name and the count, `orders 60`,
  and the prompt reads `Every item in sales.orders. Type the container name and the
  item count:`. A confirmation is never remembered.
- **Preview.** Before → after is computed locally by `mutate.Preview`, which applies
  the item's operations to the previewed body. One line per path: `~` changed, `+`
  added, `-` removed, `=` already that value. The rows have the shape of 19's
  structural diff (`{op, path, before, after}`) and are drawn by its renderer when 19
  has landed, as 19 records; before that, by a dozen lines here.
- **Warnings never block.** They are: `WHERE true`; a `WHERE` with no top-level
  equality on the first key path (found with 10's `splitConjuncts`, the only thing
  Alchemist reads out of the condition, and only to warn); targets with no partition
  key; an `UNSET` that made the dry run read whole items; items that need no operation
  at all (`38 items already lack /tmp and are left out`); the snapshot line.
- **The snapshot line** appears only when 19 has landed (`Options.Snapshots` is set).
  It is one call in a `tea.Cmd`, 19's `snapshot.Newest(loc snapshot.Location)
  (Record, error)`, which reads the newest record file and nothing else:
  the line shows that snapshot's age, or, on `snapshot.ErrNoSnapshot`, that there is
  none. It is advice, not a key: the confirmation field has the keyboard, a capture is
  itself a job, and only one job runs at a time. Without 19 the line names 18's clone.
- **RU.** The selection's charge is measured. The write figure is a labeled planning
  number, `mutate.PlanningChargePerPatch` (10) times the count, and is replaced by a
  projection from measured charges once the job runs, as in 18. On the emulator both
  read 0 and the view says `the emulator does not report RU`.

### Progress

`panes.MutationProgress`, shaped like 18's `CloneProgress`:

```
┌─ Update · prod / sales.orders ──────────────────────────────────────┐
│  Updating items                                                     │
│  ████████████░░░░░░░░░░░░░░░░░░  41%                                │
│                                                                     │
│  Items      169 of 412                                              │
│  Updated    166        skipped 3 (2 changed, 1 gone)     failed 0   │
│  Rate       38 items/s                        about 6s              │
│  RU         1,702.11 so far · about 4,150 in total                  │
│  Writers    3 of 4                            throttled once        │
│                                                                     │
│  Items already updated stay updated if this stops.                  │
│                                                                     │
│  esc hide   x stop                                                  │
└─────────────────────────────────────────────────────────────────────┘
```

Ended short:

```
│  Stopped. 169 of 412 items were attempted: 166 updated, 3 skipped.  │
│  243 were not attempted and are unchanged.                          │
│                                                                     │
│  r resume   esc report                                              │
```

While hidden, the status bar carries `update prod/sales.orders 41% (w)`, then `update
done (w)`, `update stopped (w)` or `update failed (w)` until the view has been opened
and closed once. The field belongs to the job, not to an account, and `setActive`
never touches it.

### Report

Closing the ended view loads the report into the results pane, replacing what is
there, as any run does. It is an ordinary result set, served by a synthetic
`adapter.Cursor` (`mutate.NewReportCursor`) in pages of `page_size`, so `m`, `enter`,
`ctrl+e` and the status bar need no change.

```
╭─ Results · prod · updated ──────────────────────────────────────────────╮
│ Updated 409 of 412 items in sales.orders. 2 had changed, 1 was gone.    │
│                                                                         │
│ #    id    partitionKey  outcome           status                   RU  │
│ 1    o017  "c02"         updated           200 OK                10.67  │
│ 2    o034  "c04"         updated           200 OK                10.67  │
│ 3    o051  "c06"         skipped: changed  412 Precondition Fa…   1.24  │
│ 4    o068  "c08"         skipped: gone     404 Not Found          1.00  │
╰─────────────────────────────────────────────────────────────────────────╯
 prod ▪ sales.orders ▪ updated ▪ 412 items ▪ 4,322.40 RU ▪ 11.2s        ? help
```

- Outcomes: `updated`, `skipped: changed`, `skipped: gone`, `skipped: no partition
  key`, `failed`, `unknown`, `not attempted`. `Raw` for a row is `{"id",
  "partitionKey", "outcome", "status", "requestCharge", "etag"}`.
- The banner and the badge say `updated`, `stopped` or `failed`; a run with a failed or
  unknown row uses `theme.ErrorStyle()` and opens with the cursor on the first such
  row, as 17's rollback does.
- The charge is the selection plus every write, and the status bar splits it:
  `4,322.40 RU (selection 6.18 + writes 4,316.22)`, through `Stats.LeafCharges`.
- **Row cap.** The engine keeps one outcome and one charge per target, which is
  nothing. The report renders at most `mutate.MaxReportRows` (10 000) rows: every
  failed, unknown and skipped item first, then applied ones in order. Past the cap the
  banner says `showing 10,000 of 250,000 rows: every item that was not updated, then
  the first updated ones. The log names the rest`. Totals always cover every item.
- An `unknown` row carries 17's advice as its status text: `no answer: check before
  assuming. Running the statement again settles it`.

## The job

Iteration 18's model, used as it stands: a chain of `tea.Cmd`s, one message per step,
the command in flight owning the engine value, `Update` never blocking, no goroutine
the model does not know about.

```
enter → applyChunk ─ MutationChunkAppliedMsg → applyChunk … → MutationFinishedMsg
        any step ─ MutationFailedMsg → ended-short view
```

- A step is one **chunk**: `mutate.ChunkSize` (100) targets handed to 18's
  `writers.Pool` as one `Run`, returning when each has an outcome. The probe is a `Run`
  of one write; the first chunk proper is the next 99.
- Every message carries a `jobID` and the account. A message for a job that is no
  longer current is dropped.
- **The writer pool is `internal/writers`**, specified in 18 ("Why a worker pool, and
  how it stays honest") and not described again here: `NewPool(size, clock)`, `Run(ctx,
  []Write) []Outcome`, the shared throttle gate, the step-down that never steps back
  up, `writers.MaxThrottles`, the injected clock. A job makes one `Pool` and keeps it,
  so a size stepped down in chunk 3 stays down. Whichever of 18 and 21 lands first
  writes the package; the other imports it.
- **One `writers.Write` per item edit.** The closure calls `ItemEditor.EditItem`,
  records the target's outcome in the slot the job owns for it, and returns the charge.
  It returns an error only for a `*adapter.ThrottledError`, which is the pool's to
  retry. Every other result — applied, either skip, a refusal, an unknown outcome — is
  an outcome of that *item* and returns `nil`, because `Run` ends the step on any other
  error and one failed item must not stop its neighbors. A throttle that outlasts
  `MaxThrottles` comes back in `Outcome.Err` and ends the step, resumable. A write the
  pool never started comes back as `writers.ErrNotStarted` and is `not attempted`.
  `MaxFailures` and `MaxUnknown` are judged by the job between chunks, over outcomes in
  target order.
- **The pool size is 18's `writers` profile key** (`Profile.Writers`, `Account.Writers`,
  `writers.DefaultSize` 4, clamped to `writers.MaxSize` 16): one knob for every write
  job, read from the job's account, introduced by whichever of 18 and 21 lands first.
- `x` cancels the context given to `Run`, which by the pool's contract starts no new
  write and lets the ones in flight finish. The write itself does not run under that
  context: each `Write` derives its own with `context.WithoutCancel` and a deadline,
  `mutationWriteTimeout` (30 s, 17's `batchTimeout`), because cancelling a sent write
  is how an unknown outcome is made. Each step has `mutationStepTimeout` (two minutes).
- **Resume** (`r`) continues with the targets that have no outcome. The list is in
  memory, so it is exact within the session. Across sessions the resume is running the
  statement again.
- **The rest of the UI stays usable**, switching accounts included. The job holds its
  `adapter.Connection` and account name from the confirmation and never asks which
  account is active (18).

### One background job per session

18 owns the rule and its mechanism ("One background job per session" there): a clone,
a capture, an update and a delete are the same kind of thing, and the root model has
one slot for them, `Model.job{kind, id, label, accounts, target, cancel}` in
`internal/tui/job.go`, with kinds `jobClone`, `jobCapture` and `jobMutation`, one
`StatusBar.SetJob(label)` field, one quit guard and one switcher `x` guard. An update
and 22's delete are both `jobMutation`; the label tells them apart. Whichever of 18,
19, 21 and 22 lands first introduces the slot; the others register a kind.

| While this runs | Refused | With |
|---|---|---|
| an update or delete job | `y` (clone), `s` (snapshot), another update or delete | `an update is running: clones wait for it (w)` and its mirrors |
| a clone | `ctrl+r` on an update or delete, **before** the dry run spends RU | `a clone is running: updates wait for it (y)` |
| a capture | the same | `a snapshot is running: updates wait for it (v)` |

Queries, catalog browsing, history, exports and reading snapshot stores stay available
throughout.

**What the job writes.** The slot's `job.writes() (account, path, ok)` reports, for a
mutation, its account and `[database, container]`, and `job.writesTo(account,
container)` is the comparison everyone asks. Two consumers matter here. Iteration 11's
`d` guard refuses a delete of that container or its database. Iteration 17's
`startBatch` refuses a transactional batch **into that container** with `an update is
writing sales.orders: the batch waits for it (w)`: the review described those items,
and a batch rewriting them mid-job would make the job's conditional writes skip for a
reason the user caused by accident. Every other batch runs beside the job, as 17
allows, and a chunk message that arrives while one is `committing…` is processed as
usual.

Also refused while an update runs: `x` in the switcher on the job's account (`an
update is using prod: stop it first (w in the catalog)`), through `job.accounts`.
11's `t` on the target is allowed: raising throughput mid-run is the fix for a slow
one.

**Quitting.** The first `q` or `ctrl+c` opens the progress view with `An update is
running. Quit again to stop it and quit; items already updated stay updated.` The
second cancels the job, records the history entry synchronously, logs the counts and
the ids whose writes were still in flight (at most `Writers` of them, and their outcome
is unknown), and quits through `Model.quit`.

### Which account

The account active when `ctrl+r` was pressed, recorded as `runAccount` at that moment,
as 17 has it. The dry run, the read-only check, the review, the job, the report's
title and the history entry all come from that one name.

- The review is an overlay, and overlays swallow `ctrl+g`, so the account cannot move
  between the dry run and the confirmation through the keyboard.
- While `selecting…`, a switch is possible. The selection's messages carry its account,
  and the review that opens names **that** account, in its title line and its first
  row, not the one now on screen. The job binds to that account's connection.
- 14's third way, a connection arriving while no account is active, cannot apply:
  with no active account `ctrl+r` was refused with `errNoAccount`.

### Read-only accounts

17's `read_only`, enforced in the one place 17 enforces it. `Model.batcher()` hands
out a `Batcher` only for a writable account; `Model.itemEditor()` does the same for
`ItemEditor`, through the same `Account.ReadOnly`. The refusal is 17's text and is
recorded:

```
prod is read-only, so nothing was written. To allow writes on this account:
  alchemist profile set-read-only prod false
```

It is checked **before the dry run**. A read-only account is refused without spending
RU on a selection it could never act on. To see what a statement would match on a
read-only account, run the `WHERE` as a `SELECT`; that is what the editor is for.

## Adapter contract

Reused unchanged: 17's `Operation`, `OperationKind`, `OperationResult`,
`PartitionKey`, `ErrWriteOutcomeUnknown`; 18's `ItemScanner`, `ScanRequest`,
`ItemScan`, `ItemPage`, `ThrottledError`, `PartitionKeyValues`, `ErrNoPartitionKey`;
19's `ScanProjection`, `ScanIdentity`, `SplitSystemFields`, `IsSystemField`,
`ItemMeta`. Whichever
iteration lands first introduces each, to its owner's text.

Added, two things.

```go
type ScanRequest struct {
    // ...Container, From, PageSize (18); Since, Projection (19)

    // Filter keeps only the items it matches. The zero value keeps everything.
    Filter ScanFilter
}

// ScanFilter is a predicate in the backend's own query language, evaluated by
// the backend. Alias is the name Predicate calls an item.
type ScanFilter struct {
    Alias     string
    Predicate string
}
```

**Why a filtered scan and not `Connection.Query`.** The selection is a query in the
sense that the service evaluates a `WHERE`, and `Connection.Query` with `SELECT o.id,
o._etag, o.customerId FROM o WHERE …` would return the same bytes in `Page.Raw`. It is
still the wrong door, for the reasons 19 gave and one more:

- The mock answers any query text with canned pages. A selection through `Query`
  could not be tested without the emulator, and neither could anything after it.
  Through `ScanItems` the mock reads its own item store (17, 18, 19) and applies a
  predicate a test registered (below).
- The identity projection over nested and hierarchical key paths already exists once,
  in the Cosmos adapter's `scan.go` (19). `internal/query` writing a second one would
  be the duplication 19 avoided.
- `ItemPage` is unrendered items with a charge; `Page` would render ten thousand rows
  of cells nobody reads.
- `Filter` composes with `Since` by `AND`, and is the field 18's out-of-scope
  "filtered clone" needs.

In Cosmos: `SELECT <projection> FROM <Alias> WHERE (<Predicate>)`, `AND`-ed with
`Since` when both are set. The alias is passed because the predicate is the user's text
and names items by it.

```go
// ItemEditor applies one operation to one existing item. Optional: callers
// find it with a comma-ok type assertion.
type ItemEditor interface {
    // EditItem runs a patch or a delete; any other kind is ErrUnsupported. It
    // never retries. The result's RequestCharge and Status are set whenever
    // the backend answered, error or not.
    EditItem(
        ctx context.Context, container []string, key PartitionKey, op Operation,
    ) (OperationResult, error)
}

var (
    // ErrPreconditionFailed means the item no longer satisfies the
    // operation's Condition or IfMatch. Nothing was written.
    ErrPreconditionFailed = errors.New("item changed since it was selected")
    // ErrItemNotFound means the item is gone. Nothing was written.
    ErrItemNotFound = errors.New("item not found")
)
```

- **17's `Operation` is the unit**, not a new type: `Kind`, `ID`, `Body` (the patch
  array), `Condition`, `IfMatch` are exactly a single-item patch or delete. An update
  sets `Condition`; 22's delete sets `IfMatch`.
- **Errors are the contract.** `nil`: applied. `ErrPreconditionFailed`,
  `ErrItemNotFound`: not applied, and the engine's two `skipped` outcomes.
  `*ThrottledError`: not applied, retry after the delay. An error wrapping
  `ErrWriteOutcomeUnknown`: may have been applied. Anything else: refused, not applied.
  The engine and the TUI name no status code; `Status` arrives pre-rendered, as in 17.
- `ErrWriteOutcomeUnknown` is 17's sentinel, named for any write with no answer: a
  batch there, a single item here.
- One method with the container as an argument, not 18's open-then-write pair: a sink
  reads the key paths once when it opens, and here the key arrives with every call.
- `CheckMutation` asks 19's `adapter.IsSystemField(name string) bool`, which reads the
  list `SplitSystemFields` uses, so `internal/query` never spells the names.

### Cosmos

`internal/adapter/cosmos/edit.go`:

- `NewContainer(container[0], container[1])`. The three helpers every write path
  needs are 17's, in `internal/adapter/cosmos/write.go`: `partitionKey` folds the key,
  `withoutRetries(ctx)` turns azcore's retries off, `writeError(op, err)` classifies a
  failure. 17 landed first and wrote that file; its tests live in the external test
  package, so two of the three are exported as `cosmos.PartitionKey` and
  `cosmos.WriteError`.
- A patch body maps entry by entry onto `AppendSet` and `AppendRemove` (and the rest,
  for 17's sake). **Values are passed as `json.RawMessage`, never decoded:** a decoded
  `null` is a nil `any`, which the SDK's struct tag drops from the payload, turning
  `SET o.x = null` into a malformed `set`. Raw bytes also keep a number's digits.
- `Condition` goes to `SetCondition` JSON-escaped, quotes stripped, which is 17's fix
  for the SDK's `%s`. The test that captures the request body covers a predicate
  containing `"` and `\`.
- `IfMatch` becomes `ItemOptions.IfMatchEtag`. `EnableContentResponseOnWrite` stays
  false.
- Called under `withoutRetries(ctx)`, as 17's batch is. A 429 therefore arrives at
  once and becomes `*ThrottledError` with the delay read from the response, which is
  what lets the pool own the backoff.
- Status 412 → `ErrPreconditionFailed`; 404 → `ErrItemNotFound`; everything else goes
  through `writeError`, so sent-and-unanswered wraps `ErrWriteOutcomeUnknown` by the one
  definition the adapter has, and never through `wrap`'s `adapter.Unreachable` branch.
- `scan.go` grows `Filter`.

### Mock

On the item store 17, 18 and 19 build (`WithItems`, `PutItem`, `DeleteItem`,
`WithClock`):

- `WithPredicate(predicate string, matches func(item json.RawMessage) bool)` registers
  what a predicate text means. `ScanItems` with a `Filter`, and `EditItem` with a
  `Condition`, look the text up; an unregistered predicate is an error that names it.
  The mock does not parse SQL, a test says in Go what its `WHERE` means, and the same
  function serves the selection and the per-write re-check, which is the property under
  test.
- `EditItem` applies a patch with 17's patch code (top-level paths) or a delete to the
  store, under the store's lock. A missing id is `ErrItemNotFound`; a false predicate
  or a stale `IfMatch` is `ErrPreconditionFailed`, from real store state, so a test
  changes an item with `PutItem` between the review and the confirmation and gets a
  real skip. Applied writes bump the version; charges are fixed per kind.
- Injection: `WithEditConflictAt(k)`, `WithEditErrorAt(k)`, `WithEditUnknownAt(k)`
  (applies, then reports unknown), 18's `WithThrottleAt(k, retryAfter)`, `OpEdit` for
  `WithError`, and 18's recording hook for the highest concurrent call count.

## The engine

`internal/mutate` is pure: it imports `internal/adapter`, `internal/query` (for
`query.Mutation`) and `internal/writers`, no bubbletea and no concrete adapter. Plain
names; nothing thematic.

```go
type Selection struct{ /* scan in progress, targets so far */ }

func Select(
    ctx context.Context, scanner adapter.ItemScanner, m query.Mutation,
    keyPaths []string, limit int,
) (*Selection, error)
func (s *Selection) Next(ctx context.Context) (SelectionProgress, error) // one page
func (s *Selection) Targets() Targets                                    // when done

type Target struct {
    ID      string
    Key     adapter.PartitionKey // nil: no partition key, never written
    Version string
    Absent  uint16 // bit i: UNSET path i is not on this item
}

type Job struct{ /* targets, outcomes, position */ }

func NewJob(
    m query.Mutation, targets Targets, editor adapter.ItemEditor, writers int,
) *Job
func (j *Job) ApplyChunk(ctx context.Context) (Progress, error) // ctx is the stop
func (j *Job) Done() bool
func (j *Job) Summary() Summary

func Preview(item json.RawMessage, m query.Mutation) ([]FieldChange, error)
func Confirmation(m query.Mutation, container string, count int) string
func NewReportCursor(j *Job, pageSize int) adapter.Cursor
```

`mutate` builds the `adapter.Operation` for a target: the patch array minus the absent
`UNSET`s, and the condition `FROM <alias> WHERE (<where>)` with `AND
IS_DEFINED(<path>)` for each `remove` it kept, so a path removed since selection is a
skip and not a failure. `Confirmation(m, container, count) string` is the text the
review expects, decided in one place for this iteration and the next.

## Scope

- `internal/adapter/adapter.go` — `ScanRequest.Filter`, `ScanFilter`, `ItemEditor`,
  `ErrPreconditionFailed`, `ErrItemNotFound`.
- `internal/adapter/cosmos` — `edit.go`, on 17's `write.go`; `scan.go` learns `Filter`;
  compile-time check `_ adapter.ItemEditor = (*connection)(nil)`.
- `internal/adapter/mock` — `WithPredicate`, `EditItem`, the injection options.
- `internal/writers` — 18's package, which 18 wrote; imported here.
- `internal/query`
  - `mutation.go` — `IsMutation`, `ParseMutation(text) (Mutation, error)`,
    `Mutation{Kind, Target, Alias, Assignments, Removals, Where, EveryItem}`,
    `MutationKind` (`MutationUpdate`; 22 adds `MutationDelete`), `Assignment`,
    `FieldPath` with `Pointer()`, `*MutationSyntaxError`, `ErrMutationUnsupported`.
  - `mutation_check.go` — `CheckMutation(m, keyPaths) MutationCheck{Problems,
    Warnings}`, `MaxPatchOperations`, the partition-pin warning.
  - `spans.go` — `UPDATE`, `SET`, `UNSET` highlighted through a `mutationKeywords` set
    of their own, as 17 keeps `batchKeywords` out of `keywords`: `SET` can be an alias.
- `internal/mutate` (new, pure) — as above, plus `ErrTooManyTargets`, `ChunkSize`,
  `MaxFailures`, `MaxUnknown`, `MaxReportRows`, `PlanningChargePerPatch`,
  `SelectionPageSize` (1000).
- `internal/config` — `max_mutation_items` on `Profile` (`omitzero`, validated
  positive). 18 brought `writers`.
- `internal/history` — `Entry.Kind` gains the value `update`. See "History".
- `internal/tui`
  - `mutation.go` (new) — `startMutation`, `selectTargets`, `reviewMutation`,
    `confirmMutation`, `applyChunk`, `finishMutation`, the guards, and the refusals
    `errNoEditSupport` ("this adapter cannot update items"), `errJobRunning`.
    `Model.itemEditor()` is the single place an `ItemEditor` is handed out.
  - `app.go` — `startRun` branches on `query.IsMutation` after `query.IsBatch` and
    before `resolvePlan`; `overlayMutationReview`, `overlayMutationProgress`; a
    `runSelecting` run state; `job.go` with the slot if no earlier iteration brought it,
    and the `jobMutation` kind.
  - `messages.go` — `TargetsSelectedMsg`, `TargetPageMsg`, `MutationChunkAppliedMsg`,
    `MutationFinishedMsg`, `MutationFailedMsg`, each with account and `jobID`;
    `OpMutation`.
  - `keys.go` — `ShowMutation` (`w`) in the Catalog section, disabled without a job;
    `MutationReviewKeys()` and `MutationProgressKeys()` for the hint lines.
  - `history.go` — `newHistoryEntry` sets `Kind`; `recall` skips `setScope` for it.
- `internal/tui/panes` — `MutationReview`, `MutationProgress`; `StatusBar.SetJob` with
  the slot; a `selecting…` progress label; an `items` label in place of `rows`;
  `History` shows an `update` tag as it shows `batch`.
- `cmd/root.go` — passes `max_mutation_items` and `writers` into `Account`.
- `test/seed` — nothing new.
- `README.md` — "Updating by query": the grammar, what is refused, the semantics
  section in short, the cap, the keys.

### History

One log. An update entry has `Kind: "update"`, the statement as `Query`, the target as
`Scope`, the number of items updated as `Rows`, selection plus write RU, and `OK` when
the job finished with no failed and no unknown item; skips are expected and do not make
a run a failure. Otherwise `Error` says what: `stopped after 169 of 412`, `3 failed, 1
unknown`. Recorded when the job ends, however it ends, including on quit. Refusals are
recorded — syntax, checks, read-only, no support, the cap, a job already running — as
every refusal has been since 10. A review closed with `esc` is not a run and is not
recorded (17). Zero targets is recorded, as a success with `Rows` 0.

Recall leaves the catalog scope alone for this kind, as for a batch.

### Recall never bypasses the review

One door: `startRun` → `startMutation` → dry run → review → `confirmMutation`, which
is reachable only from the review's `enter` with the text matched, and `applyChunk` is
issued from nowhere else. `Model.rerun` (history) and 15's `rerunSaved` both end in
`startRun`, so `ctrl+r` in either overlay closes it and runs the dry run on the active
account. A rerun always selects afresh: a target list is never reused across runs.

## Out of scope

- **Expressions in `SET`** (`o.total = o.total * 1.1`). A patch cannot read a field, so
  this is read, compute client-side, replace with `If-Match`, retry on conflict: a
  client-side expression evaluator, a different guard, a different review, and a write
  that is not idempotent. It is a separate mode and a separate plan.
- **Increment**, for the idempotency reason above, and because `AppendIncrement` takes
  an `int64` only. `move`, array append and insert.
- **Targets chosen by another container** (a join or CTE picking the items; the
  anti-join from iteration 20 is the compelling case). The selection would run through
  `query.Engine` and produce joined rows, not items of the target; mapping them back
  is its own design. v1 refuses with the message in "Refused" and its `IN (…)` hint.
- Changing `id` or a partition key value, which is a delete and a create.
- Transactional grouping of the writes, stored procedures, and any server-side bulk
  mode. The Go SDK has no bulk executor.
- Upsert by query, `INSERT … SELECT`, `MERGE`, copying between containers (18).
- Undo. The report says what was written; 19's snapshot and diff say what it was.
- Resuming a stopped job after the session ends, other than by running the statement
  again. A persisted target list would need a story for a container that changed.
- An RU ceiling that stops the job, low-priority requests, throughput buckets.
- `max_mutation_items` past what fits in memory: an on-disk target list is not built
  until someone has that statement.

## Relationship to other iterations

- **7, history.** One log; `Entry.Kind` (17's field) gains `update`. Recording rules
  above.
- **10, cross-container.** `BuildPlan` and `Engine` are untouched; `IsMutation` is
  checked ahead of them. The report is a synthetic cursor like the union and join
  cursors. `splitConjuncts` is reused read-only for one warning. Refusals are
  recorded by 10's rule.
- **11, catalog management.** `panes.Confirm` is the confirmation widget; `d` is
  refused on a job's container; `read_only` governs both.
- **12, info view.** None beyond the optional-interface pattern.
- **13, autocomplete.** `UPDATE` at statement start; databases then containers after
  it; `SET`, then `UNSET` and `WHERE` where the grammar allows them; the target's
  observed fields after `SET`, `UNSET` and in the `WHERE`, **never `id` or a key path
  after `SET` or `UNSET`**, since completion must not offer what is refused; `TRUE`,
  `FALSE`, `NULL` after `=` and no field or function there. Report pages and selection
  pages never feed `complete.Index`. If 13 lands second, these are part of its step 1.
- **14, multiple accounts.** "Which account" above. `ItemEditor` is asserted per
  account on `AccountConnectedMsg` and kept in `accountSet`; `Account` gains
  `MaxMutationItems` and `Writers`; every message carries the account and job messages
  are never dropped for being from a background account; the switcher's `x` is refused
  on the job's account.
- **15, saved queries.** An update saves like any text, with **no scope**: it names
  its own target. `savedScope` asks `query.IsMutation` beside `query.IsBatch`, before
  `BuildPlan`, for 17's reason: `BuildPlan` refuses the text, and a refused text is
  saved as a draft *with* the current scope. Recall and run ends in `startRun`.
- **16, multi-way joins.** None.
- **17, transactions.** Hard dependency. Reused: the statement model, `ctrl+r`, the
  review-then-typed-name rule and its widget, `read_only` with its one enforcement
  point, `runAccount`, the unknown-outcome rule, `ErrWriteOutcomeUnknown` and
  `write.go`'s helpers, the batch-beside-a-job rule through `job.writesTo`, retries off
  for writes, `Operation`/`OperationResult`/`PartitionKey`, the escaping fix, the mock's
  item store and patch code, `Entry.Kind`, the report-as-a-page approach. Different on
  purpose: a batch blocks keys for the seconds it is in flight, a job does not block for
  minutes; a batch is atomic, this is not, and both say so.
- **18, cloning.** Hard dependency. Reused: the job model, the progress view's shape and
  keys, the status bar field, the quit and switcher guards, `ItemScanner`,
  `ThrottledError`, `PartitionKeyValues`, `internal/writers`, the `writers` key, the
  `job` slot with `writes`/`writesTo`. `ItemSink` is not used: an upsert of a whole item
  is the wrong write here. **18 has landed** (see its "Implementation notes"):
  `internal/writers` is there, with `NewPool(size, clock)` taking the clock
  (`writers.SystemClock` in production), `ErrNotStarted`, `DefaultSize`, `MaxSize` and
  `MaxThrottles`; `Profile.Writers`, `Account.Writers`, `profile add --writers`, and the
  TUI's `writersFor(account)` mapping zero to `DefaultSize`; the `job` slot in
  `internal/tui/job.go` with `jobNone` and `jobClone` (register `jobMutation`), no
  `label` field, `writesTo` true in either direction of the path, and the refusal
  texts per kind in `usingText` and `writingText`. `adapter.IsSystemField` and
  `SplitSystemFields` exist, in `internal/adapter/system.go`. A create refused as a
  conflict is `adapter.ErrAlreadyExists`, and `adapter.ErrItemRefused` marks what a
  sink refuses for the item's own sake.
- **19, snapshots.** Soft. `ScanIdentity` and `SplitSystemFields` are used as they
  stand, with `IsSystemField`; `Filter` is the third field added to `ScanRequest` by
  19's own rule. The one-job rule is shared. The review's snapshot line is
  `snapshot.Newest`, and the preview's rows are drawn by 19's renderer, when 19 has
  landed. If 19 has not, step 1 introduces `Projection` to 19's text.
  **19 has landed** (see its "Implementation notes"): `ScanRequest.Since`,
  `Projection`, `ScanIdentity` and `adapter.ReadItemMeta` are there, Cosmos builds the
  identity projection as a `SELECT VALUE {…}` object literal (`cosmos.IdentityProjection`),
  and the mock honors both fields. `snapshot.Newest` takes a `snapshot.Location` whose
  `Root` is `Options.Snapshots`. The structural diff is `snapshot.Structural`, whose
  `FieldChange` is `{op, path, value, before}`, and the line renderer is
  `panes.ItemDiff`. The job slot has `jobCapture`, whose status bar field is set from
  the capture's state, and `job.waitText` words a refusal (`a snapshot is running:
  updates wait for it (v)`); `warnBeforeQuit` lives in `job.go` and switches on the kind.
- **20, CTEs and join types.** If its descent parser has landed, `ParseMutation` is
  written on its token helpers (`parseFieldRef` is most of `path`); the condition stays
  an opaque token range either way. No dependency. A `WITH` before `UPDATE` is refused.
- **22, delete by query.** Built on this iteration; adds a statement kind, a guard and
  a stricter confirmation.

## Steps

Each step ships and leaves `make all` green. No write path exists before step 6.

1. Contracts: `ScanRequest.Filter`, `ItemEditor`, the two sentinels; the
   mock's `WithPredicate`, filtered `ScanItems`, `EditItem` and injections, tests
   first. `internal/writers`, `write.go` and the `writers` key where 17 and 18 have
   not brought them.
2. `query.ParseMutation`, `IsMutation`, `CheckMutation`: grammar tables and fuzz.
   Pure; ships nothing visible.
3. `mutate.Select`, `Targets`, the cap, `UNSET` reduction, `Preview`. Pure.
4. TUI dry run: the `startRun` branch, read-only and one-job refusals, `selecting…`,
   the review pane with its confirmation — and `enter` wired to a notice, `writing is
   not built yet`. **Ships: a dry run that says what a statement would touch.**
5. `mutate.Job`: chunks, the probe, outcomes, throttle, stop, resume,
   `NewReportCursor`. Pure, against the mock, under `-race`.
6. TUI job: progress view, `w`, the `job` slot and its guards, quit, report, history.
   **Ships: updates against `--adapter mock`.**
7. Cosmos `EditItem` and filtered scan, the request-capture tests, emulator
   integration. **Ships the feature.**
8. Highlighting, the snapshot line, README, help sections, plan statuses.

## Testing

**Unit — `query.ParseMutation`:** every path form and its pointer, including `~` and
`/` in a bracketed name; every value kind; both quote styles; `SET` with `UNSET`,
`UNSET` alone; alias present, absent, with `AS`; a condition containing `;`, `SET` and
`--` inside strings; a trailing `;`. One row per line of the "Refused" table, with line
and column for the syntax ones. `IsMutation` is false for every query in the planner's
corpus and for every batch. Fuzz: no input panics, and a statement that parses formats
back (`Mutation.String()`) to one that parses equal.

**Unit — `query.CheckMutation`:** one row per check, at the limit and one past it; all
problems reported; nested and hierarchical key paths, parent and child overlaps; the
pin warning present for `o.total < 50`, absent for `o.customerId = "c01" AND …`, present
under a top-level `OR`.

**Unit — `mutate`** (mock adapter, `Writers` 1 and 8, `-race`):
- Selection pages to the end; the cap plus one yields `ErrTooManyTargets` and no
  target list; zero matches; a key-less item is kept and marked.
- `UNSET`: an item with the path gets the `remove`, one without does not, one left
  with no operation is dropped and counted.
- Every target gets exactly one `EditItem` with the condition `FROM o WHERE (…)`.
- An item changed with `PutItem` after selection so it no longer matches is `skipped:
  changed` and the store shows it untouched; one changed in another field is updated
  and keeps that change; a deleted one is `skipped: gone`.
- The probe: `WithEditErrorAt(0)` ends the job after one call; the same at item 50
  does not.
- 101 failures end the job; three consecutive unknowns end it; an unknown is never
  retried (call count); `WithEditUnknownAt` items are `unknown` in the report.
- `WithThrottleAt(k)`: retried after `RetryAfter` on a fake clock, writers step down,
  totals exact. Concurrent calls never exceed `Writers`.
- Stop after chunk 2 of 5: no call starts after the context is cancelled, a write in
  flight still sees a live context, in-flight ones complete,
  `Summary` counts `not attempted`; `r` finishes with each target written once.
- **Idempotent rerun:** run, then select and run again: the second selection is empty
  when `SET` falsifies the `WHERE`, and otherwise rewrites to byte-identical bodies.
- The report: non-applied rows first past `MaxReportRows`; totals cover everything;
  `Raw` shape; pages of `page_size`.

**Unit — cosmos (no network):** the captured patch body for `SET … = null`, `false`,
`0`, `""`, a 20-digit number and an object; a condition with `"` and `\`; 412, 404,
429 with a delay header, 408, 503, a dial failure, each to its error; one request on a
503 (counting transport); the filtered scan's query text for plain, nested and
two-path keys, with and without `Since`.

**Unit — `config`:** `max_mutation_items` default and validation.

**TUI, mock adapter** (`internal/tui/test/mutation_test.go`):
- `ctrl+r` on an update issues scans and zero `EditItem` calls, then opens the review
  with the account, container, condition, count and preview.
- `enter` with an empty, wrong-case or trailing-space name writes nothing; with the
  name it starts the job. `WHERE true` needs name and count, and the name alone does
  nothing.
- `esc` in the review writes nothing, records nothing, leaves the buffer.
- History recall-and-run and (with 15) saved recall-and-run each end in a fresh dry run
  and a review with zero writes; the same statement confirmed a minute ago asks again.
- A read-only account is refused before any scan, and the refusal is recorded. No
  `ItemEditor`: `errNoEditSupport`. No active account: `errNoAccount`.
- A refused statement lists every problem, reaches no adapter, is recorded.
- The cap: refusal text names the key and the profile; zero writes.
- Progress: `esc` hides, the status bar shows the field, a query runs meanwhile, `w`
  reopens from the catalog on any row and on another account; `ctrl+g` does nothing
  with the view open. `w` with no job does nothing and is absent from help.
- A job that ends while hidden leaves `update done (w)`; closing the ended view loads
  the report; `enter` on a row opens detail; `ctrl+e` exports.
- Two accounts: a job confirmed on the first writes only to the first mock while the
  session is on the second; the entry's `Profile` is the first; `x` in the switcher on
  the first is refused, on the second works.
- A switch during `selecting…`: the review names the account the run started on.
- With an update running on `sales.orders`, a 17 batch into `sales.orders` is refused
  with the `w` notice and one into `sales.archive` commits.
- One job: with a job running, `y`, `s` and a second update are refused with their
  notices; with a clone running, `ctrl+r` on an update is refused before any scan.
- `q` once opens the warning, twice quits with the entry recorded as stopped.
- `x` then `r` reaches the full count with each item written once.
- An ordinary query and a 17 batch behave as before; the earlier suites pass unedited.
- Every new binding is grouped exactly once and appears in help and the README.

**Integration** (emulator, `//go:build integration`,
`test/integration/mutation_test.go`, in a scratch copy of seeded `sales.orders` on
`/customerId`, removed in `t.Cleanup`): the Goal's statement updates exactly the
`shipped` orders under 50 and a query sees `archived` on them and nowhere else; a
second run selects none; `UNSET o.archivedAt` on a mix of items with and without it; an
item replaced between selection and write so it no longer matches is skipped; a
two-path hierarchical key; an id containing `"`; `SET … = null` stores `null`. Charges
are asserted non-negative only. The first test probes one conditional patch and, on a
refusal that is not about the item, skips the file with `emulator image does not serve
conditional patch: <status>`, as 17 does for batch. `telemetry.events` (300 items)
with `WHERE true` and `Writers` 8 exercises the pool.

**Manual checklist** (after `make emulator-seed`):
- [x] The Goal's statement: the review shows the count and a before → after; type
      `orders`; the report lists every item; `SELECT … WHERE o.status = "archived"`
      agrees.
- [x] `ctrl+r` again: `No item … matches`.
- [ ] Remove the `WHERE`: refused with the `WHERE true` hint. With `WHERE true`: the
      prompt asks for `orders 60`.
- [ ] `SET o.customerId = "x"`: refused, naming the key; nothing read.
- [x] `telemetry.events` with `WHERE true`: `esc`, run a query, `w`, `x`, `r`.
- [x] Edit a matching item in another terminal while the review is open: the report
      shows it `skipped: changed`.
- [x] A non-local profile with no `read_only`: refused with the command that lifts it.
- [ ] 80×24: review and progress fit; the confirmation field stays visible.
- [ ] Against a real account, once: a 400 RU/s container throttles, writers step down,
      the run finishes, and the portal's RU matches the report's.

## Acceptance criteria

- `ctrl+r` on an update never writes. No write reaches an adapter before a review the
  user completed by typing the container name (and the count, for `WHERE true`), on an
  account that is not read-only, from any entry point including recall.
- The review names the account, the container, the condition, the exact number of
  items, a before → after sample, the selection's cost, and every warning.
- Every write carries the statement's `WHERE` as a server-side condition; an item that
  no longer matches is skipped and reported as such, not written and not failed.
- A write is sent once. Only a throttle is retried. An unknown outcome is reported as
  unknown.
- Every target ends in exactly one reported outcome, totals cover every target whatever
  the row cap, and a stopped run says what was and was not attempted.
- More matches than `max_mutation_items` is a refusal with nothing written.
- At most one background job per session across cloning, snapshots and mutations; the
  job survives account switches and is bound to the account it was confirmed on.
- Queries and batches are byte-for-byte unaffected.
- `Update` never blocks. `internal/mutate` imports no bubbletea and no concrete
  adapter; `internal/tui` imports the `internal/adapter` interfaces only, and names no
  status code or system field.

## Implementation notes

What landed differs from the text above in these ways. The manual checklist was walked
through on the emulator against scratch copies in `u21_shots` (the ticked items; a
`WHERE true` run over a 2,000-item container stood in for `telemetry.events`, and
`read_only = true` on a local profile for a non-local one); the real-account throttle
run and the 80×24 pass by eye were not done, though a TUI test checks that the review
and the progress view keep their prompt and keys at 80×24.

- **Mock injections are keyed by item id**, not by call index, following 18's
  `WithThrottle(id, …)` and `WithWriteError(id)`: `WithEditConflict(id)`,
  `WithEditRefusal(id)`, `WithEditUnknown(id)` (applies, then no answer) and
  `WithEditThrottle(id, times, retryAfter)`. With several writers a call index names no
  particular item. `OpEdit` fails every edit through `WithError`;
  `HighestConcurrentEdits()` and `EditedIDs()` are the recording hooks.
- **The mock reads one condition shape**, the one `mutate` sends:
  `FROM <alias> WHERE (<predicate>)`, then `AND IS_DEFINED(<alias>.<field>)` per kept
  `UNSET`, over top-level fields, as its patch is. The predicate is looked up among
  those `WithPredicate` registered; an unregistered one is an error naming it.
- **`mutate.NewJob(m, targets, editor, pool *writers.Pool)`** takes the pool, not a
  size, so a test hands it a fake clock; the TUI builds it from `writersFor`.
  `mutate.Confirmation(m, count)` reads the container from `m.Target`.
  `NewReportCursor` returns `*mutate.ReportCursor` (an `adapter.Cursor`) with
  `Banner()`, `FirstProblem()` and `Summary()`. `mutate.Changes(m)` words the review's
  "Each gets" line. `mutate.DefaultMaxTargets` (10,000) is the cap's default.
- **The probe lasts until the service has taken a write.** The job writes one target
  at a time until one is applied or refused on its condition (a 412 proves the
  predicate was evaluated); a first target `skipped: gone` proves nothing. A refusal
  while probing ends the job with `mutate.ErrProbeRefused`, whose text carries the
  `BEGIN BATCH … IF MATCH` advice. `MaxFailures` and `MaxUnknown` are judged since the
  job started or last resumed (`Job.Resume`). A write throttled past
  `writers.MaxThrottles`, or stopped during a throttle's pause, stays `not attempted`
  for the resume.
- **Report pages are 100 rows** (`reportPageSize`): the profile's `page_size` is an
  adapter setting the TUI never sees. The first page carries the whole report's
  statistics, later pages none, so appending pages counts every item once.
- **Preview rows** are `mutate.FieldChange{Kind, Path, Before, After}`, drawn by the
  review itself. 19's `panes.ItemDiff` is a whole-item line-diff overlay, and
  `snapshot.FieldChange` has no "already that value" operation, so its renderer does not
  fit; the rows take the dozen lines the plan allows.
- **`*query.MutationSyntaxError` carries `Err`**, set to `ErrMutationUnsupported` for the
  shapes the "Refused" table calls unsupported, so every refusal keeps its line and
  column; the text leaves the sentinel out. `query.MutationTarget(text)` lets the TUI
  refuse an account-qualified target with 14's text before parsing. `IsMutation` is
  also true for `WITH … UPDATE`, which the parser then refuses. `MutationKind` has
  `Applied()` ("updated") and `Ongoing()` ("updating") for 22 to extend.
- **The parsers share `statementReader`**, extracted from 17's batch parser;
  `splitConjuncts` and `topLevelBreakers` became free functions for the pin warning.
  The TUI's `lookupContainer` is 17's `checkBatch` lookup, shared by both statements.
- **Cosmos.** An integer key past 2^53 comes back from `EditItem` as
  `ErrNoPartitionKey`, unsent, and the item is `skipped: no partition key`.
  `cosmos.ScanQuery(request, keyPaths)` is the pure text builder the unit tests pin.
  **Verified on the vNext emulator:** it serves conditional patch, answers a false
  predicate with 412 and a missing item with 404; the integration test pins both.
- **Keys.** Beside `ShowMutation` (`w`), two overlay-only bindings: `StartMutation`
  (`enter start`, `MutationReviewKeys()`) and `ShowReport` (`esc report`,
  `MutationProgressKeys()`). The progress view reuses the clone's `HideClone`,
  `StopClone` and `ResumeClone`, which mean the same there. `w` is enabled only while an
  update holds the slot (`Model.withJobKeys`), so it is absent from help otherwise.
- **The job slot** gained `job.noun` and `job.named()` ("an update"), which fixed the
  article in every refusal text; 17's batch refusal now names the job's own key
  (`(w)`). The refusal of a second update is
  `an update is running on prod/sales.orders: w in the catalog shows it`.
- **History is recorded when the slot is released**: when the ended view is closed, or
  at quit, synchronously. Until then a stopped job can still be resumed, so its outcome
  is not final. `Error` joins `stopped after N of M`, `F failed, U unknown` and the
  step's error. Quit logs the ids of the chunk in flight, read with `Job.Pending()`
  before each step, since the model does not hold the job while a step runs.
- **Read-only** is enforced in `Model.itemEditor()` before the selection, again at the
  confirmation, and again on `r`: a profile turned read-only while a job was stopped
  refuses the resume.
- The status bar's `Progress.Mutation` is a `MutationBadge{Text, Failed}`; a report with
  failed or unknown rows keeps the badge `updated` in the error style. A measured charge
  of zero reads "the backend reported no request units": the TUI cannot tell an emulator
  from an account. The vNext emulator now reports request units.
- **13 landed first**, so its part is here: `UPDATE` is offered at the start of a
  buffer; after `UPDATE`, databases then containers; `SET`, `UNSET`, `AS` and `WHERE`
  where the grammar takes them; the target's fields after `SET`, `UNSET` and in the
  `WHERE`; only `TRUE`, `FALSE`, `NULL` after `=`. `query.Completion.Writable` makes
  `complete.Index` leave out the id, a key path or a field holding one, and system
  fields. Report pages never feed the index: the report has no plan.
- 23 has not landed on this branch, so there is no `Diagnose` to extend; its plan's step
  adds `MutationSyntaxError` there.
- **A WHERE's parentheses must pair up.** The condition is sent as `(<condition>)` with
  more conditions ANDed after it, both as the selection's filter (after `Since` when a
  scan has both) and as each patch's condition. `o.a = 1) OR (true` would escape that
  pair and match every item, so `ParseMutation` refuses any condition whose depth goes
  below zero or does not end at zero: `unbalanced parentheses in the WHERE`. The fuzz
  target checks that every `Where` it accepts is balanced.
- **A nested SET needs its parent** (verified on the emulator: 400 without it). The
  selection reads whole items when any `SET` has more than one step, as for `UNSET`;
  an item lacking the parent (an object for a named field, or for an index an array
  holding that element) is kept as the target outcome `skipped: no parent` and never
  sent, so the probe never blames the `WHERE` for it. An index past an array's end is
  treated the same: there the service appends, with a condition or without, and a
  rerun appends again, which is not idempotent.
- **The patch condition is written in the shapes the vNext emulator takes.** Each
  nested `SET` re-asserts `IS_OBJECT(<parent>)`, or `IS_ARRAY(<parent>)` for an index,
  and each kept `UNSET` `IS_DEFINED(<path>)`, so a parent removed or turned into a
  scalar since the selection is a 412 and `skipped: changed`. Verified refused with
  400 in a condition: a bracketed property (`o["x"]`), an array index (`o.lines[0]`),
  `ARRAY_LENGTH`, and a bare `true` beside an `AND` (`(true) AND IS_DEFINED(o.x)`;
  `(true)` alone is taken). So a guard whose path needs a bracket is left out, the
  selection's check covering it, and for `WHERE true` the condition is the guards
  alone. The mock refuses the same shapes, so the engine's tests catch a regression.
  `CheckMutation` warns of every nested `SET`, the preview marks each path an item has
  no parent for with `!`, and the review counts the items that lack a parent.
- **A comment never reaches the service.** The lexer ends a `--` comment at `\r` as
  well as `\n`, as the service does, and `Where` holds the condition with each comment
  replaced by a space: a comment the service ended where Alchemist did not would carry
  the rest of the line out of the `(<condition>)` wrapper.
- **Mid-job, a read-only account says what the job left:** `prod turned read-only: no
  further item was sent`, before the next chunk and on `r`.
- **Read-only is asked again before every chunk**, not only at the confirmation and on
  `r`: the switcher re-reads the profiles, and one turned read-only mid-job ends the job
  short (`failed`, resumable) before its next chunk.
- **Past `MaxReportRows`, every omitted row is logged** (id, partition key, outcome) as
  the report is built, which is what the banner's "The log names the rest" means.
- `IsMutation` reads a `WITH` buffer's statement as the first `UPDATE` or `SELECT`
  outside parentheses and not after a dot, so `WITH … SELECT c.update …` stays a query.
- **22 has since landed** on this engine (see its "Implementation notes"): `operation`
  switches on `Mutation.Kind`, `Confirmation` asks every delete for the name and the
  count, and the job, selection, report cursor and job slot serve both kinds unchanged.
