# Iteration 20 — CTEs and Join Types

## Goal

Finish the relational surface of the client-side engine. Iteration 10 joins two
containers, iteration 16 joins N, and both know one kind of join: inner, on one
equality. Everything else a SQL user reaches for next is refused today —
`checkMergeable` turns away every modifier but `INNER`, and a statement that opens with
`WITH` has no meaning to the service or to `query.BuildPlan`.

This iteration adds the two missing halves:

- **Join types.** `LEFT`, `RIGHT` and `FULL OUTER JOIN`, `CROSS JOIN`, and `CROSS`/
  `OUTER APPLY` over an array of the item. The seed data already asks for them: orders
  name customers that do not exist, most devices have no alert, and orders carry
  `lines[]` and `tags[]`.
- **CTEs.** `WITH name AS (…)` names a query and lets the main query read it as if it
  were a container. A CTE body over one container is sent to the service **whole**, so
  inside it the full Cosmos dialect works — functions, `TOP`, `ORDER BY`,
  `JOIN t IN`, aggregates — and the projection happens server-side. That is the answer
  to two standing refusals at once: iteration 10's leaves run `SELECT *` and pay for
  every field, and the outer `SELECT` list refuses expressions because the engine has
  no evaluator. With a CTE the user projects and computes where the service does it,
  and the engine joins what comes back.

The rule of iterations 10 and 16 stands: a shape the planner cannot **prove** it
simulates correctly is `query.ErrUnsupported`, never an approximation, and a query over
one container with none of the new syntax passes through untouched.

The scope is honestly two iterations of work. The design is one, because the plan
representation has to carry both; the Steps are split into **20a (join types)** and
**20b (CTEs)**, each shippable alone.

## Supported forms

### 1. LEFT, RIGHT and FULL OUTER JOIN

```sql
SELECT o.id, o.total, cu.name
FROM sales.orders o
LEFT JOIN sales.customers cu ON o.customerId = cu.id
WHERE o.status = "open"
```

Every open order, with its customer's name where the customer exists. `OUTER` is
optional noise, as in SQL: `LEFT OUTER JOIN` is `LEFT JOIN`.

```sql
SELECT a.message, d.site
FROM telemetry.alerts a
RIGHT JOIN telemetry.devices d ON a.deviceId = d.id
```

Every device, with its alerts where it has any.

```sql
SELECT o.id, cu.id AS customer
FROM sales.orders o
FULL JOIN sales.customers cu ON o.customerId = cu.id
```

Every order and every customer, paired where they match.

**Padding.** Cosmos distinguishes `undefined` from `null`, and the honest rendering of
"this side had no match" is `undefined`: the side is *absent*.

- In `Raw`, the missing side's key is omitted: `{"o":{…}}`, not `{"o":{…},"cu":null}`.
  A JSON export therefore says exactly what happened, and `null` stays free to mean a
  stored `null`.
- In `Rows`, the missing side's cells are empty strings. The adapters already render
  `null` as an empty cell (`renderValue`), so the table cannot tell the two apart and
  never could; `Raw` can.
- A preserved row whose join key is missing (`undefined` equals nothing, per
  `joinKey`) matches nothing and is padded. Iteration 16 drops such a row when it is
  read; that stays true only for a side no step preserves.

**Columns stay stable.** With a `SELECT` list the header is the list, fixed before any
page is read, so a side that never matches still has its columns. With `SELECT *` the
header is what the leaf pages showed, registered when a page is *read*, not when a row
matches (`placeInWrittenOrder` places every page in `unplaced`), so a held side's
columns are present even if nothing ever matches it. A side that returned **no items
at all** contributes no columns under `SELECT *`: nothing was observed, and the engine
does not invent a schema. A CTE with a written `SELECT` list is the exception — its
columns are known from the text (see form 4).

**ON may filter the joined container.** `ON` is still one equality between a field of
the joined source and a field of an earlier one (iteration 16's `parseStep`), now
optionally followed by `AND` conjuncts that each read **only the source this `JOIN`
introduces**:

```sql
LEFT JOIN sales.customers cu ON o.customerId = cu.id AND cu.vip = true
```

Such a conjunct is pushed down to that source's leaf, because filtering a source before
the join is exactly what the condition means — for `INNER` and `LEFT` steps. It is
refused on `RIGHT` and `FULL` steps, where the joined source is preserved and its
failing rows must still appear, padded; and an `ON` conjunct reading an *earlier*
source is refused on every outer step for the same reason. A top-level `OR` in `ON` is
refused; a `BETWEEN` must be parenthesized.

**WHERE pushdown.** A source is **optional** when some step can pad it: the joined
source of a `LEFT` step, every earlier source of a `RIGHT` step, both for `FULL`.
Every other source is **preserved**.

- A conjunct reading one preserved source is pushed down to its leaf, as today.
- A conjunct reading no alias goes to every leaf, as today.
- A conjunct reading an **optional** source is **refused**. Pushing it down filters
  that source *before* the join and keeps the padded rows; SQL applies it *after* and
  drops them, turning the join inner. Telling which conditions survive a padded row
  takes the expression evaluator this engine does not have. The message says what to
  write instead: `a WHERE condition on the optional side of LEFT JOIN: write it in ON
  to filter that side before the join, or use INNER JOIN`.
- One exact shape is accepted on an optional source, because it needs no evaluator:

  ```sql
  … LEFT JOIN sales.customers cu ON o.customerId = cu.id
  WHERE NOT IS_DEFINED(cu)
  ```

  keeps only the padded rows — the anti-join, "orders whose customer does not exist",
  which is what most people write an outer join to find. The test is "the side is
  absent from the combination". `IS_DEFINED(cu)` without `NOT` is refused with the hint
  `use INNER JOIN`.
- A conjunct reading two or more sources stays refused, as in 16.

**Which side streams.** For a statement with **one join step**, `streamedSide` keeps
its freedom for every kind: a preserved side may be held, because `FULL` needs
matched-flags and a final flush anyway, and `LEFT` with the left side held is that same
machinery. For a **chain with any outer step**, `Leaves[0]` streams and the hops run in
written order. Outer joins are not associative, and SQL defines a left-deep chain by
evaluating it in written order; iteration 16's re-rooting is proven for all-inner join
trees only. The cost is stated in the README: in an outer chain, write the large
container first.

**Cost.** RU is the sum of the leaf scans, exactly as for an inner join; padding and
flushing read nothing. Memory is 16's: every held side against `max_join_rows` in
total, plus one flag per held row of a preserved side.

### 2. CROSS JOIN

```sql
SELECT d.name AS department, p.name AS product
FROM hr.departments d
CROSS JOIN sales.products p
```

Every row of one side with every row of the other. No `ON` (one is refused), and `WHERE`
follows the pushdown rule: a conjunct reads one side and filters its leaf; a conjunct
over both sides is refused, so `CROSS JOIN … WHERE a.x = b.y` is not a back door to a
join — write `JOIN … ON`.

- **Two sources only.** A `CROSS JOIN` is the only step of its statement. Either source
  may be a CTE, and a CTE may be a join, so a product of a join is written by
  composition (form 6) rather than by a mixed chain whose output size the engine could
  not bound before running it.
- **Both sides are read whole before the first page**, both count against
  `max_join_rows`, and the **product** must not exceed `max_join_rows` either. It is
  checked before anything is served:

  ```
  query: join: cross join of telemetry.events (300 rows) and sales.orders (60 rows)
  is 18000 rows, past 10000: narrow that side with a WHERE filter, or raise
  max_join_rows
  ```

  `ErrJoinTooLarge`, nothing rendered, never a truncated product. One knob, because it
  is one worry: how many rows a simulated run may put in memory and on screen.
- Pages are served from memory in `maxMergedPageRows` chunks, left row major, so the
  order is deterministic.
- An empty side is an empty result, not an error.

**Cost.** Two scans, paid in full before the first page; every later page is free.

The comma form (`FROM a.b x, c.d y`) stays what iteration 10 made it — a union list,
which needs one shared alias — and two aliases there are refused with the hint
`write CROSS JOIN`.

### 3. CROSS APPLY and OUTER APPLY over an array

```sql
SELECT o.id, l.sku, l.quantity
FROM sales.orders o
OUTER APPLY l IN o.lines
```

T-SQL's `APPLY` evaluates its right side once per left row. Alchemist supports the one
case where that costs nothing: the right side is **an array of the left item**. The
syntax mirrors Cosmos's own `JOIN l IN o.lines`, so switching between the two is one
word.

- `CROSS APPLY l IN o.lines` is Cosmos's `JOIN l IN o.lines`: one row per element; an
  item whose path is missing, not an array, or empty yields nothing.
- `OUTER APPLY l IN o.lines` keeps that item as one row with `l` absent. **Cosmos SQL
  cannot express this**; it is evaluated client-side on `Raw`, with no extra query.
- An element that is an object contributes columns `l.sku`, `l.quantity`; a scalar
  element (`OUTER APPLY t IN o.tags`) contributes one column, `t`, and the `SELECT`
  list may name it bare: `SELECT o.id, t`. In `Raw` the element sits beside its item:
  `{"o":{…},"l":{…}}`; absent, its key is omitted, as for a padded join side.
- Cells for elements are rendered by the function the adapters use. `renderValue` moves
  from `internal/adapter/cosmos` to `adapter.RenderCell`, the same move iteration 13
  makes for `FlattenFields` and for the same reason: two renderers would disagree.
- `APPLY` **directly follows the source it reads**, and may chain
  (`APPLY l IN o.lines APPLY t IN l.taxes`). It expands that source's rows as they are
  read, before join keys are taken — so a key may read the element, which is the
  orders → lines → products join iteration 16 had to leave out:

  ```sql
  SELECT o.id, l.quantity, p.name
  FROM sales.orders o CROSS APPLY l IN o.lines
  JOIN sales.products p ON l.sku = p.id
  ```

- **A statement over one container whose only new syntax is `CROSS APPLY`** is
  rewritten to `JOIN … IN` and **passes through**: not simulated, no badge, the
  service's own semantics. `OUTER APPLY`, or any `APPLY` beside a cross-container join
  or a CTE source, is simulated: the leaf runs `SELECT * FROM o` plus its pushed-down
  `WHERE`, and the engine expands.
- A `WHERE` conjunct reading an `APPLY` alias is refused — it is a condition on a value
  the service never sees in a simulated run — except `NOT IS_DEFINED(l)` after an
  `OUTER APPLY` ("orders with no lines"), the same absent-side test as above. To filter
  elements, do it where the service can: a CTE with `JOIN l IN o.lines WHERE …`.
- `CROSS APPLY` directly after the joined source of a `LEFT` or `FULL` step is refused.
  SQL applies it after the join, where it drops the padded rows; expanding the source
  before the join keeps them. `OUTER APPLY` there is provably the same either way and
  is accepted.

**Cost.** Zero RU beyond the leaf scan. Held rows are counted **after** expansion.

**APPLY of a correlated query is refused in v1:**

```sql
… FROM sales.customers cu
CROSS APPLY (SELECT TOP 3 * FROM sales.orders o WHERE o.customerId = cu.id
             ORDER BY o.total DESC) recent
```

Run as written it is one query per left row — 10 000 customers, 10 000 queries, each a
cross-partition scan. Batching it (`WHERE o.customerId IN (…)` per streamed page, then
top-N per key client-side) needs literal rendering of keys, which iteration 16 already
showed loses rows past 2^53, plus a client-side `ORDER BY` that must agree with the
service's ordering of mixed types. Both belong to the semi-join iteration 16 names as
future work. Refused as `APPLY of a query over another container: it would run one
query per row`, with the hint to join a CTE. Without `TOP`/`ORDER BY` that hint is a
complete answer; per-key top-N has none in v1, and the README says so.

### 4. A CTE over one container: pushed down whole

```sql
WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu WHERE cu.region = "west"),
     big  AS (SELECT o.id, o.customerId, ROUND(o.total) AS total
              FROM sales.orders o WHERE o.total > 100)
SELECT big.id, big.total, west.name
FROM big JOIN west ON big.customerId = west.id
```

- A body that names at most one container, no CTE, and no `APPLY` is **opaque**: its
  text, with `db.container` rewritten to the alias exactly as `rewrittenLeaf` does it,
  *is* the leaf query. The planner does not parse it beyond finding its closing
  parenthesis and its `SELECT` list. Whatever the service accepts works: functions,
  `TOP`, `ORDER BY`, `GROUP BY`, aggregates, `JOIN t IN`, subqueries over the same
  container. A body with no `db.container` source (`FROM c`) takes the default scope
  through `Plan.WithDefaultScope`, like any leaf.
- **It projects server-side.** Iteration 10's leaves fetch whole items; `west` above
  fetches two fields of the west-region customers. On wide documents this is the
  largest RU and memory saving available to a simulated join, and the README leads the
  CTE section with it.
- **Its items are its columns.** The main query reads `west.name` as field `name` of
  the items the body returned — the service already applied `AS`, named a path by its
  last segment, and so on. The planner also *derives* the names where it can, into
  `Leaf.Fields`: an item ending in `AS name` or a bare trailing identifier gives
  `name`; an item that is only a path gives its last segment; `*`, `VALUE`, or any
  unnamed expression (the service would call it `$1`) makes the whole list unknown
  (`nil`). Known fields buy three things: the `SELECT *` header is fixed from the text
  even when the CTE returns nothing; `SELECT west.title` is refused as `west has no
  column title` instead of rendering an empty column; and autocomplete has the list.
- `SELECT VALUE` in a CTE the main query joins or projects is refused: its items need
  not be objects, and a scalar has no fields to join on. It is fine in the pass-through
  case below.
- `ORDER BY` inside a CTE orders what the service returns, which matters with `TOP`.
  The order of the *merged* result is not promised.
- A `WHERE` conjunct of the main query that reads a CTE source is **refused**:
  `a WHERE condition on the CTE west: filter inside the CTE`. Pushing it into the body
  would change the meaning of a body with `TOP` or `GROUP BY`; wrapping the body in a
  `FROM (…)` subquery might be provable and is recorded as future work behind an
  emulator learning test. `NOT IS_DEFINED(west)` on an optional CTE source is accepted
  like any absent-side test.

**The rename degrades to nothing.**

```sql
WITH recent AS (SELECT TOP 50 * FROM sales.orders o ORDER BY o._ts DESC)
SELECT * FROM recent
```

An opaque CTE read by `SELECT * FROM name` and nothing else plans as the body's leaf
under a root `Scan`: `Simulated()` is false, `Engine.Execute` returns the adapter's own
cursor, no badge, unprefixed columns, the adapter's paging. Zero overhead, and a
harmless way to keep a second query in the buffer.

**Cost.** One query per CTE *read*, zero for a CTE nothing reads (it is planned, so its
shape is checked, and never run).

### 5. A CTE read twice: materialized once

```sql
WITH staff AS (SELECT e.id, e.name, e.managerId FROM hr.employees e)
SELECT w.name, m.name AS manager
FROM staff w LEFT JOIN staff m ON w.managerId = m.id
```

- A CTE read **once** is inlined: its node sits where it is read, and if that is the
  streamed side it streams and holds nothing.
- A CTE read **more than once** becomes one `Materialize` node shared by reference. The
  first reader to reach it runs the body to the end into memory; every reader, the
  first included, is then served from memory. The RU is paid **once**, which is the
  point: two scans of `hr.employees` would cost twice.
- Materialized rows count against `max_join_rows` **once**, however many join tables
  index them; a table over a `Materialize` holds references, not copies. Past the cap
  the error names the CTE: `query: join: staff takes the rows held in memory past
  10000 …`, `ErrJoinTooLarge`.
- Reads stay sequential: one leaf cursor open at a time, 16's discipline.

### 6. Composition: a CTE over a simulated query

```sql
WITH everything AS (SELECT * FROM sales.orders, sales.archive AS c
                    WHERE c.status = "open"),
     named AS (SELECT o.id AS orderId, cu.name AS customer, o.sku
               FROM everything o JOIN sales.customers cu ON o.customerId = cu.id)
SELECT named.orderId, named.customer, p.name AS product
FROM named LEFT JOIN sales.products p ON named.sku = p.id
```

- A body that is itself a supported simulated shape — a union list, a join of any kind,
  an `APPLY` — is planned by the same function as the main query and read as a source.
- A CTE may read any CTE declared **before** it, in every source position the main
  query allows. A body does not see its own name or later names: there is no recursion,
  and `WITH RECURSIVE` is refused by name.
- A joined body must have a written `SELECT` list with **distinct output names** (`AS`,
  else the field name), because read as a source its rows are flat items:
  `{"orderId":…,"customer":…,"sku":…}` with columns `orderId`, `customer`, `sku`.
  `SELECT *` over a join is refused in a CTE body (`o.id` and `cu.id` would collide);
  over a union it is fine, and the items carry `_container`.
- A body that only renames or projects an earlier CTE (`SELECT b.id AS n FROM big b`)
  is a join with no steps; with a `WHERE` it is refused like any condition on a CTE.
- Composition is also how the limits above are lifted deliberately: a `CROSS JOIN` of a
  join, an outer chain with a chosen evaluation order, a union on one side of a join.

**Cost.** The sum of the leaves actually read. A composed CTE read once streams through
its parent; read twice it is materialized like any other.

### Name resolution

`containerSources` decides "container" by a two-part path not rooted at an alias. CTE
names join that rule without bending it:

- A **one-part** source path naming a visible CTE is that CTE. It shadows the
  "container in scope" reading of a bare name, so `WITH c AS (…) SELECT * FROM c` reads
  the CTE — while `FROM c` *inside* that body, where `c` is not yet visible, is still
  the scoped container.
- A CTE name that is also the **first part of any multi-part source path** in the
  statement is refused: `a CTE named sales beside the source sales.orders`. Cosmos
  allows `FROM c.lines` as a source, so `sales.orders` could be a property of the CTE
  or a container of the database, and the planner will not guess.
- CTE names are case-sensitive identifiers like aliases; a duplicate, a keyword, or a
  join modifier is refused. A CTE source takes an alias like any source and is known by
  its name without one; reading one CTE twice needs distinct aliases (16's rule).
- A union list of CTE names in the main query is refused: a list runs *one body* against
  each container, and a CTE is not a container.

## Semantics

Emission, for each kind, in iteration 16's pipeline of hops:

| Kind | Streams | Held | Unmatched rows | Emitted | Caps |
|---|---|---|---|---|---|
| `INNER` | `streamedSide` (16) | every other side | dropped | as the streamed row is expanded | held rows ≤ `max_join_rows` in total |
| `LEFT`, right side held | left | right | left row padded, right absent | at once, in stream order | same |
| `LEFT`, left side held (one step only) | right | left, with a matched flag per row | unmatched left rows padded | matches in stream order; padded rows in the **final flush**, in held order | same |
| `RIGHT` | mirror of `LEFT` | | | | same |
| `FULL` | either (one step) or `Leaves[0]` (chain) | the other, flagged | streamed: padded at once; held: final flush | both | same |
| `CROSS` | neither — both read whole | both | none | from memory, left row major | held rows **and** the product ≤ `max_join_rows` |
| `CROSS APPLY` | its source's stream | nothing of its own | item with no elements dropped | at read time, before keys | expanded rows count when held |
| `OUTER APPLY` | same | same | item kept, alias absent | same | same |

**The final flush** is what makes paging delicate. Today `joinCursor.HasMore` is
`!j.built || j.probe.cursor != nil`, and 16 adds `!j.unserved.empty()`. A preserved
held side adds rows that exist only *after* the streamed cursor closes:

- Each hop whose held side is preserved keeps its rows in arrival order beside the hash
  table, with a `matched` flag set by the pipeline.
- When the streamed side is exhausted, the flush walks those hops **in written order**.
  An unmatched held row enters the pipeline as a combination holding only its own side,
  at the hop *after* its own, so later steps treat it as SQL does: a later `INNER` step
  drops it (its key side is absent), a later `LEFT` pads it again. Written order is
  what makes a hop's unmatched set final when its turn comes, since flushed rows of
  earlier hops can still match it.
- Flushed combinations feed `unserved` like any others, so they are served in
  `maxMergedPageRows` chunks. A flush page read nothing and says so: zero
  `RequestCharge`, empty `LeafCharges`, 16's rule for a page served from `unserved`.
- `HasMore` becomes
  `!j.built || !j.unserved.empty() || j.streaming() || j.flush.pending()`, where
  `pending` is "some preserved hop still has an unmatched row not yet flushed". It is
  evaluated when the streamed cursor closes, so the page that ends the stream still
  reports `HasMore() == true` when a flush follows, and the results pane's `m` fetches
  it.
- "Never an empty page while more remain" holds: the loop in `nextMatches` runs while
  the page is empty and `HasMore()`, and moves from streaming to flushing inside one
  call. Only the very last page can be empty — the existing allowance — which happens
  when the last unmatched rows of a late hop were claimed by an earlier hop's flush.

**An empty held side** ends the run in 16. With outer steps that is true only when
nothing downstream can produce a row without it: the run ends early iff the empty
side's step is `INNER` and no later step is `RIGHT` or `FULL`.

## Hard limits

- **One budget.** `max_join_rows` caps, in total for one run: rows held by join tables,
  rows of materialized CTEs (once each), both sides of a `CROSS JOIN`, and the product
  of a `CROSS JOIN`. Crossing it is `ErrJoinTooLarge` naming the source that crossed it
  and what was already held (16's message). Nothing partial is rendered: every cap is
  checked during the build, before the first page.
- **RU is the sum of the leaves read**, per leaf in `adapter.Stats.LeafCharges`, under
  the `simulated (client-side)` badge. A CTE leaf is labeled with its name:
  `west (sales.customers) 3.10 + big (sales.orders) 6.20`.
- **An outer chain streams `Leaves[0]`**, whatever is filtered.
- **One `CROSS JOIN`, two sources.**
- **`APPLY` reads an array of its own source**, never another container.
- **No recursion.** A CTE sees earlier CTEs only.
- **One account.** Every leaf of every CTE runs on the active account's connection.
- 16's limit on the matches of one streamed row being expanded at once is unchanged.

## Plan representation

Iteration 16 keeps `Plan{Merge, Leaves, Join}` and widens `Join`. That shape is a join
of *leaves*; a CTE makes a join input something other than a leaf — another join, a
union, a shared materialization — and a flat struct cannot say so. The plan becomes a
small operator tree. `Leaves` stays a flat, written-order list that nodes index, in the
style `JoinColumn.Side` already has, so `Plan.Scope()`, `Plan.WithDefaultScope`, and
iteration 13's "the container `Plan.Leaves` gives for that alias" survive unchanged.

```go
type Plan struct {
    Leaves []Leaf // every single-container query, in the order the text names them
    Root   Node
}

// Node is one operator of a plan; its cursor serves adapter.Pages.
type Node interface{ isNode() }

type Scan struct{ Leaf int }     // served as the adapter returns it
type Union struct{ Leaves []int } // iteration 10's container list

// Join is iteration 16's join over Inputs instead of leaves.
type Join struct {
    Inputs  []Source
    Steps   []JoinStep   // Steps[i] attaches Inputs[i+1]; empty for one input
    Columns []JoinColumn // empty for SELECT *
    Absent  []int        // Inputs or applies that WHERE NOT IS_DEFINED requires absent
}

// Flatten reads a join as one relation of flat items: a joined CTE body.
type Flatten struct{ Input *Join }

// Materialize is a CTE read more than once; its readers share the pointer.
type Materialize struct {
    Name  string
    Input Node
}

type Source struct {
    Alias   string
    Rows    Node // *Scan, *Union, *Flatten or *Materialize
    Applies []Apply
    Fields  []string // columns known from the text; nil when they are not
}

type Apply struct {
    Alias string
    Array FieldRef
    Outer bool
}

type JoinKind int

const (
    InnerJoin JoinKind = iota
    LeftOuterJoin
    RightOuterJoin
    FullOuterJoin
    CrossJoin
)

type JoinStep struct {
    Kind     JoinKind
    Left     int      // index into Inputs of the earlier input
    LeftKey  FieldRef // zero for CrossJoin
    RightKey FieldRef
}

// FieldRef names a field through the alias that binds it: an input's own
// alias or one of its APPLY aliases.
type FieldRef struct {
    Alias string
    Path  []string
}

type JoinColumn struct {
    Side  int
    Alias string
    Field string // empty for a scalar APPLY alias named bare
    As    string
}
```

- `Merge` is deleted: the root's type says it. `Simulated()` is "the root is not a
  `Scan`". `Leaf` gains `Name` (the CTE it is the body of) and `Fields`.
- `JoinStep` is 16's plus `Kind`; its keys and `JoinColumn` gain an alias only because
  an input can now bind more than one. `Steps[i].Left`, written-order `Inputs`, and
  `joinFits` (now also accepting zero steps when the one input has an `Apply`, a
  `Flatten` parent, or projected `Columns`) are 16's.
- **`Join` stays n-ary.** A binary join node would be the textbook tree, and would
  forfeit 16's re-rooting of an all-inner join graph at the streamed side. The tree
  exists for what is genuinely nested — inputs — not to restate the chain.
- **Lowering.** `BuildPlan` parses the statement (below), then: a body with no new
  syntax and at most one container → `Scan`; a container list → `Union`; anything with
  a join, an `APPLY` or a CTE source → `Join`, wrapped in `Flatten` when it is a CTE
  body. CTE reads are counted first; a name read twice gets one `Materialize` that
  both `Source.Rows` point at, a name read once is inlined, a name never read leaves
  its leaves in `Plan.Leaves` and no node in the tree.
- **Execution.** `Engine.Execute` opens the root recursively; every node's cursor is an
  `adapter.Cursor`, so `runPlan`, `fetchPage`, the results pane, export and history
  need nothing. `joinCursor` reads each input through that interface instead of calling
  `Connection.Query` itself, lazily and one at a time as in 16. `meter` stops labeling
  by leaf and **adds its children's `LeafCharges` maps**, so a nested join's or a
  materialization's charges arrive under the right labels on the page that paid them.
  A row budget shared by pointer across one `Execute` replaces `joinCursor.held`.
- **Byte-for-byte.** A two-container inner join lowers to `Join{Inputs: 2 scans, Steps:
  1 InnerJoin}` and runs the same `joinCursor` code path as after 16; a container list
  lowers to `Union` over the same `unionCursor`; one container lowers to `Scan` and
  gets the adapter's cursor. The executor tests of iterations 10 and 16 pass
  **unedited**; only planner assertions that spell `Merge`/`Leaves`/`Join` move.

### The parser

`scope.go`'s `parser` is a scanner: it walks tokens for `FROM` clauses and records
sources with their clause number and parenthesis depth. `joinPlanner` then parses a
join by token index from those sources, assuming one `FROM` at depth 0 —
`checkMergeable` refuses `clause != 1` as "a subquery over another container". A CTE
body is exactly that: a second statement at depth 1 with its own `FROM`, its own
aliases, its own `WHERE` to split. Teaching the index walker about nested statements
means exceptions to every one of its positional assumptions.

Decision: **the scanner stays, as the detector; the simulated subset gets a small
recursive-descent parser** over the same `[]token`, no new dependency.

- The scanner still answers the first question — does this text use anything the
  service cannot run? — and the pass-through dialect is still **never parsed**. A
  statement that opens with `WITH`, or has an `APPLY`, or names two containers, goes to
  the descent parser; everything else passes through as today.
- The descent parser produces a private AST (`statement{with, body}`,
  `body{projection, sources, steps, conjuncts}`) and is written from the grammar below,
  one function per rule, the same function for the main query and a simulated CTE
  body. An opaque body is found by matching its parenthesis and classified by running
  the scanner over its tokens.
- `joinPlanner`'s logic moves rather than dies: `parseStep`, `parseFieldRef`,
  `parseColumn`, `splitConjuncts`, `topLevelBreakers`, `sidesRead` become methods of
  the descent parser with their rules intact. Step 1 is that move, with no behavior
  change, proven by the planner tables and `FuzzBuildPlan` passing unedited.

```
statement := [ WITH cte ("," cte)* ] body
cte       := name AS "(" ( opaque | body ) ")"
body      := SELECT list FROM source ( join )* [ WHERE conjunct ( AND conjunct )* ]
           | SELECT … FROM container ( "," container )* [ [AS] alias ] …   -- union
source    := ( db "." container | cte-name ) [ [AS] alias ] ( apply )*
apply     := ( CROSS | OUTER ) APPLY alias IN ref
join      := [ INNER | (LEFT | RIGHT | FULL) [OUTER] ] JOIN source ON ref "=" ref
             ( AND conjunct )*
           | CROSS JOIN source
list      := "*" | item ( "," item )*
item      := alias "." field [ [AS] name ] | apply-alias [ [AS] name ]
conjunct  := NOT IS_DEFINED "(" alias ")" | tokens reading at most one alias
```

`WITH` and `APPLY` join `keywords` (neither can be a bare alias; both highlight);
`RECURSIVE` is recognized only to be refused by name.

### Still refused

Each `query.ErrUnsupported`, wrapped with the shape:

| Shape | Refused as |
|---|---|
| `WHERE` conjunct on an optional source | `a WHERE condition on the optional side of <kind> JOIN: write it in ON …` |
| `IS_DEFINED(alias)` on an optional source | `… use INNER JOIN` |
| `ON` extra reading an earlier source of an outer step, or any extra on `RIGHT`/`FULL` | `an ON condition on the preserved side of <kind> JOIN` |
| top-level `OR` in `ON`, non-equality or missing `ON`, `ON` on `CROSS JOIN` | `shapeOn` (16's wording, extended) |
| `CROSS JOIN` beside another step | `CROSS JOIN in a chain: put one side in a CTE` |
| `FROM a.b x, c.d y` | `a container list with more than one alias: write CROSS JOIN` |
| `NATURAL JOIN`, `USING` | `<keyword> JOIN` |
| `APPLY (SELECT …)`, `APPLY` of a function | `APPLY of a query over another container: it would run one query per row` |
| `APPLY` reading a source other than the one it follows | `APPLY must follow the source it reads` |
| `CROSS APPLY` on the joined source of `LEFT`/`FULL` | `CROSS APPLY on the optional side of <kind> JOIN: use OUTER APPLY` |
| `WHERE` conjunct on an `APPLY` alias | `a WHERE condition on an APPLY alias: filter elements in a CTE with JOIN … IN` |
| `JOIN t IN` beside a simulated join, outside a CTE | unchanged from 16, plus the hint `or use CROSS APPLY` |
| `WITH RECURSIVE`, a body reading its own or a later name | `a recursive CTE` |
| duplicate or keyword CTE name; CTE named like a database it reads beside | named in the message |
| `WHERE` conjunct on a CTE source | `a WHERE condition on the CTE <name>: filter inside the CTE` |
| `SELECT VALUE` in a CTE that is joined or projected | `a CTE the main query joins must return objects` |
| `SELECT *` or colliding names in a joined CTE body | `a joined CTE needs a SELECT list with distinct names` |
| `SELECT x.nope` where the CTE's fields are known | `<cte> has no column <name>` |
| a union list of CTE names | `a container list of CTEs` |
| expressions, functions, nested paths in a simulated `SELECT` list | `shapeProjection`, plus `put it in a CTE, where the service evaluates it` |
| `ORDER BY`, `GROUP BY`, `OFFSET`, `LIMIT`, `TOP`, `DISTINCT`, aggregates on a simulated body | unchanged, plus the same CTE hint where it applies (`TOP`, `ORDER BY` per source) |
| a conjunct over two sources, a subquery over another container, a second statement | unchanged |

Projection stays `alias.field [AS name]`. Expressions in the outer `SELECT` list stay
refused, and the refusal now points somewhere: **put it in a CTE, where the service
evaluates it.** `ROUND(o.total) AS total` in form 4 is that hint followed.

## Scope

- `internal/query/parse.go` (new) — the AST and the descent parser; absorbs
  the join planner's file (the one holding `joinPlanner`), which is deleted.
- `internal/query/plan.go` — `Plan`, `Node` and its five implementations, `Source`,
  `Apply`, `JoinKind`, `FieldRef`; `BuildPlan` as detector → parse → lower; `Leaf.Name`
  and `Leaf.Fields`; `Simulated`, `Scope`, `WithDefaultScope` on the new shape.
  `checkMergeable` keeps its statement and subquery checks and loses the modifier one.
- `internal/query/lower.go` (new) — AST → tree: optional/preserved classification, the
  pushdown rule, `ON` extras, CTE read counting and `Materialize` sharing, field
  derivation, the pass-through degradations (`CROSS APPLY` rewrite, CTE rename).
- `internal/query/scope.go` — `WITH`, `APPLY` in `keywords`; `parseJoins` recognizes
  `APPLY` after a modifier so the scanner does not take `APPLY` for an alias.
- `internal/query/exec.go` — `Execute` opens nodes recursively; `meter` adds child
  `LeafCharges`; the shared row budget; `leafLabel` honors `Leaf.Name`.
- `internal/query`, the hash join and `pipeline.go` (16) — inputs as cursors; per-hop
  `keepStreamed`/`keepHeld` from the step kind and the hop's direction; matched flags;
  the flush; the amended early end; the absent-side filter; keeping preserved rows that
  lack a key.
- `internal/query/cross.go`, `apply.go`, `materialize.go`, `flatten.go` (new) — one
  cursor each, each small enough to read in one sitting.
- `internal/adapter` — `RenderCell`, moved from `cosmos.renderValue`; the cosmos
  `PageBuilder` calls it. No interface changes.
- `internal/tui` — nothing in `commands.go`. `panes/statusbar.go` needs no change: CTE
  labels are just longer keys, and 16's fold already handles overflow.
- `test/seed/data.go` — a customer no order names, so `FULL JOIN` on `sales` has
  unmatched rows on both sides. (As seeded, every alert names a real device —
  `(i*3)%(deviceCount+2)` only reaches `d00`, `d03`, `d06`, `d09` — while six devices
  have no alert, which is what `RIGHT JOIN` on `telemetry` needs.) `hr.employees`
  gains `managerId` for form 5.
- `README.md` — "Querying across containers": join types with the padding and pushdown
  rules, `CROSS JOIN`'s cap, `APPLY`, CTEs led by server-side projection, the refused
  list.

No new key binding, no new config key, no goroutine.

## Out of scope

- `APPLY` of a correlated query, and per-key top-N in general.
- `WITH RECURSIVE`.
- Client-side `ORDER BY`, `TOP`, `DISTINCT`, `GROUP BY` or aggregates over a simulated
  result. A client-side sort must reproduce the service's ordering across types and
  hold the whole result, and `TOP` without it is arbitrary. `TOP` and `ORDER BY` *per
  source* are available today, inside a CTE.
- `WHERE` conditions on an optional source, a CTE, or an `APPLY` alias beyond the
  absent-side test; conditions over several sources; non-equality `ON`.
- Pushing a main-query filter into a CTE body by wrapping it in a `FROM (…)` subquery.
- Re-rooting a chain that contains an outer step; choosing the streamed side from
  statistics.
- `CROSS JOIN` inside a chain; `NATURAL`/`USING`; set operators (`UNION`, `INTERSECT`,
  `EXCEPT`) between bodies.
- Spill-to-disk, concurrent leaf reads, cross-account or cross-adapter sources.

## Relationship to other iterations

- **10, cross-container.** Landed. Its union and two-container join become `Union` and
  a one-step `Join`; results, row order, columns, raw items and charges do not move.
  `Merge` and the flat `Plan.Join` go; the sentinels, `columnUnion`, `unionCursor` and
  the badge stay.
- **16, multi-way joins.** Found and read in full; this builds on it and **must land
  after it**. Kept as they are: `JoinStep`/`Steps[i].Left`, written-order inputs,
  `streamedSide` and `orientSteps` for all-inner graphs, one table per held side, lazy
  sequential reads with one cursor open, `pendingRows` and `maxMergedPageRows`,
  `max_join_rows` as a total and its message, zero-charge continuation pages, the
  status bar fold. Changed: `Join` reads `Source`s instead of leaves; keys and columns
  carry an alias; read-time dropping of key-less rows and the empty-held-side early end
  gain the conditions stated above; 16's out-of-scope "joining on an array element" is
  delivered by `APPLY`. If 16 slips, step 2 here adopts its `Steps` shape for two
  inputs and the chain rules wait for it.
- **13, autocomplete.** Not landed. It must never offer what the planner refuses, so
  these move with this iteration, edited in its document by whichever lands second:
  `LEFT`/`RIGHT`/`FULL`/`OUTER`/`CROSS` before `JOIN`, and `APPLY` after `CROSS`/
  `OUTER`, are offered; `RECURSIVE` never. `WITH` is offered at statement start, and a
  statement start is recognized after `AS (`. In source positions visible CTE names
  come first, then databases. `<cte>.` offers `Leaf.Fields`/`Source.Fields` from the
  buffer's own parse and nothing sampled; an `APPLY` alias offers the element fields
  observed under `<path>[]`. After `CROSS JOIN <source>` no `ON`. In `WHERE` of an
  outer join only preserved aliases are offered, plus `NOT IS_DEFINED(`. Its index
  files a join half under the container of its alias only when that input is a bare
  container `Scan`: a CTE's items are a projection with renames and computed fields,
  true of the CTE and not of the container.
- **14, multiple accounts.** Not landed. Nothing changes: one `Engine`, one connection,
  the active account's `MaxJoinRows` as the run's budget. `query.SourcePaths` must
  descend into CTE bodies so `errAccountInQuery` catches `prod.sales.orders` inside
  one, and must not report a one-part CTE name as a source path.
- **7, history.** Text is recorded as written, `WITH` and all; refused shapes are
  recorded as failures, as since 10. The recorded scope is `Plan.Scope()`, the first
  container the text names.

## Steps

### Phase 20a — join types

1. **Parser.** `parse.go` with the AST and the grammar's join subset as 16 leaves it;
   the join planner's file deleted; lowering still emits 16's `Plan`. No behavior
   change: planner tables and fuzz corpus pass unedited.
2. **Plan tree.** `Node`, `Scan`, `Union`, `Join{Inputs}`, recursive `Execute`, child
   `LeafCharges` in `meter`, the shared budget. No behavior change: executor, TUI and
   integration tests pass unedited; planner assertions respelled.
3. **LEFT and RIGHT.** Kinds in the parser, optional/preserved classification, the
   pushdown and `ON`-extra rules, padding, key-less preserved rows, written-order
   streaming for outer chains, matched flags and the flush with truthful `HasMore`,
   the amended early end. Ships: both joins, one step or in chains.
4. **FULL.** Both keeps on one hop; flush in written order through later hops.
5. **Absent-side filter.** `WHERE NOT IS_DEFINED(alias)`.
6. **CROSS JOIN.**
7. **Array APPLY.** `adapter.RenderCell`; the `CROSS APPLY` pass-through rewrite;
   client-side `CROSS` and `OUTER`; keys through an `APPLY` alias.
8. README, seed rows, the edits to plans 13 and 16, plan statuses.

### Phase 20b — CTEs

9. **Opaque CTEs.** `WITH` in the parser, name resolution and its refusals, opaque
   bodies as leaves with `Name` and derived `Fields`, CTE sources in every join kind,
   labeled charges, the rename pass-through. Each CTE may be read once; a second read
   is refused for one commit.
10. **Materialize.** Read counting, the shared node, the budget counted once.
11. **Composition.** Simulated bodies through `Flatten`, union bodies, CTEs reading
    earlier CTEs, zero-step projections.
12. README CTE section, integration fixtures, the edits to plans 13 and 14.

Every step leaves `make all` green and nothing half-working reachable: a shape the
executor cannot run yet is still refused by the planner.

## Testing

**Unit — planner (`internal/query/test/plan_test.go`), table-driven:**

- Every form above lowers to the expected tree: kinds per step, `Left` indexes, keys
  with aliases, `Applies`, `Absent`, `Fields`, leaf texts and `Filtered`.
- Pushdown: a preserved-side conjunct reaches its leaf for `LEFT`, `RIGHT`, and the
  preserved sides of a mixed chain; an `ON` extra reaches the joined leaf for `INNER`
  and `LEFT` and marks it `Filtered`; a constant conjunct reaches every leaf.
- An opaque CTE body is the leaf text verbatim but for the container rewrite, including
  `TOP`, `ORDER BY`, `GROUP BY`, a function, `JOIN t IN`, a string holding `)`.
- Field derivation: `AS`, bare trailing name, path → last segment; `*`, `VALUE`, an
  unnamed expression → `nil`.
- Read counting: once → inlined; twice → one `*Materialize`, pointer-equal in both
  sources; never → leaves present, no node.
- Degradations: `WITH x AS (…) SELECT * FROM x` and a lone `CROSS APPLY` are `Scan`
  roots, `Simulated()` false, with the expected single leaf text.
- A CTE body with `FROM c` takes `WithDefaultScope`; `WITH c AS (… FROM c …) SELECT *
  FROM c` reads the CTE outside and the container inside.
- **Every row of "Still refused"**, asserting `ErrUnsupported` and the shape text. The
  rows of `TestShapesThatCannotBeSimulatedAreRefused` for `left join`, `left outer join
  with no alias` and `cross join` move to accepted tables.
- `FuzzBuildPlan` keeps "never panics" and gains a corpus entry per form. A second
  target, `FuzzLowering`: for any text that plans, every `Scan`/`Union` index is within
  `Leaves`, every `Step.Left <= i`, every `JoinColumn.Side` and `Absent` entry is in
  range, and every `Materialize` is reachable from at least two sources.

**Unit — executor (`internal/query/test/`, canned pages), a table per kind.** Each
covers: both sides populated; **empty left, empty right, both empty**; duplicate keys on
either side; rows **missing the key** on either side; a `null` key; `SELECT *` and a
`SELECT` list; raw items; charges.

- `LEFT`/`RIGHT`: padded rows have empty cells and **no key for the absent side in
  `Raw`**; `SELECT *` keeps a held side's columns when nothing matches; identical row
  *sets* whichever side is held, with the held-preserved variant delivering padded rows
  last.
- `FULL`: unmatched rows of both sides exactly once; the **flush pages**: a stream
  ending on a page with matches reports `HasMore` true and the next page is the flush;
  a flush larger than `maxMergedPageRows` spans pages; flush pages charge zero; a run
  with nothing to flush ends without an extra page; no empty page before the last;
  draining yields every row exactly once.
- Chains: `A LEFT B INNER C` drops what SQL drops; `A INNER B RIGHT C` with an empty
  `B` still returns every `C` row (no early end), while `A INNER B(empty) LEFT C` ends
  early and never queries `C`; flushed rows of hop 1 match and flag rows of hop 2; an
  outer chain streams `Leaves[0]` even when it is the only filtered leaf.
- Absent-side filter: only padded rows; combined with a preserved-side pushdown.
- `CROSS`: product, left row major; pages of `maxMergedPageRows`; either side empty;
  a side past the cap, and a product past the cap with both sides under it, each
  `ErrJoinTooLarge` before any page; exactly at the cap passes.
- `APPLY`: object and scalar elements; nested path; chained applies; missing, `null`,
  non-array and empty arrays under `CROSS` (dropped) and `OUTER` (kept, alias absent);
  a join key through the alias; expansion counted against the cap when held.
- CTEs: an opaque CTE as streamed and as held input of each kind; known `Fields` fix
  the header of an empty CTE; a CTE read twice issues **one** query
  (`containers.queries`), charges once, counts once against the cap, and both readers
  see every row; a composed CTE flattens to the names of its list; a union body
  carries `_container`; nested charges arrive under CTE labels.
- Cancellation at every phase — during a held build, mid-stream, mid-materialize,
  during a nested CTE's build, during the flush — leaves `opened == closed`; `Close`
  twice closes each leaf once; at most one leaf cursor open after every page.
- A malformed tree (index out of range, `Flatten` over `SELECT *`, `CrossJoin` with
  three inputs) is `errMalformed`, never a panic.
- The executor tests of iterations 10 and 16 pass unedited.

**TUI, mock adapter (`internal/tui/test`, beside the iteration 10 and 16 cases):**

- A `LEFT JOIN` renders padded rows, shows the badge, totals both leaves; `m` after the
  last streamed page of a `FULL JOIN` appends the flush with no new leaf request.
- A refused shape shows its hint (the `ON` hint, the CTE hint) and is recorded in
  history; a product past the cap renders nothing and the TUI stays alive.
- A CTE join shows `west (sales.customers)` in the breakdown; the rename form shows no
  badge and unprefixed columns.
- JSON export of a padded row omits the absent alias; CSV leaves its cells empty.
- Drift-guard tests pass with no `KeyMap` change.

**Integration (emulator, `//go:build integration`, `make emulator-seed` data):**
fixtures computed by hand in the test, as `wantJoined` is.

- `sales.orders LEFT JOIN sales.customers`: every order; those naming `c12`–`c14`
  padded. With `WHERE NOT IS_DEFINED(cu)`: exactly those.
- `telemetry.alerts RIGHT JOIN telemetry.devices`: the six alert-less devices padded.
- `FULL JOIN` on `sales`: unmatched orders and the order-less customer, once each.
- `OUTER APPLY l IN o.lines JOIN sales.products p ON l.sku = p.id`.
- `CROSS JOIN` of `hr.departments` and a filtered `hr.employees`: row count is the
  product.
- Form 4 against the same join written without CTEs: **equal rows, strictly lower
  charge on the projected leaves** — the selling point, asserted.
- Form 5: the charge for `hr.employees` equals one scan, within rounding.
- A lone `CROSS APPLY` returns what the hand-written `JOIN … IN` returns.
- Summed charge equals the sum of `LeafCharges` within rounding, every case.

**Manual checklist:**

- [ ] `make emulator-seed`; the `LEFT JOIN` example: orders for `c12`–`c14` show empty
      customer cells; JSON export of one has no `cu` key.
- [ ] Add `WHERE cu.region = "west"`: refused, and the message says to write it in
      `ON`; do so, and the padded rows are still there.
- [ ] `FULL JOIN` on `sales`: page with `m` to the end; the unmatched customer arrives
      on the last page and `m` then does nothing.
- [ ] `CROSS JOIN` of `telemetry.events` and `telemetry.devices` fails naming the
      product; with `max_join_rows` raised it runs, and only the first page costs RU.
- [ ] `OUTER APPLY t IN o.tags` lists every order; `CROSS APPLY` alone shows no badge.
- [ ] Form 4: RU lists `west (sales.customers)` and `big (sales.orders)`, and is lower
      than the CTE-less join.
- [ ] Form 5 charges `hr.employees` once.
- [ ] `WITH recent AS (…) SELECT * FROM recent`: no badge, same RU as the body alone.
- [ ] `SELECT ROUND(o.total) FROM … JOIN …` is refused with the CTE hint.
- [ ] Every iteration 10 and 16 README example returns what it returned before.
- [ ] At 80 columns the status bar folds a CTE breakdown and keeps the elapsed time.

## Acceptance criteria

- `LEFT`, `RIGHT` and `FULL OUTER JOIN`, alone and in left-deep chains with `INNER`,
  return exactly the rows SQL's written-order evaluation defines, with the absent side
  omitted from `Raw` and empty in `Rows`.
- `CROSS JOIN` returns the full product or an `ErrJoinTooLarge` raised before the first
  page; never part of one.
- `OUTER APPLY` returns every source item at least once; a statement whose only new
  syntax is `CROSS APPLY` is not simulated.
- An opaque CTE body reaches the service verbatim but for the container rewrite; a CTE
  is queried once however often it is read, and not at all when nothing reads it;
  `WITH x AS (…) SELECT * FROM x` is a pass-through plan.
- No `WHERE` or `ON` condition is ever pushed to a leaf where doing so could change the
  result; every such condition is refused with what to write instead.
- Rows in memory — join tables, materialized CTEs, cross join sides and product — never
  exceed `max_join_rows` in total; crossing it names the source and renders nothing.
- `HasMore` is true whenever a flush page remains and false when nothing does; no page
  but the last is empty; none exceeds `maxMergedPageRows`.
- Every leaf cursor opened is closed exactly once on completion, error, cancel and
  `Close`, at most one open at a time, including inside nested and materialized CTEs.
- Single-container queries, unions, and the inner joins of iterations 10 and 16 produce
  the same rows, order, columns, raw items and charges as before.
- Everything outside the grammar is `query.ErrUnsupported` with its shape named.
- `internal/query` imports no concrete adapter; `internal/tui` changes no command and
  binds no key; no new dependency, config key or goroutine.

## Implementation notes

Landed as one change on top of 16, 18 and 19. Where the code settled differently from
the text above:

- **Files.** `parse.go` holds the statement and body parsers and the conjunct helpers;
  `lower.go` lowers a statement, its CTEs, opaque bodies and unions; `simulate.go`
  lowers a simulated body; `malformed.go` refuses a malformed tree. The executor is
  `exec.go` (engine, budget, meter), `union.go`, `relation.go` (reading an input into
  rows and rendering combinations, shared by the hash and the cross join),
  `apply.go`, the hash join and its pipeline, `cross.go`, `materialize.go` and
  `flatten.go`. The join planner's file is gone, as planned.
- **One change, not twelve commits.** The steps were built in order but land together,
  so "step 1 changes nothing" is shown by the iteration 10 and 16 executor tests
  passing with only their malformed-plan fixtures respelled, and the planner tables
  passing with `Merge`/`Join` respelled and the `LEFT JOIN`/`CROSS JOIN` rows moved to
  accepted tables. Three TUI tests used `LEFT JOIN` as their refused shape; they now
  use `NATURAL JOIN` and a `CROSS JOIN` in a chain.
- **`Join.Absent` holds aliases**, not indexes: `NOT IS_DEFINED(l)` after an
  `OUTER APPLY` names an alias that is no input. The malformed-tree check requires every one bound.
- **`Flatten` has a `Name`**, the CTE's, which labels its rows in the budget.
  `JoinKind` has a `String` method for the refusals.
- **Every leaf planned for a CTE carries its name**, a simulated body's leaves too, so
  `named (sales.customers)` files the charge of a composed CTE. `Leaf.Fields` is set
  for a CTE's opaque leaf only; the main query's leaves keep it nil.
- **A CTE body whose only new syntax is `CROSS APPLY` is opaque too**, rewritten to
  `JOIN … IN` exactly as the main query is, rather than simulated: same answer, run on
  the service.
- **A body reading its own name or a later one reads the container in scope.** The
  name-resolution rules and the test list say `WITH c AS (… FROM c …)` reads the
  container inside, which a "recursive CTE" refusal of the same shape would contradict;
  the rules win, and `WITH RECURSIVE` is the one recursion refused by name.
- **The cross join's product is checked on its own** against `max_join_rows`, before
  the first page, while its two sides draw on the shared budget like any held rows.
  The product is served from the two sides and never held, so adding it to the total
  would refuse a 100 × 100 product under the default cap for rows that are not in
  memory. A cross join whose first side is empty does not read the second.
- **A run with a dead stream skips it.** When a hop's table is empty and the hop keeps
  nothing unmatched from the streamed side, every streamed row would be dropped there:
  the run ends at once when no hop from there on flushes, as planned, and otherwise
  holds the rest and goes straight to the flush without querying the streamed input.
  `A JOIN B(empty) RIGHT JOIN C` returns every row of `C` and never reads `A`.
- **Key-less rows, in general.** A row is dropped as it is read when it lacks the key
  of a hop that does not keep its side unmatched: a held row of a hop that does not
  flush, or a from-row of a hop that does not pad. That is 16's rule where nothing is
  preserved, and keeps every row an outer step can still emit.
- **Refusals the text left open.** A `WHERE` condition reading no alias is refused
  beside a CTE input, which has no leaf of this body to take it. An extra `ON`
  condition of an inner join that reads an earlier source is refused with 16's `ON`
  wording. A
  `USING` is refused as `JOIN ... USING`. On a `RIGHT` or `FULL` join the hint for a
  condition on a padded side is to filter it in a CTE, since `ON` cannot take it.
- **`Plan.ObservedFields`** replaces the TUI's reading of `Merge` and `Join.Columns`:
  it files a join half only for an input that is a container of the query, never for
  a CTE, and leaves out renamed columns, as 13's "as built" rule does.
- **Autocomplete (13 landed first).** The moves listed under "Relationship to other
  iterations" are made in `query.Context` and `internal/complete`: the join words and
  `OUTER`/`APPLY` after a source and after each modifier, `IN` after an `APPLY` alias,
  no `ON` after a `CROSS JOIN` source, `WITH` at a statement's start and `AS`, then
  `SELECT` in its header, the visible CTEs first in every source position
  (`Completion.CTEs`), `cte.` completing the columns its select list names
  (`Alias.CTE`, `Alias.Fields`, never sampled nor bound to the scope), and a `WHERE`
  of an outer join offering only the aliases no step pads. `NOT IS_DEFINED(` is not
  offered as a unit; `IS_DEFINED` is already a function suggestion.
- **Seed.** Customer `c15` is named by no order; employees report in teams of six to
  the first of each team, who has no `managerId`.
- **Integration fixtures** live in their own `e20_joins` database, computed by hand, so
  the emulator's seeded databases are only read. The `vnext-preview` emulator charges
  a page the same whatever it holds, so "strictly lower charge on the projected
  leaves" is asserted as "not higher"; the materialized CTE's charge equals one scan.
- **`FuzzBuildPlan`'s pass-through invariant** (text untouched when no container is
  named) now excludes statements with a `WITH` or an `APPLY`, which the planner
  rewrites by design; `FuzzLowering` checks the tree.
