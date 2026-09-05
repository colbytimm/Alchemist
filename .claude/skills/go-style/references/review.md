# Reviewing Go code

Read the diff for these, in this order. Stop at the first category that has findings
worth reporting rather than listing everything at once.

## 1. Correctness

- Every returned error checked, or discarded with a comment saying why.
- `%w` vs `%v` chosen deliberately; no error both logged and returned.
- Type assertions use comma-ok. No `panic` outside `main`.
- No `return nil, nil` from a value-or-error function.
- Contexts threaded to the I/O call; every `WithCancel`/`WithTimeout` has `defer cancel()`.
- Goroutines have an owner that waits for them and a stop condition.
- Maps and slices crossing a goroutine or an API boundary are guarded or copied.
- Nothing in `pitfalls.md`'s trap list: typed nil interface, slice aliasing, `defer` in
  a loop, deferred `Close` on a writer, map iteration order in output, shadowed `err`.

## 2. The architectural invariant

- `internal/tui` imports `internal/adapter` only, never `internal/adapter/cosmos`.
- Concrete adapters constructed in `cmd/`, not deeper.
- New mutable package-level state — justified, or replaced with injection?
- New `init()` — almost certainly wrong; ask.

## 3. API shape

Changes to exported signatures are the expensive kind of mistake.

- Interface defined by its consumer, unless it is the published contract in `internal/adapter`.
- Constructors return concrete types.
- `ctx` first; `error` last; no naked `bool` parameters; signature fits one line.
- Struct literals use field names.
- Enum zero value is either meaningful or `iota + 1`.
- Anything marshaled has field tags.

## 4. Naming and docs

- Initialisms uniform (`ID`, `URL`, `RU`) — the linter does *not* check this here.
- No stutter, no `Get` prefix, no `util`/`helpers` package.
- Receivers one or two letters, consistent per type.
- Comments earn their place. Flag narration, restated signatures, and `// Foo
  implements Bar.` — a missing doc comment is not a finding.
- Any doc comment that survives starts with its name and ends with a period.
- Package comment present and adjacent to the `package` clause.
- Error strings lowercase, unpunctuated, prefixed `package: operation:`, no "failed to".

## 5. Tests

- New behaviour and every new error path covered.
- Test in `<pkg>/test/`, `package <pkg>_test`, using `testify`.
- `require.Equal(t, want, got)` in that order.
- Table tests use `name`/`want`/`wantErr` and `t.Run`.
- Helpers call `t.Helper()`; cleanup uses `t.Cleanup`.
- Errors matched with `ErrorIs`/`ErrorAs`, never string comparison.
- Bug fixes have a test that fails without the fix.
- Integration tests carry `//go:build integration` and skip when their env is absent.

## 6. Noise

- Commentary that restates the code.
- Unrelated reformatting mixed into the diff.
- Abstraction with exactly one caller.
- A `v := v` loop-variable shadow (unnecessary since Go 1.22).
- Functions nearing `gocognit` 30 that want extraction, not a comment.

## Reporting

Lead with the finding that would break something. Give `file:line`, what goes wrong,
and the concrete fix. Cite the rule only when it is not self-evident. Say plainly when
the diff is clean — do not manufacture findings to fill a review.
