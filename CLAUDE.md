# Alchemist

A terminal IDE for Azure Cosmos DB. Go 1.26.8, module `github.com/colbytimm/alchemist`.

## Clean Code

**Read `.claude/CLEAN_CODE.md` in full before any code, test, refactor, review, or
documentation change.** Its rules are mandatory and override default behavior. Read
the file itself — never a summary of it, and never the SessionStart hook's preview.

## Comments

**Hard rule: if a competent reader could get it from the code, do not write it.**
No narration, no restating the signature, no explaining an obvious choice, no
listing what a function does step by step. When a comment feels necessary, fix
the name or split the function first — that is almost always the real fix.

What may stay:

- One terse line of godoc on an exported identifier whose purpose is not already
  obvious from its name and signature. One line, not a paragraph. `// Foo
  implements Bar.` and `// Name returns the name.` are noise — delete them.
- A fact the code genuinely cannot carry: a protocol or vendor quirk, why an
  error is deliberately ignored, an overridden safety, a non-obvious constraint
  a future edit would otherwise break.

Nothing in `.golangci.yml` requires a doc comment on anything. Never add one to
satisfy a rule that is not there. Deleting a comment that violates this is
always in scope, in any file you touch. `references/comments.md` in the
`go-style` skill has the long form.

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
