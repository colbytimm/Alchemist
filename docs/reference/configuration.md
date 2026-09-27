# Configuration

Settings live in `~/.config/alchemist/config.toml`. The connect form and
[`alchemist profile`](cli.md#alchemist-profile) write it, and you can edit it by hand.
It never holds a key.

| Setting | Meaning |
|---|---|
| `default_profile` | the profile `alchemist` connects to when none is named |
| `snapshot_dir` | where snapshots are stored; `--snapshot-dir` overrides it |
| `[profiles.<name>]` | one table per profile |

## Profile settings

| Setting | Default | Meaning |
|---|---|---|
| `adapter` | | `cosmos` for an Azure Cosmos DB account |
| `endpoint` | | the account endpoint URL |
| `insecure_skip_verify` | `false` | skip TLS verification, for the emulator's self-signed certificate |
| `database` | | database to open in the catalog on start |
| `page_size` | adapter default | rows per result page |
| `max_join_rows` | `10000` | rows a [join](../language/cross-container.md#limits) may hold in memory |
| `sample_fields` | `true` | let [autocomplete](../using/editor.md#field-sampling) sample a container for field names |
| `read_only` | `true` unless the endpoint is local | refuse writes; see [read-only accounts](../data/profiles.md#read-only-accounts) |
| `writers` | `4` | concurrent item writes for clones, updates and deletes, 1 to 16 |
| `max_mutation_items` | `10000` | most items one update or delete may match |
| `snapshot_max_items` | `5000000` | largest container a snapshot accepts |
| `diagnostics` | `curly` | [error hint](../using/editor.md#terminal-support) style: `curly`, `underline` or `off` |

Example:

```toml
default_profile = "emulator"
snapshot_dir = "/mnt/big/alchemist-snapshots"

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true
database = "sales"
page_size = 100
max_join_rows = 5000
sample_fields = false
read_only = false
writers = 8
max_mutation_items = 50000
snapshot_max_items = 10000000
diagnostics = "underline"

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

## Environment variables

| Variable | Meaning |
|---|---|
| `ALCHEMIST_<NAME>_KEY` | key for profile `<name>` (upper-cased, dashes as underscores), used when the keychain has none |
| `COSMOS_CONNECTION_STRING` | connection string, used only for the profile whose endpoint it names |
| `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME` | move the config, state and data directories |

See [keys](../data/profiles.md#keys) for the lookup order.

## Files

On Windows, `~` is your user profile folder, `%USERPROFILE%`.

| Path | Contents |
|---|---|
| `~/.config/alchemist/config.toml` | profiles and settings |
| `~/.config/alchemist/queries/<account>/*.sql` | [saved queries](../using/history.md#saved-queries) |
| `~/.local/state/alchemist/history.jsonl` | [query history](../using/history.md#history) |
| `~/.local/state/alchemist/` | log file |
| `~/.local/share/alchemist/snapshots/` | [snapshots](../data/snapshots.md) |

Keys are stored in the OS keychain under the service `alchemist`.
