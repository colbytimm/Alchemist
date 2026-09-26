# 14. Writing an Adapter

The TUI only knows the interfaces in
[`internal/adapter`](../../internal/adapter/adapter.go): `Adapter` opens a
`Connection`, which serves a lazy `Catalog` tree and runs a `Query` into a `Cursor` of
pages. The Cosmos DB adapter is the first adapter, not a special case.

- The in-memory adapter in [`internal/adapter/mock`](../../internal/adapter/mock/mock.go)
  is the smallest complete example, and the one every TUI test runs against.
- [`internal/adapter/cosmos`](../../internal/adapter/cosmos) is the real one.

A new adapter is registered in [`cmd/root.go`](../../cmd/root.go). Nothing under
`internal/tui` may import it, and `make lint` fails if something does: `depguard`
enforces that the TUI depends on the interfaces alone.

Everything beyond reading is an optional interface a `Connection` may also implement:
`CatalogAdmin`, `ThroughputEditor`, `Inspector`, `FieldSampler`, `Batcher`,
`ItemDrafter`, `ItemEditor`, `DefinitionReader`, `ItemScanner` and `ItemWriter`.
`management` in `cmd/root.go` detects each with a comma-ok type assertion, and the TUI
offers only what the connection supports: a backend that cannot manage its catalog
does not offer the [catalog keys](../data/catalog.md), and they disappear from the
help overlay.

The design of the adapter layer is in the plan:
[02-adapter-core](../plan/02-adapter-core.md) and
[03-cosmos-adapter](../plan/03-cosmos-adapter.md).

---

[← IV. Configuration](../reference/configuration.md) · [Contents](../README.md) · [15. Building and Testing →](building.md)
