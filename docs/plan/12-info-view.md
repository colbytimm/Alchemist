# Iteration 12 — Info View

## Goal

Press `i` on a database or container and read everything the account knows about it,
in an overlay that works like the help screen: it takes the screen, it scrolls, `esc`
closes it. Cosmos exposes a great deal of per-resource metadata — partition key kind,
indexing policy, TTL, document count, storage size, physical partition count,
provisioned throughput and its floor — and today the catalog fetches a chunk of that,
uses the partition key paths, and discards the rest.

This is a read-only iteration. Nothing here changes a resource.

## Layout

```
┌─ Info · sales.orders ───────────────────────────────────────┐
│  Identity                                                   │
│    Database          sales                                  │
│    Container         orders                                 │
│    Resource ID       hY1TAKjvBQA=                           │
│    Last modified     2026-08-29 14:02:11 UTC                │
│                                                             │
│  Partition key                                              │
│    Kind              Hash (version 2)                       │
│    Paths             /customerId                            │
│                                                             │
│  Throughput                                                 │
│    Mode              autoscale                              │
│    Maximum           4000 RU/s                              │
│    Minimum           1000 RU/s                              │
│    Scaling           complete                               │
│                                                             │
│  Storage                                                    │
│    Documents         1284                                   │
│    Documents size    4.2 MB                                 │
│    Physical partitions  1                                   │
│                                                             │
│  Indexing                                                   │
│    Mode              consistent, automatic                  │
│    Included          /*                                     │
│    Excluded          /"_etag"/?                             │
│                                                             │
│  ↑/↓ scroll   r refresh   esc close                         │
└─────────────────────────────────────────────────────────────┘
```

The header — title, Identity, Partition key — draws the moment the key is pressed, from
the `Node` the tree already holds. The rest is one request away and replaced by a
spinner line until it lands. A section the account cannot answer keeps its heading and
says why, rather than vanishing:

```
│  Throughput                                                 │
│    Inherited from database sales; this container has no     │
│    throughput of its own.                                   │
```

## Interaction

| Key | Where | Action |
|---|---|---|
| `i` | catalog | open the info view for the node under the cursor |
| `↑`/`↓`, `k`/`j` | info view | scroll |
| `r` | info view | re-inspect (the existing Refresh binding) |
| `esc`, `i` | info view | close |

`i` on nothing selected does nothing. The cursor can never sit on a metadata field row
— `selectableRows` excludes `NodeField` — so there is no third case to define.

Routing follows the help overlay exactly: an `showInfo` branch at the top of
`handleKey`, before the global shortcuts, so `r` scrolls-and-refreshes the overlay
rather than refreshing the tree behind it, and `q` does not quit out from under a
half-read screen.

## Adapter contract

```go
// Inspector serves the metadata behind one catalog node. Optional: a
// Connection implements it when its backend has metadata worth a screen.
type Inspector interface {
    Inspect(ctx context.Context, n Node) (Details, error)
}

// Details is one node's metadata, pre-rendered into ordered sections the way
// Page pre-renders rows. The TUI lays it out without interpreting it: only
// the adapter knows what its backend's fields mean, what to call them, or
// what order they read in.
type Details struct {
    Sections []Section
    Raw      json.RawMessage // the backend's own representation of the node
}

// Section is one titled group. Note explains an empty Properties list — a
// resource with no throughput of its own, a value the account declined to
// serve — so a section never disappears without saying why.
type Section struct {
    Title      string
    Properties []Property
    Note       string
}

type Property struct {
    Name  string
    Value string
}
```

`Node.Meta` stays what it is: the handful of strings the *tree* needs to draw itself.
`Details` is the document the info view renders, and the two do not overlap.

Pre-rendering into strings is the same call `Page` already makes for result rows, and it
is what keeps `internal/tui` free of Cosmos vocabulary — no `IndexingMode`, no
`PartitionKeyKind`, nothing to update in the TUI when the SDK grows a policy.

## What Cosmos can serve

Everything below is reachable from `azcosmos` v1.5.0 with the account key we already
connect with. Cost is per `Inspect` call.

| Section | Source | Cost |
|---|---|---|
| Identity — ID, resource ID, ETag, last modified | `ContainerProperties` / `DatabaseProperties` | ① |
| Partition key — kind (`Hash`/`MultiHash`), paths, version | `PartitionKeyDefinition` | ① |
| Time to live — default TTL, analytical store TTL | `DefaultTimeToLive`, `AnalyticalStoreTimeToLiveInSeconds` | ① |
| Indexing — mode, automatic, included/excluded paths, composite, spatial, vector, full-text indexes | `IndexingPolicy` | ① |
| Policies — unique keys, conflict resolution, vector embedding, full-text | `UniqueKeyPolicy`, `ConflictResolutionPolicy`, `VectorEmbeddingPolicy`, `FullTextPolicy` | ① |
| Storage — document count, documents size, collection size | `x-ms-resource-usage` / `x-ms-resource-quota` headers | ① |
| Throughput — manual RU/s or autoscale maximum, autoscale increment, minimum RU/s, scaling in progress | `ReadThroughput` → `ThroughputProperties`, plus `MinThroughput` and `IsReplacePending` on the response | ② |
| Physical partitions — count and key ranges | `ReadFeedRanges` → `[]FeedRange` | ③ |
| Raw | `ContainerProperties` marshals back to the service's own JSON | ① |

① is a single `ContainerClient.Read(ctx, &ReadContainerOptions{PopulateQuotaInfo: true})`
— the properties and the quota headers arrive together. So a container costs three
requests, a database two (no feed ranges). Sequential, not concurrent: three gateway
round trips behind a spinner is not a latency problem worth the machinery.

Two of these need saying out loud:

- **Storage numbers are header scraping.** `ReadContainerOptions.PopulateQuotaInfo` is a
  public option, but the SDK surfaces the result only as raw headers on
  `Response.RawResponse` — a `;`-delimited `key=value` string that must be parsed by
  hand. The parser gets its own unit test against a captured header, and a shape it
  cannot parse produces a section Note, never a wrong number.
- **A shared-throughput container has no offer of its own**, so `ReadThroughput` fails
  for it by design. That specific failure is the Note in the sketch above, not an error.

Out of reach and worth recording so nobody re-derives it: account-level region lists,
write regions, and default consistency (`accountProperties` is unexported in the SDK);
anything on the ARM control plane (backup policy, capabilities, firewall rules); Azure
Monitor server-side metrics (normalized RU consumption, 429 rate); and stored
procedures, triggers, UDFs, conflicts, users, and permissions — the Go SDK has no
public client for any of them.

## Scope

- `internal/adapter/adapter.go` — `Inspector`, `Details`, `Section`, `Property`.
- `internal/adapter/cosmos/inspect.go` — the implementation and the section order above;
  the quota-header parser lives here with its own test.
- `internal/adapter/mock/mock.go` — `Inspector` over the fixture, including one
  container that reports shared throughput and one that reports a parse-less quota
  header, so both Note paths have a test subject. Adds `OpInspect` to `WithError`.
- `internal/tui/panes/info.go` — `Info`: a `bubbles/viewport` inside the shared frame,
  a header built from the `Node`, section rendering with the theme's label/value styles,
  a spinner line while a request is in flight, and an error panel when one fails. It
  keeps the last `Details` it was given per node path, so reopening a node already read
  is instant and `r` is the only thing that spends a request on it again.
  `bubbles/viewport` is in the module already; no new dependency.
- `internal/tui/keys.go` — the `Info` binding, disabled when no `Inspector` is wired so
  it stays out of the generated help overlay.
- `internal/tui/app.go`, `messages.go`, `commands.go` — `showInfo` state, overlay key
  routing, `inspect` command with the standard `loadTimeout`, and
  `DetailsLoadedMsg{Path, Details}` / `ErrMsg{Op: OpInspect}`.
- `cmd/root.go` — assert the connection to `adapter.Inspector` and pass what it
  satisfies.

## Out of scope

- Editing anything shown. The info view is a read; iteration 11 owns writes.
- Copying values to the clipboard — no clipboard dependency in the module yet, and
  picking one is its own decision.
- A raw-JSON section in the rendered view. `Details.Raw` is populated this iteration
  because it costs nothing to carry, but rendering and paging it belongs with the
  results pane's document viewer (iteration 5), which will already have that widget.
- Per-query metadata — activity ID, query metrics, index metrics, SDK diagnostics.
  These are per-*execution*, not per-*resource*, and belong in the results pane. Worth
  a note for whoever gets there: the SDK sets
  `x-ms-documentdb-populatequerymetrics: true` on every query unconditionally
  (`cosmos_query_request_options.go`), so `QueryItemsResponse.QueryMetrics` is already
  paid for and `cursor.NextPage` currently drops it.

## Relationship to iteration 11

Both read throughput, and they want different things from it: the info view wants a
rendered line, the management form wants a typed `Throughput` it can edit and send
back. They coexist without either depending on the other — `Inspect` formats, and
`ThroughputEditor` round-trips. Whichever ships first, the other needs no rework.

## Steps

1. `Details` and `Inspector`; mock implementation; mock tests first.
2. Cosmos `Inspect` for a database (the smaller shape), then a container; quota parser
   with its own table test.
3. `Info` pane against hand-built `Details` values — scrolling, section rendering, notes,
   errors — before anything fetches.
4. Root model wiring, key routing, `cmd/` assertion, disabled-binding path.
5. Integration test against the emulator.

## Testing

**Unit:**
- Quota header parser: a well-formed header yields the three numbers; a truncated one,
  an unknown key, and an empty header each yield no numbers and no error.
- Mock `Inspect`: sections come back in a stable order; the shared-throughput fixture
  produces a Note and no Properties; an injected error surfaces as `ErrMsg`.
- `Info` pane: renders every section in the order given; a Note renders under its
  heading; scrolling clamps at both ends; a `Details` taller than the frame scrolls and
  a shorter one does not; the error panel replaces the body without dropping the header.
- Root model: `i` with no selection does nothing and issues no command; `i` opens and
  fires exactly one `inspect`; `r` inside the overlay re-inspects the same node and does
  not refresh the tree; `esc` and `i` close; `q` inside the overlay does not quit.
- With no `Inspector`, `i` does nothing and the binding is absent from the help overlay
  (the existing help drift test covers the second half).

**Integration** (emulator, `//go:build integration`): inspect a seeded container and
assert the document count matches the number of seeded items, the partition key paths
match the fixture, and the throughput section is non-empty. Assert nothing about exact
byte sizes — they are the service's business and will drift.

**Manual checklist:**
- [ ] `--adapter mock`: `i` on a database and on a container; scroll a long indexing
      policy; `esc` returns to the tree with the cursor where it was.
- [ ] Against the emulator: document count matches a `SELECT VALUE COUNT(1) FROM c`.
- [ ] A container in a shared-throughput database shows the inherited note, not an error.
- [ ] 80×24: the overlay fits, nothing wraps into the border, scrolling reaches the last
      line.
- [ ] Kill the emulator, then press `i` — the overlay shows the failure and the TUI
      stays alive.

## Acceptance criteria

- `internal/tui` names no Cosmos concept: the info pane renders `Details` and knows
  nothing about indexing modes or partition key kinds.
- A failed or unparsable piece of metadata degrades to a Note in its own section; the
  view never shows a blank where a number should be, and never invents one.
- `Update` never blocks; a slow or hung `Inspect` leaves scrolling, `esc`, and `ctrl+c`
  working.
- Opening the view costs at most three requests for a container and two for a database,
  and pressing `i` twice on the same node without `r` costs nothing the second time.

## As built

Decisions the sections above left open, recorded once the code settled them:

- **`Inspector` rides in `Management`.** The keymap is built before a connection
  exists, so the interface reaches the TUI the way `CatalogAdmin` and
  `ThroughputEditor` do: through the `Manager` that `cmd/` supplies and the session
  calls on every connection it gets. The struct's doc now says it covers reading one
  node as well as changing the catalog.
- **Section order is short-to-long**, not the order of the source table: Identity,
  Partition key, Throughput, Storage, Physical partitions, Time to live, Indexing,
  Policies. What a node is and costs fits on the first screen; the policies, which run
  to many lines, scroll below.
- **No `bubbles/viewport`.** The document overlay already scrolls a slice of lines by
  offset, so the info view does the same and the clamp is shared between the two. A
  viewport would have brought key handling and styles neither overlay uses.
- **`q` is inert inside the overlay; `ctrl+c` still quits.** The help overlay quits on
  `q`; this one does not, as the interaction table asks, using the same plain-letter
  guard the dialogs use.
- **The mock reports storage as strings**, so the fixture can say `4.2 MB` without a
  second copy of the size formatter. A created container reports `0 B`; a fixture with
  no size stands in for a header the Cosmos adapter could not parse.
- **The Linux emulator image serves a usage header of zeros** whatever a container
  holds, and gives a database created with no offer one while its containers get none.
  The document-count integration check skips on that image rather than passing on a
  stub, and the throughput assertions accept a note as well as figures.
