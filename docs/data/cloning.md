# Cloning

`y` on a container or database copies it under a new name, with or without its items.
The copy can go to the current account or any other profile. For example, you can
clone from `prod` into the local emulator.

Cosmos DB has no clone operation available to account keys, so Alchemist reads every
item and writes it to the target. As a result:

- It costs RU on both accounts, and a new provisioned container is billed from the
  moment it exists. The progress view estimates the total cost after the first page.
- It is not a point-in-time copy. Items that change during the clone are copied as
  they were when read.
- It can take a long time. The session stays usable while it runs.

## Options

| Option | Choices | Default |
|---|---|---|
| Copy | definition and items, definition only | |
| Definition | `full`, or `portable`, which leaves out settings the target account may not support: analytical store TTL, conflict resolution, vector and full-text policies | `portable` across accounts |
| Throughput | `minimum` (400 RU/s manual), `same as source`, `none` | `minimum` |

Read-only accounts are not offered as targets. `enter` opens a review. To start, type
the target account's name. Alchemist never writes into an existing database or
container.

## What is copied

| Copied | Not copied |
|---|---|
| partition key | stored procedures, triggers, user-defined functions |
| indexing policy, unique keys, default TTL | computed properties, change feed and client encryption policies |
| (`full` only) analytical TTL, conflict resolution, vector and full-text policies | `_rid`, `_self`, `_etag`, `_attachments`, `_ts` |
| every item's `id`, `ttl` and fields | change feed history, users, permissions |

Because `_ts` is not copied, TTL clocks restart when each item is written. Items the
target cannot accept, such as items missing a partition key value, are skipped and
logged. More than 100 skipped items in one container stop the clone.

## Progress

Up to four writes run at a time (`writers` on the target profile, 1 to 16). When the
target throttles, all writers wait and one is removed.

| Key | Where | Action |
|---|---|---|
| `esc` | clone progress | hide; close once the clone has ended |
| `y` | catalog | show the running clone |
| `x` | clone progress, running | stop |
| `r` | clone progress, stopped | resume after the last complete page |
| `d` | clone progress, stopped | delete the partial copy |

While hidden, the status bar shows `clone prod/sales.orders → emulator 41% (y)`.
Nothing is deleted automatically.

While a clone runs, you cannot disconnect either account or delete the target, and
batches into the target wait. The first `q` shows the clone; a second `q` stops it
and quits.
