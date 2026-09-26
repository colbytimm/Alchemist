# 5. History and Saved Queries

- [5.1. Query history](#51-query-history)
- [5.2. Saved queries](#52-saved-queries)
- [5.3. Saved queries on disk](#53-saved-queries-on-disk)

## 5.1. Query history

Every query that reaches the account is appended to `history.jsonl` under
`$XDG_STATE_HOME/alchemist` (`~/.local/state/alchemist` by default), beside the log
file: the query as typed, the scope it ran in, and its statistics, never a key or an
endpoint. Failed queries are recorded too, with the error, since fixing one is the
usual reason to look back.

`ctrl+o` opens the history of the account the session is on, newest first; switch
accounts to see another's.

| Key | Action |
|---|---|
| `/` | filter by query text or scope |
| `enter` | load the selected query into the editor, with its scope restored |
| `ctrl+r` | load it and run it at once |
| `ctrl+s` | save the selected query under a name |

`alchemist --history=false` records nothing for that session.

The file is one JSON object per line, so `jq . < history.jsonl` reads it. A line a
session never finished writing is skipped, and the file is trimmed to its newest
2,500 entries once it passes 5,000.

## 5.2. Saved queries

`ctrl+s` saves the query in the editor under a name, for the account the session is
on. `ctrl+l` lists that account's saved queries by name, with the same keys as
history, and two more:

| Key | Action |
|---|---|
| `/` | filter by name, text or scope |
| `enter` | load the selected query into the editor |
| `ctrl+r` | load it and run it |
| `r` | rename |
| `d`, then `y` | delete |

To update a saved query, load it, edit it, press `ctrl+s` (the name is filled in) and
end the name with `!` to replace it. A name is letters, digits, spaces, `.`, `-` and
`_`, up to 64 characters.

## 5.3. Saved queries on disk

Each query is a plain `.sql` file under `queries/<account>/` in the config directory
(`~/.config/alchemist/queries/prod/open orders.sql`), so `ls`, `cat`, `mv` and `rm`
work on them, and a `.sql` file dropped there is listed the next time the overlay
opens. A query saved against the selected container starts with one header line
recording it, and loading it selects that container again:

```sql
-- alchemist: scope=sales/orders
SELECT c.id, c.total FROM c WHERE c.status = "open"
```

A query that names its own containers (`FROM sales.orders c`) is saved without one.

`alchemist profile remove <name>` keeps the profile's saved queries and snapshots and
says where they are, so a profile removed and added again under the same name finds
them. `--purge` deletes them too.

---

[← 4. Results and Export](results.md) · [Contents](../README.md) · [6. Querying Across Containers →](../language/cross-container.md)
