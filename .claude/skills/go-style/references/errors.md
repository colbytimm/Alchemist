# Errors

## Which construct to use

| Caller needs to match it? | Message | Use |
| --- | --- | --- |
| No | static | `errors.New("...")` inline |
| No | dynamic | `fmt.Errorf("...: %v", x)` |
| Yes | static | package-level `var ErrFoo = errors.New("...")` |
| Yes | dynamic / carries fields | custom type with an `Error() string` method |

Do not export a sentinel or define a type until something actually matches on it.
Every exported error is API surface you cannot remove.

## Message text

Lowercase, no trailing punctuation, no capital unless a proper noun starts it.
Errors get concatenated — `"cosmos: create client: dial tcp: connection refused"` —
so each layer adds exactly one hop of context.

This repo's convention is `package: operation: detail`:

```go
return fmt.Errorf("adapter: register %q: nil factory", name)
return fmt.Errorf("cosmos: create client: %w", err)
return fmt.Errorf("adapter: unknown adapter %q", name)
```

Banned padding: `"failed to "`, `"error while "`, `"unable to "`, `"an error occurred"`.
The value is already an error; saying so twice is noise. Quote user-supplied strings
with `%q` so empty values are visible.

## Wrapping

- `%w` when a caller up the stack may reasonably `errors.Is`/`errors.As` it. Put `%w` last in the format string.
- `%v` to deliberately sever the chain — at a system/API boundary, or when the underlying error is an implementation detail you do not want to promise.
- Return `err` unchanged when you have nothing to add. A wrap that only repeats the callee's message is worse than no wrap.
- `errors.Join(err1, err2)` for genuinely independent failures (cleanup plus operation).
- Only one `%w` per `fmt.Errorf` unless you intend a multi-error.

```go
// good — adds this layer's domain, preserves the chain
if err := c.client.Ping(ctx); err != nil {
    return fmt.Errorf("cosmos: ping %s: %w", c.account, err)
}
```

## Matching

Never `==` and never string matching. `errorlint` is on and will flag both.

```go
if errors.Is(err, query.ErrUnsupported) { ... }

var respErr *azcore.ResponseError
if errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound { ... }
```

## Handle each error exactly once

Pick one of: **log and degrade**, **match and branch**, or **wrap and return**.
Doing two of them produces the same failure printed three times at three layers.

```go
// bad — the caller will log this too
if err != nil {
    log.Printf("cosmos: query failed: %v", err)
    return err
}
```

Library and `internal/` code returns. Only `cmd/` and `main` decide what to print.

## Never swallow

```go
_ = f.Close()                 // bad on its own
defer func() { _ = f.Close() }()  // only if a close error is genuinely irrelevant

// good — say why, or handle it
// Close errors on a read-only handle carry no information.
defer func() { _ = resp.Body.Close() }()
```

`errcheck` and `bodyclose` are on. Every HTTP response body must be closed.
Test files are exempt from `errcheck` in `.golangci.yml` — do not lean on that in
non-test code.

## Do not panic

- No `panic` in `internal/` or library code. Return an error.
- `os.Exit` and `log.Fatal` belong in `main` only, and only after deferred cleanup has run — remember neither runs `defer`s. Wrap the body in `run() error` and exit once on its result.
- `recover` only at a goroutine boundary you own, and only to convert a panic into an error you then return. Never to make control flow.
- Type assertions use comma-ok. `v := x.(T)` is a panic waiting to happen.

## In-band errors

Do not signal failure with `-1`, `""`, or a zero struct. Return a second value.

```go
// bad
func Lookup(k string) string

// good
func Lookup(k string) (string, bool)
func Lookup(k string) (string, error)
```

`nilnil` is on: never `return nil, nil` from a function whose contract is
"value or error". Return a sentinel, or restructure the signature.

## Errors in the TUI layer

`internal/tui` receives errors as `tea.Msg` values and renders them; it must not
`log.Fatal` or write to stderr directly — that corrupts the alternate screen buffer.
Carry the error into the model and let the view display it.
