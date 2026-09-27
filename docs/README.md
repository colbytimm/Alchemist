# Alchemist documentation

Start with the [quick start](../README.md#quick-start). `alchemist --help` describes
every command, and `?` in the app lists the keys for the current pane.

The examples use the sample data from `make emulator-seed`. `sales.orders` means the
`orders` container in the `sales` database.

## Working in the editor

- [Writing queries](using/editor.md): error hints, autocomplete
- [Results and export](using/results.md)
- [History and saved queries](using/history.md)
- [Themes](using/themes.md)

## The query language

- [Queries across containers](language/cross-container.md): unions, joins, `APPLY`,
  CTEs
- [Transactional batches](language/transactions.md)
- [Updating by query](language/update.md)
- [Deleting by query](language/delete.md)

## Accounts and data

- [Profiles and accounts](data/profiles.md): keys, switching accounts, read-only
  accounts
- [Managing the catalog](data/catalog.md): create, delete, throughput, node info
- [Cloning](data/cloning.md)
- [Snapshots](data/snapshots.md)

## Reference

- [Keys](reference/keys.md)
- [Statements](reference/statements.md)
- [Commands](reference/cli.md)
- [Configuration](reference/configuration.md)
