# IV. Configuration

- [config.toml](#configtoml)
- [Profile settings](#profile-settings)
- [Environment variables](#environment-variables)
- [Files and directories](#files-and-directories)

## config.toml

The configuration file is `config.toml` in the config directory,
`$XDG_CONFIG_HOME/alchemist` (`~/.config/alchemist` by default). The connect form and
[`alchemist profile`](cli.md#alchemist-profile) write it; it can also be written by
hand. It never holds a key.

| Setting | Meaning |
|---|---|
| `default_profile` | the profile `alchemist` connects when started with none |
| `snapshot_dir` | where snapshots are kept, instead of under `$XDG_DATA_HOME`; `--snapshot-dir` overrides it |
| `[profiles.<name>]` | one table per profile, with the settings below |

## Profile settings

| Setting | Default | Meaning |
|---|---|---|
| `adapter` | | the adapter the profile connects with: `cosmos` for an Azure Cosmos DB account |
| `endpoint` | | the account endpoint URL |
| `insecure_skip_verify` | `false` | skip TLS verification; for the emulator's self-signed certificate only |
| `database` | | the database opened in the catalog on start |
| `page_size` | the adapter's | rows per result page |
| `max_join_rows` | `10000` | rows a [cross-container join](../language/cross-container.md#67-limits) holds across its held sides |
| `sample_fields` | `true` | whether [autocomplete](../using/editor.md#35-where-field-names-come-from) may query a container for its fields |
| `read_only` | `true`, unless the endpoint is `localhost`, `127.0.0.1` or `::1` | refuse every write on the account; see [Read-only accounts](../data/profiles.md#104-read-only-accounts) |
| `writers` | `4` | item writes a clone, update or delete keeps in flight on this account, 1 to 16 |
| `max_mutation_items` | `10000` | the most items one update or delete may select |
| `snapshot_max_items` | `5000000` | the largest container a snapshot takes on |
| `diagnostics` | `curly` | how the editor [marks problems](../using/editor.md#33-squiggles-and-terminals): `curly`, `underline`, or `off` |

A full example:

```toml
default_profile = "emulator"
snapshot_dir = "/mnt/big/alchemist-snapshots"   # snapshots here, not under $XDG_DATA_HOME

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # opened in the catalog on start
page_size = 100
max_join_rows = 5000             # rows a join holds across its held sides; 10000 when unset
sample_fields = false            # autocomplete never queries a container for its fields
read_only = false                # allow writes; unset, only a local endpoint allows them
writers = 8                      # item writes a clone or an update keeps in flight here; 4 when unset
max_mutation_items = 50000       # the most items one update may select; 10000 when unset
snapshot_max_items = 10000000    # the largest container a snapshot takes on; 5000000 when unset
diagnostics = "underline"        # squiggles as curly (default), underline, or off

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

## Environment variables

| Variable | Meaning |
|---|---|
| `ALCHEMIST_<NAME>_KEY` | the key for profile `<name>`, upper-cased, with dashes as underscores; used when the keychain has none |
| `COSMOS_CONNECTION_STRING` | a whole connection string; serves only a profile whose endpoint is the account it names |
| `XDG_CONFIG_HOME` | the base of the config directory |
| `XDG_STATE_HOME` | the base of the state directory |
| `XDG_DATA_HOME` | the base of the data directory |
| `NO_COLOR` | draw the editor without color, and every diagnostic as a plain underline |

The order keys are looked up in is in
[Where keys come from](../data/profiles.md#103-where-keys-come-from).

## Files and directories

| Path | Holds |
|---|---|
| `~/.config/alchemist/config.toml` | profiles and settings |
| `~/.config/alchemist/queries/<account>/*.sql` | [saved queries](../using/history.md#53-saved-queries-on-disk) |
| `~/.local/state/alchemist/history.jsonl` | the [query history](../using/history.md#51-query-history) |
| `~/.local/state/alchemist/` | the log file, beside the history |
| `~/.local/share/alchemist/snapshots/` | [snapshots](../data/snapshots.md#135-where-it-lives), per profile, database and container |

Each path follows its `XDG_*` variable when set. Keys are kept in the OS keychain
under the service `alchemist`, never in a file.

---

[← III. Command-Line Programs](cli.md) · [Contents](../README.md) · [14. Writing an Adapter →](../development/adapters.md)
