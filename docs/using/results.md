# 4. Results and Export

- [4.1. The results pane](#41-the-results-pane)
- [4.2. Exporting a result set](#42-exporting-a-result-set)
- [4.3. Where the file lands](#43-where-the-file-lands)

## 4.1. The results pane

The results pane shows the rows fetched so far, one column per field. Its title names
the account the rows came from, which is not always the one the session is on now.

| Key | Action |
|---|---|
| `↑/k`, `↓/j` | move between rows |
| `h/←`, `l/→` | scroll left, scroll right |
| `enter` | the row's full document |
| `m` | fetch the next page, while the status bar says `(+more)` |
| `ctrl+e` | export to a file |
| `ctrl+b` | add the row to a [batch](../language/transactions.md#72-review-and-commit) |

The status bar carries the request charge. A [simulated](../language/cross-container.md)
result is marked `simulated (client-side)`, and its charge is broken down per
container.

## 4.2. Exporting a result set

`ctrl+e` in the results pane asks for a file name and writes the rows fetched so far.
The extension picks the format, and `tab` switches the name between the two:

- `.json` writes the original documents as an indented array, exactly as the account
  returned them.
- `.csv` writes the columns in the order the results pane shows them, with nested
  objects and arrays as compact JSON in their cell.

Export never fetches. If the status bar says `(+more)`, press `m` until it does not,
or export the part you have.

## 4.3. Where the file lands

A bare name such as `results.json` lands in the directory Alchemist was started from.
A path works too, relative, absolute, or starting with `~/`, and folders it names that
do not exist yet are created. The prompt shows the full path it will write to as you
type. An existing file is left alone unless the name ends in `!`, as in
`~/exports/orders.csv!`.

---

[← 3. Writing Queries](editor.md) · [Contents](../README.md) · [5. History and Saved Queries →](history.md)
