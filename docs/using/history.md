# History and saved queries

## History

Every query that reaches the account is recorded in
`~/.local/state/alchemist/history.jsonl`, failed ones included. An entry holds the
query, the container it ran on, and its statistics. It never holds a key or an
endpoint.

`ctrl+o` opens the history of the current account, newest first.

| Key | Action |
|---|---|
| `/` | filter by text or container |
| `enter` | load the query into the editor |
| `ctrl+r` | load and run it |
| `ctrl+s` | save it under a name |

`--history=false` turns recording off for a session. The file has one JSON object per
line, so `jq` reads it. It is trimmed to the newest 2,500 entries once it passes 5,000.

## Saved queries

`ctrl+s` saves the editor's query under a name. `ctrl+l` lists the current account's
saved queries.

| Key | Action |
|---|---|
| `/` | filter by name, text or container |
| `enter` | load into the editor |
| `ctrl+r` | load and run |
| `r` | rename |
| `d`, then `y` | delete |

To change a saved query, load it, edit it, press `ctrl+s`, and add `!` to the end of
the name to replace it. Names can use letters, digits, spaces, `.`, `-` and `_`, up to
64 characters.

Each query is a `.sql` file in `~/.config/alchemist/queries/<account>/`, so you can
manage them with ordinary file tools. Files you add there show up in the list. A query
saved with a container selected starts with a header line, and loading it selects
that container again:

```sql
-- alchemist: scope=sales/orders
SELECT c.id, c.total FROM c WHERE c.status = "open"
```

`alchemist profile remove` keeps a profile's saved queries unless you pass `--purge`.
