# Commands

`--help` on any command shows the same information. `--version` prints the build.

## alchemist

```
alchemist [profile] [options]
```

Starts the app on the named profile, or on the default profile. With no profiles, it
opens the connect form.

| Option | Effect |
|---|---|
| `--adapter <name>` | connect without a profile; `mock` serves fixture data |
| `--ascii` | draw with ASCII instead of Unicode |
| `--verbose` | log at debug level |
| `--history=false` | do not record [history](../using/history.md#history) |
| `--sample-fields=false` | do not [sample containers](../using/editor.md#field-sampling) for field names |
| `--read-only` | refuse writes on every account for this session |
| `--diagnostics <style>` | `curly`, `underline` or `off`; overrides the profile |
| `--snapshot-dir <dir>` | where snapshots are stored; applies to every command |

## alchemist profile

Manages profiles in `config.toml`. See [profiles and accounts](../data/profiles.md).

```
alchemist profile list
alchemist profile add <name> --endpoint <url> [options]
alchemist profile set-key <name>
alchemist profile set-read-only <name> <true|false>
alchemist profile remove <name> [--purge]
```

| Command | Effect |
|---|---|
| `list` | list profiles and where each key comes from |
| `add` | add a profile and store its key in the keychain |
| `set-key` | replace a profile's key |
| `set-read-only` | refuse (`true`) or allow (`false`) writes |
| `remove` | remove a profile and its key; `--purge` also deletes its saved queries and snapshots |

Options for `add`:

| Option | Effect |
|---|---|
| `--endpoint <url>` | account endpoint |
| `--adapter <name>` | adapter, `cosmos` by default |
| `--insecure-skip-verify` | skip TLS verification, for the emulator |
| `--database <name>` | database to open on start |
| `--page-size <n>` | rows per page |
| `--max-join-rows <n>` | rows a join may hold in memory (default 10000) |
| `--writers <n>` | concurrent item writes, 1 to 16 (default 4) |
| `--max-mutation-items <n>` | most items one update or delete may match (default 10000) |
| `--diagnostics <style>` | `curly`, `underline` or `off` |
| `--sample-fields=false` | do not sample containers for field names |
| `--read-only[=false]` | refuse or allow writes; by default only local endpoints allow them |
| `--default` | make this the default profile |

## alchemist snapshot

Takes, lists, compares and exports snapshots. Only `take` and `diff --live` connect to
the account. See [snapshots](../data/snapshots.md).

```
alchemist snapshot take   <profile> <db>[.<container>] [--note <text>] [--full]
alchemist snapshot list   [<profile> [<db>[.<container>]]] [--json]
alchemist snapshot diff   <profile> <db>.<container> [<from> [<to>]] [--live] [-o <file>]
alchemist snapshot export <profile> <db>.<container> [<snapshot>] -o <file>
alchemist snapshot delete <profile> <db>.<container> <snapshot>
alchemist snapshot prune  <profile> <db>[.<container>] --keep-last N [--keep-daily D] [--dry-run]
alchemist snapshot verify <profile> <db>[.<container>] [--deep] [--rebuild-index]
```

A snapshot is named by its ID, a unique prefix of it, `latest` or `previous`. For a
database or container name that contains a dot, use `--database` and `--container`.

| Command | Effect |
|---|---|
| `take` | snapshot a container, or every container in a database. `--full` reads every item even when a key scan would do |
| `list` | list snapshots and their size on disk, for every store when no profile is given |
| `diff` | compare two snapshots, `previous` and `latest` by default. `--live` takes a new snapshot first. `-o` writes `.json` or `.csv` |
| `export` | write a snapshot's items to `.jsonl` or `.json` |
| `delete` | delete one snapshot and the data only it used |
| `prune` | keep the newest N snapshots and the newest of each of the last D days (UTC). The newest is always kept |
| `verify` | check checksums and that every snapshot is complete. `--deep` also decompresses every item. `--rebuild-index` rebuilds pack indexes first |

```sh
alchemist snapshot take prod sales.orders --note "before the migration"
alchemist snapshot diff prod sales.orders 20260918 latest -o changes.csv
alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30 --dry-run
```
