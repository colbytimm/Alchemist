# Managing the catalog

Cosmos DB has no `CREATE` or `DROP` statements. Use these keys in the catalog instead:

| Key | Action |
|---|---|
| `n` | create a database, optionally with shared throughput |
| `c` | create a container in the selected database, with a partition key of up to three paths (`/tenantId, /customerId`) |
| `d` | delete the selected database or container |
| `t` | view or change throughput, manual or autoscale |
| `i` | show the node's details |

Deleting asks you to type the name. Cosmos DB has no undo.

Throughput rules vary by account type, so Alchemist sends the request and shows the
service's error if it fails. A failed create keeps the dialog open so you can fix the
input. A container that shares its database's throughput is changed on the database.

Cosmos DB cannot rename databases or containers.

Read-only accounts, and adapters that cannot manage a catalog, do not offer `n`, `c`,
`d` or `t`.

## Node details

`i` opens the details of a database or container:

![Details of sales.orders](../images/node-info.png)

It shows the ID and last-modified time, partition key, throughput, document count and
size, physical partitions, time to live, indexing policy, and unique key, conflict,
vector and full-text policies. If the account does not provide a value, the section
says why.

`r` refreshes, and `esc` or `i` closes. Details already loaded open without a new
request.
