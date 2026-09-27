# Statements

In a synopsis, `[ ]` is optional, `{ a | b }` is a choice, and `…` repeats.

## SELECT

```
[ WITH cte_name AS ( select ) [, …] ]
SELECT select_list
FROM source [ [AS] alias ]
    [ { [INNER] | LEFT [OUTER] | RIGHT [OUTER] | FULL [OUTER] } JOIN source [ [AS] alias ]
          ON alias.path = earlier_alias.path [ AND condition … ] ] …
    [ CROSS JOIN source [ [AS] alias ] ]
    [ { CROSS | OUTER } APPLY element_alias IN alias.path ] …
[ WHERE condition ]

source:
    database.container
    cte_name
    alias                  the container selected in the catalog, as in FROM c

union:
    SELECT select_list FROM database.container, database.container [, …] [AS] alias [ WHERE condition ]
```

A query over one container is sent to the service unchanged. Queries over several
containers run on the client. See [queries across containers](../language/cross-container.md).

## BEGIN BATCH

```
BEGIN BATCH database.container PARTITION key_value [, …];
  operation;
  …
COMMIT

operation:
    CREATE body
    UPSERT body            [ IF MATCH "etag" ]
    REPLACE "id" body      [ IF MATCH "etag" ]
    DELETE "id"            [ IF MATCH "etag" ]
    READ "id"
    PATCH "id" [ patch_entry, … ] [ WHERE "condition" ] [ IF MATCH "etag" ]
```

Up to 100 operations and 2 MB, all committed or none. See
[transactional batches](../language/transactions.md).

## UPDATE

```
UPDATE database.container [ [AS] alias ]
    { SET alias.path = literal [, …] [ UNSET alias.path [, …] ]
    | UNSET alias.path [, …] }
    WHERE condition
```

Up to 10 operations. Each matching item is patched separately. See
[updating by query](../language/update.md).

```sql
UPDATE sales.orders o
SET o.status = "archived"
UNSET o.draft
WHERE o.status = "shipped"
```

## DELETE

```
DELETE FROM database.container [ [AS] alias ] WHERE condition
```

Each matching item is deleted separately, unless it changed since the review. See
[deleting by query](../language/delete.md).
