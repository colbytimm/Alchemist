# 8. Updating by Query

- [8.1. Grammar](#81-grammar)
- [8.2. The dry run and review](#82-the-dry-run-and-review)
- [8.3. Semantics](#83-semantics)
- [8.4. The job](#84-the-job)

Cosmos DB has no `UPDATE … WHERE`: its SQL only reads. Alchemist does what a careful
script would, and says so: it selects the matching items with a query the service
evaluates, then writes to each one with a patch.

```sql
UPDATE sales.orders o
SET o.status = "archived", o.archivedAt = "2026-01-01"
WHERE o.status = "shipped" AND o.total < 50
```

## 8.1. Grammar

- The target is always `<database>.<container>`, and the alias (`c` when none is
  written) starts every path: `o.status`, `o.shipTo.region`, `o["order-id"]`,
  `o.lines[0].qty`.
- `SET path = value` sets the field, creating it where it is missing. A value is a
  literal: a string in either quote, a number, `true`, `false`, `null`, or JSON.
  `UNSET path` removes a field; an item that lacks it is left out of that operation.
  A patch creates only the last step of a path: `SET o.shipTo.region` on an item with
  no `shipTo` would be refused, so that item is `skipped: no parent` and never sent.
- `WHERE` is required. `ctrl+r` pressed one clause early must not select a whole
  container, so the every-item form is written out: `WHERE true`. The condition is the
  service's own dialect and is never parsed; it is sent as written, twice.
- Refused, with the reason: an expression or another field on the right of `=` (a
  patch cannot read the item), `+=` and increment (not safe to run twice), `SET o =`,
  array appends and moves, `id`, a partition key path or a system field, two paths
  that overlap, more than 10 operations, a second container anywhere (`FROM`, `JOIN`,
  `WITH`, or a `database.container` inside the `WHERE`), `TOP`, `ORDER BY`, `OFFSET`,
  `LIMIT`, `RETURNING`, and anything after the statement's `;`. Every problem is listed
  at once, and nothing is read.

The synopsis is in the [statement reference](../reference/statements.md#update).

## 8.2. The dry run and review

`ctrl+r` never writes. It is a dry run: it checks the statement, reads which items
match (their identities only, or whole items when an `UNSET` must see which have the
path), and opens a review. The review names the account, the container and its key,
the condition, the exact number of items and when they were read, what the selection
cost, a before → after of a few of them, a rough write cost, and every warning:

- a `WHERE` that does not pin the partition key;
- `WHERE true`;
- items with no partition key value;
- items that already lack an `UNSET` path;
- whether a snapshot of the container exists.

It starts only once the container's name is typed back exactly, and for `WHERE true`
the name and the item count (`orders 60`); `esc` writes nothing and records nothing. A
statement recalled from history or a saved query is reviewed again, every time. More
matches than `max_mutation_items` (10,000 unless the
[profile](../reference/configuration.md#profile-settings) says otherwise) is a refusal
with nothing written, never a truncated run. A
[read-only account](../data/profiles.md#104-read-only-accounts) refuses before it reads
anything.

## 8.3. Semantics

What it means, in plain words:

- **Not atomic.** Each item is its own write. A run that stops, fails or is quit has
  updated some items and not others. There is no rollback and no undo; the report
  lists exactly which.
- **Not isolated.** The targets are the items that matched when the dry run read them.
  Every write carries the `WHERE` as its condition, so an item changed since so that it
  no longer matches is `skipped: changed` and left alone; one deleted since is
  `skipped: gone`. A change to another field survives: a patch touches only its paths.
- **A write is sent once.** A throttled write was not applied and is retried after the
  wait the service asked for, with one writer fewer. A write with no answer is
  `unknown`, never retried, and three in a row stop the job. So do more than 100
  failed items, and a refusal of the very first write, which usually means the service
  does not take the `WHERE` as a patch condition.
- **Running it again is safe.** `SET` to a literal and `UNSET` give the same document
  however often they run. Items already updated often stop matching and are not even
  selected; the rest are written to the same values. That is also how an `unknown`
  item is settled, and how a run stopped in an earlier session is finished.
- Before a large run, take a copy: a [snapshot](../data/snapshots.md) of the container,
  and its diff afterwards, or a [clone](../data/cloning.md).

## 8.4. The job

The job runs in the background, up to four writes at a time or `writers` on the
profile, and holds the session's one job slot, which a clone and a snapshot use too.

| Key | Where | Action |
|---|---|---|
| `esc` | progress, running | hide the view |
| `w` | catalog | show it again |
| `x` | progress, running | stop after the writes in flight |
| `r` | progress, ended short | resume with the items not yet attempted |
| `esc`, `enter` | progress, ended | load the report into the results |

While hidden, the status bar carries `update prod/sales.orders 41% (w)` on every
account. While it runs, `x` in the switcher refuses its account, `d` refuses its
container, a batch into its container waits, and so do a clone, a snapshot and another
update. The first `q` shows it; the second stops it and quits.

The report is one row per item with its outcome (`updated`, `skipped: changed`,
`skipped: gone`, `skipped: no partition key`, `skipped: no parent`, `failed`,
`unknown`, `not attempted`), the service's status and its charge, and a status bar
that splits the charge between the selection and the writes. Past 10,000 items it
shows every item that was not updated first, and the log names the rest; the totals
always cover every item.

---

[← 7. Transactional Batches](transactions.md) · [Contents](../README.md) · [9. Deleting by Query →](delete.md)
