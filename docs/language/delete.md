# 9. Deleting by Query

- [9.1. Grammar](#91-grammar)
- [9.2. The version guard](#92-the-version-guard)
- [9.3. Confirmation](#93-confirmation)
- [9.4. Running it](#94-running-it)
- [9.5. When this is the wrong tool](#95-when-this-is-the-wrong-tool)

Cosmos DB has no `DELETE … WHERE` either. Alchemist simulates it the way it
[simulates an update](update.md), and as openly: it selects the matching items with a
query, then deletes each one.

```sql
DELETE FROM sales.orders o WHERE o.status = "cancelled"
```

## 9.1. Grammar

- The grammar is `DELETE FROM <database>.<container> [[AS] alias] WHERE condition`.
  `WHERE` is required, as for an update; to delete every item, write `WHERE true`.
- Refused, with the reason: `DELETE` without `FROM`, a field before `FROM` (removing a
  field is `UPDATE … UNSET`), a second container (`DELETE o FROM`, `USING`, `JOIN`,
  `WITH`, or a `database.container` inside the `WHERE`), `TOP`, `ORDER BY`, `OFFSET`,
  `LIMIT`, `RETURNING`, and anything after the `;`. `TRUNCATE` is not intercepted.
- Choosing targets with another container ("orders whose customer is gone") is not
  built: run that as a query, then `DELETE … WHERE o.id IN (…)`.

## 9.2. The version guard

**The guard is the version, not the `WHERE`.** A delete takes no condition, so each one
is sent with `If-Match` and the ETag the dry run read. An item anyone has written
since, in any field, is `skipped: changed` and stays, even when it still matches: it is
no longer the document the review counted, and a skipped item costs a rerun while a
wrongly deleted one costs a restore. An item already gone is `skipped: gone`, counted
apart from `deleted`, and never makes a run a failure.

## 9.3. Confirmation

**The confirmation is the container name and the item count, always**, one space
apart, the count in plain digits: `orders 20`, which the review's prompt spells out.
The name proves you know where; the count is the one number that differs between the
delete you meant and the one a loose `WHERE` selected.

The review shows the first items that would go on one line each, a rough cost (7 RU
per 1 KB item), the warnings an [update](update.md#82-the-dry-run-and-review) shows,
whether a snapshot of the container exists, and that deleted items cannot be brought
back from Alchemist.

![The review of a delete: the first items that would go, the rough cost, the warnings, and the prompt 'Type orders 9 to delete'](../images/delete-review.png)

## 9.4. Running it

Everything else is the update's: `ctrl+r` is a dry run that deletes nothing, recalled
statements are reviewed again, read-only accounts refuse before anything is read, a
delete is sent once and only a throttle is retried, an unanswered delete is `unknown`,
and the [job](update.md#84-the-job), its progress view, `w`, `x`, `r`, the report and
the one job slot are the same, with `deleted` for `updated`. Running the statement
again is safe and is how a stopped or `unknown` run is settled: deleted items no
longer match.

**Deleted is deleted.** Alchemist keeps no copy. Before a large delete, take a
[snapshot](../data/snapshots.md), whose `.jsonl` export is the way back and whose diff
afterwards lists exactly the removed items, or a [clone](../data/cloning.md). An
account with continuous backup has a point-in-time restore, which is the one restore
point consistent across items.

## 9.5. When this is the wrong tool

- **Every item.** Deleting and recreating the container (`d`, then `c` in the
  [catalog](../data/catalog.md)) spends no request units per item. It is a new
  container, so its throughput, indexing policy and other settings must be given again.
- **Items that age out.** For "remove everything older than ninety days, from now on",
  set a time to live: a container `DefaultTimeToLive` or a per-item `ttl`. The service
  then deletes expired items in the background from leftover throughput, with no
  client and no per-item request. `UPDATE … SET o.ttl = 86400 WHERE …` is the one-off
  form for items already there. Do not schedule a nightly `DELETE` for this.
- **A known handful, all or nothing.** `BEGIN BATCH … DELETE "id" IF MATCH "…"` deletes
  them atomically within one partition key; see [Chapter 7](transactions.md).
- **One whole partition.** Cosmos DB's delete-by-partition-key operation is a preview
  the Go SDK cannot call, so it is not used, even for a `WHERE` that is exactly a key
  equality.

---

[← 8. Updating by Query](update.md) · [Contents](../README.md) · [10. Profiles and Accounts →](../data/profiles.md)
