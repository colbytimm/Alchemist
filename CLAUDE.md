# Alchemist

A terminal IDE for Azure Cosmos DB. Go 1.26.4, module `github.com/colbytimm/alchemist`.

## Clean Code

**Read `.claude/CLEAN_CODE.md` in full before any code, test, refactor, review, or
documentation change.** Its rules are mandatory and override default behavior. Read
the file itself — never a summary of it, and never the SessionStart hook's preview.

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
