# 6. Querying Across Containers

- [6.1. How a simulated query runs](#61-how-a-simulated-query-runs)
- [6.2. Unions](#62-unions)
- [6.3. Joins](#63-joins)
- [6.4. Join types](#64-join-types)
- [6.5. APPLY over an array](#65-apply-over-an-array)
- [6.6. Common table expressions](#66-common-table-expressions)
- [6.7. Limits](#67-limits)
- [6.8. What is refused](#68-what-is-refused)
- [6.9. Sample data](#69-sample-data)

## 6.1. How a simulated query runs

Cosmos DB SQL reads exactly one container per query. Alchemist accepts shapes that
name more, and shapes the service has no syntax for, runs them as one query per
container, and merges the pages itself. The status bar marks such a result
`simulated (client-side)`, and its RU figure is the sum of the underlying queries,
broken down per container. When the status bar is too narrow for the breakdown it
folds it to a count, `(3 containers)`, and the full breakdown goes to the log.

Cosmos DB's own `JOIN alias IN c.array` is untouched: it has no `ON` and runs on the
service as it always did.

## 6.2. Unions

List containers after `FROM`. The same query runs against each, and a leading
`_container` column says where every row came from. One alias covers the whole list:

```sql
SELECT * FROM sales.orders, sales.archive AS c WHERE c.status = "open"
```

## 6.3. Joins

A join of any number of containers, each `ON` one equality between a field of the
container `JOIN` just introduced and a field of any earlier one:

```sql
SELECT o.id, cu.name, p.name AS product
FROM sales.orders o
JOIN sales.customers cu ON o.customerId = cu.id
JOIN sales.products p ON o.sku = p.id
WHERE cu.region = "west"
```

Each `ON` may reach back to the first container, as here, or to the one before it
(`… JOIN telemetry.devices d ON e.deviceId = d.id`), or any mix of the two. The same
container may appear twice under distinct aliases. The equality may be followed by
`AND` conditions that read only the container `JOIN` introduces
(`ON o.customerId = cu.id AND cu.vip = true`); they filter that container before the
join.

Columns come back prefixed with their alias (`o.total`, `cu.name`) unless the select
list renames them (`cu.name AS customer`). The select list is `*` or top-level
`alias.field` items; the `ON` fields may be nested (`o.customer.id`). A `WHERE`
condition is sent to the container it reads, so it must read one side only. A side
with no alias is known by its container name. An expression in the select list is
refused; put it in a [CTE](#66-common-table-expressions), where the service evaluates
it.

## 6.4. Join types

`INNER JOIN` (or plain `JOIN`), `LEFT`, `RIGHT` and `FULL [OUTER] JOIN`, and
`CROSS JOIN`:

```sql
SELECT o.id, o.total, cu.name
FROM sales.orders o
LEFT JOIN sales.customers cu ON o.customerId = cu.id
WHERE o.status = "open"
```

Every open order, with its customer's name where the customer exists. A row with no
match on the other side is **padded**: its cells for that side are empty, and its JSON
leaves that side out altogether (`{"o":{…}}`), so an export tells a missing customer
from a stored `null`. An order with no `customerId` matches nothing and is padded
too.

### Filtering an outer join

A `WHERE` condition may read only a side no join pads. One on the padded side of an
outer join is refused, because SQL applies it after the join and would drop the
padded rows: write it in `ON` to filter that side first, or use `INNER JOIN`. The one
exception is the absent-side test, which keeps only the padded rows — orders whose
customer does not exist:

```sql
SELECT o.id FROM sales.orders o
LEFT JOIN sales.customers cu ON o.customerId = cu.id
WHERE NOT IS_DEFINED(cu)
```

### Order of evaluation

A `FULL JOIN` serves the rows nothing matched on the held side after the rest, on a
page of their own that `m` fetches without reading anything new. A chain with any
outer join runs in the order it is written, as SQL defines it, and streams its first
container: write the largest container first.

### Cross joins

`CROSS JOIN` pairs every row of one container with every row of another. It is the
only join of its query (put one side in a CTE to go further), takes no `ON`, and its
`WHERE` conditions each read one side. Both sides are read whole before the first
page, and the product counts against `max_join_rows` like a held side: past it, the
run fails naming both sides and their sizes before anything is shown. A list with two
aliases (`FROM sales.orders o, sales.customers cu`) is refused with the hint to write
`CROSS JOIN`.

## 6.5. APPLY over an array

`CROSS APPLY alias IN path` ranges over an array of the item before it, like Cosmos
DB's own `JOIN alias IN path`; `OUTER APPLY` also keeps an item whose array is
missing, empty or not an array, with the alias absent:

```sql
SELECT o.id, l.sku, l.quantity
FROM sales.orders o
OUTER APPLY l IN o.lines
```

A query over one container whose only new syntax is `CROSS APPLY` is sent to the
service as `JOIN … IN`, and is not simulated. Anything else with an `APPLY` is
expanded client-side from the items the container returns, at no extra charge. An
element that is an object has its fields as columns (`l.sku`); a scalar one is one
column named by its alias, which the select list may name bare (`SELECT o.id, t FROM
sales.orders o OUTER APPLY t IN o.tags`).

An `APPLY` expands its item before any join key is read, so a join may read an
element — orders to their lines to products:

```sql
SELECT o.id, l.quantity, p.name
FROM sales.orders o CROSS APPLY l IN o.lines
JOIN sales.products p ON l.sku = p.id
```

A `WHERE` condition on an `APPLY` alias is refused (filter elements in a CTE with
`JOIN … IN … WHERE`), but for `NOT IS_DEFINED(l)` after an `OUTER APPLY`: orders with
no lines. An `APPLY` of a subquery is refused, since it would run one query per row;
join a CTE instead. A per-key top N (the three largest orders of each customer) has no
form yet.

## 6.6. Common table expressions

`WITH name AS (query)` names a query that the main query then reads like a container.
A CTE over one container is sent to the service whole, so the service projects,
filters and computes what the join reads — the largest saving on wide documents, where
a join otherwise fetches every field of every item:

```sql
WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu WHERE cu.region = "west"),
     big  AS (SELECT o.id, o.customerId, ROUND(o.total) AS total
              FROM sales.orders o WHERE o.total > 100)
SELECT big.id, big.total, west.name
FROM big JOIN west ON big.customerId = west.id
```

Inside such a CTE everything the service accepts works: functions, `TOP`, `ORDER BY`,
`GROUP BY`, aggregates, `JOIN t IN`. Its items are its columns, named as the service
names them (`west.name`); a column its select list does not name is refused by name.
The RU breakdown files each CTE under its name: `west (sales.customers) 3.10`.

- A CTE read by `SELECT * FROM name` and nothing else runs as its body alone: no
  badge, the service's own columns and paging.
- A CTE read twice runs once, into memory, and both readers share it; its rows count
  once against `max_join_rows`.
- A CTE may read the CTEs declared before it, and may itself be a union, a join of
  any type, or an `APPLY`, as long as a joined CTE has a select list with distinct
  names. A bare name inside a CTE's own body (`FROM c`) is still the container in
  scope. `WITH RECURSIVE` is refused.
- A `WHERE` condition of the main query on a CTE is refused: filter inside the CTE.
  A CTE that returns `SELECT VALUE` cannot be joined.

## 6.7. Limits

The held sides of a join are read one after another and may hold `max_join_rows` rows
between them (10 000 unless the
[profile](../reference/configuration.md#profile-settings) says otherwise), and that one
budget also covers a materialized CTE and both sides and the product of a
`CROSS JOIN`. Past it the run stops with an error naming what crossed the line rather
than a partial answer; filter that side, or raise the cap. A merged page holds at most
1 000 rows; a join that fans out further serves the rest on the next `m` without
reading anything new.

In a join with no outer step, one side streams and every other side is held in
memory. The streamed side is the first container in written order that no `WHERE`
condition filters, or the `FROM` container when every side is filtered: the side
expected to be largest.

## 6.8. What is refused

Anything else across containers is refused before it runs, never approximated, with
what to write instead where there is something:

- `ON` with anything but one `=` between the new container and an earlier one before
  its `AND` conditions, and an `OR` in `ON`;
- a `WHERE` condition over more than one side;
- `JOIN t IN` beside a container join (use `CROSS APPLY`);
- a container list mixed with a join;
- `NATURAL JOIN` and `USING`;
- `ORDER BY`, `GROUP BY`, `OFFSET` and `TOP` on a simulated query (put them in a CTE);
- subqueries over another container or a CTE.

## 6.9. Sample data

`make emulator-seed` loads data made for these examples; see
[1.5](../tutorial/getting-started.md#15-sample-data). Every example above has rows to
pad.

---

[← 5. History and Saved Queries](../using/history.md) · [Contents](../README.md) · [7. Transactional Batches →](transactions.md)
