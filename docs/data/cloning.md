# 12. Cloning

- [12.1. Starting a clone](#121-starting-a-clone)
- [12.2. What it costs](#122-what-it-costs)
- [12.3. What is copied](#123-what-is-copied)
- [12.4. Progress, stop and resume](#124-progress-stop-and-resume)

`y` on a container or a database in the catalog copies it under a new name: the
definition alone, or the definition and every item. The copy can land in the account
the session is on or in any other the session knows, connected or not; the prompt
connects a target on its own and leaves the session where it was. `prod` → `emulator`
works with an untouched config, because a real account's profile is read-only until
told otherwise, a clone only reads its source, and the emulator's endpoint is local.

## 12.1. Starting a clone

The prompt offers every account that is not read-only, and says for each read-only one
why it is missing.

| Field | Choices | Default |
|---|---|---|
| **Copy** | `definition and items`, `definition only` | |
| **Definition** | `full`, or `portable`, which leaves out the analytical store TTL, the conflict resolution policy and the vector and full-text policies and indexes: whatever depends on a feature the target account may lack | `portable` across accounts |
| **Throughput** | `minimum` (400 RU/s manual), `same as source`, `none` | `minimum` |

The `minimum` default means a source at 40,000 RU/s autoscale is never copied at that
price by pressing `enter` twice. A container drawing on its database's throughput keeps
doing so wherever the target database can have some.

`enter` starts nothing: it opens a review that restates the job, what it creates and at
what throughput, and asks for the **target account's** name typed back. Nothing that
exists is ever written into: a target that exists is refused in the form, and again by
the service at create.

## 12.2. What it costs

Cosmos DB has no clone a data-plane key can call, so a clone is a client-side copy:
read the definition, create the target, read every item, write every item. That makes
three things true, and the prompt, the review and the progress view say them:

- It spends request units on both sides. Reading spends them on the source, writing on
  the target, and a new provisioned container bills from the moment it exists. The
  cost cannot be known up front; the progress view projects it from the first page on.
  No `COUNT` query is ever spent on an estimate: the item count and size come with the
  definition, labelled "about".
- It is not a snapshot. A source that changes while it is read is copied as it was
  seen, page by page.
- It can take a long time. The session stays usable meanwhile, switching accounts
  included.

## 12.3. What is copied

| Copied | Not copied |
|---|---|
| partition key paths, kind and version | stored procedures, triggers, user-defined functions |
| indexing policy, unique keys, default TTL | computed properties, change feed and client encryption policies |
| (`full` only) analytical TTL, conflict resolution, vector and full-text policies | `_rid`, `_self`, `_etag`, `_attachments`, `_ts` |
| every item's `id`, `ttl` and fields, byte for byte | change feed history, users, permissions |

Two consequences of dropping `_ts`: every copied item's TTL clock restarts at the
moment it is written, and "last modified" order is the copy's order, not the source's.
An item the target cannot take, one with nothing at a partition key path say, is
skipped, counted, and named in the log; more than 100 in one container stop the clone.

## 12.4. Progress, stop and resume

Writes go through up to four at a time, or `writers` on the target's profile (1 to 16).
When the target throttles, every writer waits as long as it asked, and one writer is
dropped for good: a 400 RU/s target settles at one writer within a few pages.

| Key | Where | Action |
|---|---|---|
| `esc` | clone progress | hide; close once the clone has ended |
| `y` | catalog, any row | show the running clone again |
| `x` | clone progress, running | stop after the writes in flight |
| `r` | clone progress, ended short | resume after the last page written in full |
| `d` | clone progress, ended short | delete the partial target |

While hidden, the status bar carries `clone prod/sales.orders → emulator 41% (y)` on
every account. A clone that stopped, failed or ran out of retries says exactly what it
left behind. Deleting the partial target goes through the same typed confirmation as
the catalog's `d`; nothing is ever deleted automatically.

While a clone runs, `x` in the switcher refuses its two accounts, `d` in the catalog
refuses its target, and a batch into its target waits. The first `q` during a clone
shows it, and the second stops it and quits.

---

[← 11. Managing the Catalog](catalog.md) · [Contents](../README.md) · [13. Snapshots →](snapshots.md)
