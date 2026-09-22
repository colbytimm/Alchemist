# Iteration 10 — Cross-Container Queries (Client-Side)

## Goal

Query across containers even though the service can't: Cosmos DB SQL operates on
exactly one container — there is no cross-container `JOIN` or `UNION` in the language,
and the SDK (any version) cannot express one. The old `main`-branch CLI had
"simulate joins across containers" on its roadmap for exactly this reason but never
built it. Alchemist implements it as a **client-side query engine** layered above the
adapter interfaces: fan the work out as multiple single-container queries, then merge
locally.

This iteration is adapter-agnostic by construction — the engine only consumes
`adapter.Connection`/`Cursor`, so it works for any future adapter, and the mock
adapter tests it without a network.

## Supported forms (v1)

**1. Multi-container scan (UNION ALL):**

```sql
SELECT * FROM sales.orders, sales.archive AS c WHERE c.status = "open"
```

The same body runs against each listed container; result streams are concatenated.
A synthetic leading `_container` column identifies each row's origin.

**2. Simulated equi-join (two containers):**

```sql
SELECT o.id, o.total, cu.name
FROM sales.orders AS o
JOIN sales.customers AS cu ON o.customerId = cu.id
```

Executed as a client-side hash join:
1. Run the smaller/filtered side (`customers`) fully, keyed by the ON field →
   in-memory hash table.
2. Stream the other side (`orders`) page by page; probe the hash table; emit combined
   rows with prefixed columns (`o.total`, `cu.name`).
3. Inner join only in v1; `ON` supports a single equality between one field per side.

**Hard limits (spelled out in the UI, not hidden):**
- The build side is fully materialized: capped at a configurable
  `max_join_rows` (default 10 000); exceeding it aborts with a clear error suggesting
  a WHERE filter — never silent truncation.
- RU cost is the **sum of all underlying queries** and is reported as such; the status
  bar marks results as `simulated (client-side)` so nobody mistakes this for a
  server-side capability.
- No cross-account joins in v1 (both scopes resolve within the active profile's
  connection).

## Scope

- `internal/query/plan.go` — extend the scope parser into a small planner:
  - Detect multi-scope FROM lists and two-sided `JOIN ... ON a.x = b.y` where each
    side is a `db.container` scope; produce a `Plan` of per-container `adapter.Query`
    leaves + a merge step (`UnionAll` | `HashJoin{LeftKey, RightKey}`).
  - Single-scope queries produce a trivial pass-through plan — the iteration-5 run
    flow becomes "execute plan", with zero behavior change for one-container queries.
  - Anything the planner can't prove it can simulate (three-way joins, non-equi ON,
    OUTER, cross-scope subqueries) → explicit "not supported, run server-side per
    container" error, never a wrong answer.
- `internal/query/exec.go` — plan executor over `adapter.Connection`:
  - Runs leaves via the normal cursors (each leaf is itself cross-partition per
    iteration 3), merges into synthetic `adapter.Page`s so the results pane, export,
    and history need **no changes**.
  - Aggregated `Stats`: summed RU, wall-clock elapsed, per-leaf breakdown in `Meta`
    for the status bar tooltip.
  - Cancellation: context cancel tears down all in-flight leaf cursors.
- TUI: status bar `simulated` badge; error surface for the unsupported-shape and
  join-cap messages.
- Config: `max_join_rows` per profile (iteration 6 schema gains the key).

## As built

Decisions the sections above left open, recorded once the code settled them:

- **`JOIN ... ON` stays the syntax.** Cosmos's own join is always `JOIN alias IN path`
  (or a subquery) and has no `ON`, so the two never collide: a `JOIN` whose source is a
  `db.container` path is cross-container, everything else passes through untouched.
- **One entry point.** `query.BuildPlan` replaced `ParseScope` and `ErrMultiContainer`;
  `query.Engine.Execute` returns an ordinary `adapter.Cursor`, which is why the results
  pane, export, and history needed no changes.
- **Union grammar.** The container list must be the whole first `FROM` clause and share
  one alias (default `c`). Leaves run one after another, so a page never mixes
  containers and one leaf cursor is open at a time. Raw items gain `_container` too.
- **Join grammar.** `SELECT` list is `*` or top-level `alias.field [AS name]` items; `ON` fields
  may be nested. Leaves run `SELECT * FROM alias` plus the pushed-down `WHERE`.
- **WHERE pushdown.** The clause is cut at top-level `AND`s and each conjunct goes to
  the side it reads; an `OR` or `BETWEEN` at top level keeps the clause whole. A
  conjunct reading both sides is refused: evaluating it would need a client-side
  expression engine.
- **Build side.** The side with a pushed-down filter when only one has it, otherwise
  the joined (right) container. Sizes are unknown before the queries run.
- **Key equality** follows the service: strict on type, `1`, `1.0` and `1e0` equal,
  integers past 2^53 compared digit for digit, a missing field equal to nothing.
- **Probe pages with no match are skipped**, not served empty; their RU still counts.
- **Per-leaf RU lives in `adapter.Stats.LeafCharges`**, a typed map rather than the
  string-keyed `Meta` sketched above. A terminal has no tooltip, so the status bar prints
  the breakdown beside the total: `10.00 RU (sales.customers 7.50 + sales.orders 2.50)`.
- **Errors.** `query.ErrUnsupported` and `query.ErrJoinTooLarge`, both sentinels,
  wrapped with the shape or the container that tripped them.
- **History records refused runs too.** A query the planner or the TUI turns away (no
  scope, unsupported shape) is logged as a failure so it can be recalled and fixed,
  reversing iteration 7's "never reached the adapter, never recorded". Only an empty
  buffer records nothing.
- `make emulator-seed` (`test/seed`) loads sample databases for the manual checklist.

## Out of scope

- OUTER/LEFT joins, multi-way joins, cross-scope aggregation pushdown, cross-account
  or cross-adapter joins, spill-to-disk — recorded as future work in 00-overview.

## Steps

1. Planner: grammar + `Plan` type, table-driven tests (below) before any execution.
2. Executor: UNION ALL first (simpler, reuses paging end-to-end), then hash join.
3. Wire the run flow to "execute plan"; add badge + config key; docs/README examples.

## Testing

**Unit (mock adapter — this is where most coverage lives):**
- Planner table: single scope → pass-through; two-scope FROM → UnionAll; JOIN with
  equality ON → HashJoin with correct keys; unsupported shapes (3 scopes + JOIN,
  `ON a.x > b.y`, LEFT JOIN) → typed unsupported errors.
- UNION ALL: rows from both mock containers appear with correct `_container` values;
  paging across leaf boundaries; RU is the sum of leaves.
- Hash join: matching rows combine with prefixed columns; unmatched rows dropped
  (inner); duplicate keys on the build side produce a row per match; empty build side
  → empty result, no error.
- Join cap: build side larger than `max_join_rows` aborts with the suggest-a-filter
  error; nothing partial is rendered.
- Cancellation mid-join closes every leaf cursor (mock records `Close` calls).

**Integration (emulator):** seed `orders` (25 rows, 3 partitions) + `customers`
(5 rows); assert the join result matches a precomputed fixture; assert summed RU > 0
and equals the sum of leaf charges within rounding.

**Manual checklist:**
- [x] UNION-scan two emulator containers; `_container` column present; badge shows
      `simulated`.
- [x] The join example above returns customer names on orders; RU shows the sum.
- [x] A join exceeding the cap fails with the helpful message; TUI stays alive.

## Acceptance criteria

- Single-container queries are byte-for-byte unaffected (plan pass-through proven by
  the iteration-5 test suite still passing untouched).
- The engine never returns a silently wrong or truncated result — every unsupported
  or capped case is an explicit error.
- No cosmos imports anywhere in `internal/query` (depguard-clean; engine is proven
  against the mock adapter).
