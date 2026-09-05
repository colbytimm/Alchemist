# Iteration 11 — Manage Databases and Containers

## Goal

Create and delete databases and containers, and change provisioned throughput, from
the catalog pane. Cosmos DB has no DDL — `CREATE TABLE` and `DROP` do not exist in its
SQL dialect — so the editor can never reach these operations however good it gets, and
every other terminal workflow ends in the portal or `az`. Management belongs to the
catalog, next to the tree it changes.

Every operation here is irreversible: Cosmos has no undo and no recycle bin. The second
goal is that nothing is destroyed by a single keystroke.

## Capabilities (v1)

| Operation | Target | Notes |
|---|---|---|
| Create database | account | optional shared throughput |
| Create container | database | partition key required, up to 3 paths (hierarchical) |
| Delete database | database | removes every container inside it |
| Delete container | container | |
| Read / replace throughput | database, container | manual or autoscale RU/s |

Renaming is absent because Cosmos cannot rename a database or a container — the ID is
the resource identity. The UI does not offer what the service cannot do.

## Interaction

Four bindings, all in the catalog pane, all added to `keys.go` so the help overlay picks
them up for free:

| Key | Action |
|---|---|
| `n` | new database |
| `c` | new container in the database under the cursor |
| `d` | delete the database or container under the cursor |
| `t` | edit throughput of the database or container under the cursor |

No context-derived meanings: `n` and `c` name what they create, so nothing depends on
where the cursor happens to sit except which database a new container lands in. The
cursor can never sit on a metadata field row — `selectableRows` excludes `NodeField` —
so every binding here has exactly two cases to handle.

```
┌─ New container ─────────────────────────────┐
│  Database       sales                       │
│  Name           shipments                   │
│  Partition key  /customerId                 │
│  Throughput     ▸ autoscale                 │
│  RU/s           4000                        │
│                                             │
│  ✗ Owner resource does not exist            │
│                                             │
│  enter create   tab next field   esc cancel │
└─────────────────────────────────────────────┘
```

Deletion asks for the name back, the way the portal does, for both kinds — one rule,
and both are equally unrecoverable:

```
┌─ Delete database ───────────────────────────┐
│  Deleting sales removes every container in  │
│  it and all of their documents. This cannot │
│  be undone.                                 │
│                                             │
│  Type the database name to confirm:         │
│  > sales                                    │
│                                             │
│  enter delete   esc cancel                  │
└─────────────────────────────────────────────┘
```

Both render centered over a cleared screen, like the help overlay: lipgloss v1 has no
layering API, and compositing a true floating dialog is not worth a dependency here.

## Adapter contract

Two optional interfaces in `internal/adapter`. `Connection` does not grow — a backend
that cannot manage anything, or a session that must not, simply does not implement
them, and the comma-ok assertion lives in `cmd/` with the rest of the wiring.

```go
// CatalogAdmin creates and deletes the databases and containers a Catalog
// reads. A Connection implements it when its backend allows it; callers
// detect support with a comma-ok type assertion.
type CatalogAdmin interface {
    CreateDatabase(ctx context.Context, spec DatabaseSpec) error
    DeleteDatabase(ctx context.Context, name string) error
    CreateContainer(ctx context.Context, spec ContainerSpec) error
    DeleteContainer(ctx context.Context, path []string) error
}

// ThroughputEditor reads and replaces provisioned capacity. It is separate
// from CatalogAdmin because a backend can manage a catalog without having a
// throughput concept at all.
type ThroughputEditor interface {
    Throughput(ctx context.Context, path []string) (Throughput, error)
    SetThroughput(ctx context.Context, path []string, t Throughput) error
}

type DatabaseSpec struct {
    Name       string
    Throughput Throughput // ThroughputNone leaves it without shared throughput
}

type ContainerSpec struct {
    Database      string
    Name          string
    PartitionKeys []string // "/customerId"; more than one is a hierarchical key
    Throughput    Throughput
}

// Throughput is provisioned capacity. RUs is the manual rate or the autoscale
// maximum, and is meaningless in the other two modes.
type Throughput struct {
    Mode ThroughputMode // manual | autoscale | shared | none
    RUs  int32
}
```

`ThroughputShared` is a container drawing on its database's throughput;
`ThroughputNone` is a serverless account, which provisions nothing. `SetThroughput`
rejects both with a message naming why, rather than sending a request the service will
refuse.

**No client-side limit checks.** Minimum RU/s, autoscale step size, and which modes an
account may use vary by account type (serverless, shared-throughput, free tier), so the
adapter passes the request through and surfaces the service's own error. Duplicating
those rules buys a slightly faster message and guarantees drift.

## Scope

- `internal/adapter/adapter.go` — the interfaces and value types above, plus
  `ErrUnsupported` for the mode rejections.
- `internal/adapter/cosmos/admin.go` — `azcosmos` implementation:
  `Client.CreateDatabase`, `DatabaseClient.Delete`/`CreateContainer`,
  `ContainerClient.Delete`, and `Read`/`ReplaceThroughput` on both clients, with
  `NewManualThroughputProperties`/`NewAutoscaleThroughputProperties`. A container in a
  shared-throughput database has no offer of its own, and the read for it fails; that
  specific failure maps to `ThroughputShared`, not to an error.
- `internal/adapter/mock/mock.go` — implements both interfaces against a **per-adapter
  copy of the fixture**. The fixture is a package-level `var` today and every connection
  serves the same one; a mutating mock has to own its state or tests stop being
  isolated. Move it into `Adapter` in `New`, and add `WithError` ops for each new call.
- `internal/tui/panes/form.go` — `Form`: a titled list of labeled fields (text on
  `bubbles/textinput`, plus a cycling choice field for throughput mode), an error line,
  and a key hint line. Values are read by field constant, so a later operation adds
  fields rather than a pane. Constructors — `NewDatabaseForm`, `NewContainerForm(db)`,
  `NewThroughputForm(path, current)` — set the title and fields; the root model
  assembles the spec.
- `internal/tui/panes/confirm.go` — `Confirm`: the consequence text and a name field
  that must match exactly before `enter` does anything.
- `internal/tui/panes/catalog.go` — two additions: `RefreshPath(path)` (what `Refresh`
  becomes, with the cursor-driven case delegating to it) and `Select(path)` so the
  cursor can land on a node that did not exist when the key was pressed.
- `internal/tui/app.go`, `keys.go`, `messages.go`, `commands.go` — bindings, the
  overlay branch in `handleKey`, the mutation commands, and the reload that follows one.
- `cmd/root.go` — assert the connection to both interfaces and pass what it satisfies;
  a nil `Admin` disables the four bindings, which also removes them from the help
  overlay (the `key.WithDisabled` pattern already used for `Run` and `History`).

## Superseding an in-flight read

The catalog pane drops a fetch for a key that is already loading, so two responses can
never race to be the one the tree keeps. A mutation breaks that rule's assumption: the
reload after a create *must* overtake a read that started before it, or the new node
does not appear.

So `Fetch` carries a token, the pane keeps the latest token per key, and
`CatalogLoadedMsg`/`ErrMsg` echo it back; `SetChildren` ignores a response whose token
is no longer current. Deduplicating reads stays — that is what keeps prefetch cheap —
but it is no longer the only thing standing between the tree and a stale answer.

## Message flow

`CatalogChangedMsg{Op, Target, Parent}` reports a completed mutation: reload `Parent`,
put the cursor on `Target` after a create, and let the status bar name what happened.
Failures reuse `ErrMsg` with the new op constants, routed to the open form instead of
the tree — the form stays up with the service's message under the fields, so a rejected
name is one edit away from a retry rather than a re-typed dialog.

Deleting the container the query scope points at clears the scope
(`ScopeChangedMsg{}`); leaving the status bar naming a container that no longer exists
would be a lie about what the next `ctrl+r` would run.

## Out of scope

- Editing an existing container's TTL, indexing policy, or unique keys — the natural
  follow-up, and the one that needs a JSON-editing surface rather than a field form.
- Item-level writes (insert/update/delete documents), stored procedures, triggers, UDFs.
- Restore, failover, region management, and anything else on the ARM control plane
  rather than the data-plane SDK.
- Showing live throughput as a metadata leaf under every container. It reads well but
  costs a control-plane request per expansion, on a path that exists to browse.

## Steps

1. Interfaces and value types; mock implementation with the fixture moved onto the
   adapter; mock tests first — they define the contract the Cosmos side has to meet.
2. Cosmos implementation plus integration tests against the emulator.
3. Catalog pane: request tokens, `RefreshPath`, `Select` — proven on their own before
   anything calls them.
4. `Form` and `Confirm` panes with their own tests, still wired to nothing.
5. Root model: bindings, overlay routing, commands, post-mutation reload; `cmd/` wiring
   and the disabled-bindings path.

## Testing

**Unit** (`internal/adapter/mock/test/`, `internal/tui/test/`, `internal/tui/panes/test/`):
- Mock: create/delete round-trips show up in the next `Root`/`Children` call; two
  adapters from `New` do not see each other's mutations; duplicate name and unknown
  database return errors; injected errors surface per operation.
- Throughput modes: manual and autoscale round-trip; `SetThroughput` on a shared or
  serverless target returns `ErrUnsupported` without a call.
- Form: `esc` cancels and issues no command; `q` and `d` typed into a name field neither
  quit nor open a dialog; submitting with an empty required field keeps the form open.
- Confirm: `enter` does nothing until the typed name matches exactly; a near-miss
  (trailing space, wrong case) does not delete.
- Root model: a successful create reloads exactly the parent subtree and moves the
  cursor to the new node; a delete leaves the cursor on the nearest surviving ancestor;
  a failure keeps the form open with the error text; deleting the scoped container
  clears the scope.
- Token supersession: a create's reload lands after a read that started before it, and
  the tree shows the post-mutation list — the regression test for the race above.
- With no `Admin`, the four bindings are absent from the help overlay and their keys do
  nothing (the existing help drift test covers the first half).

**Integration** (emulator, `//go:build integration`): create a database with autoscale
throughput → it appears in `Root` → create a container with a two-path hierarchical key
→ it appears in `Children` with both paths → read throughput → replace it → read back
the new value → delete the container → delete the database → neither appears. Skip
rather than fail if the emulator image rejects autoscale, and say so in the skip
message.

**Manual checklist:**
- [ ] `--adapter mock`: create a database and a container, watch them appear under the
      cursor; delete them; the tree settles with no ghost rows.
- [ ] Against the emulator: create a container with `/tenantId` and manual 400 RU/s, run
      a query against it, then delete it.
- [ ] Submit a container name containing `/` — the Cosmos error renders in the form and
      the form stays open.
- [ ] Resize to 80×24 with a form open: the dialog fits and nothing wraps into the
      border.
- [ ] Press `d` on a database, type a wrong name, press `enter` — nothing happens.

## Acceptance criteria

- No mutation reaches an adapter without a confirmation step the user completed by
  typing.
- Every management call goes through a `tea.Cmd` with a timeout; `Update` never blocks,
  and a slow delete leaves `ctrl+c` working.
- A connection implementing neither interface produces a TUI that offers no management
  at all — nothing in the help overlay, no dead keys, no error dialogs.
- `internal/tui` still imports no concrete adapter; the type assertions live in `cmd/`.
- Service errors are shown verbatim; nothing is reworded into a message that hides which
  constraint was violated.
