# Iteration 7 — Query History

## Goal

Every executed query is recorded to a local, append-only log; a history overlay lets
the user browse, filter, and recall past queries into the editor or re-run them
directly.

## Storage design

`~/.local/state/alchemist/history.jsonl` (respect `$XDG_STATE_HOME`), one JSON object
per line:

```json
{"ts":"2026-08-27T21:04:05Z","profile":"emulator","scope":["sales","orders"],
 "query":"SELECT * FROM c WHERE c.pk = \"x\"","ok":true,"rows":42,"ru":12.6,
 "elapsed_ms":180}
```

- Append-only writes (single `O_APPEND` write per entry — safe enough for one process;
  no locking v1).
- Load: read tail (last N=500 entries) newest-first; **corrupt/truncated lines are
  skipped, never fatal**.
- Trim: on startup, if the file exceeds ~5000 entries, rewrite keeping the newest 2500.
- Failed queries are recorded too (`ok:false`, error string truncated to 200 chars) —
  recalling a failed query to fix it is a primary use case.
- Query text only — never keys or connection info.

## Scope

- `internal/history/history.go` — `Entry` struct, `Store` interface
  (`Append(Entry) error`, `Recent(n int) ([]Entry, error)`), JSONL implementation,
  no-op implementation (`--history=false` / unwritable state dir).
- `internal/tui/panes/history.go` — history overlay (`ctrl+o`):
  - `bubbles/list`-style scrollable entries: relative time, ok/fail glyph
    (✓ Verdigris / ✗ Cinnabar), scope, first line of query, RU.
  - `/` filter (substring match on query text and scope).
  - `enter` → load query + scope into the editor (does not run); `ctrl+r` → load
    and run immediately; `esc` closes.
- Root model: append an entry on every `PageLoadedMsg` (first page) and
  `QueryFailedMsg`; recording failures must not interfere with the render path
  (fire-and-forget `tea.Cmd`; a history write error logs a warning, never surfaces
  modally).

## Out of scope

- Named/saved queries with descriptions (the old CLI's `saved_query` table) — a future
  iteration can promote a history entry to a named saved query.
- Cross-machine sync, dedup.

## Steps

1. Store: write + tail-read + trim + corruption tolerance, fully table-driven tests.
2. History pane model, fed by `Store.Recent` via `tea.Cmd`.
3. Wire recording into the query flow and recall into the editor; add `--history`
   (on by default; `--history=false` turns it off, since flags are named positively).

## Testing

**Unit:**
- Store round-trip: append N, `Recent(n)` returns newest-first with correct fields.
- Corruption: file with garbage line + half-written last line → both skipped, rest load.
- Trim triggers at threshold and preserves newest entries.
- Unwritable dir → constructor returns no-op store + warning (TUI unaffected).
- History model: filter narrows list; `enter` recalls the entry (assert editor
  receives text + scope); `ctrl+r` additionally triggers the run flow.
- Recorded entry for a failed query has `ok:false` and no panic on nil stats.

**Manual checklist** (walked by `TestIntegrationQueryHistory` in
`test/integration/tui_test.go` under `make emulator-up`, against the emulator):
- [x] Run 3 queries (1 failing) against the emulator; `ctrl+o` shows all 3, newest first.
- [x] Filter by container name narrows the list.
- [x] `enter` recalls into the editor with scope restored; `ctrl+r` re-runs.
- [x] `jq . < ~/.local/state/alchemist/history.jsonl` parses every line.

## Acceptance criteria

- History I/O never blocks or breaks the query flow (all failure paths degrade to
  logging).
- No sensitive material in the history file (query text, scope, stats only).
- Overlay opens in <50ms with a 5000-entry file (tail-read, no full parse of old data).
