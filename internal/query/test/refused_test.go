package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

func TestShapesOfJoinTypesAndCTEsThatCannotBeSimulatedAreRefusedByName(t *testing.T) {
	const (
		orders = "SELECT * FROM sales.orders o "
		west   = `WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu) `
	)
	tests := []struct {
		name  string
		input string
		shape string
	}{
		{name: "a WHERE on the optional side of LEFT", input: leftJoin + ` WHERE cu.region = "west"`, shape: "optional side of LEFT JOIN: write it in ON"},
		{name: "a WHERE on the optional side of RIGHT", input: rightJoin + ` WHERE a.open`, shape: "optional side of RIGHT JOIN"},
		{name: "a WHERE on either side of FULL", input: fullJoin + ` WHERE o.open`, shape: "optional side of FULL JOIN"},
		{name: "IS_DEFINED on an optional side", input: leftJoin + " WHERE IS_DEFINED(cu)", shape: "use INNER JOIN"},
		{name: "an ON condition on the preserved side of LEFT", input: leftJoin + " AND o.open", shape: "an ON condition on the preserved side of LEFT JOIN"},
		{name: "any extra ON condition of RIGHT", input: rightJoin + " AND d.site = 'Lima'", shape: "an ON condition on the preserved side of RIGHT JOIN"},
		{name: "an extra ON condition of FULL", input: fullJoin + " AND cu.vip", shape: "an ON condition on the preserved side of FULL JOIN"},
		{name: "OR in ON", input: leftJoin + " OR cu.vip", shape: "ON clause"},
		{name: "BETWEEN in ON", input: leftJoin + " AND cu.n BETWEEN 1 AND 2", shape: "BETWEEN in parentheses"},
		{name: "ON on CROSS JOIN", input: orders + "CROSS JOIN sales.customers cu ON o.customerId = cu.id", shape: "none on CROSS JOIN"},
		{name: "outer join without ON", input: orders + "LEFT JOIN sales.customers cu", shape: "ON clause"},
		{name: "CROSS JOIN in a chain", input: leftJoin + " CROSS JOIN sales.products p", shape: "CROSS JOIN in a chain"},
		{name: "a list with two aliases", input: "SELECT * FROM sales.orders o, sales.customers cu", shape: "write CROSS JOIN"},
		{name: "NATURAL JOIN", input: "SELECT * FROM sales.orders NATURAL JOIN sales.customers", shape: "NATURAL JOIN"},
		{name: "USING", input: orders + "JOIN sales.customers cu USING (id)", shape: "USING"},
		{name: "APPLY of a subquery", input: "SELECT * FROM sales.customers cu CROSS APPLY (SELECT TOP 3 * FROM sales.orders o WHERE o.customerId = cu.id) recent", shape: "one query per row"},
		{name: "APPLY of a function", input: "SELECT * FROM sales.customers cu OUTER APPLY f(cu.id)", shape: "one query per row"},
		{name: "APPLY reading another source", input: leftJoin + " OUTER APPLY l IN o.lines", shape: "APPLY must follow the source it reads"},
		{name: "CROSS APPLY on the optional side of LEFT", input: leftJoin + " CROSS APPLY t IN cu.tags", shape: "CROSS APPLY on the optional side of LEFT JOIN: use OUTER APPLY"},
		{name: "a WHERE on an APPLY alias", input: orders + "OUTER APPLY l IN o.lines WHERE l.quantity > 1", shape: "a WHERE condition on an APPLY alias"},
		{name: "JOIN IN beside a simulated join", input: "SELECT * " + sprintfJoin("JOIN") + " JOIN l IN o.lines", shape: "use CROSS APPLY"},
		{name: "WITH RECURSIVE", input: "WITH RECURSIVE r AS (SELECT * FROM r) SELECT * FROM r", shape: "a recursive CTE"},
		{name: "a duplicate CTE name", input: "WITH x AS (SELECT * FROM a.b), x AS (SELECT * FROM a.c) SELECT * FROM x", shape: "a CTE named x twice"},
		{name: "a keyword as a CTE name", input: "WITH select AS (SELECT * FROM a.b) SELECT * FROM a.c", shape: "which is a keyword"},
		{name: "a CTE named like a database it reads beside", input: "WITH sales AS (SELECT * FROM hr.employees e) SELECT * FROM sales.orders", shape: "a CTE named sales beside the source sales.orders"},
		{name: "a WHERE on a CTE", input: west + "SELECT * FROM west JOIN sales.orders o ON west.id = o.customerId WHERE west.name = 'x'", shape: "a WHERE condition on the CTE west: filter inside the CTE"},
		{name: "an ON condition on a CTE", input: west + "SELECT * FROM sales.orders o JOIN west ON west.id = o.customerId AND west.name = 'x'", shape: "an ON condition on the CTE west"},
		{name: "SELECT VALUE in a joined CTE", input: "WITH v AS (SELECT VALUE o FROM sales.orders o) SELECT * FROM v JOIN sales.customers cu ON v.customerId = cu.id", shape: "must return objects"},
		{name: "SELECT * in a joined CTE body", input: "WITH j AS (SELECT * " + sprintfJoin("JOIN") + ") SELECT * FROM j", shape: "the joined CTE j needs a SELECT list with distinct names"},
		{name: "colliding names in a joined CTE body", input: "WITH j AS (SELECT o.id, cu.id " + sprintfJoin("JOIN") + ") SELECT * FROM j", shape: "distinct names"},
		{name: "a column a CTE does not have", input: west + "SELECT west.title FROM west JOIN sales.orders o ON west.id = o.customerId", shape: "west has no column title"},
		{name: "a list of CTEs", input: west + "SELECT * FROM west, west AS w2", shape: "a container list of CTEs"},
		{name: "a subquery over a CTE", input: west + "SELECT * FROM sales.orders o WHERE EXISTS (SELECT VALUE 1 FROM west)", shape: "a subquery over the CTE west"},
		{name: "an expression in a simulated SELECT", input: "SELECT ROUND(o.total) " + sprintfJoin("JOIN"), shape: "put it in a CTE, where the service evaluates it"},
		{name: "TOP on a simulated body", input: "SELECT TOP 5 o.id " + sprintfJoin("LEFT JOIN"), shape: "put it in a CTE"},
		{name: "ORDER BY on a simulated body", input: leftJoin + " ORDER BY o.total", shape: "ORDER in a cross-container join: put it in a CTE"},
		{name: "GROUP BY on a simulated body", input: leftJoin + " GROUP BY o.total", shape: "GROUP in a cross-container join"},
		{name: "a condition over two sources", input: leftJoin + " WHERE o.total > cu.limit", shape: "more than one side"},
		{name: "a subquery over another container", input: west + "SELECT * FROM west w WHERE EXISTS (SELECT VALUE 1 FROM hr.people p)", shape: "a subquery over another container"},
		{name: "a WITH clause with no query", input: "WITH x AS (SELECT * FROM a.b)", shape: "no query after it"},
		{name: "a WITH clause that is not name AS (query)", input: "WITH x (SELECT * FROM a.b) SELECT * FROM x", shape: "name AS (query)"},
		{name: "an unclosed CTE", input: "WITH x AS (SELECT * FROM a.b SELECT * FROM x", shape: "never closed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.BuildPlan(tt.input)

			require.ErrorIs(t, err, query.ErrUnsupported)
			assert.ErrorContains(t, err, tt.shape)
		})
	}
}

func sprintfJoin(kind string) string {
	return "FROM sales.orders o " + kind + " sales.customers cu ON o.customerId = cu.id"
}
