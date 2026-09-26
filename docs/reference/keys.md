# I. Key Bindings

Every binding, grouped by where it applies. `?` lists the same bindings inside the
app, for the pane you are in. While the editor has the keyboard, plain letters are
text; `ctrl+c` always quits.

![The key overlay, grouped by pane](../images/help.png)

- [Anywhere](#anywhere)
- [Catalog](#catalog)
- [Results](#results)
- [Editor](#editor)
- [Batch review](#batch-review)
- [Update and delete](#update-and-delete)
- [Clone progress](#clone-progress)
- [Saved queries](#saved-queries)
- [Snapshots and diffs](#snapshots-and-diffs)
- [Account switcher](#account-switcher)

## Anywhere

| Key | Where | Action |
|---|---|---|
| `tab` / `shift+tab` | anywhere | next pane / prev pane |
| `e` | anywhere | editor |
| `ctrl+r` | anywhere | run query |
| `ctrl+o` | anywhere | history |
| `ctrl+s` | anywhere, history | save query |
| `ctrl+l` | anywhere | open saved |
| `ctrl+g` | anywhere | accounts |
| `?` | anywhere | help |
| `esc` | anywhere | close |
| `q` | anywhere but a text field | quit |

## Catalog

| Key | Where | Action |
|---|---|---|
| `↑/k`, `↓/j` | catalog, results | up, down |
| `enter/space` | catalog | expand/collapse |
| `r` | catalog | refresh node |
| `n` | catalog | new database |
| `c` | catalog | new container |
| `d` | catalog | delete node |
| `t` | catalog | throughput |
| `i` | catalog | node info |
| `y` | catalog | clone; with a clone under way, show it |
| `s` | catalog, snapshots | take snapshot |
| `v` | catalog | snapshots; with a capture under way, show it |
| `w` | catalog, an update or delete under way | show update/delete job |

See [Managing the Catalog](../data/catalog.md), [Cloning](../data/cloning.md) and
[Snapshots](../data/snapshots.md).

## Results

| Key | Where | Action |
|---|---|---|
| `enter` | results | row detail |
| `h/←`, `l/→` | results | scroll left, scroll right |
| `m` | results | fetch more |
| `ctrl+e` | results | export to file |
| `ctrl+b` | results, row detail | add to batch |

See [Results and Export](../using/results.md).

## Editor

| Key | Where | Action |
|---|---|---|
| `ctrl+space` | editor | complete |
| `tab` | editor, list open | accept suggestion |
| `↑/↓`, `esc` | editor, list open | choose, dismiss |

See [Autocomplete](../using/editor.md#34-autocomplete).

## Batch review

| Key | Where | Action |
|---|---|---|
| `enter` | batch review, name typed | commit |
| `↑/↓` | batch review | scroll |

See [Transactional Batches](../language/transactions.md#72-review-and-commit).

## Update and delete

| Key | Where | Action |
|---|---|---|
| `enter` | update or delete review, confirmation typed | start |
| `↑/↓` | update or delete review | scroll |
| `esc` | update or delete progress, running | hide |
| `x` | update or delete progress, running | stop |
| `r` | update or delete progress, ended short | resume |
| `esc`, `enter` | update or delete progress, ended | report |

See [Updating by Query](../language/update.md#84-the-job).

## Clone progress

| Key | Where | Action |
|---|---|---|
| `esc` | clone progress | hide; close once the clone has ended |
| `x` | clone progress, running | stop |
| `r` | clone progress, ended short | resume |
| `d` | clone progress, ended short | delete the partial target |

See [Cloning](../data/cloning.md#124-progress-stop-and-resume).

## Saved queries

| Key | Where | Action |
|---|---|---|
| `r` | saved queries | rename |
| `d`, then `y` | saved queries | delete |

See [Saved queries](../using/history.md#52-saved-queries).

## Snapshots and diffs

| Key | Where | Action |
|---|---|---|
| `space` | snapshots | mark |
| `enter` | snapshots | diff |
| `d`, then `enter` | snapshots | delete snapshot |
| `x` | snapshots, capturing | cancel capture |
| `enter` | snapshots, note prompt | take |
| `enter` | diff | fields |
| `tab` | diff | all/added/removed/modified |

See [Snapshots](../data/snapshots.md#131-taking-and-comparing).

## Account switcher

| Key | Action |
|---|---|
| `enter` | switch to the selected account, connecting it first if it is not connected |
| `a` | add account: the connect form, empty |
| `x` | disconnect the selected account |
| `/` | filter by name or endpoint |
| `esc` | clear the filter, or close |

See [Switching accounts](../data/profiles.md#102-switching-accounts).

---

[← 13. Snapshots](../data/snapshots.md) · [Contents](../README.md) · [II. Statements →](statements.md)
