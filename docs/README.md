# Alchemist Documentation

Alchemist is a keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API). This
manual covers everything it does. The [project README](../README.md) is the short
version; each chapter here is the long one.

## Table of Contents

[Preface](#preface)

**Part I. Tutorial**

1. [Getting Started](tutorial/getting-started.md)
   - 1.1. Building from source · 1.2. A first look, with fixture data ·
     1.3. Connecting to an account · 1.4. The local emulator · 1.5. Sample data
2. [A Tour of the Workspace](tutorial/workspace.md)
   - 2.1. The three panes · 2.2. Choosing what a query reads · 2.3. Running a query ·
     2.4. Reading the results · 2.5. Where to go next

**Part II. Working in the Editor**

3. [Writing Queries](using/editor.md)
   - 3.1. Highlighting · 3.2. Diagnostics · 3.3. Squiggles and terminals ·
     3.4. Autocomplete · 3.5. Where field names come from
4. [Results and Export](using/results.md)
   - 4.1. The results pane · 4.2. Exporting a result set · 4.3. Where the file lands
5. [History and Saved Queries](using/history.md)
   - 5.1. Query history · 5.2. Saved queries · 5.3. Saved queries on disk

**Part III. The Query Language**

6. [Querying Across Containers](language/cross-container.md)
   - 6.1. How a simulated query runs · 6.2. Unions · 6.3. Joins · 6.4. Join types ·
     6.5. APPLY over an array · 6.6. Common table expressions · 6.7. Limits ·
     6.8. What is refused · 6.9. Sample data
7. [Transactional Batches](language/transactions.md)
   - 7.1. Writing a batch · 7.2. Review and commit · 7.3. Outcomes
8. [Updating by Query](language/update.md)
   - 8.1. Grammar · 8.2. The dry run and review · 8.3. Semantics · 8.4. The job
9. [Deleting by Query](language/delete.md)
   - 9.1. Grammar · 9.2. The version guard · 9.3. Confirmation · 9.4. Running it ·
     9.5. When this is the wrong tool

**Part IV. Accounts and Data**

10. [Profiles and Accounts](data/profiles.md)
    - 10.1. Profiles · 10.2. Switching accounts · 10.3. Where keys come from ·
      10.4. Read-only accounts
11. [Managing the Catalog](data/catalog.md)
    - 11.1. Creating and deleting · 11.2. Throughput · 11.3. Inspecting a node
12. [Cloning](data/cloning.md)
    - 12.1. Starting a clone · 12.2. What it costs · 12.3. What is copied ·
      12.4. Progress, stop and resume
13. [Snapshots](data/snapshots.md)
    - 13.1. Taking and comparing · 13.2. From a shell · 13.3. What a snapshot is ·
      13.4. What it costs · 13.5. Where it lives · 13.6. Background captures

**Part V. Reference**

- I. [Key Bindings](reference/keys.md)
- II. [Statements](reference/statements.md) — `SELECT` extensions, `BEGIN BATCH`,
  `UPDATE`, `DELETE`
- III. [Command-Line Programs](reference/cli.md) — `alchemist`, `alchemist profile`,
  `alchemist snapshot`
- IV. [Configuration](reference/configuration.md) — `config.toml`, environment
  variables, files and directories

**Part VI. Development**

14. [Writing an Adapter](development/adapters.md)
15. [Building and Testing](development/building.md)
    - 15.1. Make targets · 15.2. The typing benchmark gate · 15.3. CI and releases

## Preface

### What is Alchemist?

Alchemist is a terminal IDE for Azure Cosmos DB, modeled on
[harlequin](https://github.com/tconbeer/harlequin): a catalog of databases and
containers, a SQL editor, and a results viewer on one screen. It is Cosmos-aware
where that matters: it shows the request charge (RU) of every query, runs
cross-partition queries by default, and pages with continuation tokens rather than
loading a whole result set.

It also goes where the service cannot. Cosmos DB SQL reads one container and never
writes, so Alchemist simulates cross-container queries, updates, and deletes on the
client, and says so wherever it does.

### Conventions

- `ctrl+r`, `tab`, `enter` are keys. Where a key depends on the pane, the
  [key reference](reference/keys.md) names the pane.
- `sales.orders` is a container named `orders` in a database named `sales`. The
  examples use the databases `make emulator-seed` loads (see
  [1.5](tutorial/getting-started.md#15-sample-data)).
- `prod` and `emulator` are profile names, which are yours to choose.
- **Refused** means Alchemist rejects a statement before anything reaches the account,
  and says what to write instead where there is something.

### Further information

- The [implementation plan](plan/00-overview.md) records the design of every
  feature, iteration by iteration, with the reasoning behind each decision.
- `alchemist --help` and `alchemist <command> --help` describe every command and flag.
- `?` inside the app lists the key bindings of the pane you are in.
