# Iteration 15 — Saved Queries

## Goal

Give a query a name, and get it back later by that name. History (iteration 7) keeps
everything that ran and forgets it after a few thousand entries; a saved query is one
the user chose to keep, and it stays until they delete it.

Saved queries belong to an account. The ones saved under `prod` are offered while
`prod` is the active account, and no others: a query written against one account's
databases means nothing in another's. Iteration 14 defines the account, its identity
(`config.Profile.Name`), and the active account; this iteration builds on its "Contract
for iteration 15" and changes none of it.

A saved query is a `.sql` file the user can open in their own editor, keep in a
dotfiles repository, or copy to a colleague. The application is one way to write and
read those files, not the only one.

## Storage design

```
~/.config/alchemist/            $XDG_CONFIG_HOME/alchemist when set
  config.toml
  queries/
    prod/
      open orders.sql
      late-shipments.sql
    staging/
      open orders.sql
    mock/
      every-customer.sql
```

One directory per account, one file per query. The account directory is the account
identity, unescaped, which 14's contract guarantees is safe as a path segment. The file
name is the query's name plus `.sql`. The file is the query text, under an optional
header:

```sql
-- alchemist: scope=sales/orders
SELECT c.id, c.total
FROM c
WHERE c.status = "open"
```

### Why the config directory

`history.jsonl` and the log live in the state directory (`logging.Dir`) because they
are a by-product of running the program: nobody writes them by hand and losing them
costs nothing. A saved query is authored. It belongs beside `config.toml`, so that
whoever syncs or checks in `~/.config/alchemist` carries their profiles and the queries
filed under them together. There is no key or endpoint in a query file, so nothing
about it is less safe to sync than `config.toml` already is.

### Why one file per query

| Layout | Verdict |
|---|---|
| One TOML file for everything | One writer rewrites every account's queries; stale-account guarding gets harder, not easier; `profile remove` has to edit a shared file. |
| One TOML file per account | `BurntSushi/toml` v1.6.0 encodes every string on one line with `\n` escapes and, as `config.Store.Save` already notes, drops comments. The first save from the TUI would flatten every hand-formatted query in the file. Two sessions saving at once lose one of the writes. |
| **One `.sql` file per query** | The text is stored byte for byte. Editors highlight it. `ls` lists, `cat` shows, `rm` deletes, `mv` renames, `git diff` reads. A save touches one file, so two sessions cannot lose each other's work, and a mangled file costs that query only. |

The cost is that metadata has no natural home in a `.sql` file. There is one piece of
it, the scope, and it goes in the header line.

### The header

- Leading lines that start with `-- alchemist:` are the header; the store strips them,
  so `Query.Text` never contains one and nothing depends on whether the service or the
  lexer understands `--` comments (the lexer does not until iteration 13).
- The one key is `scope`. Its value is the scope path joined with `/`, not `.`: Cosmos
  forbids `/` in a database or container id and allows `.`, so the path splits back
  without ambiguity. An adapter with deeper paths gets more segments for free.
- A header key the store does not know is ignored and dropped on the next save of that
  query. A `scope` with an empty segment loads as no scope.
- A file with no header is a query with no scope. Dropping a plain `.sql` file into the
  directory is a supported way to add one.
- Everything after the header is the text, with `\r\n` read as `\n` and trailing blank
  lines trimmed. Nothing else is touched.

### What is saved, and what is not

| Field | Stored as | Why |
|---|---|---|
| name | the file name | one source of truth; a rename is `mv` |
| text | the file body | the point |
| scope | the header, optional | recall restores it, as `Model.recall` does for a history entry |
| saved time | the file's modification time | free; shown as an age in the overlay. A `git checkout` resets it, which costs a cosmetic column and nothing else |
| account | the directory | never inside the file, so a file copied to another account's directory simply belongs to it |

No description, tags, or folders: the name is the description, and the overlay filters
on name, text and scope. No run statistics: those are history's. No key, endpoint, or
anything else from a profile, the same rule `internal/history` states in its package
comment.

### Which scope is saved

`history.Entry.Scope` records `query.Plan.Scope()`, the first leaf's container. That is
right for a log and wrong here: a union or join (iteration 10) names every container in
its text, and restoring "the first one" on recall would move the catalog scope for no
reason.

The rule: **a scope is saved only when the text needs one.** At save time the root
model builds the plan. If `query.BuildPlan` succeeds and every leaf already names its
container, the query is self-contained and is saved with no scope. Otherwise — a bare
`FROM c`, a join with one bare side, or text `BuildPlan` refuses that is not a batch
(iteration 17), which is still worth saving as a draft — the active account's current
scope is saved, and that may be empty.

| Text | Saved scope |
|---|---|
| `SELECT * FROM c` with `sales.orders` selected | `sales/orders` |
| `SELECT * FROM c` with nothing selected | none |
| `SELECT * FROM sales.orders c` | none |
| `SELECT * FROM sales.orders, sales.archive AS c` | none |
| `… FROM sales.orders o JOIN sales.customers cu ON …` | none |
| `… FROM c JOIN sales.customers cu ON …` with `sales.orders` selected | `sales/orders` |
| `SELEC * FORM c` (draft) with `sales.orders` selected | `sales/orders` |
| `BEGIN BATCH sales.orders … COMMIT` (iteration 17) with anything selected | none |

### Names

- Letters, digits, space, `.`, `-` and `_`; starts with a letter or digit; does not end
  with a space or `.`; 1 to 64 characters. As a pattern:
  `^[A-Za-z0-9]([A-Za-z0-9 ._-]{0,62}[A-Za-z0-9_-])?$`.
- The alphabet has no `/`, no `\`, no leading `.`, and no `!`. The first three keep a
  name inside its account directory and keep it from colliding with the store's
  temporary files; the last keeps the prompt's overwrite mark unambiguous.
- Unique within an account, compared with `strings.EqualFold`. `Open Orders` and
  `open orders` are one query, on every filesystem, for the same reason 14 makes
  `Config.Put` refuse case-only profile names.
- Replacing a query keeps the spelling of the file already there. Changing the spelling
  is a rename, and a case-only rename is allowed.

### Writes

- Directories `0o700`, files `0o600`, the modes `internal/config` and
  `internal/history` use.
- A save writes `.<name>.sql.tmp` in the account directory and renames it over the
  final path, the way `history.File.trim` replaces the log, so a crash leaves the old
  file or the new one and never half of either. Names cannot start with `.`, so a
  leftover temporary file is never listed.
- Nothing is created until the first save. Opening the store does no I/O, which keeps
  `sessionFlags.run`'s promise that an invocation which never reaches the TUI leaves
  nothing behind.
- No locking. Two sessions creating the same new name in the same instant both succeed
  and the later rename wins; both files were whole. That is the only lost update this
  layout allows, and it needs two people typing the same name.

### Files the store did not write

`List` never fails because of one file. It returns what it could read and names what it
could not:

| Found in an account directory | Result |
|---|---|
| a subdirectory, a dotfile, a file without `.sql` | ignored silently |
| a `.sql` file whose name breaks the name rules | skipped, reported |
| two files whose names differ only in case (possible on Linux) | the first in byte order is listed; the other is skipped, reported |
| an empty or whitespace-only body | skipped, reported |
| text that is not valid UTF-8, or a file over 1 MiB (`maxFileSize`) | skipped, reported |
| an unknown or malformed header line | the query loads; the line is dropped |
| a file that cannot be read (permissions) | skipped, reported |

Reported means a `Skipped` entry in the `Listing`: the overlay shows a count, and the
log names each file with its reason. An account directory that does not exist is an
empty list, not an error.

## Layout

### Save prompt

`ctrl+s` opens it over a cleared screen, like the export prompt it is modeled on:

```
╭─ Save query ─────────────────────────────────────────────╮
│open orders                                               │
│account  prod                                             │
│scope    sales.orders                                     │
│query    SELECT c.id, c.total                             │
│                                                          │
│letters, digits, spaces, . - and _. End the name with !   │
│to replace a saved query of that name.                    │
│✗ "open orders" is already saved for prod                 │
│                                                          │
│enter save   esc close                                    │
╰──────────────────────────────────────────────────────────╯
```

- The `account` line is always shown. The save goes to the account the prompt was
  opened for, and the user sees which one before pressing `enter`.
- The `scope` line shows what will be saved under the rule above, `none` included, in
  `scopeText` form.
- The `query` line is `firstLine` of the text: enough to confirm which buffer this is.
- The trailing `!` is `panes.ExportPrompt`'s `overwriteMark`, with the same meaning.
- The name field opens empty, with one exception: when the editor holds a query that
  was recalled from the saved overlay, the field is seeded with that query's name. The
  `!` is never seeded. Updating a saved query is recall, edit, `ctrl+s`, `!`, `enter`.
- Renaming (below) is the same pane titled `Rename query`, seeded with the current
  name, without the `scope` and `query` lines.

### Saved queries overlay

`ctrl+l` opens it. It is the history overlay's sibling and looks like it:

```
╭─ Saved queries · prod ───────────────────────────────────────────────────╮
│/ filter by name, query or scope                                          │
│ every-customer                   SELECT * FROM sales.customers c  12d ago│
│ late-shipments    sales.orders   SELECT c.id, c.shippedAt FROM c   3d ago│
│ open orders       sales.orders   SELECT c.id, c.total              2h ago│
│                                                                          │
│                                                                          │
│──────────────────────────────────────────────────────────────────────────│
│SELECT c.id, c.total                                                      │
│FROM c                                                                    │
│WHERE c.status = "open"                                                   │
│1 file skipped, see the log                                               │
│/ filter  enter recall  ctrl+r recall and run  r rename  d delete         │
╰──────────────────────────────────────────────────────────────────────────╯
```

- The title names the account being listed, in the form 14 gives the results pane
  (`Results · staging`). The list is always the active account's.
- Rows are sorted by name, case-insensitively. A curated list is looked up by name;
  recency is what history is for. Columns: name, scope (capped at `maxScopeWidth`, blank
  when none), `firstLine` of the text, and the age from `relativeTime`.
- The lower part previews the selected query's whole text, plain, up to
  `maxPreviewLines` (8) lines, and gives way entirely when that would leave the list
  fewer than `minPaneHeight` rows. A saved query is usually several lines long and its
  first line is often just `SELECT`.
- The skipped line appears only when `Listing.Skipped` is not empty.

| State | The body shows |
|---|---|
| no active account | title `Saved queries`; `errNoAccount`'s text: "no account connected: ctrl+g to choose one" |
| the account has nothing saved | "nothing saved for prod yet: ctrl+s saves the query in the editor" |
| the filter matches nothing | `noMatchHint`, as in history |
| the directory could not be read | the error, through `Saved.Fail`, as `History.Fail` does |
| delete pending | the hint line becomes `delete "open orders"?  y delete   any other key keeps it` |

## Interaction

| Key | Where | Action |
|---|---|---|
| `ctrl+s` | main layout, any pane, the editor included | open the save prompt for the editor's text |
| `ctrl+s` | history overlay | open the save prompt for the selected entry |
| `ctrl+l` | main layout, any pane | open the saved queries overlay; in the overlay, close it |
| `/` | saved overlay | filter by name, query text or scope |
| `enter` | saved overlay | recall: text into the editor, scope restored when one was saved, editor focused |
| `ctrl+r` | saved overlay | recall and run |
| `r` | saved overlay | rename the selected query |
| `d` | saved overlay | delete the selected query, after `y` |
| `↑/k`, `↓/j` | saved overlay | move; only the arrows while the filter has the keyboard |
| `esc` | saved overlay | clear the filter if there is one, else close |
| `enter` | save prompt | save |
| `esc` | save prompt | close, back to whatever was underneath |

Checked against `internal/tui/keys.go`, the README key table, and plans 11–14:

- `ctrl+s` and `ctrl+l` are bound by nothing in `DefaultKeyMap`, reserved by no plan,
  and absent from the key maps of `bubbles/textarea` and `bubbles/textinput` v1.0.0, so
  both work while the editor has the keyboard. They must be ctrl bindings for that
  reason: `typesIntoBuffer` hands every plain rune to the buffer. bubbletea puts the
  terminal in raw mode, so `ctrl+s` arrives as a key and does not freeze output.
- `Filter`, `Recall`, `Rerun`, `Up`, `Down`, `Close` and `Save` are the existing
  bindings, reused so the two overlays answer to the same keys with the same words.
- `d` and `r` are plain runes read only while the saved overlay is open and its filter
  is not focused, the same routing that lets `q` quit from the history overlay. `d`
  means delete here as it will in the catalog (iteration 11). `r` is `Refresh`
  elsewhere; the overlay has nothing to refresh, since it reloads every time it opens.
- `y` confirms a delete and is read only while the overlay is asking. Iteration 18's
  `y` is a catalog key and never reaches an overlay.
- `ctrl+g` (14's `Accounts`) is a key of the main layout. Both new overlays swallow it
  like every other global key: seeing another account's saved queries is `esc`,
  `ctrl+g`, `enter`, `ctrl+l`.
- `n`, `c`, `t`, `i` (11, 12), `ctrl+space` (13), `a` and `x` (14, switcher only),
  `ctrl+b` (17, results and detail only) and the catalog's `y` (18) are untouched.
  17 also reads `ctrl+r` in the editor; `Rerun` here ends in `startRun`, so a saved
  batch reaches 17's review like a typed one. No function key is bound.

Deleting asks for one `y`, not for the name typed back as iteration 11's `Confirm` does
for a database: the loss is one query, not a container of documents. It asks at all
because `d` sits next to `j`.

Rename never replaces. A rename onto a name another query holds is refused with
`saved.ErrExists`, and because `!` is not in the name alphabet, `name!` is refused as an
invalid name instead of being read as an overwrite. Replacing a query is what saving is
for.

### The active account

The overlay and the prompt read `Model.accounts.active` and nothing else.

- The list is loaded for the active account each time the overlay opens, in a `tea.Cmd`,
  and the overlay opens on the response, as `openHistory` does.
- `Model.setActive(account) (Model, tea.Cmd)` is the single hook. It clears the pane's
  rows, forgets the recalled name, and, when the saved overlay is open, returns a load
  for the new account. The keyboard cannot change the account under an open overlay,
  since overlays swallow `ctrl+g`; the one case left is 14's third way, a connection
  arriving while no account was active, which turns the overlay's `errNoAccount` text
  into that account's list.
- Every message carries the account it was made for. A `SavedLoadedMsg` whose `Account`
  is not the active one is dropped: a slow listing of `prod` must not land in an overlay
  that now says `staging`.
- The save prompt captures its account when it opens and writes there, whatever happens
  to the active account meanwhile. Its result carries that account too.
- Recall sets the scope through the active account's scope, which 14 keeps per
  account. As with history under 14, there is no "other account" case to define: what
  is listed is the active account's by construction.
- With no active account `ctrl+s` opens nothing and the status bar shows
  `errNoAccount`'s text. An empty editor likewise shows "nothing to save: type a
  query in the editor".

`alchemist --adapter mock` has one account called `mock`, per 14, so its queries live in
`queries/mock/` and persist like any other. There is no special case: the fixtures are
the same every run, which makes saved queries against them useful for demos and for the
manual checklist. A profile that happens to be called `mock` shares the directory, as it
shares the identity.

### Saving from history

`ctrl+s` in the history overlay opens the save prompt for the selected entry: its
`Query` as the text, and its scope decided by the same rule, with `Entry.Scope` standing
in for the current scope.

The save goes to the active account, like any other. 14 scopes history to the account
the session is on (`Store.Recent(account, n)`, title `History · prod`), so every entry
listed has the active account as its `Entry.Profile` and there is no second account to
choose between. On save or `esc` the history overlay is back, cursor where it was.

## Removing a profile

14 leaves orphans to this iteration. The decision: **`alchemist profile remove` keeps
the account's saved queries unless told otherwise.**

- There is no `profile edit` and no rename, so `remove` followed by `add` under the
  same name is how a profile is re-pointed or repaired today. Identity follows the
  name; the queries should still be there afterwards. 14 says the same of
  `history.jsonl`.
- They are authored work, and a deleted file cannot be brought back. Removing a
  connection is not a request to destroy them.

So `removeProfile` reports what it left, and offers the way to remove it:

```
$ alchemist profile remove staging
removed profile staging
kept 7 saved queries in ~/.config/alchemist/queries/staging
remove them too with: alchemist profile remove staging --purge

$ alchemist profile remove staging --purge
removed profile staging and its 7 saved queries
```

`--purge` removes the account directory after the profile and the keychain entry are
gone; a failure there is reported the way a keychain failure is today ("removed profile
staging, but not its saved queries: …"). With nothing saved, neither line mentions
queries. An orphaned directory is inert: the TUI lists accounts from `Options.Accounts`,
never from the disk, so it is never offered, and it is plain to see with `ls`. Adding a
profile of that name adopts it. History entries are untouched either way.

## Scope

- `internal/saved` (new, pure: standard library only, no TUI, config, or adapter
  imports)

  ```go
  // DirName is the directory of saved queries inside the config directory.
  const DirName = "queries"

  const Extension = ".sql"

  var (
      ErrExists         = errors.New("a saved query already has that name")
      ErrNotFound       = errors.New("saved query not found")
      ErrInvalidName    = errors.New("invalid name")
      ErrInvalidAccount = errors.New("invalid account")
      ErrEmptyQuery     = errors.New("nothing to save")
  )

  type Query struct {
      Name  string
      Text  string
      Scope []string  // empty when the text names its own containers
      Saved time.Time // the file's modification time; not written
  }

  // Listing is what one account directory held.
  type Listing struct {
      Queries []Query // by name, case-insensitively
      Skipped []Skipped
  }

  type Skipped struct {
      File string
      Err  error
  }

  type Store interface {
      List(account string) (Listing, error)
      // Create refuses a name already taken with ErrExists.
      Create(account string, q Query) error
      // Replace overwrites a query of that name, or creates it.
      Replace(account string, q Query) error
      Rename(account, from, to string) error
      Remove(account, name string) error
  }

  // Dir is the store on disk, one directory per account under Root.
  type Dir struct{ Root string }

  func Open(root string) Dir
  func (d Dir) AccountPath(account string) string
  func (d Dir) RemoveAccount(account string) (removed int, err error)

  // Unavailable is the store of a session whose config directory cannot be
  // located: it lists nothing and refuses every write with Err.
  type Unavailable struct{ Err error }
  ```

  - `Create` and `Replace` are two methods rather than one with an overwrite flag, the
    split `export.WriteFile` and `export.OverwriteFile` already make.
  - `RemoveAccount` and `AccountPath` are on `Dir`, not on `Store`: only `cmd` needs
    them, and the interface the TUI sees stays as small as what the TUI does.
  - Every method validates `account` against `^[A-Za-z0-9_-]+$` and every name against
    the name pattern before building a path. The account pattern repeats
    `config.profileNamePattern` on purpose: the store guards its own paths and does not
    import `config` to do it. `List` of an account that fails the pattern is
    `ErrInvalidAccount`, not an empty list.
  - `Unavailable` is not `history.Discard`. Recording is implicit, so a history that
    cannot be written degrades to silence; saving is something the user asked for, so
    a store that cannot write says why, in the prompt.
  - `codec.go` holds `encode(Query) []byte` and `decode(name string, data []byte)
    (Query, error)`: the header, the line-ending rule, the UTF-8 and size checks.
- `internal/query` — `func (p Plan) NeedsDefaultScope() bool`: whether any leaf has no
  scope of its own, the question `WithDefaultScope` answers leaf by leaf.
- `internal/config` — `configDir` is exported as `Dir`, mirroring `logging.Dir`, so
  `cmd` can place `queries/` beside `config.toml`.
- `internal/tui/panes`
  - `save.go` — `SavePrompt`, shaped like `ExportPrompt`: `NewSavePrompt(keys)`,
    `SetSize`, `Update`, `Saving`, `StartSaving`, `Fail`, `View`, and

    ```go
    // SaveDraft is what the prompt was opened for.
    type SaveDraft struct {
        Account string
        Text    string
        Scope   []string
        Name    string // seeds the field; the query being renamed, in a rename
    }

    // SaveTarget is what the prompt would do as typed.
    type SaveTarget struct {
        Account string
        Name    string
        Replace bool
    }

    func (p SavePrompt) OpenSave(draft SaveDraft) SavePrompt
    func (p SavePrompt) OpenRename(draft SaveDraft) SavePrompt
    func (p SavePrompt) Renaming() bool
    func (p SavePrompt) Draft() SaveDraft
    func (p SavePrompt) Target() SaveTarget
    ```

    `overwriteMark` is shared with `ExportPrompt`.
  - `saved.go` — `Saved`, shaped like `History`: `NewSaved(icons, keys)`, `SetSize`,
    `SetListing(account string, listing saved.Listing, now time.Time)`,
    `SetAccount(account string)` (title and an empty list, until the listing arrives),
    `Fail`, `StartFilter`, `Filtering`, `ClearFilter`, `Update`, `CursorUp`,
    `CursorDown`, `Selected() (saved.Query, bool)`, `AskDelete`, `Deleting() bool`,
    `CancelDelete`, `View`. After a reload the cursor returns to the query of the same
    name when it is still listed.
  - The filter line, `matches`, the cursor window and `body` are the same code in
    `History`, `Saved` and 14's `Accounts`. They live in an unexported `filterList` in
    `panes`. Whichever of 14 and 15 lands first extracts it, with `History`'s tests
    unchanged; the other builds on it.
- `internal/tui`
  - `Options.Saved saved.Store`; nil means `saved.Unavailable{}` with an error saying
    no store was configured, as a nil `History` means `history.Discard{}`.
  - `saved.go` (new): `openSavePrompt`, `handleSavePromptKey`, `submitSave`,
    `finishSave`, `openSaved`, `handleSavedKey`, `closeSaved`, `recallSaved`,
    `rerunSaved`, `confirmDelete`, `savedScope(text string, current []string)
    []string`. `recallSaved` mirrors `Model.recall` and records `recalledName`;
    `Model.recall` clears it.
  - `overlaySaved` and `overlaySavePrompt`. The prompt remembers the overlay it was
    opened over (`overlayNone`, `overlayHistory`, or `overlaySaved`) and returns there.
  - Messages and commands, every one tagged with its account:

    ```go
    const (
        OpSavedList   = "saved queries"
        OpSaveQuery   = "save query"
        OpRemoveQuery = "remove saved query"
    )

    type SavedLoadedMsg struct {
        Account string
        Listing saved.Listing
    }

    // QuerySavedMsg reports a create, a replace, or a rename.
    type QuerySavedMsg struct {
        Account string
        Name    string
    }

    type QueryRemovedMsg struct {
        Account string
        Name    string
    }
    ```

    `loadSaved(account)`, `saveQuery(target, draft)`, `renameQuery(account, from, to)`
    and `removeQuery(account, name)` are `tea.Cmd`s over the `Store`; nothing in
    `Update` or `View` touches the disk. Failures travel as `ErrMsg` with the `Account`
    field 14 adds: `OpSavedList` goes to `Saved.Fail` when the account is still active
    and is dropped otherwise; `OpSaveQuery` goes to `SavePrompt.Fail`; `OpRemoveQuery`
    becomes a status bar notice. `saved.ErrExists` is shown as
    `"open orders" is already saved for prod: end the name with ! to replace it`.
  - `QuerySavedMsg` closes the prompt, sets the notice `saved "open orders" to prod`,
    logs it, and reloads the list when the saved overlay is what it returns to.
    `QueryRemovedMsg` reloads. Each `Listing.Skipped` entry is logged at warn level
    when its listing is accepted.
  - `Model.setActive` gains the hook described under "The active account".
  - `KeyMap`: `SaveQuery` (`ctrl+s`, "save query"), `Saved` (`ctrl+l`, "saved
    queries"), `Rename` (`r`), `Delete` (`d`), `Confirm` (`y`). `SaveQuery` and `Saved`
    join `globalKeys`; `SavedKeys()` returns `Filter, Recall, Rerun, Rename, Delete`;
    `SavePromptKeys()` returns `Save`; `HistoryKeys()` gains `SaveQuery`. If iteration
    11 has landed and already owns a `Delete` binding on `d`, reuse it. The drift-guard
    test in `keys_test.go` concatenates the two new groups.
- `cmd`
  - `root.go` — `savedStore(logger)` beside `historyStore`: `config.Dir`, then
    `saved.Open(filepath.Join(dir, saved.DirName))`; a directory that cannot be located
    logs a warning and yields `saved.Unavailable{Err: err}`. Passed as
    `tui.Options.Saved`.
  - `profile.go` — `--purge` on `remove`, and the two messages above, through
    `Dir.List` and `Dir.RemoveAccount`.
- `README.md` — a "Saved queries" section (where the files are, that they are plain
  `.sql`, the header line, what `profile remove` does); the key table; one sentence in
  "Query history" about `ctrl+s`.
- `docs/plan/00-overview.md` — `saved/` in the architecture tree, when the code lands.

## Out of scope

- A `query` subcommand (`alchemist query list|show|remove`). With one `.sql` file per
  query in a documented directory, `ls`, `cat` and `rm` already are that command, and
  they compose better. `profile remove --purge` is the one thing the shell cannot do
  in step with the config, so it is the one thing added.
- Switching accounts from inside the saved overlay or the prompt; 14 rules it out for
  every overlay.
- Browsing or recalling another account's saved queries without making it active, and
  copying a query between accounts from the TUI. `cp` works.
- Queries shared by every account. A query is tied to an account's databases. A
  shared set can be added later beside the account directories without touching them.
- Descriptions, tags, folders, favorites, and sorting by anything but name.
- Parameters or placeholders in saved text (`WHERE c.id = :id`).
- Running a saved query from the command line without the TUI.
- Watching the directory. A file added by hand appears the next time the overlay opens.
- Migrating queries when a profile is renamed. 14 has no rename; `mv` on the directory
  is the migration.
- Syntax highlighting in the preview. `highlightRows` is written against the textarea's
  rendered view and does not apply to free text as it stands.
- Windows reserved device names (`CON`, `NUL`) as query names. No Windows build is
  released; the name check is where that rule would go.

## Relationship to other iterations

- **6, profiles.** `config.Dir` is exported; `profile remove` gains `--purge` and says
  what it kept. `config.toml` is unchanged: no key points at the queries directory.
- **7, history.** `ctrl+s` in the overlay promotes an entry, which 07's "Out of scope"
  anticipated. The log, `history.Store` and recording are untouched by this iteration;
  14 is what changes `Store.Recent` to take the account and titles the overlay
  `History · prod`, and `ctrl+s` there builds on that: the entry saved is always one
  of the active account's. `HistoryKeys()` gains `SaveQuery`. `History`, `Saved` and
  `Accounts` share `filterList`.
- **8, export.** The save prompt copies the export prompt's shape, its `!` convention,
  its `Saving` hold, and its `Fail`-keeps-what-was-typed behavior.
- **10, cross-container.** A saved union or join carries no scope, by the rule in
  "Which scope is saved"; recalling one leaves the catalog scope alone, and running it
  behaves exactly as typing it would. A text that names an account
  (`FROM staging.sales.orders`) saves fine and is refused at run time by 14's
  `errAccountInQuery`, like any other text.
- **11, catalog management.** None, beyond sharing `d` as the delete key. A saved query
  whose container was since deleted fails at run time with the service's error, as a
  recalled history entry does.
- **12, info view.** None.
- **13, autocomplete.** Recall goes through `Editor.SetValue`, which is not typing, so
  the suggestion list must not open on it. With the list open, `ctrl+s` and `ctrl+l`
  are not suggestion keys: `handleSuggestionKey` lets them through and the list closes
  as the editor blurs, which 13 already requires. The save prompt's name field and the
  overlay's filter complete nothing, consistent with 13's out-of-scope list. Saved
  query names are not completion candidates.
- **14, multiple accounts.** Everything here stands on its contract: identity is the
  profile name, usable as a path segment; one active account or none; `setActive` as
  the only place it changes; per-account scope; `ErrMsg.Account`; overlays that do
  not switch accounts. The reload rides on the `tea.Cmd` `setActive` returns. The
  overlay is titled the way 14 titles history and results. `accountSet.known` is not
  needed here: nothing in this iteration handles an account other than the active one.
- **16, multi-way joins.** None. A longer join chain names its containers too, so it
  saves with no scope by the same rule.
- **17, transactions.** A batch is text and saves like any other. It names its own
  target, so `savedScope` checks `query.IsBatch` before `BuildPlan` and saves it with no
  scope, complete or not. Recall and run hands it to
  `startRun`, where 17's review takes over. No key is shared but `ctrl+r`.
- **18, cloning.** None. Its `y` is the catalog's; the `y` here exists only while the
  saved overlay asks about a delete.

## Steps

1. `internal/saved`: name rules, the codec, `Dir`, `Unavailable`; `config.Dir`;
   `query.Plan.NeedsDefaultScope`. All pure, all tested alone.
2. `panes.SavePrompt`, `Options.Saved`, `ctrl+s` from the main layout, `savedScope`,
   the `cmd` wiring. Ships: queries can be saved, and are plain files on disk.
3. Extract `filterList` from `History`, if 14 has not. Then `panes.Saved`, `ctrl+l`,
   filter, preview, recall, recall-and-run, the `setActive` hook and the account
   guard, the recalled name seeding the prompt. Ships: the feature end to end.
4. Delete with `y`, and rename through the prompt.
5. `ctrl+s` in the history overlay.
6. `profile remove`: the kept-queries line and `--purge`.
7. README, help sections, the overview's architecture tree, plan statuses.

## Testing

**Unit — `saved` names and codec** (`internal/saved/test`): a table of accepted and
refused names (empty, 65 characters, leading `.`, trailing space, trailing `.`, `/`,
`\`, `..`, `!`, non-ASCII); a round trip with and without a scope; a scope segment
containing `.` survives; text that itself begins with a `--` comment keeps it; an
unknown header key is dropped and the query loads; a malformed scope loads as none;
`\r\n` is normalized; trailing blank lines are trimmed; empty, non-UTF-8 and oversized
bodies are refused. A fuzz target: `decode` never panics, and `decode(encode(q))`
returns `q` for any valid `q`.

**Unit — `saved.Dir`** (on `t.TempDir()`): `List` of a missing directory is empty and
creates nothing; `Create` makes the account directory `0o700` and the file `0o600`;
`Create` of a taken name, in any case, is `ErrExists` and leaves the file as it was;
`Replace` overwrites and keeps the existing spelling; `Replace` of a new name creates
it; no `.tmp` file remains after either; `Rename` moves, refuses a taken name, allows a
case-only change, and is `ErrNotFound` for a missing source; `Remove` likewise; `List`
sorts case-insensitively and reports each row of the "Files the store did not write"
table as `Skipped` while returning the rest; an account of `../x` is `ErrInvalidAccount`
for every method and touches nothing outside the root; `RemoveAccount` removes only that
account and reports the count; two accounts with a query of the same name stay separate.
`Unavailable` lists nothing and fails every write with its `Err`.

**Unit — `query.Plan.NeedsDefaultScope`:** the rows of the "Which scope is saved"
table that parse.

**Unit — `cmd`:** `profile remove` with saved queries prints the kept line and leaves
the directory; with `--purge` the directory is gone and the message counts it; with
nothing saved neither mentions queries; the session builds its store under
`$XDG_CONFIG_HOME/alchemist/queries`.

**TUI, mock adapter** (`internal/tui/test/saved_test.go`; the store is `saved.Open` on
`t.TempDir()`, and a failing stub `Store` where a failure is the point):
- `ctrl+s` with the editor focused opens the prompt and types nothing into the buffer;
  `q`, `r`, `d` and `!` typed into the prompt are text.
- `enter` writes the file; the notice names the query and the account; the prompt
  closes onto the pane that had focus.
- An empty buffer, and no active account, each show their notice and open nothing.
- A taken name keeps the prompt open with the `!` hint and what was typed; the same name
  with `!` replaces the file.
- The saved scope follows the table: bare `FROM c` saves the selected container; a
  `FROM db.container` text, a union and a join save none; a draft that `BuildPlan`
  refuses is saved with the current scope.
- `ctrl+l` lists the active account's queries by name with the account in the title;
  an account with none shows the empty hint; with no account it shows `errNoAccount`'s
  text.
- `/` narrows by name, by text and by scope; `esc` clears the filter first and closes
  second; `j` and `d` typed into the filter are text.
- `enter` puts the text in the editor, focuses it, and sets the saved scope on the
  active account only; a query with no scope leaves the scope as it was; `ctrl+r`
  additionally reaches the recording connection of the active account and no other.
- After a recall, `ctrl+s` seeds the prompt with that name and no `!`; after a history
  recall or an account switch it opens empty.
- `d` then `y` removes the file and the row; `d` then any other key removes nothing;
  the cursor stays on a neighbor.
- `r` renames through the prompt and returns to the overlay with the cursor on the new
  name; a taken name and `name!` are both refused in the prompt.
- Two accounts: each lists only its own. Close the overlay, switch through 14's
  switcher, `ctrl+l`: the title and the rows are the other account's. `ctrl+g` pressed
  in the overlay or in the prompt does nothing.
- A `SavedLoadedMsg` for the account that was active before a switch is dropped and
  opens nothing; an `OpSavedList` failure for it is dropped too.
- The overlay open with no active account, then an `AccountConnectedMsg`: the overlay
  now lists that account, titled with its name.
- A prompt opened for `prod` writes to `prod`.
- History: `ctrl+s` on an entry opens the prompt for the active account with the
  entry's text and the scope the rule gives it; saving or `esc` returns to the history
  overlay with its cursor where it was.
- A store whose `List` fails shows the error in the overlay; one whose `Create` fails
  shows it in the prompt; `Skipped` files show the count line and are logged.
- 13, when landed: recall does not open the suggestion list; `ctrl+s` with it open
  opens the prompt.
- Every new binding is grouped exactly once and appears in the help overlay and README.

**Integration:** none. Nothing here reaches an adapter; the run that follows a recall
is the run flow iterations 5, 10 and 14 already cover against the emulator.

**Manual checklist:**
- [ ] Save a multi-line query from the editor; `cat` the file: header, then the text
      exactly as typed.
- [ ] Edit that file in another editor, reopen the overlay: the change is there.
- [ ] Drop a header-less `.sql` file into the account directory: it lists, recalls, and
      runs against the current scope.
- [ ] Drop in `bad name?.sql` and an empty `.sql`: the overlay counts two skipped files
      and the log names both.
- [ ] Recall, edit, `ctrl+s`, `!`, `enter`: the file is replaced and `ls -a` shows no
      `.tmp` file.
- [ ] Save a name under `emulator`; `ctrl+g` to `prod`, save the same name there;
      `ctrl+l` on each account lists its own copy under its own title.
- [ ] Save the iteration 10 join example; recall it with a different container
      selected: the scope does not move and the join runs.
- [ ] `ctrl+o`, `ctrl+s` on an entry: the file lands in the current account's
      directory and the history overlay is back afterwards.
- [ ] `alchemist profile remove` without and with `--purge`; re-add a profile under a
      kept name and find its queries offered again.
- [ ] `alchemist --adapter mock`: save, quit, relaunch, recall.
- [ ] `chmod 500` the queries directory and save: the prompt explains, the TUI stays
      alive.
- [ ] 80×24: the overlay keeps its hint line; shorter than that, the preview gives way.

## Acceptance criteria

- A query can be saved under a name from the editor and from history, and recalled,
  run, renamed and deleted from one overlay that uses history's keys and vocabulary.
- The queries offered are exactly those in the active account's directory. No list,
  save, or failure made for one account is ever shown or applied under another,
  including responses that arrive after a switch.
- Each saved query is one `.sql` file under `queries/<account>/` in the config
  directory, holding the text byte for byte under at most one header line; files are
  `0o600`, written by rename, and never left partial.
- A file the store cannot use costs that file only: the rest of the list loads, the
  overlay says how many were skipped, and the log says which and why.
- A query that names its own containers is saved without a scope, and recalling it
  never moves the catalog scope.
- `alchemist profile remove` never deletes saved queries unless `--purge` is given, and
  always says what it kept and where.
- No existing binding changes meaning outside the two new overlays, no function key is
  bound, and both new global keys work while the editor has the keyboard.
- `Update` and `View` never touch the disk; every store call is a `tea.Cmd`.
- `internal/saved` imports the standard library only; `internal/tui` reaches the disk
  through `saved.Store` alone and still imports no concrete adapter.
