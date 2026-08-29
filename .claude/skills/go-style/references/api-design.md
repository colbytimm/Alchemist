# API and type design

## Interfaces

- **Define interfaces in the consumer, not the producer.** The package that *uses* an
  abstraction owns it. Exception, and it is the one this repo makes deliberately:
  `internal/adapter` publishes `Adapter`/`Connection`/`Catalog`/`Cursor` because the
  contract *is* the product — the TUI and every backend implement against it.
- **Accept interfaces, return concrete types.** A constructor returns `*Client`, not
  `ClientInterface`. `cosmos.Adapter.Connect` returning `adapter.Connection` is fine:
  the interface is the declared contract, not a testing convenience.
- Keep them small. Every method you add must be one a caller actually calls.
- Do not write a wrapper interface solely so a test can stub it. Restructure instead.
- Never take a `*SomeInterface`. Interfaces already hold a pointer when they need to.
- Assert compliance at compile time for exported implementations, using the zero value:

```go
var (
    _ adapter.Adapter    = Adapter{}
    _ adapter.Connection = (*connection)(nil)
)
```

- Method set gotcha: if any method has a pointer receiver, only `*T` implements the
  interface — `T` does not. Pick one receiver kind per type and stay with it.

## Signatures

- `ctx context.Context` first, always. Never a struct field, never a package global,
  never `context.TODO()` outside genuinely unfinished code.
- Keep the signature on one line. If it will not fit, the function has too many
  parameters — group them into a struct.
- More than three parameters, or two of the same type in a row, is a bug magnet:

```go
// bad
func Query(db, container string, pageSize, maxItems int, crossPartition, upsert bool)

// good
func Query(ctx context.Context, q Query) (Cursor, error)
```

- No naked `bool` parameters. Use a named type or an options struct:

```go
type Mode int
const (
    ModeGateway Mode = iota + 1
    ModeDirect
)
```

- `error` is the last result. Values before it must be valid only when `err == nil`.
- Prefer synchronous functions that return a result over ones taking a callback or
  writing to a channel. Callers can add concurrency; they cannot remove it.
- Pass values. Do not take a pointer to avoid a copy unless the type is genuinely
  large or must be mutated. Never copy a struct from another package that has pointer
  receiver methods.

## Optional configuration

Small and stable → an options struct with a zero value that means "defaults":

```go
type Settings struct {
    Endpoint           string
    PageSize           int32
    InsecureSkipVerify bool
}
```

Open-ended and likely to grow → functional options, with an opaque interface so the
option set stays extensible:

```go
type Option interface{ apply(*config) }

type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

func WithPageSize(n int32) Option {
    return optionFunc(func(c *config) { c.pageSize = n })
}
```

Do not use a variadic `...Option` on a constructor that takes zero required arguments
just to look flexible. Required inputs stay positional.

## Structs

- Field names in every literal for types outside the current package; strongly
  preferred inside it too. Positional literals break silently when a field is added.
- Omit zero-value fields unless the explicit zero carries meaning.
- `var s Settings` for a zero-value struct, not `s := Settings{}`.
- `&T{...}` to initialize a struct reference, not `new(T)` followed by assignments.
- Do not embed a type in an exported struct — embedding leaks the inner type's whole
  method set into your API and locks you into its changes. Name the field.
- Struct field tags on anything marshaled. Do not rely on the default field name:

```go
type Page struct {
    Items    []json.RawMessage `json:"items"`
    RUCharge float64           `json:"ruCharge"`
}
```

- Copy slices and maps at the boundary. A stored or returned slice that the caller
  still holds is shared mutable state:

```go
func (c *Catalog) SetPath(p []string) {
    c.path = make([]string, len(p))
    copy(c.path, p)
}
```

## Enums

Start at `iota + 1` so the zero value is not silently a valid member — unless the zero
value genuinely *is* the sensible default, in which case name it explicitly
(`KindUnknown`).

## Zero values

Design types so the zero value works. `var mu sync.Mutex`, `var buf bytes.Buffer`, and
`var s []string` are all immediately usable. Prefer that over a mandatory `New` when
there is nothing to configure.

## Globals and `init`

- No mutable package-level state. Dependencies come in through constructors. This
  repo's `internal/adapter` registry is the one deliberate exception, and it is
  guarded by `registryMu`.
- Avoid `init()`. It runs before `main` can do anything about failures, in an order
  you do not control, and it cannot return an error. No I/O, no environment reads, no
  goroutines. Adapter registration is wired explicitly in `cmd/`, not via blank imports.
- Flags are defined in `package main` only.

## Time

- `time.Time` for instants, `time.Duration` for spans. Never an `int` of unspecified
  units in an internal API.
- `t.Add(d)` for absolute elapsed time; `t.AddDate(y, m, d)` for calendar arithmetic —
  they differ across DST boundaries.
- Compare with `t.Before`/`t.After`/`t.Equal`, never `==`.
- When an external format cannot carry a `Duration`, put the unit in the name:
  `TimeoutMillis`, `IntervalSeconds`.

## Generics

Use them when the alternative is the same function copied per type. Do not
parameterize a type that only ever has one instantiation, and do not build a generic
`Map`/`Filter` layer — a `for` loop is clearer and the repo has no such helpers.
