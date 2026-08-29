# Doc comments and commenting

## What the linter catches

`godot` fails any comment that does not end in a period — that one is mechanical.
Doc-comment *presence* is not reliably enforced by this repo's `revive` settings, so
treat "every exported symbol is documented" as your responsibility, not CI's.

## Doc comment form

Start with the name being documented, be a complete sentence, end with a period.

```go
// Register makes an adapter factory available under name. It returns an
// error if the name is empty, the factory is nil, or the name is taken.
func Register(name string, factory func() Adapter) error {
```

```go
// bad
// this function registers an adapter   <- lowercase, no name, no period
// Registers an adapter.                <- does not start with the name
```

Every exported identifier needs one: types, funcs, methods, vars, consts, struct
fields worth explaining. Unexported symbols get a doc comment when the *why* is not
obvious from the name; skip it when it is.

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
// TODO(colbytimm): support cross-container joins — see docs/plan/10-cross-container.md.
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
