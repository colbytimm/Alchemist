# Iteration 23 — Live Syntax Highlighting and Diagnostics

## Goal

Highlight the query while it is being typed, not only after the editor loses focus,
and mark what Alchemist does not recognize with a red squiggle, the way a code editor
does. The highlighting tells the kinds of token apart (clause keywords from operators,
functions from fields, a batch's keywords from a query's), takes every color from the
theme, and never makes typing slower.

Iteration 8 highlights the editor only when it is blurred: `bubbles/textarea` cannot
style regions of editable text, so the focused buffer stays plain. Iteration 13 found
the same wall for the cursor's screen position. This iteration gets past it without
forking the textarea, keeping the textarea for editing, wrapping, scrolling and the
cursor, and painting its rendered rows.

## What is highlighted

Every class below is a token the lexer or a light scan can decide on its own; no
highlight waits on the catalog, a query run, or the network.

| Class | Examples | Theme role |
|---|---|---|
| Clause keyword | `SELECT`, `FROM`, `WHERE`, `GROUP BY`, `ORDER BY`, `OFFSET`, `LIMIT`, `JOIN`, `ON`, `AS`, `TOP`, `DISTINCT`, `VALUE` | `SyntaxKeyword` |
| Operator word | `AND`, `OR`, `NOT`, `IN`, `LIKE`, `BETWEEN`, `ESCAPE`, `EXISTS`, `ARRAY` | `SyntaxOperator` |
| Literal word | `TRUE`, `FALSE`, `NULL`, `UNDEFINED` | `SyntaxLiteral` |
| Built-in function | `STARTSWITH(`, `ARRAY_CONTAINS(`, `COUNT(`: a name from `complete.Functions()` followed by `(` | `SyntaxFunction` |
| User-defined function | `udf.discount(` | `SyntaxFunction` |
| Alias | the `c` of `c.total`, a join side's `o` / `cu`, a `JOIN t IN` alias | `SyntaxAlias` |
| Property | `total`, `customer.name`, `["order-id"]` after an alias | text (unstyled) |
| Parameter | `@minTotal` | `SyntaxParameter` |
| String | `"west"`, `'it''s'` | `SyntaxString` |
| Number | `42`, `1.5e3` | `SyntaxNumber` |
| Comment | `-- note` | `SyntaxComment` |
| Punctuation and operators | `( ) [ ] , . = != < <= + - * / % ?? \|\|` | `SyntaxPunctuation` |
| Batch keyword | `BEGIN BATCH`, `PARTITION`, `CREATE`, `UPSERT`, `REPLACE`, `DELETE`, `READ`, `PATCH`, `IF MATCH`, `COMMIT` | `SyntaxKeyword` |

- **The statement decides the vocabulary.** A batch (`query.IsBatch`) is highlighted with
  the batch words, a query with the query words, as `query.Spans` already does. When
  iterations 21 and 22 land, `UPDATE … SET … UNSET` and `DELETE FROM` bring their own
  vocabularies through the same switch. Nothing in the editor knows the list of
  statement kinds.
- **A keyword after a dot is a property**, as today (`c.value`, `c.order`).
- **An alias is highlighted only where it is declared or used as an alias**: the name
  after `FROM`/`JOIN`/`AS`/`IN`, and a name before a `.` that matches one. A bare word
  matching an alias elsewhere (a string, a property) is not.
- **Properties stay plain.** They are the bulk of most queries; coloring them would
  make the other classes harder to see.

## Theme

**Every color comes from `internal/theme`.** No package outside `theme` constructs a
color, and none holds a hex value. `theme` gains one role per class above, each
defined from the existing palette, not from new hues:

| Role | Palette | Light | Dark |
|---|---|---|---|
| `SyntaxKeyword` | amethyst | `#6C3FA0` | `#9B6FD0` |
| `SyntaxOperator` | amethyst, not bold | `#6C3FA0` | `#9B6FD0` |
| `SyntaxLiteral` | copper | `#B87333` | `#D48F52` |
| `SyntaxFunction` | gold | `#D4A017` | `#F5C542` |
| `SyntaxAlias` | parchment, bold | `#5C4B37` | `#E8DCC8` |
| `SyntaxParameter` | copper, italic | `#B87333` | `#D48F52` |
| `SyntaxString` | verdigris | `#2E8B84` | `#5FD3CE` |
| `SyntaxNumber` | copper | `#B87333` | `#D48F52` |
| `SyntaxComment` | ash, italic | `#8A8378` | `#6B655B` |
| `SyntaxPunctuation` | ash | `#8A8378` | `#6B655B` |
| `DiagnosticError` | cinnabar (the squiggle's color) | `#C0392B` | `#E74C3C` |

- **Light and dark follow the terminal** through `lipgloss.AdaptiveColor`, exactly as the
  panes' colors do today, so the editor and the rest of the app always agree.
- **`SyntaxKeyword` keeps today's amethyst**, strings keep verdigris and numbers keep
  copper, so a blurred editor looks the same as before this iteration, only with more
  classes told apart.
- **Styles are built once**, in `theme`, as package-level `lipgloss.Style` values beside
  `TextStyle` and `HintStyle`, never per rune or per frame.
- **`NO_COLOR` and `--ascii`.** Under `NO_COLOR` every role renders as plain text and the
  diagnostic falls back to a plain underline (below). `--ascii` changes glyphs, not
  colors, so it changes nothing here.
- **One place to change the colors.** A later theme switch (a light/dark override, or a user
  palette) changes the roles in `theme` and nothing else. This iteration adds no config
  key for it.

## Diagnostics: the red squiggle

A diagnostic is a byte range of the buffer, a severity, and a message.

```go
// internal/query
type Diagnostic struct {
    Start, End int
    Message    string // "unknown function CONTAIN: did you mean CONTAINS?"
}

func Diagnose(text string) []Diagnostic
```

It is drawn as a curly underline in `DiagnosticError` under the range, keeping the
text's own highlight color. The message shows in the editor's hint line when the
cursor is inside the range, and nowhere else: no popup, no jump.

### What is flagged

Only what Alchemist can decide is wrong from the text alone, with no false positive a
user would have to learn to ignore:

| Flagged | Example | Message |
|---|---|---|
| An unterminated string | `WHERE c.region = "west` | `unterminated string: close it with "` |
| A character Cosmos SQL does not use | `WHERE c.total # 5` | `"#" is not part of Cosmos SQL` |
| An unknown built-in function | `CONTAIN(c.name, "A")` | `unknown function CONTAIN: did you mean CONTAINS?` |
| An alias the query never declares | `SELECT o.id FROM c` | `o is not declared: the query reads c` |
| A misspelled clause keyword in a clause position | `SELECT * FORM c` | `FORM is not a clause: did you mean FROM?` |
| A statement that does not start with a statement keyword | `SELEC * FROM c` | `a query starts with SELECT` |
| Unbalanced brackets | `WHERE (c.a = 1` | `( is never closed` |
| A malformed batch | `BEGIN BATCH sales.orders PARTITION` (no key) | `query.BatchSyntaxError`'s message, at its line and column |

- **"Did you mean"** only offers a candidate within edit distance 2 of a known word.
  Otherwise it says what is wrong without guessing.
- **Not flagged:** unknown fields (Cosmos has no schema), unknown containers or
  databases (the catalog may not be listed yet, and iteration 13's completion already
  shows what exists), and cross-container shapes the planner refuses (the planner's own
  error names them on `ctrl+r`, and a squiggle cannot explain "not supported across
  containers" better than that message does). Each would be a squiggle the user could
  not act on while typing.
- **The service decides validity, not Alchemist.** A query with no squiggle can still
  fail on the service, and the error is shown as today. The squiggles are a typing aid,
  not a validator, and the README says so.
- **One diagnostic per range.** Where several rules match the same bytes, the first rule
  in the table above wins.
- **The token being typed is not flagged.** A word the cursor is touching is not judged
  until the cursor leaves it, so `SEL` is not squiggled on the way to `SELECT`, nor is
  `STARTSW` on the way to `STARTSWITH(`. An unterminated string is not flagged while
  the cursor is inside it.

### Drawing the squiggle

`lipgloss` has no curly or colored underline, so the sequence is written by
`internal/theme`, the one package that already speaks in escape codes:

- **Curly underline:** SGR `4:3` (curly) plus SGR `58:2::r:g:b` (underline color),
  closed by `4:0` and `59`. Kitty, WezTerm, iTerm2, Ghostty, foot, GNOME Terminal
  (VTE) and Windows Terminal draw it. tmux passes it through when its
  `terminal-overrides` include `Smulx` and `Setulc`; the README says how.
- **Terminals that do not know `4:3`** read it as a plain underline, or ignore it. The
  underline color then comes from the text color, which stays the token's highlight
  color. A diagnostic is never shown only by color: the hint line always says it in
  words.
- **`NO_COLOR`:** plain underline (SGR 4), no color.
- **A setting, not a detection.** Terminal capability detection is unreliable over SSH
  and tmux. A `diagnostics` profile key (`curly` the default, `underline`, or `off`)
  and a `--diagnostics` flag cover the terminals that draw `4:3` badly. `off` also
  stops `Diagnose` from running at all.

## Typing performance

The rule: **a keystroke does no more work than it does today, and the extra work is
bounded by what is on screen, not by the length of the buffer.**

The baseline, measured on this branch before any change: iteration 13's
`BenchmarkTypingWithTheListOpen` (one keystroke narrowing an open suggestion list in a
200-line buffer, `Update` plus `View`) takes **13.8 ms/op, 8.6 MB and 47,560 allocations
per op**, measured in the development container. Step 1 records it again on the CI
runner, and that figure is the budget's reference point.

### Where the time goes, and what changes

1. **Highlight spans are computed once per edit, not once per frame.** `View` runs on
   every spinner tick, status-bar notice and resize, not only on keystrokes. The editor
   keeps `highlightCache{value string; spans []query.Span; diagnostics []query.Diagnostic}`
   and recomputes only when `area.Value()` differs from the cached value. A frame with
   no edit costs a string comparison. The comparison is cheaper than it looks, because
   Go compares the lengths first.
2. **Lexing stays linear and allocation-light.** `query.Spans` is one pass of `lex`.
   It is extended to the classes above without a second pass: function names by a
   one-token lookahead for `(`, aliases from the parse iteration 13's completion
   already runs (`parseTokens`), shared rather than repeated. The same keystroke
   already parses the buffer for completion (`query.Context`), so the editor hands both
   callers one `query.Analysis` per value instead of parsing twice.
3. **Painting is bounded by the visible rows.** Today's `highlightRows` tries every
   byte offset of the value as the start of the first visible row, which is
   quadratic in the buffer length. It changes to try, in order:
   1. the start it matched last frame;
   2. the starts of the lines around the cursor's line (`area.Line()`);
   3. only then every line start.

   A keystroke almost always matches on the first try. Per frame it then walks only the
   runes on screen.
4. **Diagnostics are debounced off the keystroke path.** `Diagnose` runs after the user
   pauses, not on every rune:
   - a keystroke that changes the value schedules a `diagnoseMsg{generation}` with
     `tea.Tick` at `diagnoseDelay` (150 ms);
   - a later keystroke bumps the generation, so the earlier tick is dropped on arrival;
   - the diagnostics shown until then are the previous ones, shifted by the edit. An
     insertion before a range moves it right, and an edit inside a range drops that
     range until the next pass.

   Lexical diagnostics (unterminated string, stray character, unbalanced bracket) come
   free with the spans and are not debounced. Only the name checks (functions, aliases,
   keywords, "did you mean") wait for the pause.
5. **Nothing blocks.** `Diagnose` is pure over the text: no catalog, no I/O, no lock.
   It runs inside the debounced `Update` in well under a frame (budget below). It gets
   no goroutine, because there is nothing to wait on and a goroutine would only add a
   stale-result race.
6. **The cursor is left alone.** The focused view keeps the textarea's own cursor. The
   painter finds the cursor's cell in the raw rendered row (the one cell the textarea
   drew in its cursor style) before stripping styles, and draws that cell in the
   textarea's cursor style over the highlighted row. Blink timing stays the
   textarea's.

### The budget, enforced

- `BenchmarkTypingWithTheListOpen` may not regress by more than **5%** in time or
  allocations against the baseline above.
- New `BenchmarkTypingPlainBuffer`: one keystroke with no list open, 200-line and
  2,000-line buffers, focused and highlighted. It must stay under **2 ms/op** at 200
  lines, and grow no worse than linearly to 2,000 (at most 10× the 200-line figure).
- New `BenchmarkViewWithoutEdit`: `View` on an unchanged 2,000-line buffer, which is the
  spinner-tick case. It must not allocate for the spans (0 allocations beyond the
  textarea's own view) and must be independent of buffer length.
- New `BenchmarkDiagnose`: `query.Diagnose` on a 2,000-line query. It must stay under
  **5 ms/op**, which is a third of a 60 Hz frame at the scale no one types by hand.
- These run in CI as `go test -run XXX -bench 'Typing|ViewWithoutEdit|Diagnose' -benchtime 50x`
  in the quality gate. A small `cmd/benchmark-gate` compares them against
  `testdata/bench-baseline.txt` and fails the gate past the thresholds. The baseline is
  refreshed deliberately, in its own commit, and never by the job itself.
- A test asserts that an edit schedules exactly one pending diagnose, and that ten
  keystrokes inside the delay run `Diagnose` once, not ten times.

## Scope

- `internal/theme`: the `Syntax*` roles and `DiagnosticError`, their styles, and
  `CurlyUnderline(text string, color lipgloss.AdaptiveColor) string`, which writes the curly
  underline or its `underline`/`off`/`NO_COLOR` fallback.
- `internal/query`:
  - `SpanKind` gains `SpanOperator`, `SpanLiteral`, `SpanFunction`, `SpanAlias`,
    `SpanParameter` and `SpanPunctuation`.
  - `Spans` classifies them in its existing single pass.
  - New `Analysis` (tokens, parse, spans) built once per value and shared with `Context`.
  - New `diagnose.go` with `Diagnostic` and `Diagnose(Analysis)`, and a small
    edit-distance helper for "did you mean".
  - The keyword vocabularies stay the ones `scope.go` and `batch.go` already hold. The
    function names come from `complete.Functions()`, moved to `internal/query` if
    `complete` cannot be imported there without a cycle.
- `internal/tui/panes`:
  - `highlight.go` paints the focused view too: the cursor cell is preserved, and a
    start hint bounds the matching.
  - `Editor` gains the highlight cache, the diagnostics, and `DiagnosticAt(cursor)` for
    the hint line.
- `internal/tui`: the debounced diagnose (`diagnoseMsg`, generation counter), and the
  hint line showing the message under the cursor.
- `internal/config`, `cmd`: the `diagnostics` profile key and the `--diagnostics` flag.
- `README.md`:
  - the highlight classes and the squiggle;
  - what is and is not flagged, including that the service stays the judge;
  - the tmux `terminal-overrides` line;
  - the `diagnostics` setting.
- `docs/plan/08-export-polish.md`: its "the editor buffer stays plain while focused"
  gets a pointer to this iteration.

## Out of scope

- Semantic checks against the catalog (unknown containers, databases, fields) and
  against the planner's cross-container rules.
- Quick fixes: applying a "did you mean" with a key. It would be a natural follow-up
  once the diagnostic under the cursor is known, but it needs a key and a design of its
  own.
- A user-configurable palette or a light/dark override. This iteration routes every
  color through `theme` so that one can come later without touching the editor.
- Highlighting anywhere but the editor: history previews, the saved-queries overlay, row
  detail, and the batch review. Each can adopt `query.Spans` later.
- Forking or replacing `bubbles/textarea`.

## Relationship to other iterations

- **8, export and polish.** Its blurred-only highlighting becomes always-on. Keyword,
  string and number colors are unchanged.
- **13, autocomplete.** Shares one parse per edit with completion. The hint line already
  hosts completion's note, and a diagnostic's message takes the line only when the
  cursor sits in a flagged range and no suggestion list is open. `BenchmarkTypingWithTheListOpen`
  is this iteration's regression guard.
- **17, transactions.** Batch statements are highlighted with their own vocabulary, and a
  `BatchSyntaxError` becomes a squiggle at its line and column while typing, instead of
  only an error on `ctrl+r`.
- **21 and 22, update and delete by query.** When they land they add their statement
  kinds' vocabularies to `Spans` and their parse errors to `Diagnose`. This plan
  reserves nothing else for them.

## Steps

1. **Baseline.** Commit the current benchmark numbers to `testdata/bench-baseline.txt`,
   add `BenchmarkTypingPlainBuffer`, `BenchmarkViewWithoutEdit` and `cmd/benchmark-gate`,
   and wire the gate. Nothing visible ships.
2. **Theme roles.** Add the `Syntax*` and `DiagnosticError` roles and move `spanStyle`'s
   colors onto them. The blurred editor must look identical, checked with a golden
   render.
3. **Cache and bounded painting.** Add the highlight cache and the start hint. The
   blurred editor gets faster, and the benchmarks prove it.
4. **Focused highlighting.** Paint the focused view and preserve the cursor cell. The
   typing benchmarks must hold.
5. **More classes.** Add operators, literals, functions, aliases, parameters and
   punctuation, and have `Spans` share one `Analysis` with `Context`.
6. **Lexical diagnostics:** unterminated strings, stray characters and brackets, drawn
   with `CurlyUnderline` and the settings.
7. **Name diagnostics** with the debounce: functions, aliases, keywords, "did you mean",
   and batch syntax.
8. **Hint line** message under the cursor. README and plan status.

## Testing

**Unit, `internal/query/test`:**

- `Spans` classifies every row of the class table, including:
  - a keyword after a dot;
  - an alias declared by `FROM`, `JOIN`, `AS` and `IN`;
  - a function name only when `(` follows;
  - a batch's words only inside a batch.
- `Diagnose` table: each "Flagged" row of the diagnostics table flags exactly its bytes
  with its message, and each "Not flagged" case produces nothing.
- The token under the cursor is not flagged, and neither is an unterminated string while
  the cursor is inside it.
- "Did you mean" offers only candidates within distance 2.
- `FuzzSpans` and `FuzzDiagnose` check that ranges stay inside the text, on rune
  boundaries, never overlapping within one list, and that nothing panics. Their corpus
  is the planner's `plannerSeeds`.

**Unit, `internal/theme/test`:**

- Every role resolves to a palette color in both light and dark.
- `CurlyUnderline` writes `4:3`, `58:2::r:g:b`, `4:0` and `59` for `curly`, SGR 4 for
  `underline` and under `NO_COLOR`, and the bare text for `off`.

**TUI, `internal/tui/test`:**

- A focused buffer shows highlighted keywords (assert on the SGR sequences in `View`),
  and the cursor cell keeps the cursor style.
- A misspelled function is underlined after the debounce and not before. The hint line
  shows its message only while the cursor is inside it.
- Ten keystrokes within `diagnoseDelay` run `Diagnose` once. A stale `diagnoseMsg` is
  dropped.
- `diagnostics = off` shows no underline and never runs `Diagnose`.
- A `View` with no edit between two calls recomputes no spans (a counter behind a test
  hook).
- The drift-guard key tests pass: no key is added.

**Benchmarks:** as listed under "The budget, enforced", run in CI.

**Manual checklist:**

- [ ] In kitty or WezTerm: type `SELECT * FORM c WHERE CONTAIN(c.name, "A")`. `FORM` and
      `CONTAIN` get red squiggles after a pause, and the hint line names the fix when
      the cursor is on them.
- [ ] In macOS Terminal (no curly underline): the same text shows plain underlines, still
      in the token colors, and the hint line explains.
- [ ] Under tmux with and without the `terminal-overrides` line: curly underline and
      plain underline respectively.
- [ ] A light-background terminal and a dark one: every class readable, with the same
      hues as the rest of the app.
- [ ] Hold a key down in a 2,000-line buffer: no visible lag, and the squiggles settle
      after release.
- [ ] `NO_COLOR=1`: no color anywhere in the editor, and diagnostics as plain underlines.

## Acceptance criteria

- The focused editor highlights every class in the table, with colors taken only from
  `internal/theme` roles that adapt to light and dark.
- A recognized-but-wrong token gets a red curly underline, or a plain underline where
  curly is not drawn. Its message is in the hint line, and nothing in the "Not flagged"
  list is ever underlined.
- `BenchmarkTypingWithTheListOpen` is within 5% of its baseline.
- A keystroke's highlight work is bounded by the visible rows.
- A frame with no edit recomputes nothing.
- `Diagnose` never runs on every rune.
- No goroutine, no new dependency, and no new key binding.

## Implementation notes

What landed differs from the text above in these ways:

- **Keywords are bold.** The role table has `SyntaxOperator` as "amethyst, not bold",
  and the two classes share a hue, so `SyntaxKeyword` is bold amethyst: without it a
  clause keyword and an operator word look the same. Comments are italic ash, as the
  table says, where the blurred editor drew them plain ash. Every other color of the
  blurred editor is unchanged; the golden render is the panes tests asserting each
  class against its `theme` role, including the colors iteration 8 used.
- **The underline API.** `theme.DiagnosticUnderline` (`CurlyUnderline`,
  `PlainUnderline`, `NoUnderline`) has `Render(text, color)`, in place of a
  `CurlyUnderline(text, color)` function that would have had to find the setting in
  package state. `ParseDiagnosticUnderline` reads the setting for `config` and `cmd`.
  `theme.SequencesOf(style)` splits a style's rendering into the escape codes before
  and after its text, so the painter writes a run without a lipgloss render per run.
  Under a 256- or 16-color profile the underline color is `58:5:n`; `58:2::r:g:b` is
  written only under true color.
- **`Diagnose(query.Analysis)`**, the signature the Scope section gives, not
  `Diagnose(text)`. `Analysis.LexicalDiagnostics` is the part that needs no pause.
  `Analysis.Context(cursor)` is completion over the same parse; `query.Context` and
  `query.Spans` remain as wrappers.
- **The token being typed is exempted by the editor, not by `Diagnose`**, which stays
  pure over the text. The editor remembers the offset of the last edit while the
  cursor stays there, and hides any diagnostic whose range contains it; moving the
  cursor clears it, so the word and an unterminated string are flagged the moment the
  cursor leaves them, without another check.
- **Unbalanced brackets and batch syntax wait for the pause.** An open parenthesis is
  the normal state while a call is being typed, so flagging it on every key would be
  the false positive the plan rules out. Only unterminated strings and stray
  characters are immediate.
- **Text put in whole is checked at once.** A history recall, a saved query, a batch
  draft and an accepted suggestion replace text rather than type it, so
  `Editor.SetValue` and `Editor.Replace` run `Diagnose` directly. Keystrokes are
  debounced in `editorUpdate`: `tui.Model` is 156 KB, over the size Go keeps on the
  stack, so a wrapper around `Update` comparing buffers cost a heap copy of the model
  per message, which the benchmarks caught.
- **What is flagged, precisely.** Stray characters are `#`, `` ` `` and `\` outside
  strings and comments; non-ASCII is not flagged. An undeclared alias is only judged
  once the query has a `FROM`, so a query typed from the top is not flagged on its way
  there, and the names it may read through are BuildPlan's (the default `c` of an
  unaliased container, a join side's container name) plus every declared alias and
  every one-word source. A misspelled clause is a word within two edits of `SELECT`,
  `FROM`, `WHERE`, `GROUP`, `ORDER`, `OFFSET`, `LIMIT` or `JOIN`, of three letters or
  more, after a complete value; `FROM c WERE` parses `WERE` as a bare alias, so a bare
  alias counts until something reads through it. A statement start is checked against
  `SELECT` only; a lone `BEGIN` is how a batch is typed. Messages quote the batch
  parser's text; `BatchSyntaxError` carries its byte offset, unexported.
- **Classes beyond the table.** `IS` is an operator word, the join modifiers (`LEFT`,
  `INNER`, …) are keywords except as a call (`LEFT(`), and in a batch `TRUE`, `FALSE`
  and `NULL` are literals.
- **The hint line** is the editor's last row, taken while the focused cursor is on a
  diagnostic and no list is open, and given back after; a blurred editor shows none.
- **The function list** moved from `complete` to `query` (`query.Functions()`), since
  `complete` imports `query`.
- **Painting.** The textarea's text and prompt styles are now empty: every row is
  repainted anyway, and an empty style makes the textarea's own per-line renders
  cheaper. Matching tries the start that matched last frame, the line starts within
  a screen of the cursor's line, every rune of those lines (a first row that starts
  partway through a wrapped line), and only then every line start. The cursor cell is
  found as the one cell the textarea drew in reverse video.
- **More than the plan asked, for the budget.** The editor keeps the buffer's value
  instead of rebuilding it from the textarea's lines on every call, and reuses a whole
  frame drawn for an unchanged editor (a stamp every change takes anew), which is what
  makes `BenchmarkViewWithoutEdit` independent of the buffer's length: the textarea
  renders every line of the buffer on each `View`, before its viewport crops.
  `ClearSuggestions` with no list open no longer relays out the pane, and the pane
  scrolls to the cursor only after a key that moves it to another row. `query.code`
  returns a text's tokens as they are when it has no comment.
- **The 2 ms ceiling for `BenchmarkTypingPlainBuffer` at 200 lines is not met, and not
  gated.** Its baseline, measured in step 1 before any change, was 7.4 ms: a keystroke
  re-renders the textarea's whole buffer and the frame redraws every pane, costs this
  iteration keeps out of scope by not replacing `bubbles/textarea`. This iteration
  takes it to about 6 ms. The 10× growth bound from 200 to 2,000 lines is met and gated.
- **`BenchmarkViewWithoutEdit`** allocates nothing for the editor on a frame with no
  edit; the allocations it still reports (about 1,600 at either length) are the other
  panes and the border.
- **The benchmark gate.** CI runners and the machine that measured the baseline differ
  in speed by more than 5%, and consecutive runs on a shared runner differ by more
  than that too, so a gate on nanoseconds against the committed baseline would flake.
  The gate holds what does not depend on the machine: allocations and bytes per
  operation, each within 5% of `testdata/bench-baseline.txt`, the median of five runs.
  Time is held only where it is robust: `BenchmarkDiagnose` under its 5 ms ceiling
  (it measures about 2 ms, leaving room for a slow runner), and ratios within one run
  (2,000 lines at most 10× 200 for typing, at most 2× for a frame with no edit). Time
  against the baseline is printed, not judged. It runs as its own `benchmarks` job
  rather than in the quality job, so no other step competes for the runner. The 5% time
  budget for `BenchmarkTypingWithTheListOpen` was checked by an interleaved A/B run of
  the baseline and final builds on one machine, below. The gate's logic is
  `internal/benchmark`, tested; `cmd/benchmark-gate` only reads files. `make bench`,
  `make bench-gate` and `make bench-baseline` wrap the commands.
- **Tests.** The TUI harness's sessions flag nothing by default, since each check
  waits on a timer the harness would otherwise wait out after every typed key;
  `newDiagnosingModel` is a session that checks, and the typing benchmarks use it. The
  "Diagnose runs once" test delivers the ten held timers' messages and shows the
  first nine change nothing and the tenth flags, rather than reading a counter;
  `Editor.Diagnoses` and `Editor.Analyses` count for the pane tests.
- `--diagnostics` is also a flag of `profile add`.

- **Iteration 21 merged in.** An `UPDATE` (`Analysis` marks it the way `IsMutation`
  does) is highlighted with a query's classes plus `UPDATE`, `SET` and `UNSET` as
  keywords; its target path is a source root and its alias a declaration, the default
  `c` when it names none. `Diagnose` places a `MutationSyntaxError` the way it does a
  `BatchSyntaxError`: 21 moved both parsers onto a shared `statementReader` that builds
  errors from a line and column, so `Diagnose` gives the reader its own error type and
  turns the line and column back into a byte offset. Unsupported shapes (`TOP` in an
  update, a join in its `WHERE`) are flagged with the parser's message too. An update
  checks its aliases without waiting for a `FROM`, and `SET`/`UNSET` join the words a
  misspelled clause is matched against. The statement-start message became `a statement
  starts with SELECT, UPDATE or BEGIN BATCH` (with `DELETE` after iteration 22).

- **Iteration 22 merged in.** A `DELETE FROM` is analysed as 22's `mutationKind`
  reads it, CTE list included, and highlighted and checked as an update is: `DELETE`
  joins the mutation keywords, its target after `FROM` is a source root and its alias a
  declaration, and its `MutationSyntaxError`s reach `Diagnose` through the same
  statement reader. The statement-start message names `DELETE` too. Comments end at
  `\r` as well as `\n` in the one lexer both the parser and the painter use.

### Benchmarks

Medians of five runs of 50 iterations each, in the development container: before is
step 1's baseline, after is `testdata/bench-baseline.txt` as this iteration leaves it.
Sessions check what they type in the after runs, as sessions do by default.

| Benchmark | Before | After | Before allocs | After allocs | Before B/op | After B/op |
|---|---|---|---|---|---|---|
| `TypingWithTheListOpen` | 13.78 ms | 12.74 ms | 47,560 | 28,199 | 8.56 MB | 7.34 MB |
| `TypingPlainBuffer/lines=200` | 7.56 ms | 6.12 ms | 22,248 | 9,709 | 2.46 MB | 2.16 MB |
| `TypingPlainBuffer/lines=2000` | 41.09 ms | 25.10 ms | 200,509 | 74,554 | 10.05 MB | 8.55 MB |
| `ViewWithoutEdit/lines=200` | 3.76 ms | 1.37 ms | 11,843 | 1,615 | 0.90 MB | 0.49 MB |
| `ViewWithoutEdit/lines=2000` | 20.61 ms | 1.48 ms | 98,273 | 1,615 | 4.59 MB | 0.49 MB |
| `Diagnose` (2,000 lines) | — | 1.94 ms | — | 9 | — | 480 B |

Separate runs on a shared machine differ by more than the 5% budget, so the
`TypingWithTheListOpen` time was also compared by running the step 1 build and the
final build alternately, eight times each: medians 15.09 ms before and 12.96 ms after
(−14%), and −22% in a run under load from other work on the machine.
