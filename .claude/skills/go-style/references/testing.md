# Testing

## This repo's layout — follow it

Tests live in a `test/` subdirectory of the package they cover, in the external test
package:

```
internal/adapter/registry.go        package adapter
internal/adapter/test/registry_test.go   package adapter_test
internal/adapter/cosmos/adapter.go       package cosmos
internal/adapter/cosmos/test/cosmos_test.go        package cosmos_test
internal/adapter/cosmos/test/integration_test.go   package cosmos_test, //go:build integration
```

This forces every test through the exported API. If a test needs an unexported
symbol, that is a signal the API is wrong — fix the API rather than moving the test
in-package.

## Assertions: testify, deliberately

The Google guide says avoid assertion libraries. **This repo overrides that** — it
standardises on `stretchr/testify`. Repo convention wins.

- `require` when the test cannot meaningfully continue (the common case here).
- `assert` only when you want several independent checks in one run.
- Never mix a bare `if got != want { t.Errorf }` into a file that uses `require`.

```go
factory, err := adapter.Get("test-fake")
require.NoError(t, err)
require.Equal(t, "test-fake", factory().Name())
```

`require.Equal(t, want, got)` — expected first, actual second. Getting this backwards
inverts every failure message in the file.

## Table-driven tests

Default shape for anything with more than two cases.

```go
func TestParseScope(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    query.Scope
        wantErr error
    }{
        {name: "database and container", input: "use db.orders", want: ...},
        {name: "two containers", input: "...", wantErr: query.ErrMultiContainer},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := query.ParseScope(tt.input)
            if tt.wantErr != nil {
                require.ErrorIs(t, err, tt.wantErr)
                return
            }
            require.NoError(t, err)
            require.Equal(t, tt.want, got)
        })
    }
}
```

- Field is `name`, subtest is `t.Run(tt.name, ...)`. Names are lowercase descriptions
  of the case, not `case1`/`case2`.
- Expected fields are `want`, `wantErr`. Actual is `got`.
- One behaviour per row. When rows need diverging logic, split the test instead of
  adding a `skip`/`mode` field.
- Match errors with `require.ErrorIs`/`ErrorAs`, never on message text.
- Add `t.Parallel()` inside the subtest only when the code under test has no shared
  state — the adapter registry does, so registry tests must stay serial.

## Failure messages

The failure must be diagnosable without opening the test. testify prints want/got for
you; when you add a message, say what the input was:

```go
require.Equal(t, tt.want, got, "ParseScope(%q)", tt.input)
```

Without testify the canonical form is actual-before-expected:
`t.Errorf("ParseScope(%q) = %v, want %v", tt.input, got, tt.want)`.

## Helpers and cleanup

- Any helper that calls `t.Fatal`/`require` starts with `t.Helper()`, or failures
  point at the helper instead of the caller.
- Helpers take `t *testing.T` (or `testing.TB`) as their first parameter.
- `t.Cleanup(func(){ ... })` for teardown, not a trailing `defer` — it runs for
  subtests and after `t.Fatal`.
- `t.TempDir()` for scratch files; it cleans itself up. Never write into the repo.
- Never call `t.Fatal` from a goroutine. `t.Error` and return.

## Test doubles

- Prefer a real implementation. `internal/adapter/mock` exists for exactly this.
- Hand-written fakes over generated mocks. No mocking framework in this repo.
- A fake defined only for one test file goes in that file, unexported:
  `type fakeAdapter struct{ name string }` in `registry_test.go` is the pattern.
- Do not add an interface to production code purely so a test can substitute it.

## Integration tests

Guarded by a build tag and excluded from `go test ./...`:

```go
//go:build integration

package cosmos_test
```

Run them with `make test-integration`, which needs `make emulator-up`. They must skip
cleanly rather than fail when their environment is absent:

```go
endpoint := os.Getenv("COSMOS_EMULATOR_ENDPOINT")
if endpoint == "" {
    t.Skip("COSMOS_EMULATOR_ENDPOINT not set; run make emulator-up")
}
```

Each integration test creates and cleans up its own database or container via
`t.Cleanup`. Never assume state from another test.

## What to test

- Exported behaviour and error paths, including every sentinel error.
- Boundaries: empty input, nil slice, zero struct, cancelled context, page size 1.
- Bug fixes get a test that fails without the fix. Write it first and watch it fail.
- Do not test the standard library, the Azure SDK, or getters that only return a field.

## Running

```
make test                                  # go test ./...
go test -race ./...                        # any change touching goroutines
go test -run TestParseScope ./internal/query/test/
make coverage-html                         # ./app ./cmd ./internal
```

`.golangci.yml` exempts `_test.go` from `errcheck`, `gosec`, and `unparam`. That is a
lint concession, not a licence to ignore errors that matter to the assertion.
