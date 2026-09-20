# Iteration 16 — Multi-Way Cross-Container Joins

## Goal

Join more than two containers in one query. Iteration 10 simulates an inner hash join
of exactly two containers; a third is refused with `query.ErrUnsupported` ("a join over
more than two containers"). The refusal is honest but it lands on the most ordinary
question a document store raises: an order names a customer *and* a product, and
reading all three together takes three queries and a spreadsheet.

This iteration generalizes the join from a pair to a chain of N containers. It adds no
new kind of join. Everything iteration 10 refuses for two containers — `OUTER`,
non-equality `ON`, a `WHERE` condition over several sides, `ORDER BY` — stays refused
for N, for the same reason: the engine returns a right answer or an explicit error,
never an approximation.

It stays adapter-agnostic. The engine consumes `adapter.Connection` and `Cursor` only,
and is proven against the canned-page connection in `internal/query/test` and the mock
adapter.

## Supported forms

**1. A lookup star.** Every `ON` reaches back to the first container:

```sql
SELECT o.id, cu.name, p.name AS product
FROM sales.orders o
JOIN sales.customers cu ON o.customerId = cu.id
JOIN sales.products p ON o.sku = p.id
WHERE cu.region = "west"
```

**2. A chain.** Each `ON` reaches back to the container before it:

```sql
SELECT a.message, e.kind, d.site
FROM telemetry.alerts a
JOIN telemetry.events e ON a.deviceId = e.deviceId
JOIN telemetry.devices d ON e.deviceId = d.id
WHERE a.open = true AND e.kind = "pressure"
```

**3. Any mix of the two,** over any number of containers, across databases of the one
account, with `SELECT *` or a list of `alias.field [AS name]` items exactly as
iteration 10 has them.

### Grammar

```
SELECT list
FROM db.container [AS] [alias]
  ( [INNER] JOIN db.container [AS] [alias] ON ref = ref )+
[WHERE conditions]
```

- The chain is **left-deep and read in written order**. There are no parentheses
  around joins and no comma lists beside them.
- Each `ON` is **one equality between a field of the container that `JOIN` just
  introduced and a field of exactly one earlier container — any earlier one**, not only
  the one immediately to its left. Restricting it to the immediate left would refuse
  form 1, where `p` joins `o` across `cu`, and that is the common case. Either side of
  the `=` may be written first, as today. Key paths may be nested.
- The join graph this grammar can express is always a tree: every container after the
  first attaches to exactly one earlier container. **A cycle cannot be written**,
  because it would take a compound `ON` or a second `ON` for one container, and both
  are refused. No cycle detection is needed or built.
- **The same container may appear more than once** under distinct aliases (employees
  joined to their managers: `… e JOIN hr.employees m ON e.managerId = m.id`, on items
  that carry such a field; the seed's do not). It is two leaves and two queries, and
  already plans today for two containers; nothing about it is special at N. Aliases
  must be distinct, and a side with no alias is known by its container name, so a
  repeat written without aliases collides and is refused by the existing alias rule.
- There is **no limit on the number of containers**. Memory is bounded by rows held
  (below), and request units by what the user asked to read.

### Still refused

Each with `query.ErrUnsupported`, wrapped with the shape:

| Shape | Refused as |
|---|---|
| `LEFT`/`RIGHT`/`FULL`/`OUTER`/`CROSS JOIN` anywhere in the chain | `<modifier> JOIN` (unchanged) |
| `ON a.x > b.y`, `ON … AND …`, `ON a.x = 5`, no `ON` | `shapeOn`, reworded below |
| `ON` between two earlier containers, or reading the new one on both sides | `shapeOn` |
| `ON` reading a container joined further right | `shapeOn` |
| `JOIN t IN o.lines` anywhere before, between, or after the container joins | `JOIN ... IN alongside a cross-container join` |
| a container list beside a join (`FROM a.b, c.d JOIN e.f …`, `… ON … , x.y`) | `a container list mixed with a join` |
| two sides sharing an alias | `a join whose sides share an alias` (unchanged) |
| a `WHERE` conjunct reading two or more sides | `a WHERE condition over more than one side of a join` |
| `ORDER BY`, `GROUP BY`, `OFFSET`, `LIMIT` | `<keyword> in a cross-container join` (unchanged) |
| `TOP`, `DISTINCT`, `VALUE`, computed or nested `SELECT` items | `shapeProjection` (unchanged) |
| a subquery over another container, a second statement | unchanged |

`shapeOn` becomes "an ON clause other than one equality between a field of the joined
container and a field of an earlier one". `shapeMultiWay` is deleted.

## Hard limits

- **One side streams; the other N−1 are held in memory.** `max_join_rows` (default
  `query.DefaultMaxJoinRows`, 10 000) caps the rows held **in total across all held
  sides**, not per side. Exceeding it aborts the run with `query.ErrJoinTooLarge`;
  nothing partial is rendered. See "Memory" for why it is a total and why there is no
  second key.
- **RU cost is the sum of every leaf**, reported per container in
  `adapter.Stats.LeafCharges`, under the `simulated (client-side)` badge.
- **Every leaf runs on the active account's one connection.** Cross-account joins stay
  out.
- **The matches of one streamed row are expanded at once.** A row that matches `d`
  items on each of `k` sides yields `d^k` rows, all built before the first is served.
  Lookup joins match one item per side, so this is 1; the limit is recorded because it
  is the one place where memory is not governed by `max_join_rows`.

## Plan representation

`Plan` and `Leaf` keep their shape. `Join` trades its one key pair for one step per
`JOIN`:

```go
type Join struct {
    // Steps has one entry per JOIN, in written order: Steps[i] attaches
    // Plan.Leaves[i+1] to an earlier leaf.
    Steps   []JoinStep
    Columns []JoinColumn // empty for SELECT *
}

type JoinStep struct {
    Left     int      // index into Plan.Leaves of the earlier leaf, at most i
    LeftKey  []string // field path within an item of Leaves[Left]
    RightKey []string // field path within an item of Leaves[i+1]
}

// JoinColumn is one projected field; Side indexes Plan.Leaves.
type JoinColumn struct {
    Side  int
    Field string
    As    string
}
```

- `Join.LeftKey` and `Join.RightKey` are removed rather than kept beside `Steps`: two
  spellings of one fact is the duplication `CLEAN_CODE.md` forbids. A two-container
  plan is `Steps: []JoinStep{{Left: 0, LeftKey: …, RightKey: …}}`, and the assertions
  in `plan_test.go` move to that spelling. The *result* of executing it does not move
  by a byte (see Acceptance criteria).
- `JoinColumn.Side` keeps its name and its documented meaning — it already "indexes
  `Plan.Leaves`" — and simply takes values past 1. "Side" remains the package's one
  word for a leaf in its role within a join (`joinSide`, "build side").
- `Leaves` stay in written order, `Leaves[0]` being the `FROM` container, so
  `Plan.Scope()` and `WithDefaultScope` are untouched, and iteration 13's "the
  container `query.Plan.Leaves` gives for that alias" holds for N as it does for 2.
- `Leaf.Filtered` is unchanged and still the only size evidence the engine has.
- `Engine.Execute` accepts `HashJoin` when the plan fits its steps and returns
  `errMalformed` otherwise:

  ```go
  func joinFits(plan Plan) bool // len(Steps) == len(Leaves)-1 >= 1, every
                                // Steps[i].Left <= i, every Columns[i].Side in range
  ```

## Execution

### Which side streams

Sizes are unknown before the queries run, exactly as in iteration 10, so a pushed-down
filter is still the only evidence. The rule, in `streamedSide(plan Plan) int`, replaces
`buildSide`:

> The streamed side is the first leaf in written order with no pushed-down filter; when
> every leaf is filtered, it is `Leaves[0]`.

The side most worth *not* holding is the one expected to be largest: an unfiltered one,
and among those the `FROM` container, conventionally the fact table. For two containers
this is `buildSide` restated — left filtered only → right streams; every other case →
left streams — so which side is held, and therefore the row order, is identical.

In form 1 above `cu` is filtered, so `o` streams and `cu` and `p` are held. With
`WHERE o.status = "open"` instead, `cu` streams and the filtered orders are held.

### Written order is not join order

The join graph is a tree, so it can be rooted at whichever side streams. `openJoin`
orients the steps away from that root once, up front:

```go
// hop is one step of the pipeline, oriented away from the streamed side: a row
// of from, already in the combination, finds its matches in the table of into.
type hop struct {
    from, into       int // Plan.Leaves indexes
    fromKey, intoKey []string
}

func orientSteps(plan Plan, streamed int) []hop
```

`orientSteps` starts with the streamed side as the only one reached, and repeatedly
takes the first step in written order with exactly one end reached, until none is left.
The order is deterministic and N is small; the quadratic scan is the readable one. For
form 1 with `o` streaming the hops are `o→cu`, `o→p`; with `cu` streaming they are
`cu→o`, `o→p`.

Every held side is the `into` of exactly one hop, so there is **one hash table per held
side**, keyed on that hop's `intoKey`.

### Build, then pipeline

`joinCursor` loses its `build`/`probe` pair and its `[2]` arrays:

```go
type joinCursor struct {
    connection adapter.Connection
    sides      []joinSide // written order; index == Plan.Leaves index
    streamed   int
    hops       []hop
    tables     []map[string][]joinRow // parallel to hops
    held       int                    // rows across all tables, against maxRows
    unserved   pendingRows            // see "Page sizing"
    // selectAll, maxRows, columns, built, unplaced as today
}

type joinRow struct {
    cells []string
    page  *leafPage
    raw   json.RawMessage
    keys  []string // parallel to hops; set for the hops that touch this row's side
}
```

1. **Build**, on the first `NextPage`: for each hop in order, read its `into` side to
   the end into `tables[h]`. Sides are read **one after another, one cursor open at a
   time**, each opened when its turn comes and closed after its last page — the
   discipline `unionCursor` already has. `openJoin` therefore stops opening cursors;
   a `Connection.Query` failure surfaces from the first `NextPage`, which `runPlan`
   turns into the same `QueryFailedMsg`.
2. A held side that turns out **empty ends the run** at once: no later side is read,
   nothing streams, `HasMore` is false. This is iteration 10's "empty build side →
   empty result, no error", and with N sides it also saves the request units of
   everything not yet read.
3. **Pipeline.** A streamed row starts a combination of one. Each hop in order extends
   every combination by every match of `combination[hop.from].keys[h]` in `tables[h]`;
   a combination with no match at any hop is dropped (inner join). What survives the
   last hop is a merged row.

`readRows` computes `keys` from the whole item **before** `projectRaw` cuts it down,
for every hop touching that side, with `joinKey` unchanged: strict on type, `1`, `1.0`
and `1e0` equal, integers past 2^53 digit for digit. An item missing any of its side's
keys can match nothing and is dropped at read time, the N-sided form of today's rule.

### Columns and raw items

- With a `SELECT` list the header is the list, in written order, fixed before any page
  is read — as today.
- With `SELECT *` the header is every column of `Leaves[0]`, then `Leaves[1]`, and so
  on, each as `alias.column`, whichever side was read first. `placeLeftFirst` already
  sorts unplaced leaf pages by side index before placing them; it is renamed
  `placeInWrittenOrder` and otherwise stands. Columns first seen on a later page still
  join at the end, so nothing on screen moves.
- A raw item nests every side under its alias in written order:
  `{"o":{…},"cu":{…},"p":{…}}`. `pairRaw` becomes `combinedRaw` over the combination.
  With a `SELECT` list each half holds its projected fields only, as today.

### Page sizing

A merged page is what one streamed page produced, and with several held sides that can
be many times its size. Pages are **re-chunked**:

```go
const maxMergedPageRows = 1000
```

- `NextPage` serves at most `maxMergedPageRows` rows. What did not fit waits in
  `unserved`: the rest of the current combination set and the streamed rows of the
  current leaf page not yet expanded. Streamed rows are expanded **one at a time**, so
  memory beyond the tables is one merged page, one leaf page, and one row's matches.
- `NextPage` reads a new streamed page only when `unserved` is empty, and stops reading
  as soon as it has a row: it never reads ahead to fill a page. One `m` in the results
  pane costs at most the stretch of streamed pages it takes to find a match, as today.
- **Never an empty page while more remain**: the loop in `nextMatches` keeps reading
  streamed pages while the page is empty and the streamed cursor has more. Only the
  very last page can be empty.
- `HasMore` stays truthful by construction:
  `!j.built || !j.unserved.empty() || j.sides[j.streamed].cursor != nil`.
- A page served wholly from `unserved` read nothing, and says so: zero
  `RequestCharge`, empty `LeafCharges`. The model's running totals
  (`totalLeafCharges` in `app.go`) add pages up and need no change.
- The cap is ten default leaf pages. A lookup join yields at most one row per streamed
  row and never reaches it, so two-container page boundaries move only for a join that
  already fanned a 100-row page out past 1 000 rows.

### Memory

`max_join_rows` is reused and means **the rows held in memory across all held sides**.

- A per-side cap lets memory grow with every `JOIN` written; the user who set 5 000
  did not mean 5 000 × (N−1). The flag's own help text already reads "rows a
  cross-container join may hold in memory".
- A second key (`max_join_total_rows`) beside a per-side one is two knobs for one
  worry, and for two containers they would be the same number.
- For two containers there is one held side, so the meaning is unchanged.

The count is checked after every page of a held side, as today. The error names the
side that crossed the line and what was already held, so the fix is aimed:

```
query: join: sales.products takes the rows held in memory past 10000
(sales.customers 8200): narrow that side with a WHERE filter, or raise max_join_rows
```

With nothing held before it the parenthesis is omitted. The sentinel and its text
(`ErrJoinTooLarge`) are unchanged; callers match with `errors.Is`.

### Request units and the status bar

`meter.charge` already files each leaf page under `leafLabel(leaf)`, and nothing about
it is two-sided. A container joined to itself is two leaves under one label, so its
charges add up under that label, which is what the container cost.

The status bar's breakdown grows with every leaf —
`10.00 RU (sales.customers 7.50 + sales.orders 2.50 + sales.products 1.20)` — and
`StatusBar.View` truncates the whole left side when it overflows, which would cut the
elapsed time to keep a breakdown. Instead:

- `leafChargesLabel` is unchanged: every leaf, sorted by name, whenever the bar fits.
- When the fields overflow `width`, `View` renders them again with the breakdown
  folded to a count: `10.00 RU (3 containers)`. Only if that overflows too does
  `ansi.Truncate` apply, as today.
- The full breakdown is written to the log when a simulated run's page arrives, so a
  narrow terminal loses nothing for good.

No new key is bound; nothing in `KeyMap` changes.

### WHERE pushdown

`parseWhere` and `splitConjuncts` keep their rules; only the arity changes:

- A conjunct reading **one** alias is pushed down to that leaf and marks it `Filtered`.
- A conjunct reading **no** alias (`1 = 1`) goes to every leaf, as it goes to both
  today.
- A conjunct reading **two or more** aliases is refused. Evaluating it client-side
  takes an expression evaluator for Cosmos SQL — operators, functions, three-valued
  logic over `undefined` — and an evaluator that disagrees with the service in one
  corner is a silently wrong result, the thing this engine exists to never produce.
  N sides do not change that argument.
- A top-level `OR` or `BETWEEN` still keeps the clause whole, so it must read one side.

No condition is inferred: `WHERE cu.id = "c01"` is not turned into
`o.customerId = "c01"`, for the reason the next section gives.

### Semi-join key pushdown: out

After holding a small side, the next leaf could be filtered with
`WHERE o.customerId IN (…held keys…)`, and on a filtered lookup that would cut request
units by an order of magnitude. It is left out of this iteration:

- **It can lose rows.** Client-side equality is `joinKey`'s: integers past 2^53 compare
  digit for digit. A SQL number literal cannot say that, so a pushed-down `IN` would
  drop matches the hash table would have found. Object and array keys have no literal
  at all. It would need a rule for which key sets qualify, and a fallback.
- **The plan stops describing the run.** `Leaf.Query` is fixed by `BuildPlan`, which is
  what the planner tests, the log, and iteration 13's index rely on. Pushdown makes
  leaf text depend on data read mid-run.
- **It is not about N.** It applies to two containers just as well, and deserves its
  own iteration with its own limits (list length against the service's query size
  limit, string escaping, batching several `IN` queries).

Building in hop order, nearest the streamed side first, is what that iteration would
need; nothing here has to be undone for it.

### Cancellation and concurrency

- **Sequential.** Leaves are read one after another. Reading held sides concurrently
  would turn the build's wall clock from a sum into a maximum, and would pay for it
  with a shared row count, a shared `meter`, and N cursors to unwind on one failure.
  The leaves also draw on one account's provisioned throughput, where parallel
  cross-partition scans are how a 429 is earned. `CLEAN_CODE.md` asks for a real
  benefit first; there is no measurement showing one.
- At most one leaf cursor is open at any moment: the side being held, then the
  streamed side. `NextPage` still ends every failure with
  `errors.Join(err, j.Close())`, and `Close` closes whatever is open and is safe to
  call twice. A cancelled context fails the leaf's `NextPage`, which takes that path.

## Scope

- `internal/query/plan.go`
  - `Join`, `JoinStep` as above. `BuildPlan` still sends a query whose last container
    was introduced by `JOIN` to `planJoin`.
  - `planUnion` refuses a list with a joined container as "a container list mixed with
    a join"; `shapeMultiWay` goes.
- `internal/query`, the join planner (the file holding `joinPlanner`)
  - `joinPlanner.aliases` becomes `[]string`; `parseWhere` returns `[][]string`;
    `sidesRead` returns `[]bool`; `side(tok)` is unchanged in meaning.
  - `planJoin` requires `containers[0]` not joined and every later one joined, then
    walks the chain:

    ```go
    // parseStep reads the ON clause of the container at index joined and
    // returns the index just past it.
    func (j joinPlanner) parseStep(i, joined int) (JoinStep, int, error)
    ```

    built on `parseFieldRef`, which already returns the side a reference reads.
    `parseStep` accepts a pair only when exactly one reference reads `joined` and the
    other reads a side below it.
  - `checkSources` takes the whole chain: distinct aliases across all sides, `FROM`
    before the first, and only `JOIN`/`INNER` between the end of one `ON` clause and
    the next container. What follows the last `ON` is `WHERE` or the end; a `JOIN`
    there is a property join and gets the `JOIN ... IN` message rather than `shapeOn`.
  - `parseProjection` and `parseColumn` are untouched: `side` already resolves any
    alias in `aliases`.
- `internal/query`, the hash join (the file holding `joinCursor`) — as above:
  `orientSteps`, `streamedSide`, lazy sequential builds, the pipeline, `pendingRows`,
  `combinedRaw`, `placeInWrittenOrder`, the total-row cap and its message. It is 376
  lines already; the pipeline and `pendingRows` go to a new `pipeline.go` so neither
  file tells two stories.
- `internal/query/exec.go` — `Execute`'s `HashJoin` case uses `joinFits`; the comment
  on `ErrJoinTooLarge` and `Engine.MaxJoinRows` says "sides", plural.
- `internal/tui/panes/statusbar.go` — the folded breakdown in `View`.
- `internal/tui/app.go` — log the per-leaf breakdown of a simulated run's page.
  `commands.go` (`runPlan`, `fetchPage`) is untouched: the engine still returns an
  ordinary `adapter.Cursor`.
- `internal/config/config.go`, `cmd/profile.go` — the `MaxJoinRows` comment and the
  `--max-join-rows` help say "across all the sides held in memory". No new key.
- `test/seed/data.go` — `orders()` gains a top-level `"sku"`, the `sku` of the order's
  first line. Orders carry `lines[].sku` today, and a join key cannot be an array
  element (`JOIN l IN o.lines` beside a cross-container join stays refused), so form 1
  has nothing to join `products` on without it. `archivedOrders` inherits the field.
  The seeder drops and recreates its databases, so re-running `make emulator-seed` is
  the whole migration. Form 2 runs on the seed as it is.
- `README.md` — the cross-container section: the N-container example, the streamed-side
  rule, `max_join_rows` as a total, the still-refused list.
- `docs/plan/00-overview.md` — "multi-way joins" leaves the future-work sentence;
  "semi-join key pushdown" joins it.

## Out of scope

- `OUTER`/`LEFT` joins, non-equality and compound `ON`, `WHERE` conditions over several
  sides, `ORDER BY`/`GROUP BY`/`TOP`/`DISTINCT` over a join. Each needs client-side
  evaluation or sorting; none is unlocked by N sides.
- Semi-join key pushdown and inferred filters, for the reasons above.
- Joining on an array element (`lines[].sku`), which needs a property join inside the
  chain.
- Concurrent leaf reads, spill-to-disk, and streaming expansion of a single row's
  matches.
- Choosing the streamed side from container statistics. Iteration 12's metadata could
  supply item counts; the planner does not read the catalog and does not start here.
- Cross-account and cross-adapter joins.
- Mixing a union list with a join.

## Relationship to other iterations

- **10, cross-container.** Landed; this builds directly on it and changes none of its
  answers. `Join`'s key pair becomes `Steps`; the union path, `columnUnion`, `meter`,
  `LeafCharges`, both sentinels and the badge are reused as they are.
- **13, autocomplete.** Not landed. Its cross-container rules were written against the
  two-container planner and three of them move:
  - "no third `JOIN <db>.` source" is dropped: after a complete `ON a.x = b.y`, the
    clause keywords offered are `JOIN` and `WHERE`.
  - "after `ON`: the two side aliases" becomes: the alias `JOIN` just introduced and
    every alias declared **before** it, never one declared further right. After
    `ON <new>.<field> = ` only earlier aliases are offered; after
    `ON <earlier>.<field> = ` only the new one. That is `parseStep`'s rule, so
    completion cannot offer an `ON` the planner refuses.
  - Its index takes a join's raw item apart by alias through `Plan.Leaves`; an item
    with N halves needs nothing new. A container joined to itself files both halves
    under one container, which is correct.

  Still never offered once a cross-container `JOIN` is present: `LEFT`/`OUTER`/`CROSS`,
  `ORDER BY`/`GROUP BY`/`OFFSET`, and `IN` after `JOIN <alias>`. Sampling stays one
  query per container, so a three-container join samples at most three. Whichever of
  13 and 16 lands second makes these edits in the other's document.
- **14, multiple accounts.** Not landed. `startRun` hands `query.Engine` the active
  account's connection and `Account.MaxJoinRows`; every leaf of every step runs there.
  `query.SourcePaths` lists all N sources, so `errAccountInQuery` catches an account
  name on any of them. `LeafCharges` keys stay `db.container`, since a run never spans
  accounts. Nothing in either plan has to change for the other.
- **12, info view.** Unrelated today; the natural source of container sizes if the
  streamed-side rule ever wants more than `Filtered`.

## Steps

1. **Plan shape.** `JoinStep`, `Join.Steps`, `joinFits`; `planJoin` and `openJoin`
   produce and consume a one-step plan. Pure refactor: every iteration 10 test passes
   with only the `LeftKey`/`RightKey` assertions respelled. Ships: nothing visible.
2. **Planner.** The chain grammar, `parseStep`, N-sided `checkSources` and `parseWhere`,
   the reworded refusals; planner tables below. `Execute` still refuses more than one
   step with `errMalformed`, so nothing half-working is reachable: the TUI shows that
   error instead of `ErrUnsupported` for one commit.
3. **Engine.** `streamedSide`, `orientSteps`, sequential lazy builds, the pipeline,
   `combinedRaw`, written-order columns, the total cap. `Execute` accepts N steps.
   Ships: multi-way joins work end to end.
4. **Page sizing.** `maxMergedPageRows`, `pendingRows`, the `HasMore` rule.
5. **Status bar** fold and the log line.
6. Seed field, integration test, README, config and flag wording, plan statuses.

## Testing

**Unit — planner (`internal/query/test/plan_test.go`):**

| Input | Expect |
|---|---|
| form 1 | three leaves in written order; `Steps` `{Left: 0, customerId, id}`, `{Left: 0, sku, id}`; `cu` leaf `Filtered`, text `SELECT * FROM cu WHERE (cu.region = "west")` |
| form 2 | second step `{Left: 1, deviceId, id}`; filters on `a` and `e`, none on `d` |
| `ON` written new-side-first (`ON p.id = o.sku`) | the same step as earlier-side-first |
| four containers, mixed star and chain | `Left` of each step as written |
| a side with no alias in the middle of a chain | known by its container name |
| one container twice, distinct aliases | two leaves, one scope |
| `SELECT p.name, o.id, cu.name` | `Columns` with `Side` 2, 0, 1 in list order |
| a conjunct reading no alias | filters every leaf |
| two-container join | one step, `Left: 0`; leaf text identical to iteration 10's |

Refused, each asserting `ErrUnsupported` and the shape text: `ON` between two earlier
sides; `ON` reading a side joined further right; `ON` reading the new side twice;
compound `ON` in the second step; `LEFT JOIN` as the third source; `JOIN t IN o.lines`
before, between, and after container joins; a list before a join and after one; one
container twice with no aliases; a `WHERE` conjunct over two of three sides; `ORDER BY`
after a three-way join. The existing "three-way join" row of
`TestShapesThatCannotBeSimulatedAreRefused` moves to the accepted table. `FuzzBuildPlan`
keeps its guarantee, with the two forms above added to its corpus: no panic on any
input.

**Unit — engine (`internal/query/test`, beside the iteration 10 join tests, canned
pages):**

- A three-way star combines a row per order under `o.`, `cu.`, `p.` columns; an order
  whose customer or product is missing is dropped.
- A chain joins through the middle side; an item missing the key of *either* hop it
  takes part in matches nothing.
- The streamed side is the first unfiltered leaf: recorded query order and row order
  prove it for filter-on-first, filter-on-middle, filters-on-all, and no filter.
- Rooted at a middle side (`cu` streams, hops `cu→o`, `o→p`) the rows equal the ones
  rooted at `o`, order aside.
- `SELECT *` lists columns in written order even when the last side was read first; a
  raw item nests three aliases in written order; a `SELECT` list projects each half.
- Duplicate keys on two held sides yield the product of their matches.
- An empty held side ends the run: no row, no error, and the sides after it were never
  queried (`containers.queries`).
- The cap is a total: two held sides of 6 rows under `MaxJoinRows: 10` abort with
  `ErrJoinTooLarge` naming the second; each alone passes; exactly 10 in total passes.
- Charges: the sum of all leaves, `LeafCharges` with a key per container; a container
  joined to itself reports one key holding both leaves' charge.
- Page sizing: a streamed page fanning out past `maxMergedPageRows` is served as full
  pages then the remainder, `HasMore` true until the last, the continuation pages
  charging zero; draining yields every row exactly once. A stretch of unmatched
  streamed pages never surfaces as an empty page.
- At most one leaf cursor is open at a time (`opened - closed <= 1` after every page).
- Cancelling during the second build, and mid-stream, leaves `opened == closed`;
  closing twice closes each leaf once.
- A malformed plan (`Steps` short of `Leaves`, `Left` pointing forward, `Side` out of
  range) is `errMalformed`, not a panic.
- The iteration 10 join tests pass unedited: same rows, order, columns, raw items and
  charges for every two-container case.

**TUI, mock adapter (`internal/tui/test`, beside the iteration 10 cross-container
tests):**

- A three-container join renders, shows the badge, and totals three leaf charges.
- At a width that fits, the status bar lists all three containers; narrower, it reads
  `(3 containers)` and still shows rows and elapsed time; a two-container breakdown at
  today's test width is unchanged.
- A join over the total cap fails with the fix in the message and renders nothing.
- `m` on a fanned-out join appends the unserved rows without a new leaf request.
- A refused shape from the table above is shown and recorded in history.
- The drift-guard tests pass with no `KeyMap` change.

**Integration (emulator, beside `TestIntegrationCrossContainerJoin`):** `seedSales`
gains a `products` container and a `sku` on its orders; the three-way join matches a
fixture computed by hand as `wantJoined` is, including orders dropped for a missing
customer; the summed charge is positive and equals the three leaf charges within
rounding.

**Manual checklist:**
- [ ] `make emulator-seed`, then form 1: customer and product names on west-region
      orders; the badge shows; RU lists three containers.
- [ ] Form 2 on `telemetry` as seeded: alerts fan out over their device's events, and
      `m` never lands on an empty page.
- [ ] Form 2 without its `WHERE`, under `max_join_rows = 100`: `alerts` streams, and
      the run fails naming `telemetry.events`; the TUI stays alive.
- [ ] The iteration 10 two-container example returns what it returned before.
- [ ] `hr.employees e JOIN hr.departments d ON e.departmentNumber = d.number` still
      joins on a number.
- [ ] A `LEFT JOIN` as the third source is refused with its shape named.
- [ ] At 80 columns the status bar folds the breakdown and keeps the elapsed time.

## Acceptance criteria

- Any left-deep chain of inner equality joins over containers of the active account
  returns exactly the rows a relational inner join would, however many containers it
  names and whichever side streams.
- Every two-container join produces the same rows, row order, columns, raw items and
  charges as before this iteration; its page boundaries move only past
  `maxMergedPageRows`.
- Single-container and union queries are untouched.
- Rows held in memory never exceed `max_join_rows` in total; crossing it is an explicit
  error naming the side, never a truncated result.
- No merged page is empty while more remain, none exceeds `maxMergedPageRows`, and
  `HasMore` is never true with nothing left to serve beyond the one final page.
- Every leaf cursor opened is closed exactly once, on completion, error, cancel, and
  `Close`; at most one is open at a time.
- Every shape outside the grammar is refused with `query.ErrUnsupported` and its shape
  named; nothing is approximated.
- No goroutine, no new config key, no new key binding, and no cosmos import anywhere
  in `internal/query`.
