---
name: go-style
description: Mandatory style, idiom, and review rules for ALL Go work in this repo — writing, editing, refactoring, or reviewing any .go file, including tests and Go snippets in docs. Distills the Google Go Style Guide, Effective Go, go.dev/wiki/CodeReviewComments, and the Uber Go Style Guide, reconciled against this repo's own conventions. Invoke BEFORE reading or writing Go code, not after.
---

# Go style

Rules that fire on nearly every Go change live here. Everything else is in
`references/` and is loaded **only** when the task touches it — do not read a
reference speculatively, and do not read more than the two or three the routing
table points at.

## Precedence when the guides disagree

1. **This file/package** — match the code you are editing. Never restyle surrounding code to a different guide's taste.
2. **`.golangci.yml`** — if a linter enforces it, it is not negotiable.
3. **Go core** — Effective Go + CodeReviewComments.
4. **Google Go Style Guide.**
5. **Uber Go Style Guide** — adopted selectively; `references/repo.md` lists what this repo does *not* adopt.

## Always

**Formatting & structure**
- `gofmt -s` clean, `goimports -local github.com/colbytimm/alchemist`. Three import groups: stdlib, third-party, local.
- Keep the happy path at minimal indentation. Handle errors and special cases first, then `return`/`continue`. No `else` after a branch that returns.
- No naked returns outside short functions. Name result params only when they document something or a `defer` mutates them.

**Naming**
- `MixedCaps`/`mixedCaps` — never `snake_case`, never `SCREAMING_CASE`, not even for unexported constants.
- Initialisms keep uniform case: `URL`, `ID`, `API`, `RU`, `appID`, `cosmosURL` — never `Url`, `Id`, `apiId`.
- Name length scales with scope: `i`, `n`, `q` in a tight loop; descriptive names for package-level and long-lived values.
- No stutter: `adapter.Adapter` is fine, `adapter.AdapterRegistry` is not. No `Get` prefix on getters.
- Receivers are one or two letters, consistent across every method on the type; never `self`/`this`/`me`.

**Errors**
- Return errors; do not log-and-return the same error, and do not `panic` in library code. `os.Exit`/`log.Fatal` only in `main`.
- Never discard an error with `_` without a comment saying why. `errcheck` is on.
- Error strings: lowercase, no trailing punctuation, no "failed to". Prefix with the package and operation: `fmt.Errorf("cosmos: create client: %w", err)`.
- `%w` when a caller may need `errors.Is`/`errors.As`; `%v` to deliberately break the chain at a boundary. Put `%w` at the end of the string.
- Never compare errors with `==` or string matching — `errors.Is` / `errors.As`.

**Types & control flow**
- `context.Context` is the first parameter, named `ctx`. Never store it in a struct.
- Type assertions always use comma-ok: `v, ok := x.(T)`. Bare assertions panic.
- `var s []string` (nil slice), not `s := []string{}`. Test emptiness with `len(s) == 0`; never design an API where nil and empty differ.
- Use field names in struct literals for types from other packages.
- Pass values, not pointers, unless the callee must mutate or the value is genuinely large.

**Comments**
- Write a comment only when a competent reader could not get it from the code. Fix the name or split the function first — that is usually the real fix. No narration, no restating the signature, no `// Foo implements Bar.`
- A doc comment that survives is one terse line, starting with the identifier's name and ending with a period. Exported identifiers are not automatically entitled to one.
- Every package has exactly one package comment, adjacent to the `package` clause, starting `Package foo ...`. `godot` is on.

## Load a reference before you write

| The change involves | Read |
| --- | --- |
| Naming a package, type, func, var, file, or sentinel error | `references/naming.md` |
| Doc comments, package docs, TODOs, godoc-visible prose | `references/comments.md` |
| Creating, wrapping, matching, or handling errors | `references/errors.md` |
| Interfaces, function signatures, options, struct/enum/receiver design, `init`, globals, time | `references/api-design.md` |
| Goroutines, channels, `sync`, cancellation, background work | `references/concurrency.md` |
| Anything in `cmd/` or `main.go` — Cobra commands, flags, args, build/ldflags | `references/cli.md` |
| Any `_test.go` file, test doubles, fixtures, integration tests | `references/testing.md` |
| Debugging odd behaviour, or hot-path/allocation work | `references/pitfalls.md` |
| Layout, tooling, lint config, build tags, which Uber rules this repo rejects | `references/repo.md` |

Reviewing rather than writing? Read `references/review.md` instead of the table.

## Before you call it done

```
make fmt        # gofmt -s + goimports -local
make lint       # golangci-lint (see .golangci.yml)
make test       # go test ./...
```

`make all` runs fmt-check, lint, test, build. Integration tests need the emulator:
`make emulator-up && make test-integration`.

Do not report Go work as complete until `make lint` and `make test` both pass, and
say so plainly with the output if they do not.
