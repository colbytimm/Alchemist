# II. Statements

The editor accepts four kinds of statement. A `SELECT` is Cosmos DB SQL, which the
service evaluates, plus the cross-container forms Alchemist simulates. The other three
are Alchemist's own. The editor holds one statement at a time.

- [SELECT](#select) — read, from one container or several
- [BEGIN BATCH](#begin-batch) — write atomically within one partition key
- [UPDATE](#update) — patch every item a condition matches
- [DELETE](#delete) — delete every item a condition matches

In a synopsis, `[ ]` is optional, `{ a | b }` is a choice, and `…` repeats what comes
before it.

## SELECT

Query one container, or join and union several.

### Synopsis

```
[ WITH cte_name AS ( select ) [, …] ]
SELECT select_list
FROM source [ [AS] alias ]
    [ { [INNER] | LEFT [OUTER] | RIGHT [OUTER] | FULL [OUTER] } JOIN source [ [AS] alias ]
          ON alias.path = earlier_alias.path [ AND condition … ] ] …
    [ CROSS JOIN source [ [AS] alias ] ]
    [ { CROSS | OUTER } APPLY element_alias IN alias.path ] …
[ WHERE condition ]

where source is one of:

    database.container
    cte_name
    alias                      alone, as in FROM c: the container selected in the catalog

and a union is:

SELECT select_list FROM database.container, database.container [, …] [AS] alias [ WHERE condition ]
```

### Description

A query over one container with none of the forms above is sent to the service
unchanged: everything Cosmos DB SQL accepts works, and paging, `ORDER BY`, `GROUP BY`,
`OFFSET` and `TOP` are the service's own. A query that names several containers, or
uses `ON`, `CROSS JOIN` or `OUTER APPLY`, is simulated on the client; see
[Chapter 6](../language/cross-container.md) for what a simulated query accepts, what
it refuses, and its limits.

### Examples

```sql
SELECT o.id, cu.name FROM sales.orders o
LEFT JOIN sales.customers cu ON o.customerId = cu.id
WHERE NOT IS_DEFINED(cu)
```

## BEGIN BATCH

Commit up to 100 operations on one partition key of one container, all or none.

### Synopsis

```
BEGIN BATCH database.container PARTITION key_value [, …];
  operation;
  …
COMMIT

where operation is one of:

    CREATE body
    UPSERT body            [ IF MATCH "etag" ]
    REPLACE "id" body      [ IF MATCH "etag" ]
    DELETE "id"            [ IF MATCH "etag" ]
    READ "id"
    PATCH "id" [ patch_entry, … ] [ WHERE "condition" ] [ IF MATCH "etag" ]
```

### Description

`PARTITION` takes one value per key path, in order. A batch that writes is reviewed
and confirmed by typing the container's name; a batch that only reads runs at once.
The service takes at most 100 operations and 2 MB. See
[Chapter 7](../language/transactions.md).

### Examples

```sql
BEGIN BATCH sales.orders PARTITION "c01";
  PATCH "o007" [{"op": "set", "path": "/status", "value": "cancelled"}];
  DELETE "o003";
COMMIT
```

## UPDATE

Set or remove fields on every item a condition matches.

### Synopsis

```
UPDATE database.container [ [AS] alias ]
    { SET alias.path = literal [, …] [ UNSET alias.path [, …] ]
    | UNSET alias.path [, …] }
    WHERE condition
```

### Description

Alchemist reads the matching items, reviews them with you, then patches each one with
the `WHERE` as its condition. It is not atomic. `WHERE` is required; `WHERE true`
matches every item. At most 10 operations. See [Chapter 8](../language/update.md).

### Examples

```sql
UPDATE sales.orders o
SET o.status = "archived"
UNSET o.draft
WHERE o.status = "shipped"
```

## DELETE

Delete every item a condition matches.

### Synopsis

```
DELETE FROM database.container [ [AS] alias ] WHERE condition
```

### Description

Alchemist reads the matching items, reviews them with you, then deletes each one on
the condition that its version has not changed since. Confirm with the container's
name and the item count. `WHERE` is required; `WHERE true` matches every item. See
[Chapter 9](../language/delete.md).

### Examples

```sql
DELETE FROM sales.orders o WHERE o.status = "cancelled"
```

---

[← I. Key Bindings](keys.md) · [Contents](../README.md) · [III. Command-Line Programs →](cli.md)
