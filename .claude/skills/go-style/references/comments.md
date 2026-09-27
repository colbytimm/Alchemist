# Doc comments and commenting

## The bar

**If a competent reader could get it from the code, do not write it.** This repo
prefers no comment to a comment that restates the signature. When one feels
necessary, fix the name or split the function first — that is almost always the real
fix.

Nothing in `.golangci.yml` requires a doc comment, on exported identifiers or
anything else. `revive`'s `exported` rule is off. Do not add one to satisfy a rule
that is not there.

Write a comment only for something the code cannot carry:

- a protocol, vendor, or platform quirk
- why an error is deliberately ignored, or a safety deliberately overridden
- a non-obvious constraint a future edit would otherwise break
- what an exported identifier is *for*, when its name and signature genuinely do not
  say — one line, in godoc

Delete on sight: narration, step-by-step retelling, `// Foo implements Bar.`,
`// Name returns the name.`, rationale for an obvious choice. Removing such a comment
is always in scope, in any file you touch.

## Doc comment form

When you do write one: start with the name being documented, be a complete sentence,
end with a period, and keep it to one line unless a second genuinely earns its place.

```go
// Register makes an adapter factory available under name.
func Register(name string, factory Factory) error {
```

```go
// bad
// this function registers an adapter   <- lowercase, no name, no period
// Registers an adapter.                <- does not start with the name
// Register registers an adapter.       <- says nothing the signature does not
```

## Package comments

Exactly one per package, immediately above the `package` clause, no blank line
between. Starts `Package <name> ...`. In multi-file packages it goes in the file
named after the package (`adapter.go` for `package adapter`).

```go
// Package adapter defines the backend-agnostic contract every database
// adapter implements. The TUI depends only on these interfaces — never on a
// concrete implementation such as internal/adapter/cosmos.
package adapter
```

State what the package provides and any invariant a caller must respect. Do not
restate the file list.

## Grouped declarations

A comment above a `var`/`const` block documents the group; individual entries can
carry their own.

```go
// Compile-time contract checks.
var (
    _ adapter.Adapter    = Adapter{}
    _ adapter.Connection = (*connection)(nil)
)
```

## Interface methods

Document the contract at the interface, not at each implementation. Implementations
say what is *different* — or nothing at all.

```go
type Cursor interface {
    // NextPage fetches the next page of results.
    NextPage(ctx context.Context) (Page, error)
    // HasMore reports whether another page is available.
    HasMore() bool
}
```

Phrase boolean-returning methods as "reports whether", not "returns true if".

## Inline comments

Explain **why**, never **what**. If a comment restates the code, delete the comment.

```go
// bad
i++ // increment i

// good
// Gateway mode: the emulator's direct-mode ports are not reachable from CI.
opts.ConnectionMode = azcosmos.ConnectionModeGateway
```

Match the comment density of the file you are editing. Do not add a running narration
to code that has none.

## TODOs

```go
// TODO(colbytimm): support a per-key top N in joins — see issue #42.
```

Owner or issue link, and what "done" means. A bare `// TODO` is noise.

## Deprecation

```go
// Deprecated: use ParseSettings instead. Removed after v0.4.
```

Blank comment line before `Deprecated:`, and it must be the last paragraph.

## Formatting quirks that matter

- A comment line indented relative to its neighbours renders as a `<pre>` block in godoc. Keep continuation lines flush.
- Wrap around 80–100 columns. Do not reflow an entire existing comment block to satisfy a column count.
- `[Name]` links to a symbol in godoc; use it for cross-references instead of bare prose.
