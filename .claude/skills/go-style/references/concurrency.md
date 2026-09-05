# Concurrency

## The two rules that prevent most bugs

1. **No fire-and-forget goroutines.** Every goroutine needs a defined stop condition
   and a way for its creator to wait for it. If you cannot say when it exits, do not
   start it.
2. **Do not add concurrency the caller did not ask for.** Write the synchronous
   function. A caller can wrap it in `go`; it cannot unwrap yours.

```go
// bad — nobody knows when this ends, errors vanish
go c.refreshCatalog(ctx)

// good — lifetime is owned and observable
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    if err := c.refreshCatalog(ctx); err != nil {
        errCh <- err
    }
}()
// ... later
wg.Wait()
```

Never start a goroutine in `init()`.

## Context

- `ctx` is the first parameter and is threaded all the way to the I/O call. A network
  call reached without a context is a bug.
- Check `ctx.Err()` in any loop that can run long; return `ctx.Err()` when it fires.
- Every `context.WithCancel`/`WithTimeout` is paired with `defer cancel()` on the very
  next line — even when the context is known to expire. Leaking the cancel leaks the
  timer.
- Do not put a `context.Context` in a struct field. Pass it per call.
- `context.Value` for request-scoped data only, keyed by an unexported type. Not for
  optional arguments.

## Channels

- Size 0 (unbuffered, for synchronisation) or 1 (a signal that must not block).
  Any other size needs a comment explaining why that number cannot deadlock.
- `chan struct{}` for pure signalling — it allocates nothing and cannot be misread as
  carrying data.
- The sender closes; the receiver never does. Closing a channel with multiple senders
  is a race — use a `done` channel or a `WaitGroup` instead.
- Receiving from a closed channel yields the zero value immediately, forever. Use
  `v, ok := <-ch` when the difference matters.
- `select` with `ctx.Done()` on every blocking send *and* receive that can outlive the
  request.

```go
select {
case results <- page:
case <-ctx.Done():
    return ctx.Err()
}
```

## Mutexes

- Zero value is ready: `var mu sync.Mutex`. Never `new(sync.Mutex)`, never a `*sync.Mutex` field.
- Do not embed a mutex, not even in an unexported struct — it publishes `Lock`/`Unlock`
  as methods of the outer type.
- Declare the mutex directly above the fields it guards, and say which those are:

```go
var (
    registryMu sync.RWMutex
    registry   = map[string]func() Adapter{}
)
```

- `defer mu.Unlock()` immediately after `Lock()`. Only drop the `defer` when the
  critical section must end before the function does, and keep that section tiny.
- `RWMutex` only when reads genuinely dominate and are non-trivial; it is slower than
  `Mutex` under contention.
- Never copy a struct containing a mutex. `go vet`'s copylocks catches most of it.

## Shared state

- Prefer passing ownership over sharing. If exactly one goroutine touches a value, no
  synchronisation is needed.
- `sync/atomic` for counters and flags only. Anything with an invariant across two
  fields needs a mutex. Use the typed forms (`atomic.Int64`, `atomic.Bool`), not the
  bare functions.
- `sync.Once` for lazy init that must happen exactly once. Note it will not retry
  after a failure — if init can fail, use a mutex and store the error.
- Maps are not safe for concurrent use; a concurrent write is a hard runtime crash,
  not a race you might miss. Guard every map that crosses a goroutine.

## Bubble Tea specifics

`internal/tui` runs a single-threaded update loop. All blocking work — adapter calls,
paging, catalog loads — happens inside a `tea.Cmd` that returns a `tea.Msg`.

- Never call an adapter directly from `Update` or `View`.
- Never mutate the model from inside a `tea.Cmd`; return a message and mutate in `Update`.
- `View` must be pure and allocation-light; it runs on every frame.
- Cancel in-flight commands when the user moves on — hold the `context.CancelFunc` in
  the model and call it before issuing the next query.

## Testing concurrent code

`go test -race ./...` before claiming a concurrency change works. Never call `t.Fatal`
from a spawned goroutine — it only stops the goroutine it runs in; use `t.Error` and
return. Do not use `time.Sleep` to sequence goroutines in a test; synchronise on a
channel or a `WaitGroup`.
