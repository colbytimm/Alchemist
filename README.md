# Alchemist

A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API). Browse databases,
write SQL, and page through results in one screen.

![Alchemist showing the catalog, a query and its results](docs/images/workspace.png)

<!-- TODO: record docs/demo.tape with `vhs docs/demo.tape` and embed docs/demo.gif here -->

Alchemist is in early development.

## Features

- Request charge (RU) for every query, cross-partition queries by default, and paging
  with continuation tokens.
- An editor with error hints, and autocomplete for databases, containers, functions
  and fields.
- Joins and unions across containers, run on the client.
- Transactional batches, and `UPDATE` and `DELETE` by query, each reviewed before
  anything is written.
- Create, delete and resize databases and containers, and clone them between accounts.
- Snapshots of a container on disk, with diffs between them.
- Query history, saved queries, and export to JSON or CSV.
- Several accounts in one session. Accounts are read-only unless you allow writes.
- Installs with Homebrew on macOS and Linux, and winget on Windows.

## Quick start

### Install

On macOS and Linux, install with [Homebrew](https://brew.sh):

```sh
brew install colbytimm/tap/alchemist
```

Download the file for your platform from the
[releases page](https://github.com/colbytimm/Alchemist/releases):

| Platform | File | Install |
|---|---|---|
| macOS | `alchemist_<version>_darwin_universal.tar.gz` | `tar -xzf` it and move `alchemist` to a directory on your `PATH` |
| Linux | the `.deb`, `.rpm` or `.apk` for your architecture | `sudo apt install ./<file>.deb`, `sudo dnf install ./<file>.rpm` or `sudo apk add --allow-untrusted ./<file>.apk` |
| Windows | none, winget downloads it | `winget install --id ColbyTimm.Alchemist -e`, then open a new terminal |
| Windows, without winget | `alchemist_<version>_windows_amd64.msi`, or the `.zip` | run the MSI, then open a new terminal |

On Windows, install with winget or the MSI, not both.

[Installing](docs/install.md) covers every file, the keychain on Linux, verifying a
download, uninstalling and building from source.

Try it without an account. The mock adapter serves fixture data:

```sh
alchemist --adapter mock
```

### Connect to an account

Run `alchemist`. The first run asks for a profile name, the account endpoint and
the key:

![The connect form](docs/images/connect.png)

The profile is saved to `~/.config/alchemist/config.toml`. The key goes to the OS
keychain if you tick the box, and is never written to the file. Next time,
`alchemist` connects to the default profile and `alchemist <profile>` to any other.

### Use the local emulator

The Cosmos DB emulator runs in a container, so it needs Docker or Podman. These
commands start it on port 8081, load it with sample `sales`, `telemetry` and `hr`
databases, and open the app on it:

```sh
alchemist emulator start
alchemist emulator seed
alchemist emulator
```

From a clone, run `./bin/alchemist` in place of `alchemist`.

`start` adds the `emulator` profile, which uses the emulator's well-known key, so there
is no key to paste. The emulator is local, so its profile allows writes. See
[the local emulator](docs/data/emulator.md) for ports, data and troubleshooting.

### Run a query

Expand `sales` in the catalog, select `orders`, press `e` and type a query. `ctrl+r`
runs it:

![A query of sales.orders and its results](docs/images/first-query.png)

The status bar shows the row count and the request charge. `m` fetches the next page
when there is one, and `enter` on a row opens the document. `tab` moves between
panes, `?` lists every key, and `q` quits.

A query can also name its container, whatever is selected:

```sql
SELECT o.id, o.total FROM sales.orders o WHERE o.status = "open"
```

## Documentation

| Topic | Pages |
|---|---|
| Installing | [Install, verify and uninstall](docs/install.md) |
| Editor | [Writing queries](docs/using/editor.md), [Results and export](docs/using/results.md), [History and saved queries](docs/using/history.md), [Themes](docs/using/themes.md) |
| Query language | [Queries across containers](docs/language/cross-container.md), [Transactional batches](docs/language/transactions.md), [Updating by query](docs/language/update.md), [Deleting by query](docs/language/delete.md) |
| Accounts and data | [Profiles and accounts](docs/data/profiles.md), [The local emulator](docs/data/emulator.md), [Managing the catalog](docs/data/catalog.md), [Cloning](docs/data/cloning.md), [Snapshots](docs/data/snapshots.md) |
| Reference | [Keys](docs/reference/keys.md), [Statements](docs/reference/statements.md), [Commands](docs/reference/cli.md), [Configuration](docs/reference/configuration.md) |

The examples use the sample data from `alchemist emulator seed`. `sales.orders` is the
`orders` container in the `sales` database.

## Credits

Alchemist is inspired by [harlequin](https://github.com/tconbeer/harlequin), the SQL
IDE for the terminal.

## License

[MIT](LICENSE)
