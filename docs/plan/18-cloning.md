# Iteration 18 — Database and Container Cloning

## Goal

Press `y` on a container or a database in the catalog and get a copy of it under a new
name: the definition alone, or the definition and every item. The copy can land in the
account the session is on or, building on iteration 14, in any other account the
session knows. `prod` → `emulator` is the case this exists for: a realistic local data
set without a script, the portal, or a Data Factory pipeline. It works out of the box:
iteration 17 makes every non-loopback profile read-only until told otherwise, cloning
only reads from its source, and the emulator's endpoint is loopback, so it is writable.

Cosmos DB has no clone operation a data-plane key can call. Container copy jobs exist
on the ARM control plane only, which this tool does not reach (iterations 11 and 12
record the same boundary). So a clone is a **client-side copy**: read the source
definition, create the target, read every item, write every item. That makes three
things true that the UI says out loud rather than hides:

- It spends request units on both sides, and a new provisioned container bills from
  the moment it exists.
- It is not a point-in-time copy. A source that changes while it is read is copied as
  it was seen, page by page. Snapshots are iteration 19's subject.
- It can run for a long time. The session stays usable while it does, switching
  accounts included.

The second goal is the one iteration 11 set: nothing is written by a single keystroke,
and nothing that exists is ever overwritten.

## Layout

The prompt is iteration 11's `panes.Form` with one more constructor. The first block
is read-only and fills in when the source has been read (`prepareClone`, below); until
then it shows the spinner line the info view uses.

```
┌─ Clone container ───────────────────────────────────────┐
│  Source          prod / sales.orders                    │
│                  about 30,112 items · 41.8 MB           │
│                  partition key /customerId              │
│                                                         │
│  Target account  ▸ emulator                             │
│  Database        sales                                  │
│  Name            orders                                 │
│  Copy            ▸ definition and items                 │
│  Definition      ▸ portable                             │
│  Throughput      ▸ minimum (400 RU/s, manual)           │
│                                                         │
│  prod → emulator: items leave prod.                     │
│  Reading spends RU on prod, writing spends RU on        │
│  emulator. The cost cannot be known up front; it is     │
│  projected once the first page has been copied.         │
│                                                         │
│  enter review   tab next field   esc cancel             │
└─────────────────────────────────────────────────────────┘
```

`enter` does not start anything. It moves to the review step, iteration 11's
`panes.Confirm`, which restates the job and asks for the target account's name:

```
┌─ Clone container ───────────────────────────────────────┐
│  prod / sales.orders  →  emulator / sales.orders        │
│                                                         │
│  Creates database sales on emulator (it does not exist).│
│  Creates container orders at 400 RU/s manual. On a      │
│  billed account that costs money until it is deleted.   │
│  Copies about 30,112 items. Items changed on prod while │
│  the copy runs may or may not be included.              │
│  Not copied: stored procedures, triggers, functions,    │
│  _ts and _etag values, change feed history.             │
│                                                         │
│  Type the target account name to confirm:               │
│  > emulator                                             │
│                                                         │
│  enter clone   esc back                                 │
└─────────────────────────────────────────────────────────┘
```

The account name is what is typed back, for every clone, same-account included. The
container name was typed a moment ago and retyping it proves nothing; the account was
chosen by cycling a field, which is the easiest thing here to get wrong and the most
expensive.

The progress view, `panes.CloneProgress`, takes the screen like every other overlay:

```
┌─ Clone · prod/sales.orders → emulator/sales.orders ─────┐
│  Copying items                                          │
│  ████████████░░░░░░░░░░░░░░░░░░  41%                    │
│                                                         │
│  Items      12,400 of about 30,112      skipped 0       │
│  Rate       212 items/s                 about 1m 24s    │
│  RU read    1,912.40 on prod                            │
│  RU write   78,204.11 on emulator                       │
│  Projected  about 194,000 RU in total                   │
│  Writers    3 of 4                      throttled twice │
│                                                         │
│  The source can change while this runs. This is a copy, │
│  not a snapshot.                                        │
│                                                         │
│  esc hide   x stop                                      │
└─────────────────────────────────────────────────────────┘
```

A database clone shows one row per container above the same counters, which then
describe the container in progress:

```
│  hr → hr-copy on emulator              2 of 2 containers│
│  ✓ departments     6 items        41.20 RU              │
│  ▸ employees       120 of about 400                     │
```

While the view is hidden the status bar carries one extra field,
`clone prod/sales.orders → emulator 41% (y)`, through a new
`StatusBar.SetClone(label)`. It belongs to the job, not to an account, so it stays
where it is through every switch and always names both ends; `setActive` never touches
it. When a hidden job ends it reads `clone done (y)`, `clone stopped (y)` or
`clone failed (y)` until the view has been opened and closed once. It is text only; the
bar's spinner stays bound to `Progress.Running`, the query run.

When a job stops short, by `x`, by a failure, or because retries ran out, the view
changes to its ended form and says what is left behind:

```
│  Stopped. emulator/sales.orders holds 12,400 of about   │
│  30,112 items and is incomplete.                        │
│                                                         │
│  r resume   d delete the partial target   esc keep it   │
```

The bar is drawn with `strings.Repeat`, not `bubbles/progress`, which would bring
`harmonica` into the module for an animation nobody asked for. `theme.IconSet` gains
`BarFull` and `BarEmpty` (`█`/`░`, ASCII `#`/`-`).

## Interaction

| Key | Where | Action |
|---|---|---|
| `y` | catalog, container row | open the clone prompt for that container |
| `y` | catalog, database row | open the clone prompt for that database and every container in it |
| `y` | catalog, any row or none, on any account, while a clone exists | reopen the progress view; no second prompt |
| `tab`, `enter`, `esc` | prompt, review | as iteration 11's `Form` and `Confirm` define them |
| `esc` | progress view | hide it; the clone keeps running |
| `x` | progress view, running | stop after the writes in flight |
| `r` | progress view, ended short | resume from the last page that was fully written |
| `d` | progress view, ended short | delete the partial target, through `Confirm` |
| `esc` | progress view, ended | close it and keep whatever exists |

`y` (`KeyMap.Clone`, help text "clone") is free in `internal/tui/keys.go`, in the
README key table, and in everything plans 11–17 reserve: `n`, `c`, `d`, `t`, `i` in
the catalog, `ctrl+g`, `ctrl+space`, `ctrl+s`, `ctrl+l`, `ctrl+b`. Iteration 14 adds no
catalog key; its `a` and `x` live in the switcher. `p` was the other candidate and
stays unbound. No function key is bound.

**The source is always on the account the session is on**, because `y` acts on a row
of the visible tree: `Catalog.SelectedNode` and `Node.Path`, exactly as today. There is
no account row and nothing to clone an account from.

**Reopening the progress view.** A clone exists from the moment the review is
confirmed until its ended view has been closed. While one exists, `y` in the catalog
pane means "show the clone" and is answered before the row under the cursor is looked
at. It works on any row, on an empty tree, and on whichever account the session has
switched to since, the source and target included or not. The status bar field ends in
`(y)` as the reminder. There is no separate global key: every `ctrl+letter` that
survives the editor is spoken for, and one key that means "clone" in both states is
easier to remember than two. From the editor or the results it is `tab` to the catalog,
then `y`.

The progress view is an overlay, and overlays swallow `ctrl+g` (iteration 14). To
switch accounts during a clone: `esc` to hide the view, `ctrl+g`, switch, and `y` to
see it again from there.

`esc` hides rather than stops because `Close` means "close this overlay" everywhere
else, and a clone that died whenever someone looked away from it would make "the
session stays usable" a lie. `x`, `r` and `d` are local to the progress view, where
they echo what they mean in the catalog: end something without destroying the source
(`Disconnect`), try again (`Refresh`), delete.

The binding is disabled, and so absent from the help overlay, unless the active
account's connection implements `DefinitionReader`. That is the `key.WithDisabled`
pattern iteration 11 uses for its four keys, re-evaluated in `setActive` like theirs.
Whether a target can be written to is not known until it is connected, so that check
belongs to the prompt. A source with no `ItemScanner` offers `definition only`.

### Prompt fields

| Field | Values | Default |
|---|---|---|
| Target account | every known account that is not read-only, with its state | the source's account when it is writable, else the first writable one by name |
| Database | text | the source database (container clone only) |
| Name | text | source name plus `-copy` same-account; the source name cross-account |
| Copy | `definition only` · `definition and items` | `definition and items` |
| Definition | `full` · `portable` | `full` same-account, `portable` cross-account |
| Throughput | `minimum` · `same as source` · `none` | see below |

- Defaults follow the target account as it is cycled. A name the user has edited is
  never rewritten.
- **Which accounts are offered.** Every account `accountSet` knows, connected or not,
  the connect form's additions included, in the switcher's order, each with the state
  label the switcher uses (`emulator · connected`, `local-b · not connected`). The
  user does not have to have visited the target first: needing a detour through the
  switcher to use an account as a destination would be a rule with no reason behind
  it.
- **A target that is not connected is connected by the prompt.** `enter` on the form
  puts the account into `connecting` and issues the same `openAccount` with a ping
  that the switcher issues, under `connectTimeout`. The form shows
  `connecting local-b…` and stays open. On `AccountConnectedMsg` the account is
  `connected` in the background, exactly as if it had been visited and left. The
  session does not move, because the switcher is not waiting for it, and `Prepare`
  runs next. A failure closes the connection and shows under the fields, and `enter`
  retries. `ErrCredentialsNeeded` is not answered with the connect form, since an
  overlay does not open an overlay: the form says
  `local-b has no key: add it from the account switcher (ctrl+g), then clone again`.
  A connection opened this way stays open after the clone, like any visited account,
  until `x` in the switcher or quit.
- A connected target whose connection does not implement `CatalogAdmin`, or
  `ItemWriter` when items are asked for, is refused in the form by name.
- **Read-only accounts.** Iteration 17 owns the key: `Profile.ReadOnly *bool`,
  `Profile.IsReadOnly()`, delivered as `Account.ReadOnly`. With `read_only` absent only
  a loopback endpoint (`localhost`, `127.0.0.1`, `::1`) is writable, so every
  real-account profile is read-only until someone sets `read_only = false`, and
  `alchemist --read-only` makes the whole session so. A read-only account is never
  offered as a clone target, and the prompt says why under the field, once per account
  left out: `prod is read-only; set read_only = false on the profile to write to it`.
  It is always a valid source. With no writable account at all the field is empty,
  the same lines explain it, and `enter` does nothing. If 17 has not landed when this
  does, `Account.ReadOnly` does not exist yet and every known account is offered; the
  rule arrives with the field.
- For a database clone the Database field is absent and Name is the new database.
- A container clone into a database that does not exist creates that database, with no
  throughput of its own, and the review step says so.

### Throughput

Iteration 11 owns the vocabulary (`adapter.Throughput`, `ThroughputMode`) and the rule
that limits are the service's to enforce. Cloning adds a choice of three:

| Choice | Target gets |
|---|---|
| `minimum` | manual `clone.MinimumRUs` (400) where the source had capacity of its own |
| `same as source` | what `ThroughputEditor.Throughput` reads from the source, mode and RU/s |
| `none` | nothing provisioned: a serverless target, or a container drawing on its database |

The default is `minimum`. A source at 40,000 RU/s autoscale must never be duplicated
at that price by pressing `enter` twice. It becomes `none` when the source itself
reads `ThroughputNone`, and when the source reads `ThroughputShared` and the target
database already exists. A source account with no `ThroughputEditor` offers `minimum`
and `none` only.

Nothing detects a serverless account up front, since the SDK exposes no account
properties (iteration 12). Choosing `minimum` against one is refused by the service,
the refusal shows verbatim in the form, and `none` is one keystroke away. The slow
copy that 400 RU/s implies is visible in the rate and the time left, and because the
session stays usable, iteration 11's `t` on the target raises it mid-clone.

## What is and is not copied

Verified against `azcosmos` v1.5.0 in the module cache. A Cosmos definition travels as
the JSON `ContainerProperties.MarshalJSON` writes and `UnmarshalJSON` reads back; the
identity fields (`id`, `_rid`, `_etag`, `_self`, `_ts`) are reset before the create.

| Thing | `full` | `portable` | Notes |
|---|---|---|---|
| Partition key paths, kind, version | yes | yes | `PartitionKeyDefinition{Kind, Paths, Version}`; `MultiHash` for a hierarchical key |
| Indexing policy | yes | without vector and full-text indexes | `IndexingPolicy` |
| Unique key policy | yes | yes | `UniqueKeyPolicy` |
| Default TTL | yes | yes | `DefaultTimeToLive` |
| Analytical store TTL | yes | no | `AnalyticalStoreTimeToLiveInSeconds`; needs an account feature a target may lack |
| Conflict resolution policy | yes | no | `ConflictResolutionPolicy`; a custom one names a stored procedure that is not copied |
| Vector embedding, full-text policies | yes | no | `VectorEmbeddingPolicy`, `FullTextPolicy` |
| Throughput | by choice | by choice | `ReadThroughput` → `ThroughputProperties` (`ManualThroughput`, `AutoscaleMaxThroughput`); `CreateContainerOptions.ThroughputProperties` on create |
| Database shared throughput | by choice | by choice | `DatabaseClient.ReadThroughput`, `CreateDatabaseOptions.ThroughputProperties` |
| Item `id`, `ttl`, every user field | yes | yes | bodies are copied as bytes, never decoded into Go numbers |
| `_rid`, `_self`, `_etag`, `_attachments`, `_ts` | no | no | stripped; the target assigns its own |
| Computed properties, change feed policy, client encryption policy, spatial config | no | no | `ContainerProperties` has no field for them and `UnmarshalJSON` reads named attributes only, so they are lost on read and cannot even be reported |
| Stored procedures, triggers, user-defined functions | no | no | the SDK has the resource type constants and no public client for them |
| Change feed history and continuation state | no | no | the target's feed starts with the copy |
| Users, permissions, RBAC assignments, conflicts feed | no | no | no public client; RBAC is control plane |
| Autoscale increment percentage | no | no | readable (`AutoscaleIncrement`) but iteration 11's `Throughput` has no field for it |

`portable` exists because the most useful target, the emulator, is the one most likely
to refuse a policy. What it keeps is what decides how data is laid out and queried;
what it drops is what depends on an account feature. The Cosmos adapter owns that
split, since only it knows what its policies mean.

Two consequences of stripping `_ts`: every copied item's TTL clock restarts at the
moment it is written, and "last modified" ordering is the copy order, not the
source's. Both are stated in the README section.

An item the target cannot take is skipped, counted, and named in the log, never
silently dropped. Two known causes:

- No value at a partition key path. The SDK builds keys from strings, bools, numbers
  and null (`NewPartitionKeyString`, `AppendBool`, `AppendNumber`, `AppendNull`) and
  has no public constructor for the undefined value such an item is filed under.
- An integer key past 2^53. `NewPartitionKeyNumber` takes a `float64`, so the key
  header would disagree with the body.

The summary reports the count. More than `clone.MaxSkipped` (100) ends the job,
because at that point something systematic is wrong.

## Adapter contract

Iteration 11 already defines creating and deleting (`CatalogAdmin`, `ContainerSpec`,
`DatabaseSpec`) and capacity (`ThroughputEditor`, `Throughput`). Cloning **reuses all
of it unchanged** and adds one field to `ContainerSpec`, plus three optional
interfaces, found by comma-ok assertion where `AccountConnectedMsg` lands and kept per
account, as iteration 14 moved the others.

```go
// PolicyDocument is a backend's own description of a container beyond its
// name, partition key and throughput. Only the adapter named by Backend can
// read Raw; the zero value means the backend's defaults.
type PolicyDocument struct {
    Backend string
    Raw     json.RawMessage
}

type ContainerSpec struct {
    // ...Database, Name, PartitionKeys, Throughput as iteration 11 has them
    Policies PolicyDocument
}

type DefinitionFidelity int

const (
    DefinitionFull DefinitionFidelity = iota
    DefinitionPortable
)

// DefinitionReader reads what CatalogAdmin.CreateContainer needs to make a
// container like an existing one.
type DefinitionReader interface {
    ContainerDefinition(
        ctx context.Context, path []string, fidelity DefinitionFidelity,
    ) (ContainerDefinition, error)
}

type ContainerDefinition struct {
    PartitionKeys []string
    Policies      PolicyDocument
    Size          SizeEstimate
}

// SizeEstimate is what the backend believes a container holds. Known is
// false when it would not say; Items and Bytes then mean nothing.
type SizeEstimate struct {
    Items int64
    Bytes int64
    Known bool
}
```

`CreateContainer` with a `Policies.Backend` that is not its own returns
`adapter.ErrUnsupported`. That single check is what keeps a cross-adapter clone from
half-working.

Typed common fields plus one opaque blob is deliberate. The TUI shows partition key
paths and sizes and must not learn what an indexing policy is (iteration 12's rule);
the adapter must lose nothing it can carry. A typed mirror of every Cosmos policy in
`internal/adapter` would do both jobs worse.

### The scan primitive

```go
// ItemScanner reads every item of a container, a page at a time, in an order
// the backend keeps stable for as long as a ScanPosition is outstanding.
type ItemScanner interface {
    ScanItems(ctx context.Context, request ScanRequest) (ItemScan, error)
}

type ScanRequest struct {
    Container []string
    From      ScanPosition // zero value: the beginning
    PageSize  int32        // zero: the connection's page_size
}

// ScanPosition resumes a scan after the page that carried it. It is opaque
// and valid only for the container and the backend that issued it.
type ScanPosition string

type ItemScan interface {
    NextPage(ctx context.Context) (ItemPage, error)
    HasMore() bool
    Close() error
}

type ItemPage struct {
    Items         []json.RawMessage // whole items as stored, system fields included
    Next          ScanPosition      // empty on the last page
    RequestCharge float64
}
```

Decisions:

- **A plain cross-partition `SELECT * FROM c`, not a query per partition range.**
  `QueryOptions` in v1.5.0 has no feed range field; `ReadFeedRanges` exists, but only
  `ChangeFeedOptions.FeedRange` accepts what it returns. The gateway already walks the
  ranges in a fixed order for an unordered query, and
  `QueryItemsResponse.ContinuationToken` with `QueryOptions.ContinuationToken` makes it
  resumable.
- **Not the existing `adapter.Cursor`.** `Page` pre-renders `Rows` and `Columns` for a
  table nobody will look at, and it hides the continuation token, which is the whole
  point here. `ItemScan` has the same three-method shape on purpose, so whoever holds
  one follows the ownership rule `fetchPage` documents.
- **Items arrive unmodified.** Stripping system fields is the clone's business
  (`clone.StripSystemFields`). Another consumer may want `_ts` and `_etag`.
- **"Ordered" means stable, not sorted.** The guarantee is that resuming from `Next`
  yields exactly the items that would have followed, so no item is read twice or
  missed because of the pause itself. Writes to the source in the meantime are a
  different matter, and the Goal says so.
- The change feed read from the beginning, per feed range, is the future parallel
  reader behind the same interface. Reading is not the bottleneck, so it is not built.

### Writing items

```go
// ItemWriter writes whole items into an existing container.
type ItemWriter interface {
    OpenItemSink(ctx context.Context, container []string) (ItemSink, error)
}

// ItemSink upserts items into one container and reports what each write
// cost. It is safe for concurrent use.
type ItemSink interface {
    Upsert(ctx context.Context, item json.RawMessage) (requestCharge float64, err error)
}

// ThrottledError is a write or read the backend refused for rate, after the
// adapter's own retries. RetryAfter is zero when the backend named no delay.
type ThrottledError struct {
    RetryAfter time.Duration
    Err        error
}

// PartitionKeyValues reads the value at each key path out of item, in path
// order. ErrNoPartitionKey names the first path with nothing at it.
func PartitionKeyValues(item json.RawMessage, paths []string) ([]json.RawMessage, error)
```

- **`PartitionKeyValues` is shared with iteration 17**, whose batch validation checks
  a document body against the key it was given. Whichever of 17 and 18 lands first
  introduces it. Its result is the underlying type of 17's `adapter.PartitionKey`
  (`[]json.RawMessage`, one JSON scalar per path); if 17 is first the function returns
  that named type.
- **The sink extracts the partition key; the caller never passes one.**
  `OpenItemSink` reads the target's key paths once. They are the same string
  `Node.Meta[MetaPartitionKey]` carries, split on `PartitionKeyPathSeparator`, so a
  hierarchical key is simply more than one path. `PartitionKeyValues` lives in
  `internal/adapter` beside `PartitionKeyNodes`, because the mock and Cosmos must
  extract identically, and a nested path (`/address/country`) is walked, not
  string-matched. Cosmos turns the values into
  `NewPartitionKey().AppendString/AppendNumber/AppendBool/AppendNull`.
- **Upsert, not create.** The target is new, so either would succeed once. But a write
  whose response was lost has to be retried, and a resumed job rewrites its last
  partial page. Upsert makes both idempotent; create would turn them into 409s.
  `ItemOptions.EnableContentResponseOnWrite` stays false, so no body comes back.
- **One item per request.** The SDK has no bulk executor.
  `ExecuteTransactionalBatch` could carry several items of one logical partition, but
  iteration 17 is defining batch writes concurrently and this plan does not depend on
  it. A batching `ItemSink` can replace the Cosmos one later without touching the
  engine.
- **Throttling.** `azcosmos.ClientOptions` embeds `policy.ClientOptions`, so azcore's
  retry policy is already in the pipeline: 429 is in its default status list, it
  sleeps for `Retry-After-Ms` / `x-ms-retry-after-ms` when the service sends one, and
  gives up after `MaxRetries` (3 by default). What arrives at the adapter after that
  is an `*azcore.ResponseError` with status 429; the adapter maps it to
  `*adapter.ThrottledError`, reading the delay from `RawResponse`. The engine decides
  what happens next, and the adapter's retry settings are left alone.

## The copy engine

`internal/clone` is pure: it imports `internal/adapter` and nothing else of ours, no
bubbletea and no concrete adapter. Its unit is one **step**, because that is what a
`tea.Cmd` is.

```go
type Endpoint struct {
    Account string   // for messages and logs only
    Path    []string // [database] or [database, container]
}

type Content int // DefinitionOnly | DefinitionAndItems

type Job struct {
    Source, Target Endpoint
    Content        Content
    Fidelity       adapter.DefinitionFidelity
    Capacity       Capacity // Minimum | SameAsSource | None
    Writers        int
}

// Source and Target are the capabilities a job needs, built by the caller
// from whatever its two connections satisfy.
type Source struct {
    Catalog     adapter.Catalog
    Definitions adapter.DefinitionReader
    Throughput  adapter.ThroughputEditor // nil: the backend has no such concept
    Items       adapter.ItemScanner      // nil allowed for DefinitionOnly
}

type Target struct {
    Catalog adapter.Catalog
    Admin   adapter.CatalogAdmin
    Items   adapter.ItemWriter // nil allowed for DefinitionOnly
}

func Prepare(ctx context.Context, job Job, source Source, target Target) (Plan, error)
func (p Plan) CreateNext(ctx context.Context) (Copy, error)
func (c *Copy) CopyPage(ctx context.Context) (Progress, error)
func (c *Copy) Position() adapter.ScanPosition
```

- `Prepare` reads and writes nothing on the target. It lists the source's containers
  for a database clone and reads each definition, size estimate and throughput. It
  checks through `Target.Catalog` that the target name is absent and fails with
  `clone.ErrTargetExists` when it is not. The `Plan` it returns is what the review
  step renders.
- `CreateNext` creates the database when the plan says so, then the next container,
  and opens its scan and its sink. A 409 from the service is `ErrTargetExists` too:
  the catalog check is a courtesy, the service is the guarantee.
- `CopyPage` reads one page, strips system fields, and upserts its items through at
  most `Writers` goroutines. It returns when every item of the page is written or
  skipped, or on the first failure. `Position` moves only after a fully written page,
  which is what makes `r` safe: a resumed job re-upserts at most one page.
- `Progress` is a plain value: items read, written and skipped, RU read and written,
  page duration, current writer count, throttle count, and whether the container is
  done.

**There is no merge mode.** A target that exists is refused, at `Prepare` and again at
create, with no override mark like the export prompt's `!`. An upsert into a live
container would overwrite items by `id`, which is a different feature with a different
confirmation. It is out of scope.

### Why a worker pool, and how it stays honest

A Cosmos write costs several times what reading the same item costs, and each upsert
is its own round trip. One writer on a 50 ms link tops out near 20 items a second
however much throughput the target has. Sequential writes would make a 100,000-item
clone an hour of waiting on latency. That is the real benefit CLEAN_CODE asks for.
Reading stays sequential, with no pipeline: one page in memory, the next read only when
the last is written. That is the backpressure, and the source is never asked for data
faster than the target takes it.

Kept small on purpose:

- The pool lives and dies inside one `CopyPage` call: a `sync.WaitGroup` and a
  buffered channel as the semaphore. No goroutine outlives the step and no state is
  shared beyond the page's counters, which the step owns and merges after `Wait`.
- `Writers` is configuration: `clone_writers` on the **target** profile, default 4,
  clamped to 1–16, delivered as `Account.CloneWriters`. Every engine test runs at 1
  and at 8, under `-race`.
- On a `ThrottledError` all writers pause at a shared gate for `RetryAfter` (one
  second when zero), the item is retried, and the writer count for the rest of the
  container steps down by one, never below 1 and never back up. `clone.MaxThrottles`
  (10) consecutive throttles on one item end the step with the error, and the job is
  resumable. The step-down is the whole policy: a target at 400 RU/s settles at one
  writer within a few pages instead of burning retries for the rest of the run.
- A `ThrottledError` on the read side waits the same way and retries the page. The
  source is never hit with parallel reads.
- Cancelling `ctx` stops new writes; writes in flight finish or fail on their own.

## Long-running job model

`Update` never blocks, and there is no goroutine the model does not know about. A job
is a chain of commands, each returning one message that starts the next. It is how
`runPlan` and `fetchPage` work today, stretched over more steps:

```
y → prepareClone ─ ClonePreparedMsg → form ─ enter → review ─ enter →
    createTarget ─ CloneTargetCreatedMsg → copyPage ─ ClonePageCopiedMsg → copyPage …
                                                    └ last page → createTarget (next
                                                      container) or CloneFinishedMsg
    any step ─ CloneFailedMsg → ended-short view
```

- The command in flight owns the `*clone.Copy`, as `fetchPage` owns its cursor, and
  hands it back in its message. The model never touches one a goroutine is using.
- Every message carries a `cloneID`, the counterpart of `runID`. A message for a job
  that is no longer current closes what it carries and is dropped.
- The model holds the job's `context.CancelFunc`. `x` calls it; the step in flight
  returns `CloneFailedMsg` wrapping `context.Canceled`, which renders as "Stopped",
  not as an error. Each step also gets its own deadline, `cloneStepTimeout` (two
  minutes), so a hung request ends the job instead of wedging it.
- Progress granularity is one page, `page_size` items (100 by default): fine enough
  for a moving bar, coarse enough that `Update` sees a few messages a second at most.
  Rate is a moving average over the last ten pages. Time left and projected RU scale
  what has been measured by `SizeEstimate.Items`. With `Known` false the view shows
  counts and rate only, with no percentage and no guess.
- **One clone at a time per session**, not per account. `y` during a clone reopens the
  progress view. Two jobs would compete for the same throughput, and the status bar
  has one field.
- **The rest of the UI stays usable, switching accounts included.** Queries run, the
  tree browses, other overlays open, `ctrl+g` moves the session anywhere. The job
  holds its two `adapter.Connection`s and the two account names from the moment it
  starts and never asks which account is active. An account the session leaves stays
  connected (iteration 14), so nothing is pulled out from under it. Every clone
  message carries `Source` and `Target` account names and is applied to the job, not
  to the visible account.
- **Refused while a job runs:** `x` in the switcher on the source or the target
  account, with `a clone is using prod: stop it first (y in the catalog)` under that
  row, where the switcher already draws failures; and iteration 11's `d` on anything
  inside the target, with the same words in the status bar. `x` on any other account
  works as usual.
- **Quitting mid-clone.** The first `q` or `ctrl+c` opens the progress view with
  `A clone is running. Quit again to stop it and quit; the target will be incomplete.`
  The second cancels the context, logs both endpoints and the counts, and quits
  through `Model.quit` as today. The partial target is left in place: deleting is
  never automatic, and never possible without typing a name.
- **Cleanup.** `d` in the ended-short view opens iteration 11's `Confirm` for exactly
  what the job created: the container for a container clone, the database for a
  database clone. A database the job created only to hold a cloned container is left
  alone. The delete is `CatalogAdmin.DeleteContainer` or `DeleteDatabase`, followed by
  the usual `CatalogChangedMsg` reload.
- On success, and after a cleanup delete, the job emits iteration 11's
  `CatalogChangedMsg` with `Account` set to the **target** account. Iteration 14
  routes it by that field, so the reload lands in the target's own `panes.Catalog`
  even when that account is in the background, and the new container is there on the
  next switch. `Select` moves the cursor to the new node only when the target is the
  active account; a background tree's cursor is never moved behind the user's back.
  The status bar notice reads
  `cloned 30,112 items to emulator/sales.orders (194,310.52 RU)`. The active account
  and every account's scope stay as they are. Clones are not query runs and are not
  written to `history.jsonl`; the log file has them.

### Cost and safety, in one place

| Risk | What the UI does |
|---|---|
| Unknown size | item count and bytes from the definition read, labelled "about"; never a `COUNT` query, which would itself spend RU in proportion to the container |
| Unknown RU | says so before starting; shows a projection from the first copied page onward, split by account |
| A new container bills | default `minimum`; the review step states the RU/s and that it costs money until deleted |
| Overwriting data | impossible: an existing target is refused twice, and there is no merge mode |
| Wrong account | source → target on the prompt, the review step, the progress title and the status bar; the target account's name typed to confirm |
| Data leaving an environment | cross-account prompts add `items leave <source>`; nothing blocks it, since that is the feature |
| Read-only target | never offered; the prompt names each one and how to change it |
| Switching accounts mid-clone | the job is bound to its two connections, not to the active account; the status bar field follows the session everywhere |
| Moving source | stated on the review step and on the progress view |

Where the numbers come from: `ContainerClient.Read` with
`ReadContainerOptions.PopulateQuotaInfo` returns the definition and the usage headers
in one request. Iteration 12 specifies the header parser; whichever of the two lands
first puts it in `internal/adapter/cosmos/quota.go` and the other reuses it. A header
that is missing or unparsable is `SizeEstimate{Known: false}`, and some emulator
images will take that path.

### Database clone

A database clone is its container clones in catalog order, one after another, under
one job and one confirmation.

- The target database must not exist. It is created first, then each container.
- Shared throughput: `ThroughputEditor.Throughput` on the source database says
  whether it has capacity of its own. If it does, the target database is created with
  it per the Throughput choice (`minimum`: manual 400; `same as source`: the same mode
  and RU/s; `none`: none). Containers that read `ThroughputShared` are created with
  `ThroughputShared`, and containers with dedicated capacity follow the choice.
  A shared database with many containers has a higher floor than 400. The service
  says so, the refusal is shown verbatim, and `same as source` is the way through. No
  limit is checked client-side, as in iteration 11.
- A container that fails ends the job. The containers already finished stay and are
  marked done, `r` resumes at the failed container's last position, and `d` offers
  the whole target database.
- The summary lists every container with its item count, skipped count and RU, and
  totals per account.

## Scope

- `internal/adapter/adapter.go`
  - `PolicyDocument`, `ContainerSpec.Policies`, `DefinitionFidelity`,
    `DefinitionReader`, `ContainerDefinition`, `SizeEstimate`, `ItemScanner`,
    `ScanRequest`, `ScanPosition`, `ItemScan`, `ItemPage`, `ItemWriter`, `ItemSink`,
    `ThrottledError`, `PartitionKeyValues`, `ErrNoPartitionKey`.
  - Reused from iteration 11 without change: `CatalogAdmin`, `ThroughputEditor`,
    `DatabaseSpec`, `Throughput`, `ThroughputMode`, `ErrUnsupported`.
- `internal/adapter/cosmos`
  - `definition.go` — `ContainerDefinition` over `ContainerClient.Read`; the
    full/portable split; `admin.go`'s `CreateContainer` learns to start from
    `Policies.Raw` when it is present, resetting the identity fields.
  - `scan.go` — `ScanItems` over `NewQueryItemsPager("SELECT * FROM c",
    NewPartitionKey(), …)`.
  - `sink.go` — `OpenItemSink`, the partition key build, `UpsertItem`.
  - `errors.go` — `wrap` maps a 429 to `*adapter.ThrottledError`.
- `internal/adapter/mock` — on top of iteration 11's per-adapter fixture: containers
  hold items; `ScanItems` pages over them with a decimal offset as the position; the
  sink upserts by `id` and key values; `ContainerDefinition` returns a mock
  `PolicyDocument`. Options: `WithItems(path, count)`, `WithThrottleAt(k, retryAfter)`,
  `WithWriteErrorAt(k)`, `WithUnknownSize()`, and `OpDefinition`, `OpScan`, `OpUpsert`
  for `WithError`. A recording hook reports the highest number of concurrent `Upsert`
  calls, so the writer bound is asserted rather than assumed.
- `internal/clone` (new, pure) — `Job`, `Plan`, `Copy`, `Progress`,
  `StripSystemFields`, `MinimumRUs`, `MaxSkipped`, `MaxThrottles`, `ErrTargetExists`.
- `internal/tui`
  - `clone.go` — `openClone`, overlay key handling, the five commands, the quit guard,
    the disconnect and delete guards.
  - `messages.go` — `ClonePreparedMsg`, `CloneTargetCreatedMsg`, `ClonePageCopiedMsg`,
    `CloneFinishedMsg`, `CloneFailedMsg`, each with `cloneID`; `OpClone`.
  - `keys.go` — `Clone` in the Catalog section; `CloneKeys()` for the progress view's
    hint line, as `ExportKeys()` does for the export prompt.
  - `app.go` — `overlayClone`, `overlayCloneProgress`; per-account capability
    assertions beside iteration 11's; `Clone` enabled per the active account in
    `setActive`.
  - `accounts.go` — `Account.CloneWriters`; the prompt's background connect, which
    reuses `openAccount` with a ping and does not set the account the switcher waits
    for; the switcher's `x` guard. `Account.ReadOnly` is iteration 17's.
- `internal/tui/panes`
  - `form.go` — `NewCloneForm(source, targets)`: a display-only block and one more use
    of the cycling choice field. No new field kind. `targets` are
    `[]CloneTarget{Name, State AccountState}` plus the read-only names left out, so the
    pane draws states with iteration 14's `AccountState` and learns nothing else about
    accounts.
  - `progress.go` — `CloneProgress`; `StatusBar.SetClone`.
- `internal/theme` — `IconSet.BarFull`, `IconSet.BarEmpty`.
- `internal/config` — `clone_writers` on `Profile` (`omitzero`, validated 1–16).
- `cmd/root.go` — passes `clone_writers` through. No new wiring otherwise: the
  capabilities are interfaces on the connections `Opener` already returns.
- `README.md` — a "Cloning" section: what is copied, what it costs, the snapshot
  caveat, the TTL restart; the key table; `clone_writers`.

## Out of scope

- Server-side container copy jobs and anything else on the ARM control plane.
- Continuous sync, incremental re-clone, or following the change feed after the copy.
- Merging into an existing container or database, with or without overwrite.
- Stored procedures, triggers and user-defined functions: `azcosmos` v1.5.0 has no
  client for them, so copying them means hand-written REST calls.
- Cross-adapter clones. `PolicyDocument.Backend` refuses them at create.
- Point-in-time consistency. Iteration 19 owns snapshots.
- Resuming after the session has ended. The position lives in memory; persisting it
  needs a job file and a story for a target that changed in between.
- Several clones at once, a job queue, or a background daemon.
- A filtered clone (`WHERE`), a sampled clone (`TOP n`), or renaming fields in flight.
- An RU budget that stops the job at a ceiling. The projection makes the cost visible;
  enforcing one is a follow-up.
- Low-priority requests (`QueryOptions.PriorityLevel`, `ItemOptions.PriorityLevel`
  exist in v1.5.0). They need an account feature this tool cannot detect.
- Verifying the copy with a count or checksum query afterwards: it spends RU on both
  sides to confirm what the counters already say.

## Relationship to other iterations

- **11, catalog management.** Hard dependency. Reused as is: `CatalogAdmin` (all four
  methods; the deletes are the cleanup), `ThroughputEditor.Throughput`, `Throughput`,
  `DatabaseSpec`, `ErrUnsupported`, `panes.Form` and its cycling choice field,
  `panes.Confirm`, `CatalogChangedMsg`, `RefreshPath`, `Select`, the fetch tokens, the
  disabled-binding pattern, and the rules "service errors verbatim" and "no client-side
  limit checks". Added to its contract: one field, `ContainerSpec.Policies`, whose zero
  value is exactly today's behavior, so `c` needs no change.
- **12, info view.** Soft. Both read `ContainerProperties` with `PopulateQuotaInfo`;
  they share the quota header parser and nothing else. `Inspector` renders strings for
  people, `DefinitionReader` carries a document for `CreateContainer`. Neither is built
  on the other, which is the same split 12 draws against 11's `ThroughputEditor`.
- **13, autocomplete.** A cloned container feeds the index through the catalog
  messages it already listens to. Nothing to do.
- **14, multiple accounts.** Hard dependency, in its switcher form: the session is on
  one account, the catalog is that account's tree, and accounts left behind stay
  connected. The source is `Catalog.SelectedNode` on the active account. The target is
  a name `accountSet.known` answers for. The job borrows two connections and closes
  neither. Used as they are: `openAccount` with its ping and `connectTimeout`,
  `AccountConnectedMsg` and its late-answer rule, `AccountState` for the target
  labels, the per-account `panes.Catalog` that `CatalogChangedMsg{Account}` reloads in
  the background, `setActive` as the hook that re-evaluates the `Clone` binding, and
  "overlays swallow `ctrl+g`". New rules: the prompt may connect an account without
  moving the session, and the switcher's `x` is refused on an account a clone is
  using. Before 14 lands there is one account and the Target account field has one
  value; steps 1–8 do not need it.
- **15, saved queries; 16.** No interaction.
- **17, transactions.** Two touch points and no dependency. `read_only` is its key and
  this plan obeys it as written under "Prompt fields": with the key absent only
  loopback endpoints are writable, a read-only account is never a target and always a
  valid source. `adapter.PartitionKeyValues` is shared, introduced by whichever lands
  first. Its `Batcher` is a different write path from `ItemSink` (many operations, one
  partition key, all or nothing), and a sink that packs a page's items into batches
  per logical partition is the obvious later optimization behind the same interface.
- **19, snapshots.** `ItemScanner` is written to be shared: a resumable full read in
  stable order, items unmodified with system fields intact, RU reported per page,
  position opaque. A snapshot is the same scan with a file as the sink, and
  `PartitionKeyValues` plus `ItemWriter` are what a restore needs. Iteration 19 should
  consume `ItemScanner` and `DefinitionReader` rather than define a second scan or a
  second definition read. If it needs more, such as a consistent cut or a time bound,
  that belongs in `ScanRequest` as a new field whose zero value is today's behavior,
  not in a parallel interface. Iteration 19 does exactly that (`Since`, `Projection`)
  and puts the list of system fields in one place, `adapter.SplitSystemFields`;
  `clone.StripSystemFields` calls it rather than keeping a second list, whichever of
  the two lands first. The two share the one-background-job-per-session rule: `y` is
  refused while a capture runs, and 19's `s` while a clone does.

## Steps

Each step ships and leaves `make all` green.

1. Contracts in `internal/adapter`; `PartitionKeyValues` with its table test; the
   mock: items in containers, scan, sink, definition, the injection options. Mock tests
   first, since they are the contract the Cosmos side has to meet.
2. `internal/clone`: `Prepare`, `CreateNext`, `StripSystemFields`, definition-only jobs
   for one container, all against the mock.
3. `clone.CopyPage` with `Writers` fixed at 1: paging, skips, resume from a position.
   Then the pool, the throttle gate and the step-down, under `-race`.
4. Cosmos: `ContainerDefinition` and create-from-policies, with integration tests.
   Ships nothing visible.
5. TUI, definition only, same account: `y`, `NewCloneForm`, the review step,
   `createTarget`, the reload. **Ships: clone a container's definition.**
6. Cosmos `ScanItems` and `OpenItemSink` with integration tests, including a
   two-path hierarchical key.
7. TUI items: `CloneProgress`, the step chain, `x`, `r`, `d`, the status bar field,
   the quit guard. **Ships: full container clone within an account.**
8. Database clone: sequencing, shared throughput, the summary.
9. Cross-account: the Target account field over every known account, the background
   connect from the prompt, the switcher's `x` guard, `CatalogChangedMsg` into a
   background pane, `y` reopening from any account, `portable` as the default,
   `clone_writers`, the read-only rule. **Ships: prod → emulator.**
10. README, help sections, plan statuses.

## Testing

**Unit — `adapter.PartitionKeyValues`:**
- A single path; a nested path; three paths in order.
- Each scalar kind; an explicit `null` is a value.
- A missing path and an object at the path both return `ErrNoPartitionKey` naming the
  path.
- A number keeps its digits, since the value stays `json.RawMessage`.

**Unit — `clone`** (mock adapter, every case at `Writers` 1 and 8, `-race`):
- A definition-only job creates the target with the source's key paths and
  `PolicyDocument`, and writes no item.
- An existing target fails `Prepare` with `ErrTargetExists` and creates nothing. A
  target that appears between `Prepare` and create fails the same way.
- `minimum`, `same as source` and `none` produce the expected `Throughput` for a
  dedicated, a shared and a capacity-less source.
- Items: every source item is in the target; `_rid`, `_self`, `_etag`, `_attachments`
  and `_ts` are gone; `id`, `ttl` and a number like `12345678901234567890` are
  byte-identical.
- An item with no value at a key path is skipped and counted. The 101st skip ends the
  job.
- The highest concurrent `Upsert` count never exceeds `Writers`.
- `WithThrottleAt(k)`: the item is retried after `RetryAfter` on a fake clock, the
  writer count steps down by one, and the final counts are exact.
- Ten consecutive throttles end the step with a `ThrottledError`, and `Position()`
  still names the last complete page.
- `WithWriteErrorAt(k)` ends the step. A new `Copy` from `Position()` finishes the
  job, and the target holds each item exactly once.
- A cancelled context returns promptly, starts no new write, and leaks no goroutine.
- A database job runs its containers in catalog order. A shared-throughput source
  yields a target database with capacity and containers with `ThroughputShared`. A
  failure in the second container leaves the first complete and the position in the
  second.

**TUI, mock adapter** (`internal/tui/test/clone_test.go`):
- `y` on a container opens the form and issues exactly one `prepareClone`. With no
  `DefinitionReader` on the active account `y` does nothing.
- The form's `enter` opens the review and writes nothing. In the review, `enter` does
  nothing until the target account's name matches exactly, and `esc` returns to the
  form with its values intact.
- A refused create keeps the form open with the service's text under the fields.
- Every `ClonePageCopiedMsg` issues exactly one next `copyPage`. A message with a
  stale `cloneID` issues none and closes what it carries.
- With a clone running: `esc` hides the view and the status bar shows the clone field;
  `ctrl+r` runs a query that reaches the active account's connection; `y` reopens the
  view and starts nothing.
- Switching mid-clone: hide, `ctrl+g`, `enter` on a third account. Pages keep arriving
  and are counted, the status bar field is unchanged but for its percentage, and `y`
  on a row of the third account's tree, and on its empty tree, reopens the view
  rather than a prompt. `ctrl+g` with the view open does nothing.
- A job that ends while hidden leaves `clone done (y)`; `y` opens the ended view, and
  after `esc` there `y` on a container opens a prompt again.
- The Target account field lists every known account that is not read-only, with its
  state, and no read-only one. Each account left out has its line under the field.
  With every account read-only the field is empty and `enter` issues nothing.
- A not-connected target: `enter` calls `Open` once and pings, the form shows
  `connecting`, the session stays on the source account with its tree and scope, and
  the target ends up `connected` in the switcher. A failed `Open` and a failed ping
  each show in the form, close what they opened, and retry on the next `enter`.
  `ErrCredentialsNeeded` shows the switcher hint and opens no form.
- After a cross-account clone with the session still on the source, the target
  account's pane holds the new container: switching there shows it with no request
  made at switch time, and the source tree's cursor never moved.
- `x` cancels the job context; the ended view names the partial target; `r` continues
  to the right total; `d` opens `Confirm` for the target path and nothing else.
- `q` during a clone does not quit. A second `q` cancels the job and quits, and both
  accounts are closed exactly once.
- `x` in the switcher on the source or the target account mid-clone closes nothing and
  draws the notice under that row; on a third account it disconnects as usual. `d` on
  the target container is refused likewise. After the job ends `x` works on both.
- Cross-account, with two `mock.New()` adapters: items arrive in the second adapter
  only, and the first receives no write call of any kind.
- A read-only source clones into a writable target, and the read-only account's
  connection records no write call.
- With `Known` false the view shows no percentage and no time left.
- Same-account success: the target's parent reloads, the cursor lands on the new node,
  and the scope and the active account are unchanged.
- `Clone` is grouped exactly once and appears in the help overlay and the README. With
  no `DefinitionReader` it is absent from both.

**Integration** (emulator, `//go:build integration`, `test/integration/clone_test.go`;
every created resource removed in `t.Cleanup`):
- Clone seeded `sales.orders` to `sales.orders-copy` with items. Partition key paths,
  kind and version match. The indexing policy JSON matches. Item counts match. A
  sampled item is equal once the five system fields are removed. RU read and written
  are both above zero.
- A container created in the test with the key `/tenantId`, `/userId` clones with
  `MultiHash` and both paths, and every item is readable under its full key.
- Definition only: the target exists and holds nothing.
- Clone `hr` to `hr-copy`: both containers, with counts.
- An existing target returns `ErrTargetExists` and changes nothing.
- Cancel after the first page leaves a partial target. Resuming completes it with no
  duplicates.
- Cross-account: two connections built from the same emulator settings stand in for
  two accounts, and the clone runs from one to the other. This is legitimate. An
  account is a profile, and iteration 14's own integration test uses two profiles on
  one endpoint.
- `full` fidelity against the emulator: if the image refuses a policy, skip with the
  refusal in the skip message, as iteration 11 does for autoscale.

**Manual checklist** (after `make emulator-up && make emulator-seed`):
- [ ] `--adapter mock`: clone a container; watch the bar; hide it, run a query, reopen
      it with `y`.
- [ ] Emulator: `y` on `sales.orders`, accept the defaults. `sales.orders-copy`
      appears, and `SELECT VALUE COUNT(1) FROM c` agrees on both.
- [ ] `y` on `hr`, name `hr-copy`. Both containers arrive and the summary lists them.
- [ ] `y` on `telemetry.events`, press `x` mid-copy, then `d` and type the name. The
      partial target is gone.
- [ ] The same again, but `r`. It finishes with the right count.
- [ ] Two profiles, `local-a` and `local-b`, both on `https://localhost:8081` with
      different `database` values. Launch `alchemist local-a`, never open the
      switcher, `y` on `sales.customers`, and pick `local-b · not connected` as the
      target. The form connects it, and the prompt, the review, the title and the
      status bar all show `local-a → local-b`. Typing `local-a` at the review does
      nothing.
- [ ] During that clone: `esc`, `ctrl+g`, switch to `local-b`. The status bar field is
      still there, `y` reopens the view, and when it finishes the new container is in
      `local-b`'s tree. Back in the switcher, `x` on `local-a` mid-clone is refused.
- [ ] A profile on a real endpoint with no `read_only` key: it is missing from the
      Target account field, its line explains why, and cloning *from* it into the
      emulator works untouched.
- [ ] Try to clone onto a name that exists: refused in the form, nothing created.
- [ ] `q` during a clone: the warning, then a second `q`. The log names the partial
      target.
- [ ] 80×24 with the prompt, the review and the progress view open in turn: nothing
      wraps into the border.
- [ ] Against a real account, once: `minimum` creates a 400 RU/s container, the
      writer count steps down under throttling, and the clone still finishes.

## Acceptance criteria

- A container or a database of the account the session is on can be cloned,
  definition only or with items, into that account or into any other known account
  that is not read-only, from the catalog with `y`, without visiting the target first.
- `prod` → `emulator` works with an untouched config: a read-only account is a valid
  source and is never offered as a target, and the prompt says how to change that.
- A clone survives any number of account switches. Its status bar field is visible on
  every account, `y` in the catalog reopens its view from any of them, and the target
  account's tree is up to date when the session gets there.
- No write reaches any adapter before the user has typed the target account's name,
  and no clone ever writes into a container or database that existed before it
  started.
- The prompt, the review step, the progress view and the status bar all name source
  and target accounts. A cross-account clone says that data leaves the source.
- Before starting, the UI states the estimated size and where it came from, that RU
  cannot be predicted, that new throughput costs money, and that the copy is not a
  snapshot. No RU is spent on estimating.
- A cloned Cosmos container has the same partition key definition (hierarchical keys
  included), indexing policy, unique keys and default TTL as its source. Everything
  the table marks "no" is documented as not copied.
- Every item is copied with its `id`, `ttl` and user fields byte-identical, without
  the five system fields, or is counted as skipped and named in the log. Nothing is
  dropped silently.
- `Update` never blocks. During a clone every pane, query runs, and `ctrl+c` keep
  working, and at most one clone runs per session.
- Writes never exceed `clone_writers` in flight. A throttled target slows the clone
  down, and only repeated throttling with no progress fails it.
- A stopped, failed or abandoned clone says exactly what it left behind, can be
  resumed within the session without duplicating items, and can be deleted only
  through iteration 11's typed confirmation.
- `internal/clone` imports no bubbletea and no concrete adapter. `internal/tui`
  imports `internal/clone` and the `internal/adapter` interfaces only. Capabilities
  are found by type assertion on connections `cmd` wired.
