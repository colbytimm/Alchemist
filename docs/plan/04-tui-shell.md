# Iteration 4 — TUI Shell + Catalog Tree

## Goal

The harlequin-style three-pane layout with focus management, a central keymap, a help
overlay, a status bar, and a working lazy-loading catalog tree — all driven through the
adapter interfaces (mock adapter in tests; any registered adapter at runtime).

## Layout

```
┌─ Catalog ──────┐┌─ Editor ────────────────────────────┐
│ ▾ 󰆼 sales      ││ SELECT * FROM c                     │
│   ▾  orders   ││                                     │
│     pk: /id    │└─────────────────────────────────────┘
│   ▸  custome… │┌─ Results ───────────────────────────┐
│ ▸ 󰆼 telemetry  ││ (results table)                     │
│                ││                                     │
└────────────────┘└─────────────────────────────────────┘
 profile ▪ scope ▪ rows ▪ RU ▪ elapsed          ? help
```

Icons (database/container glyphs, chevrons) come from `internal/theme` with plain-ASCII
fallbacks for terminals without Nerd Font glyphs.

## Scope

- `internal/tui/app.go` — root model: owns pane sizing (catalog ~28 cols, right column
  split editor/results), focus state, and message routing. **No business logic**; it
  delegates to pane models and converts adapter calls into `tea.Cmd`s.
- `internal/tui/keys.go` — central `key.Binding` map (single source of truth, feeds the
  help overlay):
  | Key | Action |
  |---|---|
  | `tab` / `shift+tab` | cycle focus Catalog → Editor → Results |
  | `F1` or `?` (when not editing) | help overlay |
  | `F2` | focus editor |
  | `F5` / `ctrl+enter` | run query (iteration 5) |
  | `F8` | history overlay (iteration 7) |
  | `enter` / `space` | expand/collapse catalog node |
  | `r` (in catalog) | refresh node |
  | `ctrl+c`, `q` (outside editor) | quit |
- `internal/tui/messages.go` — typed messages: `CatalogLoadedMsg{Parent, Nodes}`,
  `ScopeChangedMsg{Scope}`, `ErrMsg{Op, Err}`, plus iteration-5 query messages.
- `internal/tui/panes/catalog.go` — catalog pane: tree state (expanded set, cursor),
  lazy `Children` loading with a themed spinner per loading node, selection of a
  container sets the active scope (shown in the status bar and used as the default
  query target).
- `internal/tui/panes/statusbar.go` — profile ▪ scope ▪ placeholder stats.
- `internal/tui/panes/help.go` — `bubbles/help`-based overlay generated from `keys.go`.
- `internal/logging/logging.go` — `charmbracelet/log` to
  `~/.local/state/alchemist/alchemist.log` while the TUI owns the terminal; `--verbose`
  raises level to debug.
- `cmd/root.go` — grows a `--adapter` flag (default `cosmos`; `mock` available for
  development) and temporary `--endpoint`/`--key`/`--connection-string` flags until
  profiles land in iteration 6.

## Out of scope

- Query execution and results rendering (iteration 5) — the editor and results panes
  render as styled placeholders with correct focus behavior.

## Steps

1. Keymap + messages first; then the root layout with placeholder panes and focus
   cycling; then the status bar and help overlay.
2. Catalog tree: render from cached nodes; on expand of an unloaded node return a
   `tea.Cmd` calling `Catalog.Children` with a timeout context; handle `ErrMsg` by
   rendering the error inline under the node.
3. Wire `cmd/` flag plumbing and adapter selection via the registry.

## Testing

**Unit** (`internal/tui/test/`, `internal/tui/panes/test/`, mock adapter throughout):
- Focus cycling order and per-pane key routing (keys go to the focused pane only).
- Catalog: expand triggers load exactly once; loaded children cached; `r` re-fetches;
  error from mock renders in view; selecting a container emits `ScopeChangedMsg`.
- Help overlay lists every binding in `keys.go` (drift test: help content is generated,
  not hand-written).
- `WindowSizeMsg` resize: panes never render wider than the terminal (no wrapping
  artifacts at 80×24 minimum).

**Manual checklist:**
- [ ] `./bin/alchemist --adapter mock` — expand/collapse fixture tree, focus cycling,
      help overlay, resize behaves.
- [ ] `./bin/alchemist --adapter cosmos --connection-string <emulator>` — real databases
      and containers appear; partition key shown on expand.

## Acceptance criteria

- `internal/tui/**` imports `internal/adapter` but not `internal/adapter/cosmos`
  (depguard enforces).
- All I/O flows through `tea.Cmd`s — `Update` never blocks.
- Each pane model lives in its own file under ~300 lines.
