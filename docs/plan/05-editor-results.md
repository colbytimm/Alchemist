# Iteration 5 — Editor + Results + Run Query

## Goal

The core harlequin loop: type Cosmos SQL in the editor, run it, and page through the
results — with rows, RU, and elapsed time in the status bar, and errors rendered inline
instead of crashing the TUI.

## Scope

- `internal/tui/panes/editor.go` — editor pane on `bubbles/textarea`:
  - Plain text this iteration (highlighting is iteration 8).
  - `ctrl+r` runs the buffer; `esc` returns focus to the previous pane. No binding uses
    a function key, and `ctrl+enter` cannot be one: terminals send a bare CR for it.
  - Placeholder text hints scope syntax: `SELECT * FROM db.container AS c ...`.
- Run-query flow (root model orchestrates):
  1. Resolve scope: explicit `db.container` in the query (via `internal/query`)
     wins; otherwise the catalog-selected container; neither → inline error
     "no container in scope".
  2. `QueryStartedMsg` → status bar spinner; `Connection.Query` + first
     `Cursor.NextPage` in a `tea.Cmd` with timeout context.
  3. `PageLoadedMsg{Page, Append bool}` → results pane renders; `QueryFailedMsg{Err}` →
     error panel in the results pane (service error text preserved, e.g. Cosmos syntax
     errors).
  4. A new run cancels the previous cursor (context cancellation) and closes it.

  Queries here target one container and run **cross-partition by default** (the
  adapter's responsibility, iteration 3). Multi-container queries are rejected with a
  clear message in this iteration; iteration 10 replaces this flow's "execute query"
  step with "execute plan" to add client-side cross-container union/join without
  changing the panes.
- `internal/tui/panes/results.go` — results pane:
  - Viewport-based table render (bubbles `table` evaluated first; custom render if
    column truncation/horizontal scroll needs it): sticky header, row cursor,
    column width capping with ellipsis, `h/l` or `←/→` horizontal scroll.
  - **Fetch-more**: scrolling past the last loaded row (or pressing `m`) triggers
    `Cursor.NextPage` when `HasMore()`; appended pages keep locked column order.
  - `enter` on a row opens a raw-JSON detail overlay (pretty-printed from `Page.Raw`).
- `internal/tui/panes/statusbar.go` — grows: row count (`120 rows (+more)`),
  cumulative RU in Verdigris, elapsed time, running spinner.

## Out of scope

- History persistence (iteration 7), export (iteration 8), syntax highlighting
  (iteration 8), multiple editor buffers (future).

## Steps

1. Extend `messages.go` with the query message set; implement the run flow in the root
   model with explicit state (idle / running / loaded / failed) and cursor lifecycle.
2. Editor: textarea config (theme colors, no line numbers v1), key handling — careful
   that run keys fire while the textarea has focus but plain `q` does not quit there.
3. Results: render pipeline from `Page` (columns/rows already pre-shaped by the
   adapter), fetch-more, detail overlay.
4. Status bar wiring from `Stats`.

## Testing

**Unit** (mock adapter scenarios from iteration 2):
- Happy path: run → spinner state → first page rendered → status shows rows/RU/elapsed.
- Multi-page: fetch-more appends 3 pages, `HasMore` flips off, "+more" indicator clears.
- Error path: `WithError(query)` mock → results pane shows error panel; editor keeps
  buffer; subsequent successful run clears the error.
- Re-run mid-flight: second run cancels first (mock latency + assert only second
  results render); cursor `Close` called.
- Scope resolution precedence: explicit query scope beats catalog selection; no scope →
  inline error, no adapter call made.
- Detail overlay renders valid pretty JSON for the row under cursor.

**Manual checklist** (against the emulator, seeded via integration fixtures; walked by
`test/integration/tui_test.go` under `make emulator-up && make test-integration`):
- [x] `SELECT * FROM c` with a container selected in the catalog → rows, RU > 0,
      elapsed shown.
- [x] `SELECT * FROM sales.orders AS c WHERE c.pk = "x"` without a catalog selection
      works.
- [x] Scroll to bottom on a >100-row container fetches the next page.
- [x] Intentional syntax error shows the Cosmos error message inline; TUI stays alive.
- [x] `enter` on a row shows the raw document; `esc` closes.

## Acceptance criteria

- No blocking I/O in any `Update`; a slow query never freezes input (spinner animates,
  `ctrl+c` still quits).
- Query errors never terminate the program; nothing in `internal/tui` panics.
- RU displayed is the cumulative charge across fetched pages for the current result set.
