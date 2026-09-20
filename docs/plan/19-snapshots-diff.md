# Iteration 19 — Snapshots and Change Diff

## Goal

See how data changes over time. Take a snapshot of a container, or of every container
in a database; take another an hour or a month later; read the difference: items added,
removed and modified, a field-level diff of any modified item, and what changed in the
container's definition (indexing policy, TTL, throughput). From the catalog, or from
cron with no TUI at all.

Snapshots pile up, so **what a snapshot costs on disk is the design**, not a detail of
it. Thirty daily snapshots of a million-item container must cost about as much as one,
and the tool must show the user that they do. The second budget is request units: a
second snapshot must not read the whole container again.

This is a read-only iteration. Nothing here writes to an account.

## Storage design

### What has to be cheap

The reference case used throughout: one container, 1 000 000 items of 1 KB, 1% of
them rewritten per day, one snapshot a day for 30 days. Three things grow, and a
design that handles only the first still fails:

| Grows with | Naive cost over 30 snapshots | Why it matters |
|---|---|---|
| item bodies | 30 × 1 GB = 30 GB | the obvious one |
| the list of what is in each snapshot | 30 × 72 MB = 2.2 GB (see "Manifest") | larger than all the deduplicated, compressed bodies together |
| file count | 1.29 million files if bodies are stored loose | 4 KB minimum allocation turns 1.29 GB into 5.2 GB, before directory and inode cost |

### Alternatives considered

| Option | Verdict | Reason |
|---|---|---|
| A JSON export per snapshot | rejected | 30 GB; 7.5 GB gzipped. No sharing between snapshots, and a diff reads both files whole. |
| Content-addressed bodies as loose files (git's loose objects) | rejected | Deduplicates, but a million small files is what git itself packs to escape. Per-file compression of one 1 KB document reaches about 1.5×, because the redundancy is *between* documents. |
| **Content-addressed bodies in append-only pack files, compressed in blocks** | **chosen** | Dedupe across snapshots, 4× compression from cross-item redundancy, a few dozen files, and every file is either immutable or replaced by rename. |
| An embedded B+tree key-value file (the pure-Go Bolt fork etcd maintains) | rejected | Pure Go, one dependency. But values are stored uncompressed in 4 KB pages, so compression is per item again; the file does not shrink without a full copy; and the store is opaque to `ls` and `jq`. It would replace the pack index, which is the small part. |
| SQLite | rejected | The usual driver needs cgo and the repo builds with `CGO_ENABLED=0`. The pure-Go driver is a machine translation of the C source and one of the largest dependencies a Go binary can take. Same per-item compression problem. |
| Pebble | rejected | Block compression built in, and the most capable option; also an LSM tree with compaction and write amplification, brought in with a tree of `cockroachdb` modules, to hold data that is written once and never updated. |
| A stronger codec for the blocks (Facebook's successor to deflate) | deferred | The standard library has no writer for it, and the pure-Go module everyone uses for it is not in `go.sum` today, not even indirectly, so it would be a new direct dependency. The standard library's deflate reaches most of the ratio on 256 KB blocks of similar JSON. Every block records its codec, so a stronger codec can be added later without a format break — when a measurement says it pays. |
| A full manifest per snapshot | rejected | 72 MB each at a million items; see the first table. |
| Manifest as a content-defined chunked tree (a probabilistic B-tree, as Dolt and Noms use), unchanged chunks shared by hash | rejected | Sharing depends on churn being clustered in key order, and ids are usually GUIDs, so it is not. A chunk of *n* entries survives a 1% day untouched with probability 0.99ⁿ: 0.004% at *n* = 1000, 85% at *n* = 16. Chunks of 16 mean a 62 500-leaf tree and still 10.8 MB rewritten per snapshot. A change set for the same day is 1.2 MB and is a flat file. |
| **One full manifest for the newest snapshot, a change set between each pair of neighbors** | **chosen** | Cost per snapshot is proportional to what changed. The common diff (neighbor to neighbor) is one file read. Dropping the oldest snapshot, which is what pruning does, deletes one file. |

No new dependency. `crypto/sha256`, the deflate writer under `compress`, `hash/crc32`,
`encoding/binary` and `encoding/json` cover it, which is CLEAN_CODE's "standard library
over new dependencies" applied where it costs nothing.

### Canonical form and the content hash

An item's identity on disk is the SHA-256 of its canonical form:

1. Decode with `json.Decoder.UseNumber`.
2. Split off the backend's bookkeeping fields: `_rid`, `_self`, `_etag`,
   `_attachments` and `_ts`. Iteration 18's scan hands items over with them intact,
   and one shared helper, `adapter.SplitSystemFields` (see "Adapter contract"), does
   the split, so `internal/snapshot` never spells their names. `_etag` and `_ts` are
   not thrown away: they travel beside the body as the item's *version* and *modified
   time* and are kept in the manifest, outside the hash. They are the cheap change
   signal; they must never make identical content look different, and a replace that
   writes the same document back must dedupe to the same blob.
3. Respell numbers with the rule iteration 10 settled for join keys: `1`, `1.0` and
   `1e0` are one number, and an integer past 2^53 keeps every digit. That code is
   `canonicalNumbers` in `internal/query`'s join executor; it moves to a new
   `internal/canonical` package that both `query` and `snapshot` import, rather than
   being written twice.
4. Encode with object keys sorted, no insignificant whitespace, and
   `SetEscapeHTML(false)` so an exported body reads as it was written.

Strings are not Unicode-normalized: two spellings of one glyph are different content,
as they are to the service. Array order is content. A document with a duplicate key
keeps the last one, which is what `encoding/json` does.

The bookkeeping fields are about 190 bytes of every Cosmos item, so the canonical form
of a 1 KB item is nearer 810 bytes. The estimates below ignore that saving.

### The chosen design

```
item ──canonical──▶ body ──SHA-256──▶ hash
                     │                  │
                     ▼                  ▼
              pack (blocks of     manifest entry:
              ~256 bodies,        key → hash, version, modified
              deflate-compressed)
```

- **Blobs.** A body is stored once per container store, under its hash. An item that
  did not change between two snapshots costs nothing the second time.
- **Packs.** Bodies are appended to a pack in *blocks* of up to 256 KB uncompressed
  (about 256 items), each block compressed as one deflate stream, so the compressor sees
  the same field names and enum values hundreds of times. A capture writes its own new
  pack files, rolled at 64 MB, to a temporary name, syncs, and renames. A published
  pack is never modified. Reading one item decompresses one block.
- **Pack index.** Beside each pack, a sorted table from the first 16 bytes of a hash to
  (block offset, position in block), with a 256-entry first-byte offset table like the
  one in git's `.idx`. It is derived data: `verify --rebuild-index` recreates it by
  reading the pack. The pack itself stores no hashes, only body lengths, because a hash
  can be recomputed from its body; every read rehashes the body it found and compares
  all 32 bytes with the hash the manifest asked for, so the truncated index can never
  return the wrong item and every read is an integrity check.
- **Manifest.** The newest snapshot (the *head*) has a full manifest: every item key
  in byte order with its hash, version fingerprint and modified time.
- **Change sets.** Every snapshot but the first has a change set: one record per key
  that differs from its parent, holding the entry *before* and the entry *after*. Any
  older snapshot's contents are the head manifest with change sets undone back to it.
  Holding both sides is what makes a diff independent of the manifest.
- **Records.** One small JSON file per snapshot with everything a list needs: times,
  counts, sizes, RU, note, the hash of the definition blob. Human-readable on purpose.

A manifest entry is about 95 bytes raw — a 36-character id and a short partition key
value (50), the hash (32), an 8-byte fingerprint of the version, the modified time (4)
— and about 72 compressed, since hashes do not compress and GUID text only halves.
That is the 72 MB per million items in the tables above, and why only one full manifest
is ever kept.

### On-disk layout

```
$XDG_DATA_HOME/alchemist/snapshots/        ~/.local/share/alchemist/snapshots otherwise
  prod/                                    account = profile name (iteration 14)
    sales~5c1f0a9e/                        database
      groups/
        20260919T060000Z.json              a database snapshot: its members + definition
      orders~1f3a9c2e/                     container store
        FORMAT                             "alchemist-snapshot-store 1\n"
        store.json                         real names, partition key paths, created
        lock                               present while a capture or a prune runs
        records/
          20260919T060000Z.json            snapshot record — its rename publishes it
        changes/
          20260919T060000Z.changes         parent → this snapshot
        manifests/
          20260919T060000Z.manifest        head only
        packs/
          20260821T060000Z-0001.pack       named for the capture that wrote it
          20260821T060000Z-0001.idx
```

- **Data directory, not state.** `logging.Dir()` is `$XDG_STATE_HOME`: logs and
  history, things a reinstall may lose. Snapshots are gigabytes of user data and belong
  under `$XDG_DATA_HOME/alchemist` (`~/.local/share/alchemist`), on every platform, the
  way config and state already ignore macOS conventions. `snapshot_dir` in
  `config.toml` (top level, beside `default_profile`) or `--snapshot-dir` moves it, for
  the person whose home directory is small.
- **Account.** The first segment is the profile name, which iteration 14 guarantees is
  safe as a path segment and distinct on a case-insensitive filesystem.
- **Database and container segments** get no such guarantee: Cosmos names are
  case-sensitive and may hold spaces and dots. A segment is the name with anything
  outside `[A-Za-z0-9_-]` replaced by `_`, then `~` and the first 8 hex digits of the
  name's SHA-256. `Orders` and `orders` stay apart on macOS, the directory is still
  readable, and `store.json` holds the real name. `groups/` has no `~`, so no container
  can collide with it.
- **Permissions.** Directories `0700`, files `0600`, as `config` and `history` do.
- **Not encrypted.** A snapshot is a copy of production data sitting in a home
  directory, protected by file permissions and whatever encrypts the disk. Encryption
  at rest is out of scope, and the README, `snapshot take --help` and the overlay's
  empty state say so in those words rather than leaving it to be assumed.
- **Snapshot ids** are the capture's start in UTC, `20260919T060000Z`: sortable, and
  meaningful to `ls`. The per-store lock makes a collision impossible.

### Format

Every binary file starts with one ASCII line naming its kind and version, in the
style of `FORMAT` — `head -1` identifies any file in a store — and integers after it
are little-endian. A reader refuses a version it does not know with `ErrUnknownFormat`; it
never guesses. `FORMAT` versions the layout as a whole. The spec below is copied into
the package comment of `internal/snapshot`, which is where it is maintained.

| File | First line | Body |
|---|---|---|
| `.pack` | `alchemist-pack 1` | blocks, back to back: `uint32` compressed length, `uint32` raw length, `uint32` item count, `uint8` codec (1 = deflate), `uint32` CRC-32 of the compressed bytes, then the bytes. Decompressed: *count* variable-length integer body lengths, then the bodies. |
| `.idx` | `alchemist-pack-index 1` | `uint32` entry count, 256 × `uint32` first-byte offsets, then entries sorted by prefix: 16-byte hash prefix, `uint32` block offset, `uint16` position. 22 bytes each. |
| `.manifest` | `alchemist-manifest 1` | one deflate stream of entries sorted by key: variable-length key length, key, 32-byte hash, 8-byte version fingerprint, variable-length modified (Unix seconds). Trailer: entry count and the SHA-256 of the raw stream. |
| `.changes` | `alchemist-changes 1` | same framing; each record is key, a flags byte (has-before, has-after), then the entries present. |
| record, `store.json`, group | — | JSON with a `"format": 1` field. |

An item **key** is its partition key values followed by its id, each as a
length-prefixed canonical JSON value; a partition key path the item does not have is a
zero-length component, so *undefined* and `null` stay different, as they are in Cosmos.
Hierarchical keys are simply more components, in `adapter.MetaPartitionKey` order. The
values come from iteration 18's `adapter.PartitionKeyValues`, asked one path at a time:
for a clone a missing key is a refusal, here `ErrNoPartitionKey` is the zero-length
component, because an item without its key path is a legal item that must be snapshotted
like any other. The version fingerprint is the first 8 bytes of the SHA-256 of the
version string: format-agnostic, and a false "unchanged" needs a 2^-64 accident on one
item.

### Publishing, and what a crash leaves

A capture takes `lock` (created with `O_EXCL`, holding pid, host and start time; a lock
whose process is gone on this host is taken over), then:

1. writes packs and indexes — temporary name, `Sync`, rename;
2. writes the change set and the new full manifest the same way;
3. renames the record into `records/`. **This rename is the commit.** A snapshot with
   no record does not exist;
4. removes the previous head's manifest, now reachable through the change set.

Cancel, a crash, or a full disk at any point before step 3 leaves files no record
names: `Open` deletes temporary files, change sets and manifests without a record, and
packs no record's capture id matches *and* nothing references. After step 3 a crash
leaves at most a second full manifest, which `Open` removes. No state needs repair by
hand, and no reader ever sees part of a snapshot. `config.Store.Save` writes in place
and `history.File.trim` renames without a sync; here the sync is not optional, because
a record that outlives its pack is corruption.

### Worked size estimate

1 000 000 items × 1 KB, 1% rewritten per day, 30 daily snapshots. Compression is taken
as 4× for blocks of similar JSON; step 2 measures it and this table is corrected.

| | One snapshot | 30 snapshots |
|---|---|---|
| Bodies, unique | 1.00 GB | 1.29 GB (1M + 29 × 10k) |
| Bodies in packs at 4× | 250 MB | 323 MB |
| Body lengths in blocks | 2 MB | 3 MB |
| Pack indexes, 22 B per blob | 22 MB | 28 MB |
| Head manifest, 72 B per item | 72 MB | 72 MB |
| Change sets, ~116 B per changed key | — | 34 MB (29 × 1.2 MB) |
| Records | 2 KB | 60 KB |
| **Total** | **346 MB** | **460 MB** |

| Compared with | Size | Ratio |
|---|---|---|
| 30 JSON exports | 30 GB | 65× larger |
| 30 gzipped exports | 7.5 GB | 16× larger |
| one snapshot in this design | 346 MB | 30 snapshots cost 1.33× one |

A day costs 3.9 MB: 2.5 MB of new bodies, 0.2 MB of index, 1.2 MB of change set. A
snapshot of a container nobody touched costs a record and an empty change set, under
4 KB. If the documents are high-entropy and compress 2×, the 30-day total is 782 MB
and still 38× smaller than the exports. Inserts and deletes cost the same as or less
than the rewrites assumed here.

Memory: a capture holds the parent manifest and the index prefixes, about 100 bytes and
22 bytes per item — 130 MB at a million items. That is a ceiling worth stating rather
than discovering: a container past `snapshot_max_items` (profile key, default
5 000 000) is refused before any RU is spent, with a message naming the key, the way
`max_join_rows` refuses a join.

### Retention, garbage collection, verification

- **Delete** one snapshot. The oldest: remove its record, and the change set of the
  one after it. A middle one: compose its change set with its child's (first *before*,
  last *after*, drop keys that end where they began). The head: undo its change set
  from the manifest to make its parent the head. All O(changes) except the last.
- **Prune** applies a policy and deletes what it does not keep: `--keep-last N`, plus
  `--keep-daily D` (the newest snapshot of each of the last *D* UTC days); the union is
  kept, and the head always is. Nothing prunes by itself: a capture never deletes data,
  and a cron job that wants pruning says so on its own line. `--dry-run` lists.
- **Garbage collection** runs at the end of delete and prune, under the lock, so no
  capture can dedupe against a blob that is about to go. Mark: every hash in the head
  manifest, every *before* and *after* in every change set, every record's definition
  hash. Sweep: a pack with no live blob is deleted; a pack less than half live is
  rewritten — live bodies re-blocked into a new pack, published, then the old pack
  removed; packs at least half live are left, which bounds waste at 2× and avoids
  rewriting 64 MB to reclaim a kilobyte. More than 64 packs merges the smallest.
- **Verify** checks every first line, version, CRC and manifest trailer; that every change
  set chains to its parent; that every referenced hash is in an index; and, with
  `--deep`, decompresses every block and rehashes every body.
- **Usage** is what makes the efficiency claim visible. For a store: snapshots, items
  in the head, *logical* bytes (the sum over snapshots of their canonical body sizes —
  what 30 exports would weigh), *on-disk* bytes by kind (packs, indexes, manifest,
  change sets), and the ratio. Shown in the overlay's footer and by `snapshot list`.

## Capture

### What the SDK offers (verified in `azcosmos` v1.5.0)

- `ContainerClient.NewQueryItemsPager` with `QueryOptions.QueryParameters`,
  `PageSizeHint` and `ContinuationToken` — the pager `cosmos.connection.Query` already
  uses, cross-partition with an empty partition key.
- `ContainerClient.ReadChangeFeed(ctx, *ChangeFeedOptions)`, one page per call, with
  `StartFrom`, `FeedRange`, `PartitionKey`, `MaxItemCount` and a composite
  `Continuation` that survives partition splits; `ReadFeedRanges` lists the ranges.
- The change feed request sends `A-IM: Incremental Feed` from a constant
  (`cosmosHeaderValuesChangeFeed`) and `ChangeFeedOptions` has no mode field. So this
  SDK reads the **latest-version** feed only. That mode reports creates and updates
  and never a delete. The all-versions-and-deletes mode is not reachable from v1.5.0
  at all, and on the service it needs continuous backup and only reaches back as far
  as its retention.

So the change feed cannot find deletes, a key sweep is needed whichever way changed
bodies are fetched, and once there is a sweep that carries versions the feed adds an
adapter surface and saves nothing. **No change feed in this iteration.** A record has
an unused `resume` field so a later iteration can store a feed continuation there.

### First snapshot: full scan

`ItemScanner.ScanItems` with a zero `ScanRequest`: every item, every page split,
canonicalized, hashed, deduplicated against the index and appended. Even a first
snapshot dedupes — against a cancelled attempt's surviving packs, and against identical
documents.

### Later snapshots: sweep, compare, fetch

1. **Sweep.** `ScanItems` with `Projection: ScanIdentity`: key, version and modified
   time of every live item, no bodies. In Cosmos,
   `SELECT c.id, c._etag, c._ts, <partition key paths> FROM c`.
2. **Compare** with the parent manifest. Same key and same version fingerprint: carried
   forward, zero bytes written. Key missing from the sweep: removed. New key, or a
   different version: a *suspect*.
3. **Fetch.** `ScanItems` with `Since` set to the **oldest modified time among the
   suspects** — `WHERE c._ts >= @since`. Every suspect has a `_ts` at
   or after that by construction, so the query returns all of them. No clock of ours
   or the service's is consulted, so there is no skew to allow for.
4. **Settle.** Every body the fetch returns wins, suspect or not: it is a version at
   least as new as the sweep saw. A suspect the fetch does not return was deleted
   between the two steps and is recorded as removed. A body whose hash equals the
   parent's is an unchanged item with a new version (a no-op replace): the manifest
   takes the new version, no blob is written, and the diff does not list it.

Why this shape:

- `WHERE c._ts > @lastSnapshotTime` alone misses deletes, and misses a second write in
  the same second as the first: `_ts` has one-second granularity. It also needs a
  trustworthy "last snapshot time" on the service's clock, which a capture that ran
  for an hour does not have. Comparing **versions** finds every write exactly; `_ts` is
  used only to bound the fetch, inclusively, with a lower bound taken from the data.
- The overlap this produces (items at or after the bound that were not suspects) costs
  RU and no disk, because their hashes are already stored. If it is pathological the
  fetch degrades toward a full scan, never toward a wrong answer.
- `--full` forces a full scan, for the day someone distrusts all of the above.

One risk is recorded rather than assumed away: the default indexing policy excludes
`/"_etag"/?`, so the sweep cannot be answered from the index alone. Step 5 measures the
sweep with and without `_etag`. If `_etag` is what makes it expensive, the fallback is
`_ts` equality with every entry modified inside its own capture's window, widened by
five minutes, treated as a suspect — same structure, weaker signal, same-second hole
closed.

### RU profile

Planning figures until step 5 measures them on a real account: 0.04 RU per 1 KB item
returned by a scan, 0.015 per key returned by the sweep.

| | Reads | RU (planning) | Writes to disk |
|---|---|---|---|
| First snapshot | 1M bodies | ≈ 40 000 | 346 MB |
| Later snapshot, 1% churn | 1M keys + ≈ 10k bodies | ≈ 15 000 + 400 | 3.9 MB |
| Later snapshot, nothing changed | 1M keys | ≈ 15 000 | < 4 KB |

Said plainly: incremental capture cuts disk and wall-clock by two orders of magnitude
and RU by a factor of two or three. The sweep is the floor, and the floor exists
because nothing cheaper reports deletes. Skipping the sweep when a `COUNT` proves no
deletes was considered and deferred: the count and the fetch happen at different
moments on a live container, and the saving is not worth a wrong "removed" list.

The capture spends RU the user asked it to spend, and says how much: progress shows the
running charge, the record keeps the total, and the log has both. It does not throttle
itself. `QueryOptions` in v1.5.0 has `PriorityLevel` and `ThroughputBucket`; using them
needs account features this plan cannot verify, so they stay out.

### What a snapshot guarantees

A snapshot of a live container is **not** a point in time, and the UI never says it
is. The record stores `started` and `finished`, and the list shows the window.

- Every body is a version the service returned during the window. No item is torn.
- An item nobody wrote during the window appears exactly as it was, or is absent
  exactly if it did not exist.
- An item written during the window appears as one of its versions from the window; an
  item created or deleted during it may or may not be present.
- There is no consistency *between* items. Two documents written in one transactional
  batch can appear one old, one new.
- A change missed because it landed mid-window is caught by the next snapshot: its
  version differs from the recorded one.

Someone who needs a restore point consistent across items needs the account's
continuous backup, and the README says so.

### Definitions

A capture stores the container's definition beside its items, from the interfaces
iterations 11 and 18 define, each found by type assertion and each optional:

| Part | Source | In the definition hash |
|---|---|---|
| partition key paths | `DefinitionReader.ContainerDefinition(ctx, path, DefinitionFull)` | yes |
| policies (indexing, TTL, unique keys, conflict resolution…) | the same call: `PolicyDocument{Backend, Raw}`, `Raw` canonicalized | yes |
| throughput | `ThroughputEditor.Throughput` | yes |
| size estimate | `ContainerDefinition.Size` | no — it is a reading, kept in the record for the list |

The three hashed parts are encoded canonically as one document and stored as a blob
like any other; the record holds its hash, so thirty unchanged definitions cost one
blob, and "definition changed" is a hash comparison that an inserted row cannot trip.
A connection without `DefinitionReader` produces a record with no definition, and the
diff says "definition not captured". A database group records the database's
throughput the same way and the list of its containers; iteration 18 defines no
database definition beyond that, and neither does this.

### Database snapshots

A database snapshot is a **group**: one container snapshot per container, taken one
after another (never concurrently — one scan's RU at a time), sharing a group id, plus
the database's definition and the list of containers. A container that fails is
recorded in the group as failed; the ones that succeeded stand. There is no
cross-container consistency, for the same reason there is none across items.

## Diff

**Identity** is (partition key values, id). A changed partition key value is a removed
item and an added one, because that is what it is in Cosmos — the diff view says so in
a one-line note when a removed and an added item share an id.

**From manifests, in O(changes).** Neighbor to neighbor: read one change set. Snapshot
*i* to *j*: compose the change sets between them, keeping the first *before* and the
last *after* per key and dropping keys whose hash ends where it began (changed and
changed back). The full manifest is never read for a diff. A record whose *before* has
no entry is `added`, no *after* is `removed`, differing hashes `modified`.

**Against live** is a capture followed by a diff, offered as one action. The snapshot
is kept: its RU was spent, an unchanged container makes it a 4 KB file, and "unchanged
as of 14:02" is itself worth recording.

**Field-level diff** of a modified item, two outputs from the two canonical bodies:

- *Structural* — for counts, the "fields" column and export. Objects recurse by key.
  Numbers compare canonically. Arrays of equal length recurse by position; arrays of
  different length are one `replace` at the array's path. Result: a list of
  `{op, path, before, after}` with RFC 6901 paths, which is already an RFC 6902 JSON
  Patch.
- *Visual* — for the overlay. Both bodies are indented with the keys already sorted,
  then diffed by line with an LCS, comparing lines with their trailing comma ignored
  so adding a last member does not mark its neighbor. Because the key order is
  canonical, a line diff *is* a semantic diff, and an array that gained an element in
  the middle shows one `+` line, which is what positional comparison cannot do. Bodies
  past 2000 lines fall back to the structural list.

Arrays: LCS where a person reads it, positional-or-replace where a machine applies it.
A patch that is always valid beats one that is sometimes minimal.

**Definition diff** compares the two stored definitions: partition key paths and
throughput as the typed values they are, and `PolicyDocument.Raw` through the same
structural differ as an item, which yields lines such as
`/defaultTtl: absent → 2592000` and `/indexingPolicy/excludedPaths: replaced`. The
paths are the backend's own and are shown verbatim; the TUI interprets none of them,
which is iteration 12's rule. Two definitions with different `Backend` values are not
compared.

**Export.** The diff view's `ctrl+e` and `snapshot diff -o` write by extension, through
`export.ResolvePath` and the same refuse-to-overwrite rule as `export.WriteFile`:

| Extension | Content |
|---|---|
| `.json` | `{from, to, summary, definition, changes: [{change, partitionKey, id, patch}]}`; `added` and `removed` carry the body instead of a patch |
| `.csv` | `change,partition_key,id,fields,modified_before,modified_after` — one row per item |

A snapshot's **contents** export as `.jsonl` (one canonical body per line) or `.json`
(an array), streamed from the packs; `export.JSON` buffers a whole result set in a
`bytes.Buffer` and a million items will not fit that shape. This is as far as restore
goes here: the data comes back out as a file. Writing it into an account belongs to
iterations 17 and 18.

## Layout

`v` on a container opens its snapshots, newest first:

```
┌─ Snapshots · prod · sales.orders ──────────────────────────────────────────┐
│     Taken (UTC)        Window   Items       Changes             New data   │
│   ● 2026-09-19 06:00   48s      1 000 412   +212 −40 ~9 731       2.6 MB   │
│ ›   2026-09-18 06:00   51s      1 000 240   +198 −35 ~9 804       2.5 MB   │
│     2026-09-17 06:00   47s      1 000 077   +240 −51 ~10 112 def  2.7 MB   │
│     2026-08-21 06:00   4m12s      998 310   first snapshot        251 MB   │
│                                                                            │
│   note: nightly                                                            │
│   30 snapshots · 30.0 GB of items · 460 MB on disk · 65× smaller           │
│   s take  space mark  enter diff  ctrl+e export  d delete  esc close       │
└────────────────────────────────────────────────────────────────────────────┘
```

`●` is a marked row; `def` means the definition changed; the note of the row under the
cursor has its own line, since notes are the one unbounded column. While a capture
runs it is the top row, and the status bar carries it when the overlay is closed:

```
│   ◐ capturing…  412 000 items · 16 204 RU · 98 MB read · 1.2 MB new · x cancel │
```

A store with no snapshots says what `s` will do, what it will cost ("reads every item
once"), where the data will be kept, and that it is not encrypted.

`enter` opens the diff of the two marked rows — or, with fewer than two marked, of the
row under the cursor against the one before it:

```
┌─ Diff · sales.orders · 09-18 06:00 → 09-19 06:00 ──────────────────────────┐
│   +212 added   −40 removed   ~9 731 modified   definition: 1 setting       │
│                                                                            │
│     definition   /defaultTtl: absent → 2592000                             │
│   + c07          o-100412                                                  │
│   − c11          o-000017                                                  │
│ › ~ c03          o-000231       status, total                              │
│   ~ c03          o-000232       lines                                      │
│                                                                            │
│   all · 9 983 changes                                                      │
│   enter fields  tab all/added/removed/modified  / filter  ctrl+e export    │
└────────────────────────────────────────────────────────────────────────────┘
```

The "fields" column needs both bodies, so it is computed for the rows on screen and
cached, never for ten thousand rows at once. `+`, `−` and `~` rows use
`theme.SuccessStyle`, `theme.ErrorStyle` and `theme.TextStyle`; no new color.

`enter` on a row opens the item, built on `panes.Detail`'s frame and scrolling:

```
┌─ Item · c03 / o-000231 · modified ──────────────────────────┐
│     {                                                       │
│       "customerId": "c03",                                  │
│       "id": "o-000231",                                     │
│   -   "status": "open",                                     │
│   +   "status": "shipped",                                  │
│       … 14 unchanged lines                                  │
│   -   "total": 120.5                                        │
│   +   "total": 131                                          │
│     }                                                       │
│   2 fields · modified 2026-09-18 22:14:03 UTC               │
│   ↑/↓ scroll   esc back                                     │
└─────────────────────────────────────────────────────────────┘
```

An added or removed item shows its whole body in one color. `v` on a database lists its
groups; `enter` on a group diff lists containers with their counts (and containers that
appeared or disappeared); `enter` on one opens the container diff above.

## Interaction

| Key | Where | Action |
|---|---|---|
| `s` | catalog, container or database row | prompt for an optional note, then take a snapshot |
| `v` | catalog, container or database row | open the snapshots overlay |
| `v` | catalog, while a capture runs | reopen that capture's overlay, whatever the cursor is on |
| `↑`/`↓`, `k`/`j` | any snapshot overlay | move or scroll |
| `s` | snapshots overlay | take a snapshot now |
| `space` | snapshots overlay | mark or unmark; a third mark replaces the older one |
| `enter` | snapshots overlay | diff the marked pair, or the cursor row against its parent |
| `d` | snapshots overlay | delete the snapshot, after a confirm line (`enter` confirms, `esc` cancels) |
| `x` | snapshots overlay | cancel the running capture |
| `ctrl+e` | snapshots overlay | export the snapshot's items (`.jsonl`, `.json`) |
| `enter` | diff view | open the item or the definition diff |
| `tab` | diff view | cycle all, added, removed, modified |
| `/` | diff view | filter by partition key or id (the `Filter` binding) |
| `ctrl+e` | diff view | export the diff (`.json`, `.csv`) |
| `esc` | each overlay | back one level |

- New catalog bindings: `s` (`TakeSnapshot`) and `v` (`Snapshots`). Neither is in
  `DefaultKeyMap`, the README, or reserved by plans 11–18: those hold `n`, `c`, `d`,
  `t`, `i`, `ctrl+g`, `ctrl+space`, `ctrl+s`, `ctrl+l`, `ctrl+b`, and `y`
  in the catalog for 18. Iteration 14's `a` and `x` now live inside its account
  switcher, not the catalog. No function key is bound.
- Everything else is an existing binding doing its usual job in a new overlay (`Select`,
  `Detail`, `Filter`, `Export`, `Close`) or a key that has meaning only inside the
  overlay. `d` means delete wherever it appears, as iteration 11 has it; `x` means stop,
  as it does in iteration 18's progress view; `tab` cycles a choice inside an overlay,
  as the export prompt's `Format` does.
- Overlays route like the help overlay: their branch in `handleOverlayKey` runs before
  the global shortcuts, so `s`, `d`, `x` and `q` typed there never reach the catalog.
- On a connection without `adapter.ItemScanner`, `s` and `v` do nothing and the help
  overlay lists neither.
- **The overlay is scoped to the account the session is on**, as iteration 14 scopes
  history: it lists that account's snapshots of the node under the cursor, and an
  overlay never switches accounts. To see another account's snapshots: `esc`,
  `ctrl+g`, `v`.
- Pruning and verifying are CLI-only. They are maintenance, and a key that deletes
  twenty snapshots does not belong one slip away from `s`.

### Long-running work

A capture is a value that advances one page at a time:

```go
capture, err := store.Begin(source, snapshot.CaptureOptions{Note: note})
progress, err := capture.Next(ctx) // one page read, hashed, written
// … until progress.Done: the snapshot is published
err = capture.Abort()              // removes what this capture wrote; releases the lock
```

The TUI wraps each `Next` in a `tea.Cmd`, exactly as `fetchPage` wraps
`Cursor.NextPage`, and issues the next when `SnapshotProgressMsg` arrives. The CLI
calls it in a loop. No goroutine, no channel, no shared state; `Update` never blocks
and the interface stays live between pages.

- Progress: phase (`sweep`, `fetch`, `scan`, `publish`), items read, RU so far, bytes
  read, bytes newly stored, and therefore the dedupe ratio so far.
- **A capture survives account switches, on iteration 18's pattern.** It holds its
  `adapter.Connection` and its account name from `Begin` and never asks which account
  is active; an account the session leaves stays connected (iteration 14). While the
  overlay is closed the status bar carries one field, through the shared
  `StatusBar.SetJob(label)` (see the one-job rule below), that belongs to the job and
  not to an account — `snapshot prod/sales.orders 41% (v)`
  — and `setActive` never touches it. `v` anywhere in the catalog, on any account,
  reopens the running capture's overlay rather than the list for the cursor's node.
  When a hidden capture ends the field reads `snapshot done (v)` or
  `snapshot failed (v)` until the overlay has been opened once.
- **Refused while a capture runs:** `x` in the switcher on the capture's account, with
  `a snapshot is using prod: cancel it first (v in the catalog)` under that row, as
  18 refuses it for a clone's accounts.
- **One background job per session, one mechanism.** Cloning (18), capture (this
  plan) and update and delete by query (21, 22) all scan or write at volume against
  one throughput, so they share a single `job` slot on the root model, with a kind
  (`clone`, `capture`, `update`, `delete`), one status bar field
  (`StatusBar.SetJob`), one quit guard and one switcher guard. Whichever of the four
  lands first introduces the slot; the others register a kind. There is no
  per-feature "is a clone running" check. Reopening stays per feature: `v` here, `y`
  for a clone, `w` for an update or delete.

  | While a capture runs | |
  |---|---|
  | `y` (clone) | refused: `a snapshot is running: clones wait for it (v)` |
  | `ctrl+r` on an `UPDATE` or `DELETE` statement | refused before its dry run: `a snapshot is running: updates wait for it (v)` |
  | a second `s` | refused with the same notice |
  | `SELECT`s, iteration 17's batches, browsing, history, exports | available |
  | `v`, diffs, exports and deletes of stores on disk | available; they read the disk only |

  The mirror holds: while any other job runs, `s` is refused with that job's notice
  and reopening key. Iteration 17 blocks keys while a batch is committing; a capture
  page that arrives then is processed as usual, since it writes nothing to the account.
- **Quitting mid-capture** follows 18: the first `q` or `ctrl+c` opens the overlay with
  `A snapshot is running. Quit again to cancel it and quit; nothing will be kept.`
- A page refused for rate arrives as iteration 18's `*adapter.ThrottledError`. The
  driver — the TUI's command or the CLI's loop — waits `RetryAfter` and calls `Next`
  again, up to `maxThrottles` (5) in a row; a page that failed wrote nothing, so the
  retry is safe. Any other error fails the capture.
- Cancel (`x` in the overlay, or the second quit) cancels the
  context, stops issuing `Next`, and calls `Abort`. Nothing was published, so nothing
  partial is ever listed. Blobs already packed are reclaimed by the next GC — or reused
  by the next attempt, which makes a retried capture cheap on disk though not in RU.
- One capture per container: the store's `lock` covers other processes (cron and the
  TUI), and the root model refuses a second `s` for a store it is already capturing
  with a status-bar notice. A database snapshot queues its containers.
- Late messages carry the capture's id, and one for a capture that was cancelled is
  dropped, as a `PageLoadedMsg` for a superseded `runID` is.
- A failed capture is `Abort`, the error in the overlay's top row, and the log.

## CLI

`cmd/snapshot.go`, wired in `NewRootCmd` like `newProfileCmd`, each subcommand its own
constructor with `RunE`, `Args`, `SilenceUsage` per `references/cli.md`. All seven ship
in this iteration; cron is half the reason the feature exists.

| Command | Does |
|---|---|
| `snapshot take <profile> <db>[.<container>] [--note s] [--full]` | one container, or every container of the database as a group |
| `snapshot list [<profile> [<db>[.<container>]]] [--json]` | snapshots with counts and the usage report; with no arguments, every store on disk, including accounts with no profile, marked as such |
| `snapshot diff <profile> <db>.<container> [<from> [<to>]] [--live] [-o file]` | summary to stdout; `-o` writes `.json` or `.csv`. Defaults: `previous` and `latest` |
| `snapshot export <profile> <db>.<container> [<snapshot>] -o file` | the items, `.jsonl` or `.json` |
| `snapshot delete <profile> <db>.<container> <snapshot>` | one snapshot, then GC |
| `snapshot prune <profile> <db>[.<container>] --keep-last N [--keep-daily D] [--dry-run]` | apply the policy, then GC |
| `snapshot verify <profile> <db>[.<container>] [--deep] [--rebuild-index]` | integrity check; non-nil error on any failure |

- A snapshot argument is an id, a unique prefix of one, `latest`, or `previous`.
- `<db>.<container>` splits at the first dot, as the editor's scope does. Names that
  contain a dot are given with `--database` and `--container` instead.
- Only `take` and `diff --live` connect, through the same `config.SecretResolver` and
  registry path as `profileLaunch`. The rest read the disk, and work for an account
  whose profile is gone.
- `take` prints one progress line to stderr when it is a terminal (`charmbracelet/x/term`
  is already a dependency) and nothing otherwise, then one summary line to stdout:
  `sales.orders 20260919T060000Z: 1 000 412 items, +212 −40 ~9 731, 15 412.80 RU, 2.6 MB new`.

```
0 6 * * *  alchemist snapshot take prod sales --note nightly \
           && alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30
```

### Removing a profile

Iteration 14 leaves orphans to whoever stores data per account, and iteration 15
answered for saved queries: keep them, report them, `--purge` removes them. Snapshots
follow that rule without a second flag. `alchemist profile remove prod` prints
`kept 30 snapshots (460 MB) in ~/.local/share/alchemist/snapshots/prod`, and
`--purge` removes that directory too — someone purging an account expects copies of
its production data to go with it. A profile added later under the same name adopts
the directory, as identity-follows-the-name requires. If it points somewhere else, the
first diff shows a different resource id in the definition and every version
mismatched, so the capture re-reads everything; correct, with no special case.

## Adapter contract

**This plan defines no scan of its own.** Iteration 18 owns the primitive —
`adapter.ItemScanner`, `ScanRequest`, `ScanPosition`, `ItemScan`, `ItemPage` — and the
first snapshot is exactly its full scan with a pack file as the sink. It also owns
`adapter.PartitionKeyValues` (item identity is those values plus the id),
`DefinitionReader`, and `ThrottledError`, all consumed here as they stand. Whichever
of 18 and 19 is built first introduces them, to 18's text.

Incremental capture needs two things a full scan does not, and they are added to
`ScanRequest`, where 18 says extensions belong:

```go
type ScanRequest struct {
    Container []string
    From      ScanPosition
    PageSize  int32

    // Since keeps only items modified at or after it, to the backend's clock
    // granularity. Zero keeps everything.
    Since time.Time
    // Projection is how much of each item comes back.
    Projection ScanProjection
}

type ScanProjection int

const (
    // ScanWholeItems is the zero value and iteration 18's behavior.
    ScanWholeItems ScanProjection = iota
    // ScanIdentity reduces each item to its id, the values at its partition
    // key paths, and its system fields, each where the whole item has it.
    ScanIdentity
)
```

Both zero values are today's full scan, so a clone is untouched. An enum rather than a
`KeysOnly bool`, because a flag that changes what comes back is a smell CLEAN_CODE
names. `ScanIdentity` items keep their shape — a nested key path stays nested, built in
Cosmos as an object literal in the projection — so `PartitionKeyValues` reads a swept
item exactly as it reads a whole one, and a path the item lacks is simply absent.

Iteration 21 adds a third field by the same rule, `Filter ScanFilter{Alias,
Predicate}` — a `WHERE` evaluated by the backend — and uses it *with* `ScanIdentity`
to select the targets of an `UPDATE` or `DELETE`. The identity shape carries what it
needs and no more: `id`, the partition key path values in their nested shape, and the
system fields, `_etag` among them, which is its optimistic-concurrency version. Its
`UNSET` dry run reads whole items instead, since it must see the field it removes.
Captures leave `Filter` zero.

Why not leave the sweep and the fetch as ordinary `Connection.Query` calls, whose
`Page.Raw` would carry the same bytes: `adapter.Query.Text` is adapter-native SQL, so
`internal/snapshot` would have to write Cosmos SQL and the bracket syntax for key
paths, and would stop being adapter-agnostic; and the mock answers any query text with
canned pages, so the sweep could not be tested without teaching the mock to parse SQL.
`PageBuilder` rendering a million rows of cells nobody reads is the lesser reason.

One shared helper, in `internal/adapter` beside `PartitionKeyValues`:

```go
// ItemMeta is what a backend records about an item outside its content.
type ItemMeta struct {
    Version  string    // changes on every write; the etag in Cosmos
    Modified time.Time
}

// SplitSystemFields returns item without its system fields, and what they said.
func SplitSystemFields(item json.RawMessage) (body json.RawMessage, meta ItemMeta, err error)
```

```go
// IsSystemField reports whether name is a top-level field the backend owns.
func IsSystemField(name string) bool
```

`IsSystemField` sits beside it over the same list, so the five names are spelled in
exactly one place; iteration 21's `CheckMutation` uses it to refuse `SET` and `UNSET`
on a system field.

Iteration 18 plans `clone.StripSystemFields` for the same five names. Two lists of
them would drift, and `snapshot` must not import `clone`, so the list lives once, in
`adapter`, and `clone.StripSystemFields` becomes the body half of this call.

## Scope

- `internal/canonical` (new, pure) — `Numbers`, moved from `query.canonicalNumbers`
  with its tests, and `Marshal`. `internal/query` imports it.
- `internal/snapshot` (new; imports `internal/adapter` interfaces and
  `internal/canonical`, never a concrete adapter, never the TUI), one file per concept:
  - `pack.go`, `index.go` — block writer and reader, the index with its offset table,
    rebuild.
  - `manifest.go`, `changes.go` — sorted entry streams, compose, undo.
  - `record.go`, `layout.go` — records, groups, `store.json`, path segments, modes.
  - `store.go` — `Open` (with orphan cleanup), the lock, publish.
  - `capture.go` — `Begin`, `Capture.Next`, `Abort`; full and incremental.
  - `diff.go`, `fields.go` — change lists; structural and line diffs.
  - `retain.go`, `verify.go`, `usage.go`, `export.go`.
  - `Newest`, the read-only lookup iterations 21 and 22 call (in `record.go`).
  - Sentinels: `ErrUnknownFormat`, `ErrLocked`, `ErrCorrupt`, `ErrNoSnapshot`,
    `ErrTooManyItems`, each wrapped with the store and the file.
- `internal/adapter` — `ScanRequest.Since`, `ScanRequest.Projection`, `ScanProjection`,
  `ItemMeta`, `SplitSystemFields`; iteration 18's contracts if it has not landed.
- `internal/adapter/cosmos` — iteration 18's `scan.go` grows the two request fields:
  the `ScanIdentity` projection built from the container's partition key paths,
  `WHERE c._ts >= @since` with `@since` as a `QueryParameter`.
- `internal/adapter/mock` — on iteration 18's `WithItems` and in-memory containers:
  `(*Adapter).PutItem` and `DeleteItem`, which bump a per-item version and stamp the
  modified time from an injectable clock (`WithClock`), so a test can snapshot, mutate,
  snapshot, diff with no network. Its `ScanItems` honors `Since` and `Projection`.
  Canned `Query` results are untouched.
- `internal/export` — nothing new exported beyond what the diff writer reuses
  (`ResolvePath`, `ErrFileExists`).
- `internal/tui`
  - `Options.Snapshots` (the root directory, built in `cmd`); `snapshots.go` holds the
    handlers, as `history.go` and `export.go` do for theirs.
  - `overlaySnapshots`, `overlayDiff`, `overlayItemDiff`; the note prompt and the
    delete confirm are states of the first, like the export prompt's saving state.
  - Messages: `SnapshotsLoadedMsg`, `SnapshotProgressMsg`, `SnapshotDoneMsg`,
    `DiffLoadedMsg`, `ItemDiffLoadedMsg`, and `OpSnapshot` failures through `ErrMsg` —
    each carrying account and container path, per iteration 14.
  - `KeyMap`: `TakeSnapshot`, `Snapshots` in the Catalog section; `SnapshotKeys()` and
    `DiffKeys()` hint-line groups like `HistoryKeys()`. Drift-guard tests cover them.
- `internal/tui/panes` — `Snapshots`, `Diff`, `ItemDiff`; `StatusBar.SetJob`, and in
  `internal/tui` the `job` slot, if no other plan has introduced them.
- `internal/config` — `snapshot_dir` on `Config`; `snapshot_max_items` on `Profile`.
  The `writers` profile key (18's `clone_writers`, renamed by 21) is not used: a
  capture is a sequential read.
- `cmd` — `snapshot.go`; `root.go` builds the root directory and passes it in;
  `profile.go` reports and purges snapshots on `remove`.
- `README.md` — a "Snapshots" section: what a snapshot guarantees and does not, where
  it lives, that it is unencrypted, what it costs in RU, the cron example, the keys.

## Out of scope

- **Restore or rollback into an account.** Export to a file is in. Writing items back
  is iteration 18's `ItemWriter.OpenItemSink` fed from a snapshot instead of a scan,
  with iteration 17's batches for the careful case; it needs its own review screen and
  its own plan.
- Encryption at rest, and remote or shared snapshot stores.
- The change feed in any mode, and continuous tailing.
- Self-throttling, priority levels, or an RU budget for a capture.
- Resuming an interrupted capture's RU. Its disk work is reused; its reads are not.
- Skipping the sweep on a proven-unchanged count.
- A codec stronger than deflate, a trained dictionary, or deduplication *across*
  containers.
- A diff between two accounts or two containers. The differ would do it; the UI and
  the identity questions are another iteration's.
- Automatic pruning, a scheduler, or a daemon. Cron exists.
- Containers past `snapshot_max_items`: an on-disk sort for the sweep is the fix, and
  is not built until someone has that container.

## Relationship to other iterations

- **8, export.** Reuses path resolution, the no-overwrite rule and `panes.Export`'s
  prompt. Snapshot contents are streamed by `internal/snapshot` itself.
- **10, cross-container.** `canonicalNumbers` moves to `internal/canonical`; behavior
  and tests unchanged.
- **11, catalog management.** `ThroughputEditor.Throughput` supplies the throughput
  part of a definition. A container deleted with `d` keeps its snapshots; they are
  then the only copy, reachable from `snapshot list` and, if a container of that name
  is created again, from `v`.
- **12, info view.** No dependency. `Inspector` renders strings for people; a diff
  needs the document, which is `DefinitionReader`'s.
- **14, multiple accounts.** Stores are keyed by profile name under its identity
  contract, which its revision to an account switcher leaves unchanged. The overlays
  show the current account only and never switch it; nothing here reads the tree for
  an account, since the catalog shows one. Messages carry the account name so a
  capture that outlives a switch lands in the right place, and `setActive` is where
  an open snapshots list would be reloaded — though an overlay is never open across a
  switch. The switcher's `x` is refused on an account a capture is using.
- **15, saved queries.** Its orphan rule — keep, report, `--purge` — is extended, not
  duplicated: one flag removes both.
- **16, multi-way joins, and 20, CTEs and join types.** No interaction.
- **17, transactions.** Its `read_only` key (`Profile.ReadOnly`, absent meaning only
  loopback endpoints are writable) does not apply: a snapshot writes nothing to the
  account, so `s` and `snapshot take` work on a read-only account. A read-only
  production profile on a cron line is exactly the expected use. `ctrl+b` and the
  in-flight batch guard are untouched.
- **18, cloning.** Hard dependency on its contracts, not its code. Consumed unchanged:
  `ItemScanner`, `ItemScan`, `ItemPage`, `ScanPosition`, `PartitionKeyValues`,
  `ErrNoPartitionKey`, `DefinitionReader`, `ContainerDefinition`, `PolicyDocument`,
  `ThrottledError`, the mock's `WithItems`. **Added by this plan:** the fields
  `ScanRequest.Since` and `ScanRequest.Projection`, the type `ScanProjection` with
  `ScanWholeItems` and `ScanIdentity`, and `adapter.SplitSystemFields` with
  `ItemMeta`, which `clone.StripSystemFields` should call rather than keep a second
  list, and `adapter.IsSystemField`. The `job` slot and `StatusBar.SetJob` are shared
  as "Long-running work" sets out; `SetClone` in 18 is that field under its first name.
  `ScanPosition` is ignored here: a capture that stops is abandoned, not resumed.
  `internal/snapshot` does not import `internal/clone`, nor the reverse.
- **21, update by query, and 22, delete by query.** Soft, in both directions. They use
  `ScanIdentity`, `SplitSystemFields` and `IsSystemField` as they stand, add
  `ScanRequest.Filter`, and share the one-job slot. Their review overlay shows the age
  of the newest snapshot of the target container, or that there is none, when this
  plan has landed (`Options.Snapshots` is set). The call behind that line is
  `snapshot.Newest(root, account, database, container) (Record, error)`: it resolves
  the store directory, reads the newest file in `records/`, and returns
  `ErrNoSnapshot` when there is none. It opens no pack, no manifest and no lock, so it
  is safe to call from a `tea.Cmd` on every review. The pairing is the undo story
  those plans do not have: `s` before the run, then after it a snapshot-to-snapshot
  diff, or `snapshot diff --live` from a shell, shows exactly which items and fields
  the statement changed. Their preview rows have the shape of this plan's structural
  diff (`{op, path, before, after}`) and are drawn by the same renderer.

## Steps

Each step ships and is tested alone; the first usable tool is step 7.

1. `internal/canonical`: move `Numbers`, add `Marshal`; hash-stability tables; fuzz.
2. Packs and indexes: write, read, rebuild; the fuzzed reader. **Measure the
   compression ratio** of block sizes 64 KB–1 MB on the seed data and on a fixture of
   realistic documents; record the numbers and the chosen block size here, and correct
   the size table.
3. Manifests, change sets (compose, undo), records, `Store.Open` with orphan cleanup,
   the lock, atomic publish; crash-safety tests.
4. `adapter.SplitSystemFields`; iteration 18's scan contracts and mock items if they
   are not in yet, plus `PutItem`/`DeleteItem`; full capture through `Capture.Next`
   with definitions; the restore property test.
5. `ScanRequest.Since` and `Projection`, mock then Cosmos, and the integration test.
   **Measure RU** for the full scan and for the sweep with and without `_etag` on a
   real account; record the figures here and settle the sweep's projection.
6. Diff: change lists from change sets, structural and line item diffs, the definition
   diff, diff and content export.
7. CLI: `take`, `list`, `diff`, `export`, database groups. Ships: cron snapshots, every
   capture a full scan.
8. Retention: `delete`, `prune`, GC and compaction, `verify`, usage in `list`.
9. Incremental capture (sweep, compare, fetch, settle) and `--full`. Ships: the RU
   saving. Deliberately after 4–8, so there is a trusted full capture to test it
   against: for any mutation sequence, incremental and full must publish equal
   manifests.
10. TUI: `s`, `v`, the snapshots overlay, progress, cancel, delete.
11. TUI: diff view, item diff, definition diff, filter, export.
12. `profile remove` reporting and `--purge`; README; help sections; plan statuses.

## Testing

**Unit — `canonical`:** a table of pairs that must hash equal (key order at every
depth, whitespace, `1`/`1.0`/`1e0`, escaped and literal forms of one string) and pairs
that must not (array order, `null` against absent, `"1"` against `1`, 2^53 + 1 against
2^53). Fuzz: `Marshal` is idempotent, and invariant under shuffling object keys.

**Unit — packs:** round-trip of 0, 1, and 10 000 bodies, a body larger than a block, an
empty body; a lookup through a rebuilt index equals one through the written index; a
flipped byte fails the CRC; a body swapped for another fails the rehash. Fuzz the pack
and index readers: arbitrary bytes return `ErrCorrupt` or `ErrUnknownFormat`, never a
panic, never an allocation sized by an unchecked length.

**Unit — manifests and change sets:** sorted-order invariant; `undo(apply(m, c), c) ==
m`; compose is associative; changed-and-reverted drops out; an undefined partition key
component and `null` are different keys; hierarchical keys order by component.

**Unit — store:** a capture abandoned before each publish step leaves `Snapshots()`
unchanged and `Open` cleans the debris; a second `Begin` gets `ErrLocked`; a stale lock
is taken over; an unknown `FORMAT` is refused; modes are `0700`/`0600`.

**Unit — retention:** deleting the oldest, a middle and the head snapshot leaves every
other snapshot's exported contents byte-identical; **after any sequence of captures
and deletes, GC leaves every hash reachable from a surviving snapshot readable**
(randomized, seeded); a pack under half live is rewritten and one over is not; prune
keeps the union of `--keep-last` and `--keep-daily` and always the head.

**Property — restore:** for random item sets and random mutation sequences,
`export(snapshot)` equals the canonical form of exactly the items the mock held when it
was captured; and an incremental capture publishes the same manifest a full one does.

**Unit — `adapter.SplitSystemFields`:** the five fields leave and nothing else does; a
nested field of the same name stays; an item with none is returned unchanged with a
zero `ItemMeta`; `_ts` becomes UTC seconds; not-an-object is an error.
`IsSystemField` is true for exactly the names `SplitSystemFields` removes.

**Unit — `snapshot.Newest`:** the newest of several records; `ErrNoSnapshot` for an
empty or missing store; a record without a pack directory still answers; a held `lock`
does not block it.

**Unit — diff:** added, removed, modified, a no-op replace is not a change, a changed
partition key is remove plus add, composing three change sets equals diffing the ends;
item diff tables for nested objects, equal- and unequal-length arrays, number
respelling, type change; every structural diff applied as a JSON Patch to *before*
yields *after*; the line diff ignores a trailing comma.

**Size — the efficiency claim, as a test.** 20 000 generated order documents of about
1 KB, 30 snapshots, 1% rewritten between each, real files in `t.TempDir()`:
- the store after 30 snapshots is at most **1.5×** its size after the first;
- it is at least **20×** smaller than the 30 canonical exports together;
- a snapshot of an unchanged container adds less than **4 KB**;
- after pruning to the last 7 and GC, it is at most **1.2×** a fresh store holding the
  same 7.
A benchmark reports bytes per item and per changed item, so a regression is a number.

**TUI, mock adapter** (`internal/tui/test/snapshots_test.go`, stores in `t.TempDir()`):
- `s` on a container prompts, captures page by page, and the overlay lists one
  snapshot; keys typed between pages are handled.
- Snapshot, `PutItem`/`DeleteItem`, snapshot, `enter`: the diff lists exactly those
  items with the right signs; `enter` on a modified one shows `-`/`+` lines.
- `x` mid-capture: nothing is listed, the lock is released, a new `s` works.
- A second `s` while a capture runs is refused with a notice, and so is `s` while any
  other kind holds the `job` slot; with a capture running, `y` and `ctrl+r` on an
  `UPDATE` are refused and a `SELECT` runs; the first `q` mid-capture opens the overlay
  with the warning, the second quits, and nothing is published.
- With a capture running and the overlay closed, the status bar shows the snapshot
  field; after a switch to another account it is unchanged, `v` reopens the running
  capture, and `x` in the switcher on the capture's account closes nothing and shows
  the refusal.
- A late `SnapshotProgressMsg` for a cancelled capture is dropped.
- `space` on three rows keeps the last two marks; `enter` with none marked diffs
  against the parent; on the first snapshot it says there is nothing to compare.
- `tab` cycles the kind filter; `/` narrows by id; `ctrl+e` writes `.json` and `.csv`
  and refuses an existing file.
- `d` asks first; `esc` keeps the snapshot.
- A connection without `ItemScanner`: `s` and `v` do nothing and are absent from help.
- A changed policy or throughput shows a definition row and marks `def`; a changed
  size estimate alone does neither; a connection without `DefinitionReader` shows
  "definition not captured".
- After a switch to another account `v` lists that account's snapshots only, and a
  capture started before the switch still publishes under the account it began on.
- A `ThrottledError` page is retried after its delay; the sixth in a row fails the
  capture and publishes nothing.
- `s`, `d`, `x`, `q` inside an overlay never reach the catalog.
- Every new binding is grouped exactly once and appears in help and the README.

**Unit — `cmd`:** argument parsing for every subcommand, including dotted names through
the flags and snapshot references by prefix, `latest` and `previous`; `list` on an
orphaned account; `profile remove` reports and `--purge` removes.

**Integration (emulator)** — `test/integration/snapshot_test.go`, `//go:build
integration`, on `make emulator-seed` data: snapshot `sales.orders`; through the seed
client replace two orders, create one, delete one; snapshot again; assert the second
capture was incremental, the diff is `+1 −1 ~2` with the expected ids and field paths,
and its RU is non-zero and below the first's. A database snapshot of `sales` produces
one group with every seeded container. A `ScanIdentity` scan returns an id, a version
and a modified time for every item, nothing else but its key paths, and no key value
for an item seeded without one; a `Since` scan returns exactly the items touched after
the first capture.

**Manual checklist:**
- [ ] Emulator: `s` on `sales.orders`, edit an item elsewhere, `s`, `enter`, read the
      field diff.
- [ ] `alchemist snapshot take` from a shell with no terminal attached prints one line.
- [ ] Kill a `take` with `kill -9` mid-capture; `list` shows nothing new and the next
      `take` succeeds.
- [ ] Ten snapshots of an untouched container: `list` shows the ratio climbing and
      `du -sh` agrees with the on-disk figure.
- [ ] `prune --keep-last 3`, then `verify --deep` passes and `du` went down.
- [ ] `ls -l` under the snapshot directory shows no group or world permission bits.
- [ ] Against a real account: the RU the progress line reports matches the portal's.
- [ ] 80×24: the three overlays fit, nothing wraps into a border.

## Acceptance criteria

- A container or a database can be snapshotted from the catalog and from the CLI, and
  any two snapshots of a container diff into added, removed and modified items, a
  field-level diff per modified item, and definition changes.
- **Storage:** 30 snapshots at 1% churn occupy at most 1.5× the first snapshot and at
  least 20× less than 30 exports; an unchanged snapshot adds under 4 KB; both are
  asserted by the size test, and the overlay and `snapshot list` report logical bytes,
  on-disk bytes and their ratio for every store.
- **RU:** a later snapshot reads every key once and only the bodies modified since a
  bound taken from the data; the integration test shows it costing less than the first,
  and the measured figures replace the planning figures in this document.
- Deletes, same-second rewrites and no-op replaces are each reported correctly: removed,
  modified, and not at all.
- A cancelled, failed or killed capture publishes nothing and needs no repair; no
  sequence of delete, prune and GC makes a surviving snapshot unreadable; `verify
  --deep` proves it.
- A neighbor diff reads one change set, and no diff reads the full manifest.
- The UI and the README state what a snapshot guarantees — per-item versions from a
  stated window, not a point in time — and that snapshots are unencrypted, `0600`.
- `Update` never blocks: a million-item capture leaves the catalog, the editor, `esc`
  and `ctrl+c` responsive between pages.
- No new module in `go.mod`. `internal/snapshot` imports no TUI and no concrete
  adapter; `internal/tui` imports the `internal/adapter` interfaces only.
