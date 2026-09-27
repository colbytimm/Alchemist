# Themes

Alchemist has one theme, `alchemist`. Its colors adapt to light and dark terminals.

![The default theme on a dark terminal](../images/workspace.png)

## Alchemist

### Colors

| Color | Light terminal | Dark terminal | Used for |
|---|---|---|---|
| gold | `#D4A017` | `#F5C542` | focused borders, the spinner, table headers, help keys, warnings, functions |
| copper | `#B87333` | `#D48F52` | the selected row, literals, parameters, numbers |
| verdigris | `#2E8B84` | `#5FD3CE` | success, the request charge, strings |
| amethyst | `#6C3FA0` | `#9B6FD0` | keywords and operator words |
| parchment | `#5C4B37` | `#E8DCC8` | text and aliases |
| cinnabar | `#C0392B` | `#E74C3C` | errors and error hints |
| ash | `#8A8378` | `#6B655B` | hints, unfocused borders, comments, punctuation |

### The editor

| Token | Examples | Color |
|---|---|---|
| Clause keyword | `SELECT`, `FROM`, `WHERE`, `ORDER BY`, `JOIN`, `VALUE` | amethyst, bold |
| Operator word | `AND`, `OR`, `NOT`, `IN`, `LIKE`, `BETWEEN`, `EXISTS` | amethyst |
| Literal | `true`, `null`, `undefined` | copper |
| Function | `STARTSWITH(`, `COUNT(`, `udf.discount(` | gold |
| Alias | the `c` in `FROM c` and `c.total` | parchment, bold |
| Parameter | `@minTotal` | copper, italic |
| String, number | `'west'`, `1.5e3` | verdigris, copper |
| Comment | `-- note` | ash, italic |
| Punctuation | `( ) , . = < + ??` | ash |

Property names stay uncolored. Batches, updates and deletes color their own keywords
(`BEGIN BATCH`, `PARTITION`, `COMMIT`, `UPDATE`, `SET`, `UNSET`, `DELETE`) as clause
keywords. Error hints are underlined in cinnabar.

## Turning colors off

Set `NO_COLOR` to turn off every color. Error hints are then a plain underline.
