# 2. A Tour of the Workspace

- [2.1. The three panes](#21-the-three-panes)
- [2.2. Choosing what a query reads](#22-choosing-what-a-query-reads)
- [2.3. Running a query](#23-running-a-query)
- [2.4. Reading the results](#24-reading-the-results)
- [2.5. Where to go next](#25-where-to-go-next)

## 2.1. The three panes

![The catalog, the editor and the results, after a query of sales.orders](../images/workspace.png)

The screen holds the catalog, the editor, and the results. `tab` moves to the next
pane and `shift+tab` to the previous one; `e` jumps to the editor from anywhere. `?`
lists the bindings of the pane you are in, and `esc` closes whatever overlay is open.

- The **catalog** is the account's databases and their containers, loaded as you
  expand them. `↑/k` and `↓/j` move, `enter` or `space` expands and collapses, and `r`
  reads the node under the cursor again.
- The **editor** holds one query, a batch, an update or a delete. While it has the
  keyboard, plain letters are text: `q` does not quit there, and `ctrl+c` always does.
- The **results** pane shows the rows of the last query.

The status bar at the bottom leads with the account the session is on.

`?` opens the key overlay:

![The key overlay, grouped by pane](../images/help.png)

## 2.2. Choosing what a query reads

Select a container in the catalog and queries run against it:

```sql
SELECT c.id, c.total FROM c WHERE c.status = "open"
```

Or name one in the query itself, and the catalog's selection does not matter:

```sql
SELECT o.id, o.total FROM sales.orders o WHERE o.status = "open"
```

## 2.3. Running a query

`ctrl+r` runs what is in the editor, from any pane. Queries are cross-partition by
default. While you type, the editor [highlights](../using/editor.md#31-highlighting)
the query, flags what it can tell is
[wrong](../using/editor.md#32-diagnostics), and offers
[completions](../using/editor.md#34-autocomplete).

Every query that reaches the account is kept in the
[history](../using/history.md#51-query-history), which `ctrl+o` opens. `ctrl+s` saves
the editor's query under a name, and `ctrl+l` opens the saved ones.

## 2.4. Reading the results

The status bar shows the request charge (RU) of the query. Results come a page at a
time, following the service's continuation tokens: when the status bar says
`(+more)`, `m` fetches the next page.

In the results pane, `↑/k` and `↓/j` move between rows, `h/←` and `l/→` scroll
sideways, and `enter` opens the row's full document. `ctrl+e`
[exports](../using/results.md#42-exporting-a-result-set) the rows fetched so far to a
JSON or CSV file.

## 2.5. Where to go next

- [Chapter 6](../language/cross-container.md) joins and unions containers, which the
  service cannot.
- [Chapters 7 to 9](../language/transactions.md) write: transactional batches, and
  updates and deletes by query.
- [Part IV](../data/profiles.md) manages accounts, databases and containers, copies
  them, and keeps snapshots of them.
- The [key reference](../reference/keys.md) lists every binding.

---

[← 1. Getting Started](getting-started.md) · [Contents](../README.md) · [3. Writing Queries →](../using/editor.md)
