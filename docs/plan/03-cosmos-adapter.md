# Iteration 3 — Cosmos DB Adapter + Emulator Integration Tests

## Goal

A production-quality `adapter.Adapter` implementation for Azure Cosmos DB (NoSQL API)
built on `github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos` **v1.5.0**, verified
against the Cosmos DB emulator.

## Scope

- `internal/adapter/cosmos/adapter.go`:
  - Settings: `endpoint`, `key` (or full `connection_string`), `insecure_skip_verify`
    (emulator only), `page_size`.
  - Client construction: `azcosmos.NewClientFromConnectionString` or
    `NewClientWithKey` with gateway-mode `azcosmos.ClientOptions`; when
    `insecure_skip_verify` is set, a custom `policy.ClientOptions.Transport` with
    `tls.Config{InsecureSkipVerify: true}` — gated behind the explicit setting and
    annotated `#nosec G402` with justification (self-signed emulator cert only).
- `internal/adapter/cosmos/catalog.go`:
  - Databases via `client.NewQueryDatabasesPager`; containers via
    `database.NewQueryContainersPager`.
  - Container nodes carry `Meta["partitionKey"]` (from container properties read on
    expand) and `HasChildren` for a metadata leaf level.
- `internal/adapter/cosmos/cursor.go`:
  - **Cross-partition queries are the default.** Every query runs via
    `container.NewQueryItemsPager(query, azcosmos.NewPartitionKey(), opts)` — an empty
    partition key tells the v1.x SDK to fan the query out across all physical
    partitions natively (verify exact idiom against the v1.5.0 changelog/API before
    coding; there is no `EnableCrossPartitionQuery` flag anymore). This replaces the
    old `main`-branch approach entirely, which had to hand-roll a REST call with
    HMAC signing and the `x-ms-documentdb-query-enablecrosspartition: true` header
    because SDK v0.3.6 lacked the capability — none of that carries forward.
  - **Single-partition optimization (optional, same iteration if cheap):** when the
    query's WHERE clause pins the container's partition key to a literal
    (e.g. `c.pk = "x"` and `Meta["partitionKey"] == "/pk"`), pass a real
    `azcosmos.NewPartitionKeyString("x")` instead — same results, lower RU. Behind a
    helper with its own unit tests; skip silently on any doubt (correctness first,
    cross-partition is always correct).
  - `QueryOptions{PageSizeHint, ContinuationToken}` for paging; RU from the page
    response `RequestCharge`; elapsed time measured around each `NextPage`.
    Cross-partition queries with ORDER BY / aggregates are executed and merged by the
    service in v1.x — the adapter does not merge result streams itself.
  - Row shaping: unmarshal each item to an ordered key scan; **column order locks to
    the first page** (union of keys in first-seen order); later pages append new keys
    at the end; missing values render as empty cells; nested objects/arrays render as
    compact JSON.
- `test/integration/docker-compose.yml` + Makefile targets `emulator-up`,
  `emulator-down`, `test-integration`.
- `//go:build integration` tests in `internal/adapter/cosmos/test/`.

## Emulator notes (Apple Silicon)

- The classic Linux emulator image does not run on ARM64. Use
  `mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview`, which runs on
  Apple Silicon and serves **HTTP** on 8081 by default (`--protocol https` opt-in).
- Well-known account key (public, not a secret — annotate for gitleaks):
  `C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw==`
  with endpoint `https://localhost:8081` (classic) or `http://localhost:8081`
  (vnext-preview). The adapter must accept both schemes.
- vnext-preview has query gaps (some aggregate/ORDER BY combinations); integration
  tests stick to features it supports. CI (linux/amd64) may use the stable image.

## Out of scope

- TUI integration (iteration 4/5 consume this through the interfaces).
- Writes/upserts, stored procedures, TTL/index management.
- Cross-**container** queries — Cosmos SQL cannot join or union across containers, so
  Alchemist simulates them client-side above the adapter layer; that engine is
  [iteration 10](10-cross-container.md). This adapter only ever queries one container
  per cursor.

## Steps

1. Pin the SDK: `go get github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos@v1.5.0`.
2. Verify v1.5.0 API surface (pager types, `QueryOptions` field names, `RequestCharge`
   location) against the vendored source, then implement adapter → catalog → cursor.
3. Unit-test row shaping with canned JSON fixtures (no network).
4. Write docker-compose + Make targets; write integration tests:
   create database + container (pk `/pk`), seed ~25 items across 3 partition key
   values, then assert.
5. Register the adapter under name `cosmos`.

## Testing

**Unit** (no network): column-union ordering across pages, nested-value rendering,
settings validation (missing endpoint/key), TLS-skip only honored when the setting is
explicitly `true`.

**Integration** (`make test-integration`, `//go:build integration`):
- `Ping` succeeds against the emulator.
- Catalog: created database and container appear; container node has the partition key
  in `Meta`.
- Cross-partition query `SELECT * FROM c` returns **all** seeded items across ≥2 pages
  with `PageSizeHint: 10` — the seed data spans 3 partition key values precisely so
  this test fails if the query silently stays single-partition; continuation works;
  `Stats.RequestCharge > 0`.
- Cross-partition query with a filter that matches items in ≥2 partitions returns the
  full expected subset.
- Single-partition path: a query pinning the partition key returns only that
  partition's items (and, if the optimization landed, uses the pinned-key pager).
- Bad SQL returns an error (not a panic) with the service message preserved.

**Manual checklist:**
- [x] `make emulator-up && make test-integration && make emulator-down` passes locally.

## Acceptance criteria

- `internal/adapter/cosmos` contains no `log.Fatal`/`panic`; every failure path returns
  a wrapped error with operation context.
- No hand-rolled REST/HMAC code — the SDK covers catalog, query, paging, RU.
- Integration suite runs green against the emulator on both Apple Silicon
  (vnext-preview, HTTP) and linux/amd64 (stable image, HTTPS + TLS skip).
