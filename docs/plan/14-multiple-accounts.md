# Iteration 14 — Multiple Accounts

## Goal

Work with several Cosmos accounts in one session. The session is *on* one account at a
time — its databases in the catalog, its name in the status bar, its connection behind
`ctrl+r` — and an account switcher moves it to another without restarting. Comparing
`prod` with `staging` today means two terminals, two editors, and a query pasted
between them.

An account is not a new concept. A `config.Profile` already is one named account
connection — adapter, endpoint, per-account settings, and a secret filed under its name
— and this iteration builds on it rather than beside it. **An account is a profile, and
its identity is the profile name.** No new config file, no new config key.

What changes is the session. It is bound to exactly one connection today:
`Model.connection`, `Model.catalog`, `Model.profile`, `Model.maxJoinRows` and
`Model.defaultDatabase` are single values, `cmd` opens the connection before the TUI
starts, and `ownsConnection` records which of the two closes it. After this iteration
the session holds a set of accounts, opens and closes their connections itself, keeps
the state of each one it has visited, and knows which one it is on: the *active*
account.

The main layout does not change. The catalog is one account's tree, exactly as it is
now: no account rows, `adapter.Node.Path` and `Catalog.SelectedNode` as they are. The
status bar's first field, which already shows the profile, is the "you are here".

## Layout

`ctrl+g` opens the switcher over a cleared screen, like the history overlay it is
modeled on:

```
╭─ Accounts ───────────────────────────────────────────────────────────────╮
│/ filter by name or endpoint                                              │
│✓ prod       orders.documents.azure.com           sales   current         │
│  emulator   localhost:8081                       sales   connected       │
│◐ staging    staging.documents.azure.com                  connecting…     │
│  old-dev    dev.documents.azure.com                      not connected   │
│✗ eu-prod    eu.documents.azure.com                       failed          │
│             account unreachable: dial tcp: lookup eu.documents.azure.com:│
│             no such host                                                 │
│                                                                          │
│/ filter  enter switch  a add account  x disconnect                       │
╰──────────────────────────────────────────────────────────────────────────╯
```

- Rows are every profile in `config.toml`, from `Options.Accounts`, plus any the
  connect form added this session, sorted by name as `Config.Names()` sorts them.
- Columns: name, the endpoint's host, the profile's `database` when it has one, and the
  state.
- The overlay opens with the cursor on the current account.
- A failure is drawn under its row, wrapped rather than truncated, the way
  `Catalog.errorLines` treats a refusal. It stays until that account is tried again.
- The leading glyphs are `IconSet.Success`, the spinner, and `IconSet.Failure`; no new
  icon is needed.

| State | Meaning |
|---|---|
| `current` | the account the session is on |
| `connected` | visited earlier; its connection is open and its tree is kept |
| `connecting…` | an attempt is in flight |
| `not connected` | never opened this session, or disconnected with `x` |
| `failed` | the last attempt did not connect; the reason is under the row |

Elsewhere on screen:

- The status bar's first field, `StatusBar.profile` today, names the active account and
  the scope beside it is that account's. With none it reads `no account`.
- The results pane's title names the account its rows came from (`Results · staging`),
  because a result set outlives a switch (below).
- With no active account the catalog pane is empty but for `errNoAccount`'s text.

## Interaction

| Key | Where | Action |
|---|---|---|
| `ctrl+g` | main layout, any pane, the editor included | open the switcher |
| `ctrl+g` | switcher | close it, as `ctrl+o` closes history |
| `enter` | switcher | switch to the selected account, connecting it first if needed |
| `a` | switcher | add an account: the connect form, empty |
| `x` | switcher | disconnect the selected account |
| `/` | switcher | filter by name or endpoint |
| `↑/k`, `↓/j` | switcher | move; only the arrows while the filter has the keyboard |
| `esc` | switcher | clear the filter if there is one, else close |

Checked against `internal/tui/keys.go`, the README key table, and plans 11–13 and 15:

- `ctrl+g` (`Accounts`) is bound by nothing in `DefaultKeyMap`, reserved by no plan
  (15 takes `ctrl+s` and `ctrl+l`), and absent from the key map of `bubbles/textarea`,
  so it works while the editor has the keyboard, where it is wanted most. It joins
  `globalKeys` and `ShortHelp`, which is the hint that accounts can be switched at all.
- `a` (`AddAccount`) and `x` (`Disconnect`) are plain runes read only while the
  switcher is open and its filter is not focused, the routing that lets `q` quit from
  the history overlay. **They are not catalog bindings.** The catalog pane gains no key
  in this iteration; `a` and `x` stay free there for whoever needs them, beside the
  `n`, `c`, `d`, `t` and `i` that plans 11 and 12 reserve.
- `enter` in the switcher is a new binding, `Switch`, because the hint line has to say
  "switch". `Filter`, `Up`, `Down` and `Close` are the existing bindings.
- `x` is not `d`: a disconnect destroys nothing — the profile and its key stay where
  they are. Removing a profile remains `alchemist profile remove`.
- No function key is bound.

### Switching

`enter` on a row does one of four things:

| Row is | Result |
|---|---|
| `current` | the overlay closes |
| `connected` | the session moves there and the overlay closes; nothing is fetched |
| `not connected` or `failed` | the row turns `connecting…` and the overlay stays open until the attempt settles |
| an account with no secret anywhere | the connect form opens, seeded for it |

An attempt from the switcher opens the connection and pings it, under
`connectTimeout`. When it connects, the session moves there and the overlay closes.
When it fails, the connection is closed, the reason goes under the row, and **the
session stays on the account it was on**. `enter` again retries.

The switcher waits for one account at a time. `enter` on another row while one is
connecting moves the wait to the new row; the earlier attempt carries on and, if it
succeeds, leaves that account `connected` in the background. `esc` while waiting closes
the overlay and drops the wait the same way.

### The account you leave

Nothing about it is torn down. Its connection stays open, and its catalog — cached
nodes, expansion, cursor, inline failures — and its scope are kept, so switching back
is instant and looks the way it was left. `sales.orders` on `prod` is still selected
after a detour through `staging`, and it is never carried across: a container selected
in one account says nothing about another.

| Part | On a switch |
|---|---|
| catalog tree and scope | kept per account, restored on return |
| editor buffer | shared: there is one editor, and running the same text against the other account is the point |
| results on screen | kept, under their `Results · prod` title; detail, export, and `m` still work, since a cursor is bound to the connection that made it and that connection is still open |
| a run in flight | left to finish and shown; the next `ctrl+r` supersedes it as it would any run |
| catalog loads in flight | land in the state of the account they were made for, never in the visible tree |

The cost is one open connection and one cached tree per account visited. A Cosmos
client holds idle HTTP connections and polls nothing, so the cost is memory, and `x` is
the way to give it back: it ends that account's run if it has one, forgets its tree and
scope, and closes the connection.

`r` in the catalog is what it is today: the node under the cursor, or the top level,
of the account on screen and no other.

### The active account

The active account is the account the session is on. It decides which connection
`ctrl+r` runs on, which scope a bare `FROM c` resolves against, which `max_join_rows`
applies, which tree the catalog shows, and what the status bar names. It changes in
four ways and no others:

1. At launch: the profile named on the command line, or the default profile.
2. The switcher: `enter`, including on an account the connect form has just connected.
3. A connection arriving while no account is active.
4. `x` on the current account: the most recently used account that is still connected
   takes over; when there is none the active account is empty and the switcher, which
   is already open, stays open.

With no active account `ctrl+r` is refused with `errNoAccount` ("no account connected:
ctrl+g to choose one").

### The connect form

`panes.Connect` stops being a screen and becomes an overlay, `overlayConnect`, so it
can open over a running session. The `screen` type goes away. It opens in three cases:

| Case | Seeded with | `esc` |
|---|---|---|
| no profile exists (first run) | nothing, or the name given on the command line | quits: there is nothing behind it |
| an account has no secret anywhere (at launch, or `enter` in the switcher) | its name, endpoint and TLS choice, focus on the key | back to the switcher |
| `a` in the switcher | nothing | back to the switcher |

The first two are today's `setupLaunch` cases and keep their intro text. A submit
connects, pings, and saves exactly as `setupLaunch`'s closure does now; on success the
form and the switcher close and the session is on the new account.

The form never overwrites a working profile. Submitting a name that belongs to a
profile whose secret resolves is refused in the form with `config.ErrProfileExists`;
a name whose profile has no secret completes that profile; any other name adds one.
That rule lives in `cmd`, which is the only side that can see the config. The TUI adds
the one check only it can make: a name that is connected right now is refused before
the `Connector` is called, since an account connected with "remember the key" switched
off has no stored secret and would otherwise pass as a profile to complete.

## Contract for iteration 15

Saved queries are stored per account. What they may rely on:

- **The account identity is the profile name**: `config.Profile.Name`, the table key
  in `config.toml`. It is the same string as the keychain account under
  `config.KeychainService`, the input of `config.EnvKeyVar`, `history.Entry.Profile`,
  `tui.Account.Name`, and the status bar's first field. There is no second identifier
  and nothing derived from the endpoint.
- It matches `profileNamePattern` (`^[A-Za-z0-9_-]+$`), so it is usable unescaped as a
  file name, a path segment, a TOML key, and a map key. It is case-sensitive, and from
  this iteration `Config.Put` refuses a name that differs from an existing one only in
  case, so identities stay distinct on a case-insensitive filesystem.
- Identity follows the name, not the endpoint. Two profiles on one endpoint are two
  accounts; re-pointing a profile's endpoint keeps its identity and whatever is stored
  under it.
- There is no rename. `profile remove` followed by `profile add` under another name is
  a new identity, and removal cleans up nothing outside `config.toml` and the keychain
  — `history.jsonl` keeps its entries. Iteration 15 owns the question of orphans.
- A session started with `--adapter <name>` has one account whose identity is the
  adapter name (`mock`), as `launch{profile: s.adapter}` has it today. A profile that
  happens to share that name shares the identity.
- An account has its identity whether or not it is connected. `Options.Accounts` lists
  every one at startup; the connect form can add one mid-session.
- The active account is `Model.accounts.active`, empty when none. It changes only in
  `Model.setActive(account string) (Model, tea.Cmd)`, which is where a dependent
  feature hooks in; the command is how such a feature reloads what it shows for the
  new account.
- `accountSet.known(name string) bool` reports whether a name is an account of this
  session, connected or not, including one the connect form added mid-session.
- `ctrl+g` is a key of the main layout. An overlay swallows it like every other global
  key (`handleOverlayKey`). **Overlays do not switch accounts**: the way to see another
  account's data in an overlay is to close it, switch, and open it again. The account
  therefore cannot change under an open overlay through the keyboard; it can still
  change through case 3 above, which is why `setActive` stays the hook.

What moved since the draft plan 15 was written against, for reconciling it:

| Draft | Now |
|---|---|
| `NextAccount`: `ctrl+g` cycles to the next connected account | `Accounts`: `ctrl+g` opens the switcher (`overlayAccounts`) |
| an overlay may handle `NextAccount` itself | overlays do not switch accounts |
| `alchemist prod staging` connects several | one profile per launch, as today; others connect on first switch |
| `a` and `x` are catalog keys | they are switcher keys; the catalog has neither |
| `errNoAccount`: "…press enter on one in the catalog, or a to add one" | "no account connected: ctrl+g to choose one" |
| `enter` on a container can change the active account | it cannot; only the four ways listed above |

## Scope

- `internal/config`
  - `Config.Put` refuses a case-only collision with `ErrProfileExists`. Nothing else:
    no `default_profiles`, no `[accounts]` table.
- `internal/tui` — `accounts.go` (new) holds the set and the switcher's key handling;
  `app.go` loses the single-valued fields named under Goal, and `catalogPane`, and
  gains `accounts accountSet` and `runAccount string`.

  ```go
  // Account is one profile the session can be on.
  type Account struct {
      Name        string
      Endpoint    string // shown in the switcher; seeds the connect form
      SkipVerify  bool
      Database    string // expanded when the account's root first arrives
      MaxJoinRows int    // query.DefaultMaxJoinRows when zero
  }

  // Opener connects the saved account called name. ErrCredentialsNeeded sends
  // the session to the connect form instead of failing the attempt.
  type Opener func(ctx context.Context, name string) (adapter.Connection, error)

  var ErrCredentialsNeeded = errors.New("no key in the keychain or the environment")

  type Options struct {
      // ...Icons, Logger, History as today
      Accounts []Account // every profile, in Config.Names() order
      Launch   string    // the account the session starts on; empty on a first run
      Open     Opener
      Connect  Connector         // unchanged: connects and saves what the form submits
      Form     panes.ConnectForm // first run only: no account exists yet
  }
  ```

  `Options.Connection`, `Profile`, `Database` and `MaxJoinRows` are removed.
  - `accountSet` keeps, per account: the `Account`, a state (`disconnected`,
    `connecting`, `connected`, `failed` with its error), and for a connected one its
    `adapter.Connection`, its `adapter.Catalog`, **its own `panes.Catalog`**, and its
    scope. It also keeps `active`, the account the switcher is waiting for, and the
    order accounts were last used in. `known(name)` answers for any of them.
  - The catalog pane on screen is the active account's. `CatalogLoadedMsg` and `ErrMsg`
    gain an `Account` field and are applied to that account's pane, visible or not;
    `selectNode`, `refreshNode`, `prefetch` and `openDefaultDatabase` take the account
    they act on instead of reaching for `m.catalogPane`. A message for an account that
    is not connected is dropped.
  - `resize` sizes every account's pane, so a switch needs no layout pass. `animate`
    forwards spinner ticks to every account's pane; each spinner answers only to its
    own ticks and each pane already stops its own animation once nothing is loading.
  - **The TUI owns every connection it holds.** `cmd` no longer opens one, its deferred
    `Close` goes, and so does `ownsConnection`. `quit` closes every connected account
    synchronously, as it closes the one today; `x` closes in a `tea.Cmd` and logs a
    failure.
  - `Init` issues `openAccount` for `Options.Launch`. The launch attempt does not
    ping, as `profileLaunch` does not today: there is nowhere else to stay, so the
    session moves onto the account at once and an unreachable one shows in the catalog
    with `r to retry`, exactly as now. An attempt from the switcher pings, because
    there "stay where you were" means something.
  - Messages: `AccountConnectedMsg{Account string; Connection adapter.Connection}`
    replaces `ConnectedMsg` and is what both the form and `Opener` deliver. A new
    `OpConnect` on `ErrMsg` carries an `Opener` failure to the account's row.
    `ConnectFailedMsg` stays the form's. `ScopeChangedMsg` is unchanged: it can only
    come from the tree on screen, which is the active account's.
  - Late answers: an `AccountConnectedMsg` for an account that is not `connecting`
    closes the connection it carries, the way `loadPage` closes the cursor of a
    superseded run. That covers `x` pressed during an attempt.
  - `setActive(account string) (Model, tea.Cmd)`: records the account as most recently
    used, points the status bar at it and its scope, and returns the spinner tick when
    its pane still has loads in flight. It touches neither the editor, the results,
    nor the run.
  - Disconnect: when `runAccount` is the account going away, `endRun` and bump
    `Model.run` first, so a page still in flight is discarded and its cursor closed on
    arrival; rows already loaded stay on screen. Then drop the account's pane and
    scope, then close.
  - Runs: `startRun` takes the connection and `MaxJoinRows` of the active account into
    `query.Engine` and records `runAccount`, which titles the results pane.
    `fetchMore` needs nothing new.
  - `overlayAccounts` and `overlayConnect`. The form remembers whether it was opened
    over the switcher and returns there on `esc`.
  - `KeyMap`: `Accounts` (`ctrl+g`, "accounts") in `globalKeys` and `ShortHelp`;
    `AccountsKeys()` returns `Filter, Switch, AddAccount, Disconnect` for the overlay's
    hint line, like `HistoryKeys()`. The drift-guard test concatenates the new group.
- `internal/tui/panes`
  - `accounts.go` — `Accounts`, shaped like `History`:

    ```go
    type AccountState int

    const (
        AccountDisconnected AccountState = iota
        AccountConnecting
        AccountConnected
        AccountFailed
    )

    type AccountRow struct {
        Name     string
        Endpoint string
        Database string
        State    AccountState
        Current  bool
        Err      error // set when State is AccountFailed
    }

    func NewAccounts(icons theme.IconSet, keys []key.Binding) Accounts
    func (a Accounts) SetRows(rows []AccountRow, current string) Accounts
    func (a Accounts) Selected() (AccountRow, bool)
    ```

    plus `SetSize`, `Update`, `StartFilter`, `Filtering`, `ClearFilter`, `CursorUp`,
    `CursorDown`, `View`, as `History` has them. `SetRows` keeps the cursor on the row
    of the same name, so a state change never moves it. `Update` also runs the spinner
    while any row is connecting.
  - The filter line, `matches`, the cursor window and `body` are the code `History`
    already has. Plan 15 extracts them into an unexported `filterList` for its own
    overlay; `Accounts` is the third user. Whichever of 14 and 15 lands first does the
    extraction, with `History`'s tests unchanged, and the other builds on it.
  - `Catalog`: unchanged. One instance per connected account, from `NewCatalog`.
  - `Connect`: unchanged but for being shown as an overlay.
  - `StatusBar.SetProfile` becomes `SetAccount`; `Results` gains `SetSource(account)`
    for its title.
  - `History`: `SetEntries` takes the account and the title becomes `History · prod`,
    the form plan 15 gives `Saved queries · prod`. No account column: every row is the
    same account's.
- `internal/history` — `Store.Recent(account string, n int)`; see "History" below.
- `internal/tui/history.go`, `commands.go`, `messages.go` — `loadHistory` asks for the
  active account; `HistoryLoadedMsg` gains `Account`.
- `internal/query` — `SourcePaths(text string) [][]string`, the dotted path of every
  `FROM`/`JOIN` source, from the parser's existing `source.path`. `BuildPlan`, `Plan`
  and `Engine` are untouched and stay ignorant of accounts.
- `cmd/root.go` — `launch` carries `accounts`, `name`, `open`, `connect`, `form`.
  `profileLaunch` validates the name and builds the `Opener` over
  `config.SecretResolver` and the registry, mapping `config.ErrSecretNotFound` to
  `tui.ErrCredentialsNeeded`. Both the `Opener` and the `Connector` reload the config
  on each call, because the form may already have added a profile this session.
  Concrete adapters are still named here and nowhere in `internal/tui`.
- `README.md` — an "Accounts" paragraph under "Profiles"; the key table; the history
  paragraph.

### Command line

Unchanged. `Args` stays `cobra.MaximumNArgs(1)`.

| Invocation | Result |
|---|---|
| `alchemist` | the session starts on the default profile — as today |
| `alchemist prod` | it starts on `prod` — as today |
| `alchemist --adapter mock` | one account, `mock` — as today; with a profile name it is still an error |
| no profile exists | the connect form — as today |

There is no `alchemist prod staging`, no `--all` flag and no `default_profiles` key.
Every other profile is `ctrl+g`, `enter` away and connects the first time it is
switched to, so connecting several up front buys a second of latency once, and
connecting everything by reflex is how a query meant for `staging` reaches `prod`. An
unknown name and a missing default still fail before the TUI starts, with
`config.ErrProfileNotFound` and `config.ErrNoDefaultProfile`.

One behavior moves. A connect error at launch that is not about a missing key — a
malformed setting the adapter rejects — exits `alchemist prod` with a message today; it
now opens the switcher with the reason under the `prod` row. Opening a Cosmos client
makes no request, so in practice this path is rare and the first real failure was
always the catalog load, which still renders in the tree.

### Per-profile settings

| Key | Where it lands |
|---|---|
| `page_size` | inside `Profile.Settings`, so inside that account's connection; nothing to do |
| `insecure_skip_verify` | likewise, and `Account.SkipVerify` for seeding the form |
| `max_join_rows` | `Account.MaxJoinRows`, read for the account a run goes to |
| `database` | `Account.Database`, expanded once in that account's own tree, whether or not it is on screen when its root arrives |

### Queries and iteration 10

A query runs on one account: the one the session is on. `query.Engine` executes a plan
over one `adapter.Connection`, iteration 10 put cross-account joins out of scope, and
they stay there. There is deliberately no account qualifier in the query language:
`FROM prod.sales.orders` is already valid Cosmos SQL (a container known as `prod`, sub
root `sales.orders`), so a three-part source cannot be claimed without changing the
meaning of queries that work today.

Someone will write it anyway, so it gets an answer rather than a service syntax error.
Before planning, `resolvePlan` checks `query.SourcePaths`: a source of three or more
parts whose first part is `known` to the account set is refused with

```
FROM staging.sales.customers: a query runs on the account you are on. Remove the
account name, and switch accounts with ctrl+g.
```

(`errAccountInQuery`, wrapped with the source). It is shown in the results pane and
recorded in history like every other refusal since iteration 10. A container alias
that really is called `prod` and is read through a three-part path trips the same
check; the fix is a different alias, and the message names the source so that is
findable. Union and join over containers of the active account work as they do now.

### History

History is scoped to the account the session is on. `ctrl+o` lists the entries whose
`history.Entry.Profile` is the active account and no others, under the title
`History · prod`. A query that ran on `staging` is found by switching to `staging`.

- The log stays one `history.jsonl`. `Entry.Profile` is already written on every
  entry, so there is no migration, and entries recorded before this iteration sort
  themselves under the profile they ran on. An `--adapter mock` session sees what was
  recorded under `mock`.
- `newHistoryEntry` takes the account from the active account rather than from
  `Model.profile`.
- The filtering happens in the store, not in the overlay: `Store.Recent(n int)` becomes
  `Recent(account string, n int)`, "at most n entries of that account, newest first".
  `File.Recent` already walks the lines from the end and stops at `n`; it now skips
  the entries of other accounts on the way. Filtering afterwards would turn
  `recentHistory` (500) into "whatever of this account is among the newest 500 of all
  accounts", which for a rarely used account is a handful or nothing. `Discard` follows
  the signature. Trimming (`MaxEntries`, `KeepEntries`) stays over the whole file.
- `HistoryLoadedMsg` carries the `Account` it was loaded for and is dropped when that
  is no longer the active one. The overlay opens on the response, so a dropped response
  opens nothing. The account cannot change while the overlay is open through the
  keyboard — overlays swallow `ctrl+g` — so there is no reload-under-the-overlay case.
- With no active account `ctrl+o` opens the overlay on `errNoAccount`'s text.
- Recall and recall-and-run are exactly what they are today: every entry listed belongs
  to the active account, so its scope is restored into the account it came from and a
  recalled query can never run anywhere else. There is no cross-account case to define.

## Out of scope

- Cross-account and cross-adapter joins or unions, and any account qualifier in `FROM`.
  Already listed as future work in `00-overview.md`.
- Showing more than one account's tree at once, or a results pane per account.
- Connecting several accounts from the command line, a `default_profiles` key, an
  `--all` flag, or restoring last session's accounts.
- Editing or removing a profile from the TUI; the form adds and completes only.
- Renaming a profile, and migrating what is keyed on its name.
- A per-account editor buffer.
- Browsing another account's history without switching to it, and a history across
  all accounts. `jq` over `history.jsonl` reads everything.
- Switching accounts from inside another overlay.
- An info view for an account (iteration 12's `i`): the SDK does not expose account
  properties, as that plan records.

## Relationship to other iterations

- **6, profiles.** Unchanged on disk. `alchemist profile …` is still where profiles
  are edited and removed.
- **7, history.** Same file, same entry. `Store.Recent` takes the account, the overlay
  is titled with it, and recall is unchanged. `History` and `Accounts` share the
  filter-list code.
- **10, cross-container.** Untouched. The engine gets the active account's connection
  and cap; `LeafCharges` keys stay `db.container` because a run never spans accounts.
- **11, catalog management.** Its plan asserts `CatalogAdmin` and `ThroughputEditor`
  once in `cmd`. Connections now arrive mid-session, so the comma-ok assertion moves
  to where `AccountConnectedMsg` lands — still against `internal/adapter` interfaces
  only, so `depguard` is unaffected — and is kept per account; the bindings enable per
  the active account's capability. `CatalogChangedMsg` and the fetch tokens carry the
  account, so a reload after a create lands in that account's pane even if the user
  has switched away. Its `n`, `c`, `d`, `t` have the catalog to themselves: this
  iteration adds no catalog key.
- **12, info view.** `Inspector` is asserted per account the same way; the `Info`
  pane's cache is keyed by account and path; `DetailsLoadedMsg` carries the account.
- **13, autocomplete.** One `complete.Index` per account, created on connect and
  dropped on `x`. Suggestions come from the active account's index; pages feed the
  index of `runAccount`, not of whichever account is active when they arrive.
  `FieldsSampledMsg` and `OpSampleFields` carry the account beside the container path,
  and "one sampling query per container per session" becomes per container per
  account. `sample_fields` becomes a field of `Account`. Account names are never
  offered as completions, since no query position takes one.
- **15, saved queries.** Builds on "Contract for iteration 15", including its table of
  what moved.

None of 11–13 has landed. Whichever lands after this iteration adopts the account
field from the start; if one lands first, adding the field is part of step 2 here.

## Steps

1. `config.Put` case-collision rule; `query.SourcePaths`. Both pure, both tested alone.
2. Root model: `accountSet` holding one account, `Options.Accounts`/`Launch`/`Open`,
   the TUI owning the connection, `AccountConnectedMsg`, account-carrying catalog
   messages applied to a per-account pane. `cmd` builds the `Opener`. The test helpers
   `newModel`/`newModelWith` move to the new `Options` here, which is what keeps the
   existing suites passing unedited. Ships: nothing visible; the same session on new
   foundations.
3. The connect form as `overlayConnect`; first-run and missing-key flows through it.
4. `panes.Accounts` (extracting `filterList` if 15 has not), `overlayAccounts`,
   `ctrl+g`, lazy connect with ping, failure in the row, `setActive` restoring tree and
   scope, the status bar and the results title. Ships: switching between profiles.
5. `a` and `x` in the switcher; the fallback when the current account is disconnected;
   `errNoAccount`.
6. `errAccountInQuery`; `history.Store.Recent(account, n)`, the account-carrying
   `HistoryLoadedMsg`, and the overlay title.
7. README, help sections, plan statuses.

## Testing

**Unit — `config`:** `Put` refuses `Prod` when `prod` exists and accepts replacing
`prod` itself; a file holding both fails `validate`.

**Unit — `history`:** `Recent("prod", 2)` over a log interleaving two accounts returns
the two newest `prod` entries even when more than two newer `staging` entries follow
them; an account with no entries is an empty list, not an error; an entry with an
empty `Profile` is returned for no account; a malformed line is still skipped;
`Discard` returns nothing.

**Unit — `query.SourcePaths`:** one source; a container list; a cross-container join; a
`JOIN … IN` source; a three-part path; a source inside a subquery; a path inside a
string literal is not a source; arbitrary input never panics (shares the planner's
fuzz corpus).

**Unit — `Accounts` pane:** rows draw in the order given with each state's label and
glyph; the cursor opens on the current account; a failure wraps under its row; `/`
narrows by name and by endpoint; `esc` clears the filter first; `a` and `x` typed into
the filter are text; `SetRows` with a changed state keeps the cursor on the same name;
a pane too short for every row keeps the cursor row and the hint line on screen.

**TUI, mock adapter** (`internal/tui/test/accounts_test.go`; the `Opener` hands out
`mock.New()` connections, with `mock.WithError` and `mock.WithLatency` per account):
- Launch: the session starts on `Options.Launch`, its default database expanded; the
  tree has no account row; `Open` was called once, for that account only.
- `ctrl+g` opens the switcher from the catalog, the results, and the editor, and types
  nothing into the buffer; `ctrl+g` and `esc` close it; every profile is listed.
- `enter` on a not-connected account calls `Open` once and pings; the overlay stays
  open with the row connecting; on success the overlay closes, the status bar names the
  new account, and the catalog shows its databases.
- A failed `Open`, and a failed ping, each leave the session on the account it was on
  with its tree and scope intact, show the reason under the row, close the connection
  that failed its ping, and retry on the next `enter`.
- `enter` on a connected account switches with no `Open` and no catalog request.
- Switching away and back restores the expanded nodes, the cursor, and the scope; the
  scope of one account is never shown for the other.
- The editor buffer and the results on screen survive a switch; the results title keeps
  the account they came from; `m` still fetches from that account's connection.
- A run in flight when the switch happens lands in the results pane, titled with the
  account it ran on; the recording connections prove `ctrl+r` after the switch reaches
  the new account and not the old.
- A `CatalogLoadedMsg` and an `ErrMsg` for an account in the background change nothing
  on screen and are there when it is switched back to.
- `ErrCredentialsNeeded` opens the form seeded for that account; `esc` returns to the
  switcher; a submit lands on the account with both overlays closed.
- `a`, submit: the new account is listed, sorted into place, and current. The form
  refuses a name that is connected.
- `x` on an account in the background closes its connection once and its row reads
  `not connected`; switching to it again starts from a collapsed tree.
- `x` on the current account with another connected: the session moves to the most
  recently used one. With none: the status bar reads `no account`, the switcher stays
  open, and `ctrl+r` shows `errNoAccount`.
- `x` on the account of a run with a page in flight: the late page is discarded, its
  cursor closed, the connection closed once, loaded rows still on screen, `m` fetches
  nothing.
- An `AccountConnectedMsg` arriving after `x` closes the connection it carries.
- `enter` on a second row while the first is connecting: the session lands on the
  second; the first ends up connected in the background.
- `quit` closes every connected account exactly once and no disconnected one.
- `a` and `x` pressed in the catalog do nothing, and neither is in the Catalog help
  section.
- With no profiles the session opens on the form and `esc` quits.
- `FROM other.sales.orders c` is refused with `errAccountInQuery`, reaches no
  connection, and is recorded; the same text with a first part that is no account
  passes through.
- History: an entry records the active account; `ctrl+o` lists only the active
  account's entries, titled with its name; switching and reopening lists the other
  account's and none of the first's; a `HistoryLoadedMsg` for an account that is no
  longer active opens nothing; `ctrl+g` pressed in the overlay does nothing; with no
  active account the overlay shows `errNoAccount`'s text; recall and rerun behave as
  the existing history tests already require.
- Every new binding is grouped exactly once and appears in the help overlay and README.

**Unit — `cmd`:** no argument and one argument resolve to the right `Launch`; two
arguments are still refused; an unknown name fails before any state directory is
created; `Options.Accounts` lists every profile; the `Opener` maps a missing secret to
`tui.ErrCredentialsNeeded` and sees a profile the form added after startup; the
`Connector` refuses a name whose profile has a secret, completes one that has none, and
adds any other.

**Integration (emulator):** `test/integration/accounts_test.go` — two profiles on the
emulator endpoint with different `database` values are two accounts: start on one, run
a query, switch, run it again, and each account's history lists its own run only. A
third profile on a closed port fails in the switcher and leaves the session where it
was.

**Manual checklist:**
- [ ] `alchemist` and `alchemist <profile>` look and behave as before this iteration.
- [ ] With an empty config, `alchemist` opens the form; submitting lands in the tree.
- [ ] `ctrl+g` from the editor, `enter` on another profile: the row spins, the overlay
      closes, the status bar and the catalog change, the buffer does not.
- [ ] Run the same query on two accounts with a switch between; the results title
      follows the run, not the switch.
- [ ] Expand a few databases, select a container, switch away and back: the tree and
      the scope are as they were left, with no spinner.
- [ ] Delete a profile's keychain entry, `enter` on it in the switcher: the form asks
      for the key; `esc` returns to the switcher.
- [ ] Stop the emulator, switch to it: the failure sits under its row and the session
      has not moved; start it, `enter` again connects.
- [ ] `ctrl+o` on each of two accounts: each lists its own runs; runs recorded before
      this iteration are under the profile they ran on.
- [ ] `a`: add a profile from inside the session; `alchemist profile list` shows it.
- [ ] `x` on the current account mid-query: the TUI stays alive, the session falls back
      to the previous account, and the log shows one close.
- [ ] Twenty profiles in the config: `/` finds one; 80×24 keeps the hint line.

## Acceptance criteria

- `alchemist` and `alchemist <profile>` start on the same profile, with the same
  settings, the same catalog, and the same keys as before. The main layout gains
  nothing but a results title.
- Every profile can be switched to from inside one session, connecting the first time
  and instantly afterwards, with the tree and scope of each account restored as they
  were left.
- A switch that cannot connect changes nothing but the row that says why.
- A query runs on the account the session is on and nowhere else; the status bar always
  names that account, the results title always names the account its rows came from,
  and the history overlay lists the active account's runs only — up to `recentHistory`
  of them, however many other accounts ran in between — so a recalled query never runs
  on an account other than the one it was recorded on.
- A query that names an account is refused with a message that says what to do; no
  cross-account result is ever produced.
- No response made for one account is ever applied to another's tree, including those
  that arrive after a switch.
- Every connection the session opened is closed exactly once, on `x` or on quit.
- The account identity is the profile name everywhere it appears: switcher, status
  bar, results title, history title and entries, keychain, and the contract above.
- `Update` never blocks: connecting, pinging, listing and closing run in `tea.Cmd`s
  with timeouts, and a hung connect leaves the current account, `esc`, and `ctrl+c`
  working.
- `internal/tui` imports the `internal/adapter` interfaces only; every concrete adapter
  and all config and keychain access stay in `cmd`.
