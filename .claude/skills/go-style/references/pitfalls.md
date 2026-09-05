# Pitfalls and performance

## Correctness traps

**Typed nil in an interface.** An interface holding a nil pointer is not nil.

```go
func newConn() adapter.Connection {
    var c *connection      // nil
    return c               // interface is NON-nil; `== nil` at the caller fails
}
// Return the interface's nil explicitly, or return a concrete type.
```

**Slice aliasing.** `append` may or may not share the backing array; `s[:2]` always
does. Copy before storing or returning a slice you did not allocate.

```go
sub := make([]Node, len(nodes[:n]))
copy(sub, nodes[:n])
```

**`defer` inside a loop** runs at function exit, not iteration exit. Move the body
into its own function, or close explicitly.

**`defer` evaluates arguments immediately**, the call late:

```go
defer log.Printf("took %v", time.Since(start))  // time.Since runs NOW
defer func() { log.Printf("took %v", time.Since(start)) }()  // correct
```

**Deferred `Close` on a writer swallows the error.** For anything you write to, close
explicitly and check, then `defer` a second close as a safety net.

**Map iteration order is randomised.** Sort keys before producing any output a test or
a user compares.

**Shadowing with `:=`.** The inner `err` is a new variable and the outer one keeps its
old value. `govet`'s shadow check is not on by default — watch for it in `if`/`for`
blocks that assign to an outer variable.

**Integer division and truncation.** `int(f)` truncates toward zero, and `int32`
conversions can silently wrap — `gosec` flags the risky ones. `PageSize` is `int32`
in this repo's `adapter.Query`; convert deliberately.

**Comparing floats with `==`.** RU charges are `float64`. Compare with a tolerance
(`require.InDelta`).

**`time.Time` equality.** `==` compares monotonic clock and location too. Use
`t.Equal`.

**Range over a map/slice while modifying it.** Collect first, mutate second.

**Struct copies with a mutex or `sync.Once` inside.** Always pass a pointer.
`go vet` catches most, not all.

**Loop variables** are per-iteration since Go 1.22, so capturing them in a closure is
safe here (`go 1.26.4`). The old `v := v` shadow is no longer needed — do not add it,
and delete it if you see it.

## Performance — only after you have a reason

Do not restructure for speed without a benchmark. Then:

- `strconv.Itoa(i)` over `fmt.Sprint(i)`; `strconv.FormatInt`/`ParseInt` over the `fmt`
  equivalents. An order of magnitude, and it is the same line count.
- `strings.Builder` for concatenation in a loop, never `s += x`. The TUI `View` does
  this — keep it that way.
- Preallocate when the size is known: `make([]Node, 0, len(items))`,
  `make(map[string]Node, len(items))`.
- Hoist `[]byte(s)` / `string(b)` conversions out of loops; each one allocates and copies.
- `sync.Pool` only for large, provably hot, short-lived buffers. It is a last resort.
- Avoid `interface{}`/`any` in hot paths — it boxes and allocates.
- Do not micro-optimise the TUI render path by caching things the model can compute;
  correctness of the frame beats a few microseconds.

Measure with `go test -bench=. -benchmem`, and use `b.ReportAllocs()`. Quote real
numbers in the commit message, not intuitions.

## Complexity budget

`gocognit` fails at cognitive complexity 30. When a function approaches it, the fix is
extraction with a name, not a comment. Deeply nested `if`/`switch` in an `Update`
method is the usual offender — split per message type.

## Security-adjacent (`gosec` is on)

- `crypto/rand`, never `math/rand`, for anything a user should not predict.
- No credentials, endpoints with keys, or connection strings in source, tests, or
  fixtures — read them from the environment. `make gitleaks` scans for them.
- `InsecureSkipVerify` only behind an explicit opt-in setting, as `cosmos.Settings`
  does for the emulator, and it must be documented at the field.
- Always close `resp.Body` (`bodyclose` is on).
- Validate and bound anything derived from user input before it reaches a query.
