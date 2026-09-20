# Naming

## Packages

- Lowercase, single word, no underscores, no camelCase, no plurals: `adapter`, `query`, `theme`, `cosmos`.
- The package name is part of every reference. Name for what it *provides*, not what it contains.
- Banned names: `util`, `common`, `helpers`, `misc`, `base`, `shared`, `types`, `interfaces`. If you cannot name a package for what it provides, it is not a package.
- Directory name matches package name. Exception in this repo: `internal/<pkg>/test/` holds `package <pkg>_test`.
- Never shadow a stdlib name in a way that forces import aliasing at call sites (`url`, `context`, `time`).

## No stutter

The caller always writes `pkg.Symbol`. Strip the package name from the symbol.

```go
// bad                        // good
adapter.AdapterRegistry       adapter.Registry
query.QueryScope              query.Scope
cosmos.CosmosConnection       cosmos.Connection
theme.ThemeDefault            theme.Default
```

Exception: when the package's whole purpose *is* the one type, `pkg.Pkg` is idiomatic —
this repo does exactly that with `cosmos.Adapter` implementing `adapter.Adapter`.

## MixedCaps, always

`MixedCaps` exported, `mixedCaps` unexported. Never `snake_case`, never `MAX_RETRIES`,
not even for constants: `maxRetries`, `defaultPageSize`.

## Initialisms

Uniform case across the whole initialism — all upper or all lower, never `Url`/`Id`.

```go
ID, URL, API, HTTP, JSON, SQL, TLS, RU, TUI, DB
appID  urlPath  parseJSON  httpClient  cosmosURL  maxRUs
// not: appId, urlPath -> UrlPath, parseJson, HttpClient
```

Two adjacent initialisms just concatenate: `XMLAPI`, `DBURL`. Lowercase leading
initialism when unexported: `xmlAPI`, `jsonDecoder`. `gRPC` keeps its own casing.

`revive`'s `var-naming` rule is disabled in this repo's lint config — that means the
linter will *not* catch initialism mistakes. Get them right by hand.

## Variables and parameters

Name length is proportional to the distance between declaration and last use.

```go
for i, n := range nodes { ... }          // one-letter is correct here
ctx, cancel := context.WithCancel(ctx)   // conventional short names
var registryMu sync.RWMutex              // package-level: descriptive
```

- Drop type information the type already carries: `users`, not `userSlice`; `count`, not `numCount`.
- Conventional short names, use them: `ctx`, `err`, `i`, `n`, `b`, `r`, `w`, `mu`, `wg`, `ok`, `q`, `db`, `t`/`tb` in tests.
- Do not invent abbreviations. Use a full word or a name Go already uses.

## Receivers

One or two letters derived from the type, identical on every method of that type.

```go
func (a Adapter) Name() string
func (c *connection) Close() error
func (s Settings) validate() error
```

Never `self`, `this`, `me`, or the full type name. If a method does not use the
receiver, write it bare: `func (Adapter) Name() string { return Name }` — this repo
does that in `internal/adapter/cosmos/adapter.go`.

## Interfaces

Single-method interfaces take the method name plus `-er`: `Reader`, `Writer`, `Closer`,
`Formatter`. Multi-method interfaces get a role noun: `Adapter`, `Connection`, `Catalog`,
`Cursor`. Never prefix with `I` or suffix with `Interface`.

## Errors

- Sentinel values: `ErrFoo` exported, `errFoo` unexported. This repo: `query.ErrUnsupported`.
- Custom error types: suffix `Error` — `type ParseError struct{}`. `errname` enforces both.
- The *message* is lowercase with no trailing period; only the *identifier* is capitalized.

## Constants and enums

Name by role, not value. `defaultTimeout`, not `thirtySeconds`. No `K`/`k` prefix.

```go
type Kind int

const (
    KindUnknown Kind = iota // zero value must be a meaningful "unset"
    KindDatabase
    KindContainer
)
```

## Files

Lowercase with underscores when needed: `adapter.go`, `scope.go`, `integration_test.go`.
Group by concept, one concept per file — this repo splits `cosmos` into `adapter.go`,
`catalog.go`, `cursor.go`, `pin.go`, `settings.go`. Not `helpers.go` or `misc.go`.

## Booleans

Read as a predicate: `hasChildren`, `isValid`, `ok`, `found`, `insecureSkipVerify`.
Avoid negatives — `enabled`, not `notDisabled`.
