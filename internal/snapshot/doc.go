// Package snapshot keeps snapshots of a container's items and definition on
// disk, and diffs them. The cost of a snapshot is proportional to what
// changed since the one before it: bodies are stored once per container,
// by hash, in compressed packs; only the newest snapshot has a full
// manifest, and every other is reached from it through change sets. A
// snapshot of a live container is per-item versions read in a stated
// window, never a point in time. Snapshots are not encrypted: they are
// protected by file permissions (0700 directories, 0600 files) alone.
//
// # Layout
//
//	<root>/<account>/<database~hash>/
//	  groups/<id>.json                 a database snapshot
//	  <container~hash>/
//	    FORMAT                         "alchemist-snapshot-store 1"
//	    store.json                     the real names
//	    lock                           present while a capture, delete or prune runs
//	    records/<id>.json              a snapshot; its rename publishes it
//	    changes/<parent>..<id>.changes parent → snapshot
//	    manifests/<id>.manifest        the newest snapshot's only
//	    packs/<capture>-0001.pack      bodies, and their index
//	    packs/<capture>-0001.idx
//
// A snapshot id is its capture's start in UTC, 20260919T060000Z.
//
// # Format
//
// Every binary file starts with one ASCII line naming its kind and version;
// integers after it are little-endian. A reader refuses a version it does
// not know with ErrUnknownFormat.
//
//   - .pack, "alchemist-pack 1": blocks back to back, each a uint32
//     compressed length, uint32 raw length, uint32 item count, uint8 codec
//     (1 = deflate) and uint32 CRC-32 of the compressed bytes, then those
//     bytes. Decompressed: count uvarint body lengths, then the bodies.
//   - .idx, "alchemist-pack-index 1": uint32 entry count, 256 uint32
//     cumulative first-byte counts, then entries sorted by the 16-byte
//     prefix of the body's SHA-256, each followed by a uint32 block offset
//     and a uint16 position in the block. It is derived data: every read
//     rehashes the body it finds against the full hash asked for.
//   - .manifest, "alchemist-manifest 1": one deflate stream of entries in
//     key order, each a uvarint key length, the key, the 32-byte body hash,
//     the 8-byte version fingerprint, a uvarint body length and a varint
//     modified time in Unix seconds; then a uint64 entry count and the
//     SHA-256 of the raw stream.
//   - .changes, "alchemist-changes 1": the same framing; each record is a
//     key, a flags byte (1 before, 2 after), then the entries present.
//   - records, groups and store.json are JSON with a "format": 1 field.
//
// An item's key is its partition key values, then its id, each a
// uvarint-length-prefixed canonical JSON value; a value the item lacks is
// a zero-length component. The version fingerprint is the first 8 bytes
// of the SHA-256 of the backend's version string.
//
// # Publishing
//
// A capture writes packs, then the change set and the manifest, each
// through a synced temporary file and a rename; then renames its record
// into place, which is the commit; then removes the parent's manifest.
// Open removes whatever an interrupted capture or delete left and nothing
// else, so no state ever needs repair by hand.
package snapshot
