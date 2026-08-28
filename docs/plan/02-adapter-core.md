# Iteration 2 — Adapter Core, Mock Adapter, Scope Parser

## Goal

The adapter contract every backend implements, an in-memory mock adapter that powers all
TUI tests, and the query-scope parser that lets users write
`SELECT * FROM mydb.orders AS c ...` and have Alchemist route it to the right container.

## Scope

- `internal/adapter/adapter.go` — the interfaces exactly as sketched in
  [00-overview.md](00-overview.md#core-adapter-interfaces-sketch): `Adapter`,
  `Connection`, `Query`, `Catalog`, `Node`, `Cursor`, `Page`, `Stats`.
- `internal/adapter/registry.go` — `Register(name string, factory func() Adapter)` /
  `Get(name string)`; registration happens explicitly from `cmd/` (no `init()` magic
  required for correctness, but adapters may self-register).
- `internal/adapter/mock/mock.go` — deterministic in-memory adapter:
  - Fixture catalog: 2 databases (`sales`, `telemetry`), 2–3 containers each (e.g.
    `orders`, `customers`, `events`) with `partitionKey` metadata.
  - Canned query results: multi-page (3 pages × 10 rows) to exercise continuation,
    fake RU charges, configurable per-call errors and latency for testing
    error/loading states.
- `internal/query/scope.go` — a hand-written tokenizer (NOT regex) that:
  - Detects `FROM <db>.<container>` (and optional `AS <alias>` / bare alias) and rewrites
    the query to `FROM <alias|c>`, returning `Scope: [db, container]`.
  - Leaves already-scoped queries (`FROM c`, `FROM c JOIN t IN c.tags`) untouched —
    `c.child` property paths must NOT be mistaken for `db.container`.
  - Respects string literals and quoted identifiers; case-insensitive keywords.
  - Returns the original text plus a structured result: `Rewritten string`,
    `Scope []string`, `Explicit bool`.
  - Forward-compatibility: in iteration 10 this tokenizer grows into a planner for
    multi-scope FROM lists and cross-container `JOIN ... ON` (client-side). Design the
    token stream so multiple `db.container` references can be detected — v1 may
    reject them ("one container per query until iteration 10") but must not
    misparse them as a single scope.

## Out of scope

- Any network code; the Cosmos adapter is iteration 3.
- TUI wiring; iteration 4 consumes these packages.

## Steps

1. Write `adapter.go` with full doc comments — this file is the public contract; get it
   reviewed before the rest.
2. Write the registry with a mutex-guarded map (no global mutation after startup in
   practice, but safe anyway).
3. Build the mock adapter with an options struct (`WithError(op)`, `WithLatency(d)`,
   `WithPages(n)`) so TUI tests can compose scenarios.
4. Write the scope tokenizer as a small state machine over tokens:
   ident / dot / string / keyword. Table-driven tests first (cases below), then implement.

## Testing

**Unit** (`internal/adapter/test/`, `internal/adapter/mock/test/`, `internal/query/test/`):
- Registry: register/get/unknown-name error; duplicate registration rejected.
- Mock: catalog tree shape; cursor paging returns 3 pages then `HasMore() == false`;
  injected errors surface from the right operation.
- Scope parser table (minimum):
  | Input | Scope | Rewritten |
  |---|---|---|
  | `SELECT * FROM mydb.orders AS c WHERE c.total > 5` | `[mydb orders]` | `SELECT * FROM c WHERE c.total > 5` |
  | `SELECT * FROM mydb.orders o` | `[mydb orders]` | `SELECT * FROM o` |
  | `SELECT * FROM c` | `[]` | unchanged |
  | `SELECT t.name FROM c JOIN t IN c.tags` | `[]` | unchanged |
  | `SELECT * FROM c WHERE c.note = "FROM a.b"` | `[]` | unchanged (literal untouched) |
  | `select * from MyDb.Orders as c` | `[MyDb Orders]` | keywords case-insensitive |
  | `SELECT VALUE c.id FROM mydb.orders c` | `[mydb orders]` | `... FROM c` |

**Manual checklist:**
- [ ] None — this iteration is fully covered by unit tests (`make test`).

## Acceptance criteria

- `internal/adapter` has zero third-party imports (stdlib only).
- Mock adapter satisfies all four interfaces (compile-time `var _ adapter.Adapter = ...`
  assertions).
- Scope parser passes the full table plus fuzz-style “never panics on arbitrary input”
  test (`go test -fuzz` seed corpus committed).
