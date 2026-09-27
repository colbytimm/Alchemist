# Results and export

## The results pane

| Key | Action |
|---|---|
| `↑/k`, `↓/j` | move between rows |
| `h/←`, `l/→` | scroll sideways |
| `enter` | open the row's document |
| `m` | fetch the next page |
| `ctrl+e` | export to a file |
| `ctrl+b` | add the row to a [batch](../language/transactions.md) |

The status bar shows the row count, the request charge and the elapsed time.
`(+more)` means another page is available. The pane's title names the account the
rows came from.

## Export

`ctrl+e` writes the rows fetched so far. The file extension sets the format, and `tab`
switches between the two:

- `.json`: the documents as returned, in an indented array.
- `.csv`: the columns in the order shown, with nested values as compact JSON.

Export does not fetch more pages. Press `m` until `(+more)` disappears to export
everything.

A bare file name is written to the directory you started Alchemist from. Relative,
absolute and `~/` paths work, and missing folders are created. The prompt shows the
full path. Alchemist will not overwrite a file unless the name ends in `!`, as in
`~/exports/orders.csv!`.
