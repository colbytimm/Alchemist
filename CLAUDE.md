# Alchemist

A terminal IDE for Azure Cosmos DB. Go 1.26.4, module `github.com/colbytimm/alchemist`.

## Go work

**Invoke the `go-style` skill before reading or writing any Go code** — every edit,
new file, test, refactor, and review. It carries the repo's style rules and routes to
deeper references on demand. Do not work from memory of the upstream guides.

## Verify

`make lint` and `make test` must pass before any Go change is reported as done.
`make all` runs fmt-check, lint, test, build. `make help` lists targets.

## Invariant

`internal/tui` depends on the `internal/adapter` interfaces only — never on a concrete
adapter such as `internal/adapter/cosmos`. Adapters are wired in `cmd/`. `depguard`
enforces this.
