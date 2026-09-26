# 13. Snapshots

- [13.1. Taking and comparing](#131-taking-and-comparing)
- [13.2. From a shell](#132-from-a-shell)
- [13.3. What a snapshot is](#133-what-a-snapshot-is)
- [13.4. What it costs](#134-what-it-costs)
- [13.5. Where it lives](#135-where-it-lives)
- [13.6. Background captures](#136-background-captures)

## 13.1. Taking and comparing

`s` on a container in the catalog takes a snapshot of it: every item and the
container's definition, kept on disk. `v` lists its snapshots, newest first, and
`enter` shows what changed from the one before, or between the two marked with
`space`: the items added, removed and modified, a field-by-field diff of any modified
item (`enter` on it), and what changed in the definition, such as the indexing policy,
the TTL or the throughput. `s` and `v` on a database take and list database
snapshots, one per container.

![Two snapshots of sales.orders, the second with eight items modified, and the store's size on disk](../images/snapshots.png)

![The diff between the two snapshots: eight items modified, each listing the fields that changed](../images/snapshot-diff.png)

![The field-by-field diff of one item: archivedAt added and status changed from shipped to archived](../images/snapshot-item-diff.png)

| Key | Where | Action |
|---|---|---|
| `s` | snapshots | take snapshot |
| `enter` | snapshots, note prompt | take |
| `space` | snapshots | mark |
| `enter` | snapshots | diff |
| `d`, then `enter` | snapshots | delete snapshot |
| `ctrl+e` | snapshots | export the snapshot's items (`.jsonl` or `.json`) |
| `enter` | diff | fields |
| `tab` | diff | all/added/removed/modified |
| `ctrl+e` | diff | export the diff (`.json` with a JSON Patch per modified item, or `.csv`) |

## 13.2. From a shell

Every snapshot operation also runs with no TUI:

```sh
alchemist snapshot take prod sales.orders --note "before the migration"
alchemist snapshot diff prod sales.orders          # previous → latest
alchemist snapshot diff prod sales.orders --live   # the latest → now
alchemist snapshot list prod
alchemist snapshot export prod sales.orders latest -o orders.jsonl
alchemist snapshot verify prod sales --deep
```

and from cron, pruning on a line of its own, since nothing prunes by itself:

```
0 6 * * *  alchemist snapshot take prod sales --note nightly \
           && alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30
```

[Command-Line Programs](../reference/cli.md#alchemist-snapshot) documents every
subcommand and flag.

## 13.3. What a snapshot is

A snapshot of a live container is every item as the service returned it during the
window the list shows (`started` to `finished`), not a point in time. No item is torn,
and one nobody wrote during the window is exactly as it was; but there is no
consistency between items, and an item created or deleted during the window may or may
not be in it. A write missed that way is caught by the next snapshot. A restore point
consistent across items is the account's continuous backup, not this.

## 13.4. What it costs

The first snapshot reads every item once. After that a snapshot reads every item's key
and version (which is the only way to see deletes), and the bodies of those that
changed; a container nobody touched costs one read of its keys and under 4 KB of disk.

Items are stored once whichever snapshots hold them, compressed in blocks, so thirty
daily snapshots of a container where 1% changes a day take about 1.4× the disk of one;
`v` and `snapshot list` show the store against the exports it replaces
(`30 snapshots · 30.0 GB of items · 460.0 MB on disk · 65× smaller`). A capture spends
request units as fast as the account lets it, and the progress line shows how many.

## 13.5. Where it lives

Under `$XDG_DATA_HOME/alchemist/snapshots` (`~/.local/share/alchemist/snapshots`), per
profile, then database and container; `snapshot_dir` in `config.toml` or
`--snapshot-dir` moves it. Directories are `0700` and files `0600`.

**Snapshots are not encrypted**: they are copies of the account's data, protected by
file permissions and whatever encrypts the disk.

`alchemist profile remove` keeps an account's snapshots and says where; `--purge`
deletes them with the profile.

## 13.6. Background captures

A snapshot only reads, so it works on a
[read-only account](profiles.md#104-read-only-accounts). A capture runs in the
background like a [clone](cloning.md): `esc` hides it, the status bar carries
`snapshot prod/sales.orders 41% (v)` on every account, `v` in the catalog brings it
back, and `x` cancels it, keeping nothing. One capture, clone or other background job
runs at a time. A container past `snapshot_max_items` on the profile (5,000,000 when
unset) is refused before anything is read.

---

[← 12. Cloning](cloning.md) · [Contents](../README.md) · [Reference: Key Bindings →](../reference/keys.md)
