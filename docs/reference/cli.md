# III. Command-Line Programs

- [alchemist](#alchemist) — start the terminal IDE
- [alchemist profile](#alchemist-profile) — manage connection profiles
- [alchemist snapshot](#alchemist-snapshot) — take, list, diff and export snapshots

`--help` on any command prints the same information, and `--version` prints the
build.

## alchemist

Start the terminal IDE.

### Synopsis

```
alchemist [profile] [option…]
```

### Description

With no profile, Alchemist connects the default profile, or opens the connect form
when there is none. With one, it connects that profile. See
[Getting Started](../tutorial/getting-started.md#13-connecting-to-an-account).

### Options

| Option | Effect |
|---|---|
| `--adapter <name>` | connect an adapter with no profile; `mock` serves fixture data |
| `--ascii` | draw with ASCII glyphs instead of Unicode |
| `--verbose` | log at debug level |
| `--history=false` | record nothing to the [query history](../using/history.md#51-query-history) this session |
| `--sample-fields=false` | never let [autocomplete](../using/editor.md#35-where-field-names-come-from) read items for their fields |
| `--read-only` | refuse every write on every account this session, whatever its profile allows |
| `--diagnostics <form>` | `curly`, `underline` or `off`; overrides the profile's [diagnostics](../using/editor.md#33-squiggles-and-terminals) |
| `--snapshot-dir <dir>` | keep snapshots here; applies to every command |

### Examples

```sh
alchemist                 # the default profile, or the connect form
alchemist prod            # the profile called prod
alchemist --adapter mock  # fixture data, no profile needed
```

## alchemist profile

Manage the connection profiles in `config.toml`. The file holds endpoints, never keys:
a key lives in the OS keychain, or in `ALCHEMIST_<NAME>_KEY` for a machine without one.
See [Profiles and Accounts](../data/profiles.md).

### Synopsis

```
alchemist profile list
alchemist profile add <name> --endpoint <url> [option…]
alchemist profile set-key <name>
alchemist profile set-read-only <name> <true|false>
alchemist profile remove <name> [--purge]
```

### Subcommands

**`list`** lists profiles and where each one's key comes from, never the key itself.

**`add`** adds a profile and prompts for its key, which it stores in the keychain.

| Option | Effect |
|---|---|
| `--endpoint <url>` | account endpoint URL |
| `--adapter <name>` | adapter the profile connects with; `cosmos` by default |
| `--insecure-skip-verify` | skip TLS verification, for the emulator's self-signed certificate |
| `--database <name>` | database to open in the catalog on start |
| `--page-size <n>` | rows per result page; the adapter's default when 0 |
| `--max-join-rows <n>` | rows a cross-container join may hold in memory; 10000 when 0 |
| `--writers <n>` | item writes a clone or an update keeps in flight, 1 to 16; 4 when 0 |
| `--max-mutation-items <n>` | items one update or delete may select; 10000 when 0 |
| `--diagnostics <form>` | `curly`, `underline` or `off`; `curly` when empty |
| `--sample-fields=false` | never let autocomplete read items for their fields |
| `--read-only[=false]` | refuse, or allow, writes; unset, only a local endpoint allows them |
| `--default` | make this the default profile |

**`set-key`** stores a new key for a profile in the keychain.

**`set-read-only`** allows (`false`) or refuses (`true`) writes on a profile's
account. See [Read-only accounts](../data/profiles.md#104-read-only-accounts).

**`remove`** removes a profile and its keychain entry. It keeps the profile's saved
queries and snapshots, and says where: a profile added again under the same name picks
them back up. `--purge` deletes them too.

### Examples

```sh
alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify
alchemist profile add prod --endpoint https://myaccount.documents.azure.com:443/ --default
alchemist profile set-read-only emulator false
```

## alchemist snapshot

Take, list, diff and export snapshots of containers. Only `take` and `diff --live`
connect; the rest read the disk, and work for an account whose profile is gone. See
[Snapshots](../data/snapshots.md).

### Synopsis

```
alchemist snapshot take   <profile> <db>[.<container>] [--note <text>] [--full]
alchemist snapshot list   [<profile> [<db>[.<container>]]] [--json]
alchemist snapshot diff   <profile> <db>.<container> [<from> [<to>]] [--live] [-o <file>]
alchemist snapshot export <profile> <db>.<container> [<snapshot>] -o <file>
alchemist snapshot delete <profile> <db>.<container> <snapshot>
alchemist snapshot prune  <profile> <db>[.<container>] --keep-last N [--keep-daily D] [--dry-run]
alchemist snapshot verify <profile> <db>[.<container>] [--deep] [--rebuild-index]
```

A snapshot is named by its id, a unique prefix of one, `latest` or `previous`. Every
subcommand takes `--database` and `--container` for a name holding a dot, which the
`<db>.<container>` argument cannot express.

### Subcommands

**`take`** snapshots a container, or every container of a database. The first snapshot
reads the container once; after that it reads every item's key and version, and the
bodies of those that changed. `--note` keeps a note with the snapshot; `--full` reads
every item even when a sweep of keys would do. A progress line goes to stderr when it
is a terminal, and one summary line per container to stdout.

**`list`** shows every snapshot of each store with its window, items, changes and new
data, and what the store weighs against the exports it replaces. With no arguments it
lists every store on disk, including those of accounts no profile names any more.
`--json` writes the records and usage as JSON.

**`diff`** compares two snapshots: `previous` and `latest` unless named. `--live` takes
a snapshot first and compares it with `<from>`, `latest` by default; the new snapshot
is kept. `-o` writes the diff to a `.json` or `.csv` file.

**`export`** writes a snapshot's items to `.jsonl`, one item per line, or `.json`.

**`delete`** deletes one snapshot, then reclaims what no other needs.

**`prune`** keeps the newest N snapshots, and the newest of each of the last D UTC
days; the union is kept, and the newest snapshot always is. `--dry-run` lists what
would be deleted and deletes nothing. Nothing prunes by itself.

**`verify`** checks every file's format line, every checksum, that every change set
leads to the snapshot after it, and that every body a snapshot names is stored.
`--deep` also decompresses and rehashes every body; `--rebuild-index` rewrites every
pack's index from the pack first. It fails on any problem.

### Examples

```sh
alchemist snapshot take prod sales.orders --note "before the migration"
alchemist snapshot diff prod sales.orders 20260918 latest -o changes.csv
alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30 --dry-run
```

---

[← II. Statements](statements.md) · [Contents](../README.md) · [IV. Configuration →](configuration.md)
