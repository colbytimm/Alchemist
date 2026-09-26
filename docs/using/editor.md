# 3. Writing Queries

- [3.1. Highlighting](#31-highlighting)
- [3.2. Diagnostics](#32-diagnostics)
- [3.3. Squiggles and terminals](#33-squiggles-and-terminals)
- [3.4. Autocomplete](#34-autocomplete)
- [3.5. Where field names come from](#35-where-field-names-come-from)

## 3.1. Highlighting

The editor colors the query as you type it, focused or not:

| What | Examples | Drawn |
|---|---|---|
| Clause keyword | `SELECT`, `FROM`, `WHERE`, `ORDER BY`, `JOIN`, `VALUE` | amethyst, bold |
| Operator word | `AND`, `OR`, `NOT`, `IN`, `LIKE`, `BETWEEN`, `EXISTS` | amethyst |
| Literal | `true`, `null`, `undefined` | copper |
| Function | `STARTSWITH(`, `COUNT(`, `udf.discount(` | gold |
| Alias | the `c` of `FROM c` and of `c.total` | parchment, bold |
| Parameter | `@minTotal` | copper, italic |
| String, number | `'west'`, `1.5e3` | verdigris, copper |
| Comment | `-- note` | ash, italic |
| Punctuation | `( ) , . = < + ??` | ash |

Properties stay plain text, since they are most of any query. A batch is colored with
its own words (`BEGIN BATCH`, `PARTITION`, `UPSERT`, `IF MATCH`, `COMMIT`), and an update
or a delete with a query's plus `UPDATE`, `SET`, `UNSET` and `DELETE`. Every color is
one of the theme's, and adapts to a light or a dark terminal as the panes do.

## 3.2. Diagnostics

What Alchemist can tell is wrong from the text alone gets a red squiggle, and the hint
line at the bottom of the editor says why while the cursor is on it:

| Flagged | Example | Hint |
|---|---|---|
| An unterminated string | `WHERE c.region = "west` | `unterminated string: close it with "` |
| A character Cosmos SQL does not use | `c.total # 5` | `"#" is not part of Cosmos SQL` |
| An unknown function | `CONTAIN(c.name, "A")` | `unknown function CONTAIN: did you mean CONTAINS?` |
| An alias the query never declares | `SELECT o.id FROM c` | `o is not declared: the query reads c` |
| A misspelled clause | `SELECT * FORM c` | `FORM is not a clause: did you mean FROM?` |
| A misspelled `BY` | `ORDER BYY c.n` | `ORDER BYY needs BY: did you mean BY?` |
| A statement that does not start with `SELECT`, `UPDATE`, `DELETE` or `BEGIN BATCH` | `SELEC * FROM c` | `a statement starts with SELECT, UPDATE, DELETE or BEGIN BATCH` |
| An unbalanced bracket | `WHERE (c.a = 1` | `( is never closed` |
| A batch that does not parse | `BEGIN BATCH sales.orders PARTITION` | the batch parser's message |
| An update or a delete that does not parse | `DELETE FROM sales.orders o` | the parser's message |

"Did you mean" only offers a word within two edits of what you typed, and a call is
only flagged when there is such a word: a function far from every one Alchemist knows
may be one the service added since.

Nothing that could be right is flagged: not an unknown field (Cosmos has no schema),
not an unknown database or container (the catalog may not be listed yet), and not a
query shape the planner refuses, which `ctrl+r` explains better than a squiggle could.
The word you are typing, and a string you are typing in, are not judged until the
cursor leaves them. Strings and stray characters are flagged as you type; everything
else once you pause.

The squiggles are a typing aid, not a validator: a query without one can still fail
on the service, which stays the judge, and its error shows as before.

## 3.3. Squiggles and terminals

The squiggle is a curly underline (`SGR 4:3`) in the theme's red, which kitty,
WezTerm, iTerm2, Ghostty, foot, GNOME Terminal and Windows Terminal draw. A terminal
that does not know it draws a plain underline, or none, in the token's own color; the
hint line always says what is wrong in words.

Terminals cannot be asked reliably over SSH or through tmux, so the form is a setting:

| Setting | Effect |
|---|---|
| `diagnostics = "curly"` (default) | a curly underline |
| `diagnostics = "underline"` | a plain underline |
| `diagnostics = "off"` | nothing is flagged, and the editor stops looking |

Set it on the [profile](../reference/configuration.md#profile-settings), or pass
`--diagnostics underline` for one session. Under `NO_COLOR` the editor has no color
and every squiggle is a plain underline. An older terminal that reads the `:` in `4:3`
as a `;` shows a squiggle as dim text or a colored background instead; set
`diagnostics = "underline"` there.

tmux passes the curly form through when told the outer terminal draws it:

```tmux
set -as terminal-overrides ',*:Smulx=\E[4::%p1%dm'
set -as terminal-overrides ',*:Setulc=\E[58::2::%p1%{65536}%/%d::%p1%{256}%/%{255}%&%d::%p1%{255}%&%d%;m'
```

## 3.4. Autocomplete

Type in the editor and a list docks to the bottom of the pane with what can come
next:

- clause keywords where a clause may open;
- Cosmos system functions, with their signatures, in an expression;
- databases after `FROM`, `JOIN`, or a list comma;
- containers after `db.`;
- fields after `alias.`, nested paths included.

Aliases resolve the way the planner reads them, so in a
[cross-container query](../language/cross-container.md) each side completes its own
container's fields, a union alias completes the fields of every listed container, and
`JOIN t IN c.tags` completes `t.` from the array's elements. Nothing is offered that
the planner would refuse: no `ORDER BY`, `GROUP BY`, or `OFFSET` once a query is
simulated, no alias of a side an outer join pads in its `WHERE`, and no `ON` after
`CROSS JOIN`. After `WITH` the CTEs declared so far come first in every source
position, and `cte.` completes the columns its select list names, with nothing
sampled.

The list opens by itself after an identifier character or a dot, narrows as you type,
and closes on whitespace.

| Key | Action |
|---|---|
| `tab` | accept the selected suggestion |
| `↑`/`↓` | choose |
| `esc` | dismiss until the next word |
| `ctrl+space` | open the list on demand, even on an empty prefix |

`enter` is always a newline, and with the list closed `tab` moves panes as usual. Some
terminals swallow `ctrl+space`; nothing depends on it. Keywords take the case you are
typing in (`sel` → `select`); field names are inserted exactly as observed, in bracket
form when they are not plain identifiers (`c["order-id"]`).

## 3.5. Where field names come from

Cosmos has no schema, so fields are what the session has seen: the partition key from
the catalog, the fields of every page a query returned, and a sample.

The first time a container's fields are completed, Alchemist runs
`SELECT TOP 20 * FROM c` against it, once per container per session, and files what it
finds. That spends a few request units you did not ask for, so the hint line says
`sampling orders…` while it runs, and the log records the charge. Turn it off with
`sample_fields = false` on the profile or `--sample-fields=false` for one session. A
failed sample is logged and not retried; completion carries on with what it has.

---

[← 2. A Tour of the Workspace](../tutorial/workspace.md) · [Contents](../README.md) · [4. Results and Export →](results.md)
