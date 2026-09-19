# Iteration 8 — Export + Polish

## Goal

Get result sets out of the TUI (JSON/CSV files), make SQL easier to read in the editor,
and bring the README up to reality.

## Scope

- `internal/export/export.go` — exporters over the fetched result set:
  - `JSON`: array of the original documents from `Page.Raw` (lossless, pretty-printed).
  - `CSV`: locked column order from the results pane; nested values as compact JSON
    strings; RFC 4180 quoting via `encoding/csv`.
  - Input is `[]adapter.Page` (all pages fetched so far) — export never triggers new
    fetches; the status bar's "+more" tells the user the set is partial.
- TUI wiring: `ctrl+e` in the results pane → filename prompt (textinput overlay,
  extension picks format, `tab` switches it, default `results.json` in cwd); success reported in the
  status bar, and a failure in the prompt itself, which stays open so the name can be
  corrected; refuses to overwrite unless the name ends in `!` (`results.json!`).
- Editor syntax highlighting:
  - `bubbles/textarea` cannot style regions of editable text, so: keyword highlighting
    on a **render-styled preview** — the editor buffer stays plain while focused;
    when unfocused, the pane renders a highlighted view (Cosmos SQL keywords in
    Amethyst, strings in Verdigris, numbers in Copper) using a small keyword styler
    (evaluate `alecthomas/chroma` SQL lexer vs a ~40-keyword hand list; pick the
    lighter one that handles Cosmos SQL keywords like `VALUE`, `IN`, `JOIN`, `TOP`).
    **Decided:** the hand list. `internal/query` already lexes Cosmos SQL and holds
    its keyword list for scope parsing, so `query.Spans` reuses both; chroma would
    be a new dependency whose SQL lexer knows none of the Cosmos keywords.
- README rewrite: accurate feature list, install, quickstart against the emulator,
  profile setup, keybinding table (generated from `keys.go` content), screenshot or
  VHS-generated GIF, adapter-authoring section pointing at `internal/adapter`.
- Comment pass on the code this iteration touches, to the rule in `CLAUDE.md`: a
  comment stays only where the code cannot say it. `docs/plan` statuses updated.

## Out of scope

- Excel/Parquet export, clipboard copy, streaming export of un-fetched pages.

## Steps

1. Exporters + golden-file tests first (pure functions, no TUI).
2. Filename-prompt overlay and results-pane wiring.
3. Highlighting spike (chroma vs hand list) behind the preview-render approach; commit
   whichever passes the test cases with less code.
4. README + VHS tape (`docs/demo.tape`) recorded against the mock adapter for
   reproducibility.

## Testing

**Unit:**
- Golden files: fixture pages (nested objects, nulls, unicode, commas/quotes/newlines
  in strings, missing keys across pages) → expected `.json` / `.csv` committed under
  `internal/export/test/testdata/`.
- JSON export round-trips: `json.Valid` and doc count matches row count.
- Overwrite guard: existing file → error without `!` confirm.
- Highlighter: keyword/string/number spans for representative Cosmos SQL; no panic on
  malformed input (fuzz seed corpus).

**Manual checklist:**
- [ ] Query the emulator, `ctrl+e`, export both formats; open the CSV in a spreadsheet
      and `jq` the JSON.
- [ ] Unfocused editor shows highlighted SQL; focused editing stays responsive.
- [ ] README quickstart followed verbatim on a clean machine gets to first query.

## Acceptance criteria

- Exports are deterministic (same result set → byte-identical file, modulo trailing
  newline).
- Highlighting adds no perceptible input latency (render only on blur/change, not per
  keystroke while focused).
- README contains no stale commands (every documented command exists and works).
