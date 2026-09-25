# Iteration 22 — Delete by Query

## Goal

Delete many items with one statement:

```
DELETE FROM sales.orders o WHERE o.status = "cancelled"
```

Cosmos DB for NoSQL has no `DELETE … WHERE`. Alchemist simulates it the way iteration
21 simulates `UPDATE … WHERE`, and as openly: select the matching items with a query,
then delete each one, each delete conditional on the item still being the version that
was selected.

**This iteration is built on iteration 21 and must land after it.** 21 introduces
everything the two share: statement detection in `startRun`, the filtered identity
scan, the dry run, the review overlay and its typed confirmation, the job with its
progress view and `w`, the one-job-per-session slot, the report cursor, the history
kind, `read_only` enforcement, `adapter.ItemEditor`. Read 21 first; this document
spells out only what differs, which is four things: the grammar, the per-item guard,
the confirmation, and what the review has to say about a write that cannot be patched
back.

The safety goal is 17's and 21's, and weighs more here: an update's worst case is a
wrong value in a field; a delete's is a missing document.

What was verified, and where (21's table covers patch and the shared facts):

| Fact | Source |
|---|---|
| `ContainerClient.DeleteItem(ctx, partitionKey, itemId, *ItemOptions)`; `ItemOptions.IfMatchEtag` is sent as `If-Match` | `cosmos_container.go`, `cosmos_item_request_options.go`, azcosmos v1.5.0 |
| A delete has no filter predicate. The conditional form in Learn's *Partial document update* is a property of patch (and of a patch inside a batch) only | that page; `DeleteItem` takes no condition |
| *Delete items by partition key value* is a public preview, enabled per account with the `DeleteAllItemsByPartitionKey` capability, asynchronous, budgeted at about 10% of the container's RU/s, unable to take a prefix of a hierarchical key, and listed for the .NET, Java and Python SDKs only | Microsoft Learn, page dated 2025-12-05 |
| azcosmos v1.5.0 cannot call it: no method on `ContainerClient`, and the only trace is the unused constant `cosmosHeaderIsPartitionKeyDeletePending` | `cosmos_http_constants.go` |

## The model

21's, unchanged: a statement in the editor; `ctrl+r` runs a dry run that only reads;
the review; a typed confirmation; a background job; a report.

### Grammar

```
delete    = "DELETE" "FROM" target [ [ "AS" ] alias ] "WHERE" condition [ ";" ]
target    = identifier "." identifier
condition = every token up to the end of the statement
```

- `query.IsMutation` is true when the first two tokens are `DELETE FROM`, beside 21's
  `UPDATE`. `query.ParseMutation` returns a `Mutation` with `Kind: MutationDelete`, no
  assignments and no removals. One parser, one type, one check function.
- `WHERE` is mandatory, by 21's rule and with more reason: `DELETE needs a WHERE. To
  delete every item, write WHERE true`.
- The condition is never parsed, as in 21.

Refused, in addition to the rows of 21's table that apply (target forms, a second
container in the `WHERE`, `TOP`/`ORDER BY`/`OFFSET`/`LIMIT`/`RETURNING`, trailing
text):

| Shape | Refused as |
|---|---|
| `DELETE sales.orders o WHERE …` | `DELETE needs FROM: DELETE FROM sales.orders o WHERE …` |
| `DELETE o FROM sales.orders o …`, `DELETE … USING`, `DELETE … JOIN` | `a DELETE has one target container` |
| `DELETE o.tmp FROM …` | `DELETE removes whole items: removing a field is UPDATE … UNSET` |
| `TRUNCATE …` | not intercepted; it goes to the service like any unknown text |

`query.CheckMutation` has one check for a delete: the container is in the catalog.
Key paths are still fetched, because the selection needs them.

### Selecting the targets

21's filtered identity scan, always `ScanIdentity`: a delete never needs a body to
decide anything. Each target keeps its `id`, its key values, and its **version**
(`ItemMeta.Version` from `adapter.SplitSystemFields`; the ETag in Cosmos), which 21
already stores in `Target.Version` and does not use. The cap, the refusal past it, the
key-less items, the zero-target result and the preview read are 21's.

## Semantics, stated honestly

21's section holds: not atomic, not isolated, a write is sent once, stop leaves the
rest untouched, no rollback, no undo. What differs:

- **Each delete is conditional on the version, not on the `WHERE`.** The service has
  no predicate for a delete, so the guard is `If-Match` with the version the dry run
  read. An item anyone wrote since, in any field, is `skipped: changed` and stays. For
  an update that strictness would have been a defect (21 explains why a patch prefers
  the predicate). For a delete it is the right rule: the review showed a count of items
  as they were, the document that would be destroyed is no longer one of them, and a
  skipped item costs a rerun while a wrongly deleted one costs a restore. The report
  says `skipped: changed` for both features and means, in both, "not what you
  reviewed".
- An item already gone is `skipped: gone`. For a delete that is the wanted end state;
  it is counted apart from `deleted` so the totals stay true, and it never makes a run
  a failure.
- **Running the statement again is safe** and is the resume story: deleted items no
  longer match, so the second selection is what is left. An `unknown` delete is
  settled by the rerun: the item is selected again or it is not.
- **Deleted is deleted.** Alchemist keeps no copy. Before a large delete take
  iteration 19's snapshot, whose content export is the way back, or iteration 18's
  clone. Where an account has continuous backup, that is the restore point consistent
  across items, and the README says so.

### Single deletes, not batches, and not the partition-key operation

21's argument against 17's `Batcher` applies unchanged, and one conflict rolling back
99 deletes is if anything worse.

The by-partition-key operation is not used even when the `WHERE` is exactly a key
equality. The Go SDK cannot call it; hand-written REST against a preview API, gated on
an account capability this tool cannot detect, is not a foundation for a destructive
feature. It is also a different promise: it deletes what is in the partition when the
background task reaches it, not what the review counted, and it has no per-item
report. If the SDK gains it, it belongs behind its own statement form with its own
review text, not as a silent fast path.

## Layout and interaction

The keys are 21's table with "update" read as "delete". No binding is added: `ctrl+r`
runs, the review and the progress view have their local keys, `w` in the catalog
reopens the job. The status bar field reads `delete prod/sales.orders 41% (w)`.

### Review

```
┌─ Review delete ─────────────────────────────────────────────────────┐
│  Account     prod                                                   │
│  Container   sales.orders                     key /customerId       │
│  Where       o.status = "cancelled"                                 │
│  Items       20 matched at 14:02:11 · selection cost 3.04 RU        │
│  Writes      4 at a time · roughly 140 RU (7 RU per 1 KB item)      │
│                                                                     │
│  - o002  "c02"  {"status":"cancelled","total":45,"tags":["reagents… │
│  - o005  "c05"  {"status":"cancelled","total":82.5,"tags":["in…     │
│  - o008  "c08"  {"status":"cancelled","total":120,"tags":["gla…     │
│  … 17 more                                                          │
│                                                                     │
│  ! The WHERE does not pin /customerId: the selection read every     │
│    partition.                                                       │
│  ! No snapshot of sales.orders. esc, then s in the catalog takes    │
│    one. Deleted items cannot be brought back from here.             │
│                                                                     │
│  Items are deleted one by one. An item changed since 14:02:11 is    │
│  skipped. Stopping leaves the rest in place. There is no undo.      │
│  Type the container name and the item count to delete:              │
│  > orders 20                                                        │
│                                                                     │
│  enter delete   ↑/↓ scroll   esc cancel                             │
└─────────────────────────────────────────────────────────────────────┘
```

- **Confirmation: the container name and the count, always**, separated by one space,
  the count in plain digits (`orders 20`). 21 asks for the count only for `WHERE
  true`; a delete asks every time. The name proves the user knows where; the count
  proves they read how many, and it is the one number that differs between the delete
  they meant and the one a loose `WHERE` selected. It is `panes.Confirm` with a longer
  expected text, not a second widget.
- The preview rows are the first three items, `-` in `theme.ErrorStyle()`, the body on
  one line and cut to the width. There is no before → after to compute.
- Warnings are 21's, less the `UNSET` ones, plus one for `WHERE true`: `Every item in
  sales.orders. Deleting and recreating the container (d, then c in the catalog)
  spends no RU per item, but it is a new container and its settings must be given
  again`.
  With 19 absent the snapshot line names 18's clone when that exists.
- The planning figure is `mutate.PlanningChargePerDelete` (7), labeled as rough and
  replaced by measured projection once the job runs.

### Progress and report

21's views with the verb changed (`Deleting items`, `Deleted 17`, `Items already
deleted stay deleted if this stops`). The quit warning reads `A delete is running.
Quit again to stop it and quit; items already deleted stay deleted.`

```
╭─ Results · prod · deleted ──────────────────────────────────────────────╮
│ Deleted 19 of 20 items from sales.orders. 1 had changed and was kept.   │
│                                                                         │
│ #    id    partitionKey  outcome           status                   RU  │
│ 1    o002  "c02"         deleted           204 No Content         7.43  │
│ 2    o005  "c05"         skipped: changed  412 Precondition Fa…   1.24  │
│ 3    o008  "c08"         deleted           204 No Content         7.43  │
╰─────────────────────────────────────────────────────────────────────────╯
 prod ▪ sales.orders ▪ deleted ▪ 20 items ▪ 143.41 RU ▪ 1.1s            ? help
```

Outcomes are 21's with `deleted` in place of `updated`. The row cap, the ordering past
it, the `Raw` shape and the RU split are 21's.

## The job

21's, entirely: chunks of `mutate.ChunkSize`, the probe write, one `writers.Write`
per item through 18's `internal/writers`, 18's `writers` profile key, stop without
cancelling writes in flight, `r`, the `job` slot with kind `jobMutation` and its
`writes()` reporting the container (so 17 refuses a batch into a container a delete is
emptying, and lets every other batch run), the guards on the switcher's `x` and on
11's `d`, the
account binding, `read_only` checked before the dry run. A delete job and an update job
are the same kind of job and exclude each other and clones and captures alike.

One addition to 21's guards: while a delete runs on a container, 19's `s` on that
container is already refused by the one-job rule, so a snapshot can never be taken
halfway through a delete and mistaken for a "before".

## Adapter contract

**Nothing is added.** 21's `ItemEditor.EditItem` already takes 17's `Operation`, and a
delete is `Operation{Kind: OperationDelete, ID: id, IfMatch: version}` with the
target's `PartitionKey`. The Cosmos side maps it to `DeleteItem` with
`ItemOptions.IfMatchEtag`, retries off, and the error classification 21 defines: 412 →
`ErrPreconditionFailed`, 404 → `ErrItemNotFound`, a throttle → `*ThrottledError`, no
answer → the unknown-outcome sentinel. The mock's `EditItem` already deletes from the
item store and answers a stale `IfMatch` from real state.

What this iteration does to the contract is test the half of it that 21 leaves
unexercised.

## Scope

- `internal/query/mutation.go` — `DELETE FROM` in `IsMutation` and `ParseMutation`;
  `MutationDelete`; the refusals above. `spans.go`: `DELETE` joins
  `mutationKeywords`.
- `internal/mutate` — the operation built for a target switches on `Mutation.Kind`:
  a patch with a condition, or a delete with `IfMatch`. `PlanningChargePerDelete`. The
  outcome label and the summary sentence take the kind. Nothing else: selection,
  chunks, probe, stop, resume and the report cursor do not know which they are doing.
- `internal/tui/panes` — `MutationReview` renders the delete form: `-` rows, the
  count in the expected confirmation, the delete warnings. `MutationProgress` and the
  status bar take their verb from the kind.
- `internal/tui/mutation.go` — the expected confirmation text comes from
  `mutate.Confirmation(m, container, count)`, which is where "name" against "name and
  count" is decided, once, for both kinds.
- `internal/history` — `Entry.Kind` gains the value `delete`; `Rows` is the number
  deleted. Recording, refusals and recall follow 21.
- `internal/adapter`, `internal/adapter/cosmos`, `internal/adapter/mock`,
  `internal/config`, `cmd`, `internal/tui/keys.go` — nothing.
- `README.md` — "Deleting by query": the grammar, the guard, the confirmation, and a
  paragraph on when this is the wrong tool (below).

## Out of scope

- **The delete-by-partition-key operation**, for the reasons above.
- **TTL as the mechanism.** For "remove everything older than ninety days, from now
  on", a container `DefaultTimeToLive` or a per-item `ttl` is the right tool: the
  service deletes expired items in the background from leftover throughput, with no
  client and no per-item request. Alchemist does not set it up from a `DELETE`, but the
  README says so where someone about to schedule a nightly delete will read it, and
  `UPDATE … SET o.ttl = 86400 WHERE …` (iteration 21) is the one-off form.
- **Targets chosen by another container.** "Delete orders whose customer does not
  exist" is iteration 20's anti-join choosing the targets, and the most compelling
  follow-up to this pair. It is out of v1 for 21's reason and refused with 21's message
  and its `IN (…)` hint: run the anti-join as a query, then `DELETE … WHERE o.id IN
  (…)`.
- Emptying a container by deleting and recreating it; that is iteration 11.
- A recycle bin, soft delete, or writing deleted bodies to a file first. 19's snapshot
  export is that file, taken deliberately.
- Undo, transactional grouping, stored procedures, server-side bulk, an RU ceiling: as
  in 21.

## Relationship to other iterations

- **7, history.** `Kind: "delete"`. Otherwise 21.
- **10, cross-container.** As 21: untouched, checked ahead of.
- **11, catalog management.** Its typed-name deletion is the pattern; this asks for
  more (the count) because a `WHERE` can select more than was meant and a container
  name cannot. `d` is refused on a container a delete job is using. Deleting every item
  and deleting the container are different things, and the `WHERE true` warning says
  how.
- **12, info view.** None.
- **13, autocomplete.** `DELETE` at statement start, then `FROM` and nothing else,
  then databases and containers, then `WHERE`, then the target's fields. No `SET`, no
  `JOIN`, no `TOP` after `DELETE`.
- **14, multiple accounts.** As 21.
- **15, saved queries.** As 21: saved with no scope, `query.IsMutation` decides,
  recall and run ends in the dry run and the review.
- **16, multi-way joins.** None.
- **17, transactions.** As 21. 17's `DELETE "id" IF MATCH "…"` inside a batch is the
  tool for deleting a known handful atomically; this is the tool for deleting by
  condition, non-atomically.
- **18, cloning.** As 21. A clone is the second-best "before" copy. 18 has landed, with
  the pool, the `writers` key and the `job` slot that 21's notes on it describe.
- **19, snapshots.** Soft, and the pairing matters most here: snapshot, delete, diff
  shows exactly the removed items, and the snapshot's `.jsonl` export is the way back.
  The review's snapshot line is 21's, worded for a delete.
- **20, CTEs and join types.** No dependency; the anti-join follow-up above would
  build on it.
- **21, update by query.** Hard dependency, on everything listed under Goal.

## Steps

1. `query`: `DELETE FROM` in the parser, the refusals, tables and fuzz. Until step 3
   `confirmMutation` answers a delete with the notice `deleting is not built yet`, so
   what ships is a dry run: the review, with the count, and no write path. That is
   21's step 4 state.
2. `mutate`: the delete operation, its guard, labels, `Confirmation`. Against the
   mock, under `-race`.
3. TUI: the review's delete form and confirmation, verbs in progress, report, status
   bar and quit warning, history kind. **Ships: deletes against `--adapter mock`.**
4. Cosmos request-capture tests for the delete path; emulator integration. **Ships the
   feature.**
5. Highlighting, README with the TTL paragraph, help sections, plan statuses.

## Testing

Only what 21's suites do not already cover.

**Unit — `query.ParseMutation`:** `DELETE FROM` with alias present, absent, with `AS`;
keywords in any case; a trailing `;`; one row per refusal above; `IsMutation` false
for `DELETE` alone in a string or comment and for every query in the planner's corpus.
The fuzz targets gain the delete form.

**Unit — `mutate`** (mock, `Writers` 1 and 8, `-race`):
- Every target gets exactly one `EditItem` of kind delete with its selected version
  as `IfMatch`, and no `Condition`.
- An item changed with `PutItem` after selection — in a field the `WHERE` does not
  read, so it still matches — is `skipped: changed` and is still in the store. This is
  the row that separates the two guards, and 21's counterpart updates the same item.
- An item deleted after selection is `skipped: gone` and the run is not a failure.
- `WithEditUnknownAt(k)`: reported `unknown`, never retried; a rerun selects one fewer
  item or the same one, and settles it.
- Rerun after a stop selects exactly the items not yet deleted.
- `Confirmation` returns the name for an update, name and count for an every-item
  update, and name and count for every delete.

**Unit — cosmos (no network):** the captured request is a `DELETE` with `If-Match`
holding the version verbatim, quotes included, and the partition key header for plain
and two-path keys; one request on a 503; 412, 404 and 429 to their errors.

**TUI, mock adapter** (`internal/tui/test/mutation_test.go`, beside 21's):
- `ctrl+r` on a delete issues scans and zero edits, then the review with `-` rows.
- `enter` with the name alone, with the wrong count, with `orders  20` (two spaces) or
  `orders 020` does nothing; `orders 20` starts the job.
- The count in the expected text is the selected count: after `PutItem` adds a
  matching item and the dry run is repeated, yesterday's number no longer confirms.
- Recall-and-run from history and from saved queries reaches the review with zero
  edits, and asks again after a delete that finished a minute ago.
- A read-only account is refused before any scan; recorded.
- The report, status bar and history entry say `deleted`; `Kind` is `delete`.
- With a delete running, `s` on its container is refused with the one-job notice.

**Integration** (emulator, `//go:build integration`,
`test/integration/mutation_test.go`): in a scratch copy of seeded `sales.orders`, the
Goal's statement deletes exactly the `cancelled` orders and a `COUNT` of them is then
zero while `open` and `shipped` counts are unchanged; a second run selects none; an
item replaced between selection and delete survives with its new body; a scratch copy
of `sales.archive` emptied with `WHERE true`; `telemetry.events` (300 items) with
`Writers` 8; a two-path hierarchical key; an id containing `"`. Charges asserted
non-negative only.

**Manual checklist** (after `make emulator-seed`, on scratch copies made with 18's
`y` where that exists):
- [ ] The Goal's statement: the review shows 20 and three `-` rows; `orders` alone
      does nothing; `orders 20` deletes; the report agrees with a `COUNT`.
- [ ] `ctrl+r` again: `No item … matches`.
- [ ] No `WHERE`: refused with the hint. `WHERE true`: the container warning shows.
- [ ] Edit a matching item elsewhere while the review is open: it is kept and reported
      `skipped: changed`.
- [ ] With 19: `s`, delete, `s`, `enter` — the diff lists exactly the deleted ids.
- [ ] `telemetry.events`, `x` halfway, `esc`: the report's `not attempted` rows are
      still in the container; `ctrl+r` again finishes the rest.
- [ ] A non-local profile with no `read_only`: refused, nothing read.
- [ ] 80×24: the review fits and the confirmation field stays visible.

## Acceptance criteria

- `ctrl+r` on a delete never deletes. No delete reaches an adapter before a review the
  user completed by typing the container name **and the number of items**, on an
  account that is not read-only, from any entry point including recall.
- Every delete is conditional on the version the dry run read. An item written since
  is kept and reported `skipped: changed`; an item already gone is `skipped: gone`.
- A delete is sent once; only a throttle is retried; an unknown outcome is reported as
  unknown, and running the statement again settles it.
- Every target ends in exactly one reported outcome, and a stopped run says what was
  deleted and what was not attempted.
- The review states that there is no undo and whether a snapshot exists; the README
  states when TTL is the better tool.
- No adapter interface, config key or key binding is added beyond iteration 21's.
- Everything 21's acceptance criteria require of the shared engine still holds.
