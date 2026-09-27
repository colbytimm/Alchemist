# Transactional batches

A batch applies up to 100 operations to one partition key of one container. Either
all of them commit or none do. Write it in the editor and run it with `ctrl+r`:

```sql
BEGIN BATCH sales.orders PARTITION "c01";
  CREATE  {"id": "o900", "customerId": "c01", "status": "open", "total": 45};
  REPLACE "o004" {"id": "o004", "customerId": "c01", "status": "shipped"}
          IF MATCH "\"0800-7f3a\"";
  PATCH   "o007" [{"op": "set", "path": "/status", "value": "cancelled"}];
  DELETE  "o003";
  READ    "o011";
COMMIT
```

## Syntax

- `BEGIN BATCH` names the container as `database.container`. The catalog selection is
  not used.
- `PARTITION` takes one value per key path, in order:
  `PARTITION "tenant-a", "eu", 42`.
- Operations: `CREATE body`, `UPSERT body`, `REPLACE "id" body`, `DELETE "id"`,
  `READ "id"`, and `PATCH "id" [entries] [WHERE "condition"]`.
- `UPSERT`, `REPLACE`, `DELETE` and `PATCH` accept `IF MATCH "etag"`.
- A patch uses the service's format, an array of `{"op", "path", "value"}`. The Go SDK
  cannot send `move` or a fractional `incr`.
- A batch without `COMMIT` does not run. The editor holds one batch at a time.

The service limits a batch to 100 operations and 2 MB. Alchemist checks the limits
before sending, along with bodies without an `id`, bodies in another partition, and
repeated creates of one `id`. It lists every problem at once.

## Review

A batch that writes opens a review showing the account, container, key, each
operation, and warnings for writes without `IF MATCH`:

![The batch review](../images/batch-review.png)

Type the container name and press `enter` to commit. `esc` cancels. A batch that only
reads runs without a review.

In the results pane, `ctrl+b` on a row from a `SELECT *` query adds a `REPLACE` of
that document to the batch in the editor, conditional on its current ETag.

## Outcomes

The result is one row per operation:

![A committed batch](../images/batch-committed.png)

The status bar shows one of four outcomes:

| Outcome | Meaning |
|---|---|
| committed | every operation applied |
| rolled back | an operation failed and nothing was written. The failed operation is marked, the others show `424 Failed Dependency` |
| not applied | the request was not sent, or the service rejected it (throttling included). `ctrl+r` retries |
| outcome unknown | no response came back. The batch either committed in full or not at all. Alchemist shows a query to check before you run it again |

Alchemist never retries a batch on its own. It waits up to 30 seconds for a response.
There is no undo.
