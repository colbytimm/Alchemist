# Iteration 17 — Transactions (Transactional Batch)

## Goal

Write to a container from the editor, atomically: a group of item operations — create,
upsert, replace, delete, read, patch — against one container and one logical partition
key that commits as a whole or not at all. This is Cosmos DB's *transactional batch*,
and it is the only transaction the service offers outside a stored procedure.

Alchemist has been read-only over items until now. Iteration 11 changes the catalog;
nothing changes a document. So this iteration has a second goal that outranks the
first: no document is written by accident, against the wrong account, or twice.

What was verified, and where:

| Fact | Source |
|---|---|
| At most 100 operations per batch; payload at most 2 MB; at most 5 s of execution | Microsoft Learn, *Transactional batch operations* (page dated 2025-07-16) |
| One partition key per batch, given when the batch is made | `ContainerClient.NewTransactionalBatch(partitionKey PartitionKey)`, azcosmos v1.5.0 |
| Operations: create, upsert, replace, delete, read, **patch** | `TransactionalBatch.CreateItem`/`UpsertItem`/`ReplaceItem`/`DeleteItem`/`ReadItem`/`PatchItem`, `cosmos_transactional_batch.go` |
| `IfMatchETag` is honored on upsert, replace, delete and patch; `CreateItem` and `ReadItem` take the options argument and ignore it | same file |
| Operations run in the order given, all or nothing | the SDK always sends the batch headers atomic = `True`, ordered = `True` (`TransactionalBatchOptions.toHeaders`) |
| A rolled-back batch is a *successful call*: `err == nil`, `Success == false` (HTTP 207), one `TransactionalBatchResult` per operation in order; the cause carries its own status, every other operation 424 | `newTransactionalBatchResponse`; Learn |
| Each result has `StatusCode`, `RequestCharge`, `ETag`, `ResourceBody`; the response has the total `RequestCharge` | `cosmos_transactional_batch_response.go` |
| Write results carry no body unless `EnableContentResponseOnWrite` is set, and the SDK sets it for the whole request as soon as one operation is a read | `ExecuteTransactionalBatch` |
| Hierarchical keys are built with `NewPartitionKeyString/Number/Bool` then `AppendString/Number/Bool/Null` | `partition_key.go` |
| Patch is built only through `AppendAdd/Set/Replace/Remove/Increment` and `SetCondition`; there is no `move`, and `AppendIncrement` takes an `int64` | `cosmos_patch_operations.go` |
| A patch value is an `any` field tagged to be left out of the JSON when empty, so a nil value is dropped: `AppendSet(path, nil)` sends a `set` with no value. A `json.RawMessage("null")` is a non-nil interface and is sent as `null` | `patchOperation` in `cosmos_patch_operations.go`; found by iteration 21, re-read here |
| The SDK writes an operation's `id` and a patch condition into the payload with `%s`, unescaped | `batchOperationDelete.MarshalJSON` and siblings; `PatchOperations.MarshalJSON` |
| The Cosmos retry policy never replays a write whose request may have been sent, and never retries a write on 408 or 5xx | `clientRetryPolicy.attemptRetryOnNetworkError`, `attemptRetryOnRequestTimeout`, `attemptRetryOnServerError` |
| The generic azcore retry policy is still in the pipeline and *does* replay on 408, 429, 500, 502, 503, 504, three times by default; `policy.WithRetryOptions(ctx, policy.RetryOptions{MaxRetries: -1})` turns it off for one call | `azcosmos.newClient`, azcore v1.23.1 `runtime/policy_retry.go` (`setDefaults` maps a negative count to zero) |
| The Linux emulator supports the Batch API and Patch; request units are "not yet implemented" there | Microsoft Learn, *Linux-based emulator (vNext)*, feature table (page dated 2026-06-02) |

Learn does not say how a rolled-back batch is charged. The response carries a charge
either way, so Alchemist reports what the service reports and claims nothing more.

## The model

**A batch is a statement in the editor.** There is no staging overlay, no list of
pending edits held somewhere off screen, and no second way to build one:

```
BEGIN BATCH sales.orders PARTITION "c01";
  CREATE  {"id": "o900", "customerId": "c01", "status": "open", "total": 45};
  REPLACE "o004" {"id": "o004", "customerId": "c01", "status": "shipped"}
          IF MATCH "\"0800-7f3a\"";
  PATCH   "o007" [{"op": "set", "path": "/status", "value": "cancelled"}];
  DELETE  "o003";
  READ    "o011";
COMMIT
```

Why a statement, and not a staging area fed from the results pane:

- It is what the editor is already for. The buffer is visible, editable, recallable
  from history, and can be pasted into a ticket. A staging list is state the user has
  to remember exists.
- A batch is ordered and the order matters (create then patch the same item). Text has
  an order; a set of pending edits has to invent one.
- Everything downstream exists already: `ctrl+r` runs the buffer, the results pane
  shows a page, history records text, export writes a page. A staging model would need
  its own pane, its own persistence, and its own answer to "what happens to pending
  edits on quit".
- The target is written down. `BEGIN BATCH` always names `<database>.<container>` and
  the partition key value; a write never depends on where a cursor happened to sit in
  the catalog. The catalog scope is deliberately *not* a default here, unlike `FROM c`.

The one convenience a staging model would have given — turning a row on screen into an
edit — is kept, and it produces statement text: `ctrl+b` on a result row appends a
`REPLACE` for that document to the batch in the editor (step 8). The buffer is the
stage. There is one model.

### Grammar

Parsed by `internal/query` with the lexer it already has. String literals are opaque
tokens to that lexer, so a JSON body is found by matching `{`/`}` and `[`/`]` over
tokens — a brace inside a string cannot unbalance it — and the byte range is then
checked with `json.Valid`.

```
batch      = "BEGIN" "BATCH" target "PARTITION" key { "," key } ";"
             operation ";" { operation ";" }
             "COMMIT" [ ";" ]
target     = identifier "." identifier
key        = string | number | "TRUE" | "FALSE" | "NULL"
operation  = "CREATE"  object
           | "UPSERT"  object           [ if-match ]
           | "REPLACE" string object    [ if-match ]
           | "DELETE"  string           [ if-match ]
           | "READ"    string
           | "PATCH"   string array [ "WHERE" string ] [ if-match ]
if-match   = "IF" "MATCH" string
```

- Keywords are case-insensitive. Strings take either quote, with backslash escapes, as
  in a query; an ETag's own quotes are escaped (`"\"0800-7f3a\""`) and sent as written.
- `PARTITION` takes one value per key path, in path order. A hierarchical key names
  all of them: `PARTITION "tenant-a", "eu", 42`. A prefix is not a batch target.
- `PATCH` takes the service's own patch document shape: an array of
  `{"op", "path", "value"}`. `WHERE` is the patch condition (`"FROM c WHERE c.total >
  10"`), the service's filter predicate.
- `IF MATCH` after `CREATE` or `READ` is a syntax error: the SDK would drop it
  silently, and an ignored precondition is worse than a refused one.
- Text after `COMMIT` other than one `;` is a syntax error. A buffer holds one batch or
  one query, never both.
- A buffer is a batch when its first two tokens are `BEGIN BATCH`. No Cosmos SQL query
  starts with `BEGIN`, so nothing that runs today changes meaning.

Without a closing `COMMIT` the statement does not parse, and so cannot run. That is
deliberate: a half-typed or truncated batch is inert.

## Interaction

| Key | Where | Action |
|---|---|---|
| `ctrl+r` | anywhere, buffer is a batch | validate; then open the review, or show the refusal |
| typing | review | the confirmation field |
| `↑`/`↓` | review | scroll the operation list |
| `enter` | review | commit — only once the typed name matches |
| `esc` | review | close; nothing was sent, nothing is recorded |
| `ctrl+b` | results, detail | append a `REPLACE` for this row to the batch in the editor |

`ctrl+r` is reused because it already means "run what is in the editor". `ctrl+b` is
the one new binding (`AddToBatch`). `bubbles/textarea` binds `ctrl+b` to
cursor-left, but only while the editor has focus, and the binding lives in
`handleResultsKey` and the detail branch of `handleOverlayKey`, where the editor does
not. It collides with nothing in `keys.go`, the README, or plans 11–15 and 18. No
function key is bound. The review's own keys go in a hint line of its own through
`KeyMap.BatchKeys()`, as `ExportKeys` does.

### Review

Every batch that writes goes through this, every time, including one recalled from
history with `ctrl+r`:

```
┌─ Review batch ──────────────────────────────────────────────┐
│  Account        prod                                        │
│  Container      sales.orders                                │
│  Partition key  /customerId = "c01"                         │
│  Operations     5 (1 create, 1 replace, 1 patch, 1 delete,  │
│                 1 read) · 1.2 KB of 2 MB                    │
│                                                             │
│   1  CREATE   o900                                          │
│   2  REPLACE  o004  if match "0800-7f3a"                    │
│   3  PATCH    o007  set /status                             │
│   4  DELETE   o003                                          │
│   5  READ     o011                                          │
│                                                             │
│  ! DELETE o003 has no IF MATCH: it deletes whatever is      │
│    there now.                                               │
│                                                             │
│  All of it commits or none of it does. There is no undo.    │
│  Type the container name to commit:                         │
│  > orders                                                   │
│                                                             │
│  enter commit   ↑/↓ scroll   esc cancel                     │
└─────────────────────────────────────────────────────────────┘
```

- The confirmation is the container name, typed exactly — the rule iteration 11 uses
  for deletion, applied to every write. One rule: a `REPLACE` destroys the previous
  document as surely as a `DELETE` does. If iteration 11's `panes.Confirm` has landed,
  the name field is that widget; otherwise this iteration builds it and 11 reuses it.
- Warnings never block. They are: a `REPLACE`, `DELETE` or `PATCH` with no `IF MATCH`;
  two operations on one `id`; a batch within 10% of either limit.
- A batch of `READ`s only writes nothing, so it skips the review and runs on `ctrl+r`
  like a query. It is also the one batch a read-only account may run.
- The review is the dry run. Reaching it means every client-side check passed; `esc`
  sends nothing. There is no separate validate key to learn.

### Validation

`ctrl+r` on a batch runs `query.ParseBatch` and then `query.CheckBatch`. A failure is
shown in the results pane like any refusal since iteration 10, recorded in history, and
lists *every* problem, numbered by operation, not just the first:

```
Batch refused, nothing was sent:
  operation 1 (CREATE): body has customerId "c02"; the batch is for "c01"
  operation 2 (REPLACE "o004"): body has no "id"
  operations 1 and 5 both create "o900"
```

| Check | Refusal |
|---|---|
| 1 to 100 operations (`query.MaxBatchOperations`) | `a batch takes at most 100 operations; this one has 131` |
| estimated payload ≤ 2 MB (`query.MaxBatchBytes`) | `about 2.4 MB; the service takes 2 MB` |
| the container is in the catalog | `sales.invoices: no such container (r in the catalog reloads it)` |
| as many `PARTITION` values as the container has key paths | `sales.events is keyed on /tenantId,/deviceId: give 2 values` |
| every body is a JSON object with a non-empty string `id` | `body has no "id"` |
| a `REPLACE` body's `id` equals the statement's | `replaces "o004" with a body whose id is "o005"` |
| every body holds the batch's key value at each key path | the `customerId` message above |
| a `PATCH` array is non-empty, each entry has a known `op` and a `path`, and no path is `/id` or a key path | `cannot patch the partition key /customerId` |
| every `PATCH` entry but `remove` has a `value` key (`null` is a value) | `set /status has no value` |
| no two `CREATE`s share an `id` | `operations 2 and 6 both create "o900"` |

The size is an estimate (bodies, ids, ETags and a fixed allowance per operation) and
errs toward refusing; the service remains the judge and its 413 is shown verbatim. Key
equality follows iteration 10's join-key rule (`1`, `1.0` and `1e0` are equal, types
are strict), through the same `canonicalNumber` helper. Two operations on one `id`
that are not both creates are legal and ordered (create, then patch), so they warn.

A body's key values are read with `adapter.PartitionKeyValues(item, paths)
([]json.RawMessage, error)`, whose `ErrNoPartitionKey` names the first path with
nothing at it. Iteration 18 defines the same function for cloning; there is one
extractor, in `internal/adapter`, and whichever of 17 and 18 lands first introduces it
with its table test. `CheckBatch` compares what it returns with `Batch.PartitionKey`,
component by component, and `ctrl+b` uses it to write a new batch's header.

Key paths come from `Node.Meta[adapter.MetaPartitionKey]`. When the tree has not
loaded the target's database, the TUI fetches it through `loadChildren` first — the
same command the tree uses, as iteration 13 does for completion — and validates when
it lands.

### Read-only accounts

`read_only` on a profile disables every write Alchemist can make: a batch with a write
operation, `ctrl+b` drafting, and iteration 11's `n`, `c`, `d`, `t`.

| `read_only` in the profile | Endpoint host | The account is |
|---|---|---|
| `true` | any | read-only |
| `false` | any | writable |
| absent | `localhost`, `127.0.0.1`, `::1` | writable |
| absent | anything else | **read-only** |

Every profile that exists today points at a real account and has no such key, so every
one of them stays exactly as safe as it is now; writing to anything but an emulator is
an explicit, per-profile decision. `alchemist profile add --read-only=false` sets it
on a new profile, `alchemist profile set-read-only <name> <true|false>` (beside
`set-key`) on an existing one, and `alchemist --read-only` forces the whole session
read-only — the flag only tightens. `--adapter mock` is writable.

A refused batch says how to change it and is recorded:

```
prod is read-only, so nothing was sent. To allow writes on this account:
  alchemist profile set-read-only prod false
```

The status bar shows a `read-only` badge beside the account name, from the first frame.

## Results

The outcome is an ordinary `adapter.Page`, built by `query.BatchReport`, loaded through
`Results.Load`. `enter` on a row opens the detail overlay (a `READ`'s document, or the
result object), `ctrl+e` exports the report, and nothing in the results pane, the
detail pane or `internal/export` changes.

Committed:

```
╭─ Results · prod · committed ────────────────────────────────────────╮
│ Committed: 5 operations on sales.orders, partition "c01".           │
│                                                                     │
│ #  operation  id    outcome  status             RU  etag            │
│ 1  CREATE     o900  applied  201 Created       7.62  "0800-81c2"    │
│ 2  REPLACE    o004  applied  200 OK           10.29  "0800-81c3"    │
│ 3  PATCH      o007  applied  200 OK           10.67  "0800-81c4"    │
│ 4  DELETE     o003  applied  204 No Content    7.43                 │
│ 5  READ       o011  applied  200 OK            1.00  "0800-6e10"    │
╰─────────────────────────────────────────────────────────────────────╯
 prod ▪ sales.orders ▪ committed ▪ 5 operations ▪ 37.01 RU ▪ 48ms       ? help
```

Rolled back:

```
╭─ Results · prod · rolled back ──────────────────────────────────────╮
│ Rolled back: nothing was written. Operation 4 (DELETE "o003")       │
│ failed: 404 Not Found.                                              │
│                                                                     │
│ #  operation  id    outcome  status                 RU    etag      │
│ 1  CREATE     o900  skipped  424 Failed Dependency  0.00            │
│ 2  REPLACE    o004  skipped  424 Failed Dependency  0.00            │
│ 3  PATCH      o007  skipped  424 Failed Dependency  0.00            │
│ 4  DELETE     o003  failed   404 Not Found          1.24            │
│ 5  READ       o011  skipped  424 Failed Dependency  0.00            │
╰─────────────────────────────────────────────────────────────────────╯
 prod ▪ sales.orders ▪ rolled back ▪ 5 operations ▪ 1.24 RU ▪ 31ms      ? help
```

A rollback is a result, not an error: it renders as a table, in `theme.ErrorStyle()`
for the banner and the failed row, and the cursor opens on the failed row. The status
bar's badge sits where `simulated` does, and the charge is the response's total,
whatever the outcome. `Raw` for each row is
`{"operation", "id", "outcome", "status", "requestCharge", "etag", "body"}`.

### When the outcome is unknown

A request that was sent and never answered — the deadline passed, the connection
dropped, the service returned 408 or 5xx — may have committed. Alchemist does not
retry it, does not guess, and says so:

```
Outcome unknown. The batch was sent and no answer came back (timed out after 30s).
It committed in full or not at all. Alchemist did not retry it and will not.

Check before running it again:
  SELECT c.id, c._etag, c._ts FROM sales.orders c WHERE c.customerId = "c01"
```

The check query is generated from the batch's target, the container's key paths and
the batch's key values, so it is pinned to the one partition. It is text on screen,
not a key to press: replacing the batch in the editor with it would lose the batch.

A failure known to have happened *before* the request left — validation, a dial or DNS
failure, a refusal of the whole request (400, 401, 403, 404, 413, 429) — reads
`Not applied:` with the service's own message. A throttled batch (429) is in that
group: refused whole, never auto-retried, one `ctrl+r` from another attempt. Once
iteration 18's `*adapter.ThrottledError{RetryAfter, Err}` exists the 429 arrives as
one, and the message adds `retry after 1.2s`; the wait is still the user's to make.
Cloning retries a throttled upsert because an upsert is idempotent. A batch is not.

While a batch is in flight the status bar reads `committing…`, `ctrl+r` is refused
with a notice, `esc` cancels nothing (canceling a sent write is how an unknown outcome
is made), and `q` and `ctrl+g` are ignored with the same notice. `ctrl+c` always quits.
The wait is bounded by `batchTimeout` (30 s; the service's own limit is 5 s of
execution).

### Beside a background job

Iterations 18, 19, 21 and 22 run long work in one root-model `job` slot (`jobClone`,
`jobCapture`, `jobMutation`) shown through `StatusBar.SetJob`. A batch is not a job and
takes no slot: it holds the keyboard for seconds, not the throughput for minutes. So
`committing…` is a run state, the job label keeps its own field, and job messages
that arrive while a batch commits are processed as usual.

`ctrl+r` on a batch while a job runs is **allowed, with one exception**: it is refused
when the batch's target is the container the job is *writing* — a clone's target, an
update's or a delete's container — on the same account:

```
an update is writing sales.orders: the batch waits for it (w)
```

The job's review described those items to the user; a batch rewriting them mid-job
would make that description stale, and the job's conditional writes would then skip
items for a reason the user caused by accident. Every other container, the job's
*source*, and any container during a capture (which writes nothing) are fair game.
The check is one call on the slot, `job.writesTo(account string, container []string)
bool`, which iteration 18 defines beside `job.writes()`: it is true when the job's
write target on that account is the batch's container or the database that holds it (a
database clone claims its whole target database). It is made in `startBatch` before
validation, and the refusal is recorded like any other. Before any of those iterations lands there is no
slot and no check.

### Which account

A batch belongs to the account that was active when `ctrl+r` was pressed.
`startBatch` records it as `runAccount`, as `startRun` does for a query, and the
review, the `Batcher`, the read-only check, the report's title and the history entry
all come from that one name — never from "whatever is active now".

Under iteration 14 the session is on one account at a time and `ctrl+g` opens the
switcher. The account cannot move between `ctrl+r` and the outcome:

- The review is an overlay, and overlays swallow `ctrl+g` (`handleOverlayKey`). The
  switcher, and with it `x`, cannot open over it.
- With a batch in flight `ctrl+g` is one of the blocked keys above, so neither a switch
  nor a disconnect can start until the outcome is on screen.
- The one switch the keyboard does not make — a connection arriving while no account
  is active — cannot happen here: with no active account `ctrl+r` was refused with
  `errNoAccount` and there is no review to be under.

After the outcome lands the session may switch freely; the report stays on screen
under `Results · prod · committed`, as any result set outlives a switch.

## Adapter contract

One optional interface in `internal/adapter`, found by type assertion as iteration 12
finds `Inspector`. `Connection` does not grow. With iteration 14 in place the assertion
happens where `AccountConnectedMsg` lands and is kept per account; before it, in
`cmd/root.go` beside the others.

```go
// Batcher runs a group of item operations against one container and one
// partition key as a single transaction. Optional: callers find it with a
// comma-ok type assertion.
type Batcher interface {
    // ExecuteBatch returns a BatchResult whenever the backend answered,
    // whether it committed or rolled back. An error means no answer: it wraps
    // ErrWriteOutcomeUnknown when the batch may have been applied, and
    // otherwise guarantees that it was not. An implementation never retries.
    ExecuteBatch(ctx context.Context, b Batch) (BatchResult, error)
}

// ErrWriteOutcomeUnknown covers any write with no answer: a batch here, a
// single item in iterations 21 and 22.
var ErrWriteOutcomeUnknown = errors.New("write outcome unknown")

type Batch struct {
    Scope        []string     // ["sales", "orders"]
    PartitionKey PartitionKey
    Operations   []Operation
}

// PartitionKey is one JSON scalar per key path, in path order: a string, a
// number, true, false or null. A plain key has one component.
type PartitionKey []json.RawMessage

type OperationKind string

const (
    OperationCreate  OperationKind = "create"
    OperationUpsert  OperationKind = "upsert"
    OperationReplace OperationKind = "replace"
    OperationDelete  OperationKind = "delete"
    OperationRead    OperationKind = "read"
    OperationPatch   OperationKind = "patch"
)

type Operation struct {
    Kind      OperationKind
    ID        string          // empty for create and upsert: the body carries it
    Body      json.RawMessage // an item; for a patch, the array of patch entries
    Condition string          // patch only: the filter predicate
    IfMatch   string          // version tag the target must still have
}

type BatchResult struct {
    Committed bool
    Results   []OperationResult // one per operation, in order
    Stats     Stats             // total charge, elapsed; RowCount is len(Results)
}

type OperationOutcome int

const (
    OperationApplied OperationOutcome = iota
    OperationFailed                   // the operation that caused the rollback
    OperationSkipped                  // rolled back because another one failed
)

type OperationResult struct {
    Outcome       OperationOutcome
    Status        string          // the backend's words: "201 Created"
    ETag          string
    Body          json.RawMessage // what the backend returned; always set for a read
    RequestCharge float64
}
```

`PartitionKey` is JSON scalars rather than `[]any` or `[]string` because the parser has
the literal's bytes already, the body check compares JSON with JSON, and a string-only
key would make a numeric or boolean key unreachable. `OperationOutcome` and a
pre-rendered `Status` are what keep HTTP status codes out of `internal/tui`, the way
`Page` keeps item shapes out of it. The error contract has two states on purpose: the
TUI needs to tell "safe to run again" from "check first", and nothing finer.

`ItemDrafter` (step 8) is a second optional interface, because turning a returned item
into a write needs to know which fields the backend owns:

```go
// ItemDrafter turns an item a query returned into the operation that writes
// it back: system fields removed from the body, its version tag as IfMatch.
type ItemDrafter interface {
    DraftReplace(item json.RawMessage) (Operation, error)
}
```

### Cosmos

`internal/adapter/cosmos/batch.go`:

- `NewContainer(Scope[0], Scope[1])`, then a `PartitionKey` folded from the components
  with `NewPartitionKeyString/Number/Bool` and `Append*`; a leading `null` starts from
  `NewPartitionKey().AppendNull()`. The fold is `partitionKey(adapter.PartitionKey)
  (azcosmos.PartitionKey, error)` in `write.go` (below), not a private of this file.
- Each `Operation` maps to its `TransactionalBatch` method. `IfMatch` becomes
  `TransactionalBatchItemOptions.IfMatchETag`.
- A patch array maps entry by entry to `AppendAdd/Set/Replace/Remove/Increment`.
  **Values are passed as `json.RawMessage`, never decoded.** A decoded JSON `null` is
  a nil `any`, which the SDK's struct tag drops, turning `{"op": "set", "path": "/x",
  "value": null}` into a malformed `set`; raw bytes also keep a number's digits. `move`
  and a non-integer `incr` have no SDK call and are refused before anything is sent,
  naming the SDK as the reason. `Condition` goes to `SetCondition`.
- **Escaping.** The SDK formats `id` and the patch condition into the payload with
  `%s`. An id or predicate containing `"` or `\` would produce a malformed request, or
  a different one. The adapter passes both JSON-escaped (quotes stripped). A unit test
  captures the request body through `ClientOptions.Transport` with an id of `a"b` and
  asserts it parses to the same id, so an SDK release that starts escaping fails the
  test instead of double-escaping in production.
- **No retries.** `ExecuteTransactionalBatch` is called with
  `policy.WithRetryOptions(ctx, policy.RetryOptions{MaxRetries: -1})`. The Cosmos
  policy already refuses to replay a write; this silences azcore's generic policy,
  which would otherwise replay the POST on 408, 429 and 5xx.
- `TransactionalBatchOptions` is nil: content responses stay off, so write results
  carry no bodies unless the batch reads.
- `Success` → `Committed`. Status 424 → `OperationSkipped`; any other status ≥ 400
  → `OperationFailed`; the rest `OperationApplied`. `Status` is the code and
  `http.StatusText`. `Stats.RequestCharge` is the response's `RequestCharge`.
- **Errors.** An `*azcore.ResponseError` with 408 or ≥ 500 wraps
  `ErrWriteOutcomeUnknown`; any other `ResponseError` goes through `wrap`'s refusal
  path as today. With no response at all, a dial or DNS failure (`*net.OpError` with
  `Op == "dial"`, `*net.DNSError`) means not sent; everything else, a passed deadline
  included, wraps `ErrWriteOutcomeUnknown`. This path must not reuse `wrap`'s
  `adapter.Unreachable` branch, which calls a timeout "never reached the account" —
  true enough for a read, false for a write.
- **Shared with single-item writes.** Three unexported helpers live in
  `internal/adapter/cosmos/write.go`, because iteration 21's `edit.go` (and 22 through
  it) needs exactly them: `partitionKey`, the fold above; `withoutRetries(ctx)
  context.Context`, the azcore override; and `writeError(op string, err error) error`,
  the classification in the previous bullet — sent and unanswered wraps
  `adapter.ErrWriteOutcomeUnknown`, everything else is guaranteed not applied.
  Whichever of 17 and 21 lands first writes the file with its tests; the other calls
  it. There is one definition of "unknown" in the adapter, not one per write path.
- `DraftReplace` drops `_rid`, `_self`, `_etag`, `_attachments`, `_ts` and `_lsn` and
  moves `_etag` to `IfMatch`.

### Mock

`internal/adapter/mock` gains an in-memory item store so no TUI test needs a network:

- Items live per adapter instance, keyed by container path, partition key and `id`,
  seeded by `WithItems(path []string, items ...json.RawMessage)`. State belongs to the
  `Adapter` made by `New`, never to the package — the isolation rule iteration 11 sets
  for its mutable fixture. `Query` keeps serving its canned pages; the store is read
  through a test accessor, `Adapter.Items(path)`.
- `ExecuteBatch` applies operations in order to a copy of the partition and swaps the
  copy in only if every one succeeds. Create on an existing id fails with `409
  Conflict`; replace, delete, read and patch of a missing id with `404 Not Found`; a
  stale `IfMatch` with `412 Precondition Failed`. The rest come back `424 Failed
  Dependency`, `OperationSkipped`. Every applied write gets a fresh ETag from a
  counter; charges are fixed per kind so a test can assert totals.
- Patch supports `add`, `set`, `replace`, `remove` and `incr` on top-level paths, which
  is all the tests need; a nested path is an error that says so.
- `WithBatchFailure(k int, status string)` fails operation *k* whatever the store says.
  `WithError(OpBatch)` fails the call as not applied, and
  `WithError(OpBatchUnknown)` as `ErrWriteOutcomeUnknown`, after applying the batch to
  the store, so a test can prove the UI makes no claim either way.
- `WithLatency` applies, for the in-flight tests.
- `DraftReplace` strips `_etag` into `IfMatch`, over the same fixtures.

## Scope

- `internal/adapter/adapter.go` — everything under "Adapter contract", and
  `PartitionKeyValues`/`ErrNoPartitionKey` if iteration 18 has not brought them.
- `internal/adapter/cosmos/batch.go` — as above; compile-time checks
  `_ adapter.Batcher = (*connection)(nil)` and the `ItemDrafter` one.
- `internal/adapter/cosmos/write.go` — `partitionKey`, `withoutRetries`, `writeError`,
  if iteration 21 has not brought them.
- `internal/adapter/mock/mock.go`, `mock/items.go` — the store and the options above.
- `internal/query`
  - `batch.go` — `IsBatch(text string) bool`; `ParseBatch(text string) (adapter.Batch,
    error)`; `*BatchSyntaxError{Line, Column, Message}`.
  - `validate.go` — `CheckBatch(b adapter.Batch, keyPaths []string) BatchCheck`,
    where `BatchCheck{Problems, Warnings []string; Bytes int}` and `Problems` being
    non-empty is the refusal; `MaxBatchOperations`, `MaxBatchBytes`.
  - `report.go` — `BatchReport(b adapter.Batch, r adapter.BatchResult)
    adapter.Page` and `BatchSummary(…) string` for the banner;
    `PartitionCheckQuery(b adapter.Batch, keyPaths []string) string` for the
    unknown-outcome text; `FormatOperation(op adapter.Operation) string`, the inverse of
    the parser for one operation, used by the review list and by `ctrl+b`.
  - `spans.go` — when `IsBatch`, the batch keywords are `SpanKeyword` at statement
    level. They go in their own `batchKeywords` set, not in `keywords`, whose rule is
    "can never be a bare source alias"; `READ` and `MATCH` can.
- `internal/config` — `Profile.ReadOnly *bool` (`toml:"read_only"`) and
  `Profile.IsReadOnly() bool` holding the table under "Read-only accounts".
- `internal/history` — `Entry.Kind string`, JSON key `kind`, left out of the line when
  empty: empty is a query, `batch` is a batch. Old lines parse unchanged.
- `internal/tui/panes`
  - `review.go` — `BatchReview`: header rows, the scrolling operation list,
    warnings, the name field, the hint line. Built from `adapter.Batch` plus
    `query.BatchCheck`; knows nothing about connections.
  - `Results.SetBanner(text string, failed bool)` and the title suffix.
  - `StatusBar`: `Progress.Batch` (`committed`, `rolled back`, `outcome unknown`,
    `committing…`), an `operations` label in place of `rows`, the `read-only` badge.
  - `History`: a `batch` tag in the scope column for `Entry.Kind == "batch"`.
- `internal/tui`
  - `batch.go` (new) — `startBatch`, `reviewBatch`, `commitBatch`, `finishBatch`,
    `draftReplace`, and the refusals `errReadOnly`, `errNoBatchSupport` ("this adapter
    has no transactions"), `errBatchInFlight`. `Model.batcher()` is the single place a
    `Batcher` is handed out, and it is where read-only is enforced.
  - `app.go` — `startRun` branches on `query.IsBatch` before `resolvePlan`. The order
    in `startRun` is fixed: `IsBatch`, then iteration 21's `query.IsMutation`, then
    `BuildPlan`. The three cannot overlap, since `BEGIN`, `UPDATE`/`DELETE` and
    `SELECT`/`WITH` are different first tokens; the order is stated so each plan adds
    one branch rather than rearranging the others;
    `overlayBatchReview`; a `runCommitting` run state.
  - `commands.go` — `executeBatch` with `batchTimeout`; its context is never canceled
    by the model.
  - `messages.go` — `BatchDoneMsg{Account, Batch, Result}`,
    `BatchFailedMsg{Account, Batch, Err}`.
  - `keys.go` — `AddToBatch` (`ctrl+b`) under Results, `Commit` (`enter`) in
    `BatchKeys()`; `AddToBatch` disabled with no `ItemDrafter` or a read-only account,
    which keeps it out of the help overlay.
  - `history.go` — `newHistoryEntry` sets `Kind`; see "History".
- `cmd` — `profile add --read-only`, `profile set-read-only`, root `--read-only`;
  `Account.ReadOnly` resolved from `Profile.IsReadOnly()` and the flag.
- `test/seed` — nothing new: `sales.orders` on `/customerId` is the test bed.
- `README.md` — a "Transactions" section: the grammar, the limits, the read-only
  default and how to lift it, what "outcome unknown" means; the key table.

### History

One log, as now. A batch entry has `Kind: "batch"`, the statement as `Query`, the
target as `Scope`, the operation count as `Rows`, the total charge, and `OK` only when
it committed. A rollback records `rolled back: operation 4 DELETE "o003": 404 Not
Found`; an unknown outcome records `outcome unknown: …` so the next session still says
so. Refusals — syntax, validation, read-only, no support — are recorded, as every
refusal has been since iteration 10. A review closed with `esc` is not a run and is
not recorded.

Statement text is recorded whole up to 64 KiB. Past that it is cut at the last
operation boundary that fits, which drops `COMMIT`, so a recalled oversize batch does
not parse and cannot be run by reflex. `enter` recalls a batch into the editor like
any entry, except that it leaves the catalog scope alone: `Entry.Scope` holds the
batch's target so the log can show it, and `Model.recall` skips `setScope` for
`Kind == "batch"`, since moving the scope to a container the statement already names
would be a side effect with no use.

### Recall never bypasses the review

There is one door to `ExecuteBatch`: `startRun` → `startBatch` → the review →
`commitBatch`. `commitBatch` is reachable only from the review's `enter` with the name
matched, and `executeBatch` is called from nowhere else. Every "recall and run" is a
recall followed by `startRun` — `Model.rerun` for history, 15's `rerunSaved` for saved
queries — so `ctrl+r` in either overlay closes it and opens the review on the active
account, having sent nothing. A confirmation is never remembered: not per session,
not per statement, not for a batch that committed a minute ago.

`Entry.Profile` is the batch's `runAccount`. Iteration 14 scopes the overlay to the
active account (`Store.Recent(account, n)`, title `History · prod`), so a batch is
found, and recalled, only on the account it ran on. Running it elsewhere takes a
deliberate switch and a fresh `ctrl+r`, and that account's review names it and its
`read_only` applies.

## Out of scope

- **Cross-partition and cross-container transactions.** The service has none, and
  Alchemist will not fake one. Running two batches back to back and calling it a
  transaction is a lie the first failure exposes: the first batch has committed and
  cannot be taken back. The grammar has one target and one `PARTITION` clause, so the
  shape cannot be written; a body in another partition is the validation refusal
  above; a second `BEGIN BATCH` in the buffer is `a buffer holds one batch: the service
  cannot commit two atomically, and Alchemist will not pretend to`.
- Cross-account batches. A batch runs on the active account, as a query does.
- Stored procedures, triggers and UDFs — the Go SDK has no client for them (iteration
  12 records this).
- Bulk, non-transactional import or a batch split automatically into chunks of 100.
  Chunks are separate transactions; that belongs to an import feature with its own
  resume story.
- An item editor: a form over a document, inline cell editing, a diff of the
  before-image. `ctrl+b` drafts text and stops.
- Single-item writes outside a batch. A batch of one is a batch.
- Fetching before-images for the review. It would cost a read per operation and race
  the commit anyway; `IF MATCH` is the tool for that, and the review says when it is
  missing.
- `move` in a patch and non-integer `incr`, until the SDK can send them.
- Session tokens, consistency overrides, priority level and throughput buckets on
  `TransactionalBatchOptions`.

## Relationship to other iterations

- **10, cross-container.** `query.BuildPlan` and `Engine` are untouched; `IsBatch` is
  checked ahead of them. `BatchReport` builds a synthetic page the way the union and
  join cursors do, which is why results, detail and export need nothing. Refusals are
  recorded by the rule 10 introduced. `ctrl+b` is refused on a simulated result: a
  joined row is not a document.
- **11, catalog management.** `read_only` governs its writes too: a read-only account
  gets no `CatalogAdmin` bindings. The typed-name confirmation is shared
  (`panes.Confirm`); whichever lands first builds it. 11's "Item-level writes" out of
  scope entry is what this iteration picks up.
- **12, info view.** The same optional-interface pattern and the same rule that the TUI
  names no backend concept — hence `OperationOutcome` and a pre-rendered `Status`.
- **13, autocomplete.** In a batch buffer `query.Context` offers the batch keywords at
  statement start and databases then containers after `BEGIN BATCH`; nothing inside a
  body. A report page is synthetic and must not feed `complete.Index`; a `READ` body
  is not fed either. If 13 lands second, those three sentences are part of its step 1.
- **14, multiple accounts.** The session is on one account; a batch runs there and
  nowhere else, by the rules under "Which account". The catalog is that account's tree,
  so the key-path lookup and the on-demand `loadChildren` read the active account's
  pane and need no account in a path. `Account` gains `ReadOnly`, filled by `cmd`;
  `Batcher` and `ItemDrafter` are asserted per account on `AccountConnectedMsg` and
  kept in `accountSet`. `BatchDoneMsg` and `BatchFailedMsg` carry the account and —
  unlike a late page — are **never dropped**. The switcher shows `read-only` in a
  read-only account's row, so the state is known before switching. With no active
  account a batch is refused with `errNoAccount`, like a query. Before 14 lands there
  is one account and every rule here holds trivially.
- **15, saved queries.** Takes `ctrl+s` and `ctrl+l`; no overlap. A batch saves with
  `ctrl+s` like any text, and it is saved with **no scope**: `BEGIN BATCH` names its
  own target and nothing about a batch ever reads the catalog scope. 15's rule is "a
  scope is saved only when the text needs one", and a batch never does — complete,
  half-typed, or failing validation. `query.IsBatch(text)` is what tells:
  `savedScope` asks it before it calls `BuildPlan`, and returns none. It must ask
  first, because `BuildPlan` refuses a batch and 15 saves refused text as a draft
  *with* the current scope, which for a batch would restore a container the statement
  does not use. `Plan.NeedsDefaultScope` is not involved; a batch has no `Plan`. Recall
  and run from the saved overlay ends in `startRun`, and so in the review, by the rule
  under "Recall never bypasses the review".
- **18, cloning.** Reads this plan's `read_only` as `Account.ReadOnly` (the same field,
  set once in `cmd`) and never offers a read-only account as a clone target. Shares
  `adapter.PartitionKeyValues`/`ErrNoPartitionKey` and `*adapter.ThrottledError` as
  described above; whichever lands first introduces them. `Batcher` and 18's
  single-item write interface stay separate: one is a transaction, the other is an
  idempotent stream, and neither is built from the other. 18 takes `y` in the catalog.
- **20, CTEs and join types.** If 20 lands first and brings a recursive-descent parser
  to `internal/query`, `ParseBatch` is written on it rather than on a second
  hand-rolled token walk; the grammar above is small enough to move either way, and
  this plan does not depend on 20.
- **19, snapshots.** Nothing beyond `read_only`: a capture reads and writes only to
  the local store, so `s` and `snapshot take` work on a read-only account, and a
  capture never blocks a batch. A snapshot taken before a risky batch is the undo this
  plan does not have; the README says so.
- **21, update by query, and 22, delete by query.** Both depend on this plan and add
  no second safety model. They reuse: the statement-in-the-editor model and `ctrl+r`;
  the review overlay family and its typed-container-name rule (the same widget);
  `read_only` through the one gate, `Model.batcher()`, which grows to hand out their
  `ItemEditor` too; `runAccount`; the unknown-outcome rule, `ErrWriteOutcomeUnknown`
  and `write.go`'s classification; retries off; `Operation`, `OperationResult` and
  `PartitionKey` as the unit of a single write; the escaping and raw-value fixes; the
  mock's item store and patch code; the report as a synthetic `adapter.Page`; and
  `Entry.Kind`, which gains their values beside `batch`. They deliberately do **not**
  use `Batcher`. A batch is all-or-nothing, so one item that changed since the dry run
  would roll back up to 99 innocent ones, and skipping exactly that item while the
  rest proceed is their feature; a throttled batch is also refused whole where a
  single write can wait and retry. Atomic and resumable are different promises, and
  each plan keeps one. `startRun` checks `IsBatch` first, then `IsMutation`.
- **16.** No dependency. This plan reserves `ctrl+b` and leaves `ctrl+t` and `p` alone;
  `s`, `v` and `w` belong to 19 and 21.

## Steps

1. `adapter.Batcher` and its types; the mock's item store and `ExecuteBatch`; mock
   tests first, since they define what the Cosmos side must match.
2. `query.ParseBatch`, `IsBatch`, `FormatOperation`: grammar tables and the fuzz
   targets. Pure; ships nothing visible.
3. `query.CheckBatch`, `BatchReport`, `BatchSummary`, `PartitionCheckQuery`. Pure.
4. `config.Profile.ReadOnly`, `IsReadOnly`, the `cmd` flags, the status bar badge.
   Ships: every non-local account is marked read-only, with nothing yet to refuse.
5. The `BatchReview` pane against hand-built batches, wired to nothing.
6. Root model: the `startRun` branch, on-demand key-path fetch, review, commit,
   in-flight rules, report, unknown-outcome text, history. Ships: batches against the
   mock (`--adapter mock`).
7. Cosmos `ExecuteBatch` with the escaping and no-retry tests; emulator integration.
   Ships the feature.
8. `ItemDrafter`, mock then Cosmos, and `ctrl+b`. Separable: steps 1–7 stand alone.
9. Batch keyword highlighting; README, help sections, plan statuses.

## Testing

**Unit — `query.ParseBatch`:** every operation form; keywords in any case; both quote
styles; an escaped-quote ETag; a body containing `;`, `}`, `COMMIT` and `--` inside
strings; nested objects and arrays; one to three `PARTITION` values of every scalar
type; `COMMIT` with and without `;`. Syntax errors with line and column: no `COMMIT`,
no `;` between operations, `IF MATCH` on `CREATE` and `READ`, a bare-container target,
an empty batch, an unbalanced body, invalid JSON in a balanced body, text after
`COMMIT`, a second `BEGIN BATCH`. `IsBatch` is false for every query in the planner's
corpus. Fuzz: arbitrary input never panics; and for any batch that parses,
`FormatOperation` of each operation parses back to the same operation.

**Unit — `query.CheckBatch`:** one row per line of the validation table, at the limit
and one past it; all problems reported, not the first; numeric key `1` matches body
`1.0` and not `"1"`; a nested key path (`/shipTo/region`); a hierarchical key with the
second component wrong; `null` and boolean keys; a missing key field; two creates of
one id refused, create-then-patch warned; each warning.

**Unit — `query.BatchReport`:** committed and rolled-back pages have the columns and
`Raw` shape above; the summary names the failed operation; a result list shorter than
the operation list (a backend bug) yields an error row, not a panic.

**Unit — mock:** commit applies all; failure at *k* applies none and marks the rest
skipped; 409, 404 and 412 each from real store state; ETags change on write; two
adapters from `New` share nothing; both injected call failures.

**Unit — cosmos (no network):** the captured request body for an id and a condition
containing `"` and `\`; patch entry mapping, with `move` and fractional `incr` refused
before any request; every `PartitionKey` shape; status → outcome mapping from a canned
207 body; 408, 503, a mid-body connection reset and a passed deadline all wrap
`ErrWriteOutcomeUnknown`; a dial failure, 400, 413 and 429 do not; a counting
transport sees exactly one request when the first answer is 503. A patch entry
`{"op": "set", "path": "/x", "value": null}` is captured on the wire with
`"value":null` present, and `"value": 1e400` and `12345678901234567890` arrive digit
for digit. `writeError` and `partitionKey` have their own table tests in
`write_test.go`, which is what 21 relies on.

**Unit — `config`:** the four rows of the read-only table; `[::1]:8081` and
`localhost:8081` count as local; `--read-only` cannot loosen.

**TUI, mock adapter** (`internal/tui/test/batch_test.go`):
- `ctrl+r` on a valid batch opens the review and sends nothing; the review lists every
  operation, the container, the key and the account.
- `enter` with an empty, wrong-case or trailing-space name sends nothing; with the
  exact name it sends exactly one `ExecuteBatch`.
- `esc` sends nothing, records nothing, and leaves the buffer as it was.
- `q` typed in the review lands in the name field and quits nothing.
- A committed batch loads the report, the banner, the badge and the total charge;
  `enter` on a `READ` row shows the document; `ctrl+e` exports the report.
- A failure injected at operation 3 of 5 renders rolled back, the cursor on row 3, the
  other rows skipped, and the mock store unchanged.
- A validation failure shows every problem, reaches no adapter, and is recorded.
- A target whose database the tree has not loaded triggers one `Children` call, then
  validates; an unknown container is refused.
- A read-only account refuses with `errReadOnly`, makes no call, is recorded; a
  reads-only batch on the same account runs, with no review.
- A connection with no `Batcher` refuses with `errNoBatchSupport`.
- `OpBatchUnknown`: the results pane shows the unknown-outcome text with the check
  query, the mock saw exactly one call, and history records `outcome unknown`.
- `OpBatch`: reads `Not applied`, one call.
- With latency: the status bar reads `committing…`; a second `ctrl+r`, `esc`, `q` and
  `ctrl+g` change nothing and send nothing, and the switcher does not open; the result
  then renders. `ctrl+c` quits.
- `ctrl+g` typed in the review lands nowhere: the review stays open on the same
  account and no switcher opens.
- Two accounts (14's `Opener` handing out two mocks): a batch committed on the first
  reaches only the first mock's store; after switching, `ctrl+o` on the second does
  not list it; the entry's `Profile` is the first account.
- The same buffer run on a second, read-only account is refused there, and the first
  account can still write.
- With no active account `ctrl+r` on a batch shows `errNoAccount` and opens no review.
- With a job slot present (a fake job registered by the test): a batch on another
  container opens its review while the job runs; a batch on the container the job
  writes is refused, reaches no adapter, and is recorded; a job message delivered while
  `committing…` still updates the job label.
- History: `Kind` is `batch`; `ctrl+r` on a batch entry closes the overlay and opens
  the review with zero `ExecuteBatch` calls, and `esc` there leaves the recalled text in
  the editor and the store untouched; `enter` on a batch entry does not move the scope;
  the same batch committed once and recalled with `ctrl+r` asks for the name again.
- Saved queries (when 15 has landed): `ctrl+r` on a saved batch in the saved overlay
  opens the review with zero `ExecuteBatch` calls; saving a batch with `sales.customers`
  selected writes no scope header, for a complete batch and for one missing `COMMIT`.
- A statement over 64 KiB is recorded without `COMMIT`, and the recalled text does not
  parse.
- `ctrl+b` on a row appends a `REPLACE … IF MATCH` that parses; on a row of another
  partition it is refused with a notice; on a simulated result likewise; over a
  non-batch buffer it starts a new batch; on a read-only account the binding is absent.
- An ordinary query still runs with no review — the iteration-5 and iteration-10
  suites pass unedited.
- Every new binding is grouped exactly once and appears in the help overlay and README.

**Integration (emulator, `//go:build integration`,
`test/integration/batch_test.go`):** in a scratch container on `/customerId`: a
create-upsert-read batch commits and a query sees the items; a batch whose third
operation creates an existing id rolls back, reports `failed` on it and `skipped`
elsewhere, and a query sees none of its writes; a stale `IF MATCH` yields 412; a patch
with a condition applies; a two-path hierarchical key commits; an id containing `"`
round-trips. Charges are asserted only to be non-negative: Learn lists request units as
not yet implemented in the vNext emulator. Learn also lists the Batch API as supported
there, and `docker-compose.yml` pins the `vnext-preview` tag, which may trail it — so
the first test probes with a one-create batch and, on a refusal that is not about the
item, skips the file with `emulator image does not serve transactional batch: <status>`
rather than failing.

**Manual checklist** (after `make emulator-seed`):
- [ ] Run the example under "The model" against `sales.orders`, `"c01"`, with ids that
      exist; the report shows five applied rows and `SELECT * FROM sales.orders c WHERE
      c.customerId = "c01"` shows the changes.
- [ ] Change one body's `customerId` to `"c02"`: refused, naming the operation; nothing
      sent (the emulator log shows no request).
- [ ] Delete an id that does not exist: rolled back, and the create beside it is absent.
- [ ] `ctrl+b` on an order, edit `status`, commit; run it again unchanged and get 412.
- [ ] Point a profile at a non-local endpoint with no `read_only`: the badge shows and
      the batch is refused with the command that lifts it.
- [ ] `docker pause` the emulator after the review, commit: `Outcome unknown` after
      30 s, one request in the log after `docker unpause`, the TUI alive throughout.
- [ ] 80×24 with a 40-operation review: the list scrolls, the name field stays visible.

## Acceptance criteria

- No write reaches an adapter without a review the user completed by typing the
  container name, on an account that is not read-only. A profile with no `read_only`
  key on a non-local endpoint is read-only.
- A batch is sent at most once. No layer — Alchemist, the adapter, azcore — replays
  it, and a test with a counting transport proves it.
- Every outcome is one of four and the screen says which: committed, rolled back
  (nothing written), not applied (nothing sent or the request refused whole), or
  unknown. Unknown is never rendered as failure or success.
- A batch that spans partitions, containers or accounts cannot be written, and
  Alchemist never runs two transactions to imitate one.
- A committed or rolled-back batch is a page: detail, export and the status bar's
  charge work on it with no change to those panes' code.
- Queries are byte-for-byte unaffected: a buffer that does not start `BEGIN BATCH`
  takes the path it takes today.
- `Update` never blocks; a hung commit leaves scrolling and `ctrl+c` working, and
  resolves within `batchTimeout`.
- `internal/tui` imports the adapter interfaces only, and names no status code, ETag
  field or system property; parsing and validation live in `internal/query` with no
  TUI import.

## Implementation notes

What landed differs from the text above in these ways:

- The contract lives in `internal/adapter/batch.go` beside `adapter.go`, with two
  additions: `OperationKind.Writes`, which is how the TUI tells a batch that only reads,
  and `adapter.WithoutFields`, which both drafters use to drop system fields without
  reordering the rest of the item.
- Tests live in each package's `test/` folder and see only exported names, so two of
  `write.go`'s helpers are exported: `cosmos.PartitionKey` and `cosmos.WriteError`.
  `withoutRetries` stays unexported; a counting server proves that a 503, a 429 or a
  dropped connection is sent once. The request-body tests use a local TLS test server
  standing in for the account rather than `ClientOptions.Transport`.
- `FormatOperation` is in `query/batchtext.go` with `FormatBatch` (the header `ctrl+b`
  writes), `AppendOperation` (inserts before `COMMIT`) and `BatchPrefix` (the 64 KiB cut
  for history, which stops before `COMMIT`). `query` also exports `ItemID`,
  `OperationName`, `PartitionText`, `SamePartition`, `FailedOperation`, `EstimateBytes`
  and `FormatSize` for the review and the TUI.
- "The container is in the catalog" is checked by the TUI while it looks up the key
  paths, not by `CheckBatch`, which knows no catalog. A lookup waiting on the tree shows
  nothing until the review opens or the batch is refused.
- `--read-only` reaches the TUI as `Options.ReadOnly` and is ORed into every account,
  so an account a connect form adds obeys it too. Read-only removes `Admin`, `Throughput`
  and `Drafter` from what the account permits, which takes `n`, `c`, `d`, `t` and
  `ctrl+b` out of the keymap and the help overlay; `Model.batcher` refuses the writes.
- `ctrl+b` drafts only from a `SELECT *` run of one container (`Leaf.WholeItems`), and
  the Cosmos `DraftReplace` also refuses an item lacking `_rid`, `_etag` or `_ts`: a
  replace written from a projection would erase every field it left out.
- The saved profile is the one source of read-only. `tui.Connector` reports the
  account as `cmd` saved it, so a form connect takes the profile's `IsReadOnly()` (and
  the session flag) like any listed account. A form that changes a profile's endpoint
  clears its `read_only`, which is then derived again from the new endpoint. A batch
  waiting on the tree is dropped by any new run and by a switch of account.
- `mock.WithBatchFailure` takes the status as an `int`. The mock's patch ignores the
  condition: it has no query engine.
- Iterations 18, 19, 21 and 22 have not landed: there is no job slot to consult and no
  `ThrottledError`, so a 429 reads `Not applied:` with the service's own message.
- A batch leaves a query in flight alone until it commits: the review is an overlay.
  While it commits, `q` is refused everywhere, the editor included.
- 11's `panes.Confirm` had landed; its name field is now `nameField`, shared with
  `BatchReview`. The review's own hint line is `KeyMap.BatchKeys()`: `Commit` (enter)
  and `Scroll` (↑/↓), then `Close`.
- 13 had landed, so its part is here: in a batch buffer completion offers `BATCH`,
  databases then containers, `PARTITION`, the operation keywords after a `;`, `MATCH`
  after `IF`, and nothing inside a body. A report page never reaches `observePage`.
- The editor's textarea stopped `enter` at 99 lines, short of a 100-operation batch
  written one to a line; the limit is lifted.
- The emulator integration tests (`test/integration/batch_test.go`) compile but have
  not been run in this iteration's environment.
