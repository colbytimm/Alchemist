package query_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

func ordersWithArrays(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"sales.orders": {page(t, 1,
			`{"id":"o1","lines":[{"sku":"s1","quantity":2},{"sku":"s2","quantity":1}],"tags":["red","big"]}`,
			`{"id":"o2","lines":[],"tags":"none"}`,
			`{"id":"o3","lines":null}`,
			`{"id":"o4"}`,
			`{"id":"o5","lines":{"sku":"s1"}}`)},
		"sales.products": {page(t, 1, `{"id":"s1","name":"Lamp"}`, `{"id":"s2","name":"Desk"}`)},
	})
}

func TestApplyRangesOverTheElementsOfAnArray(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [][]string
	}{
		{
			name:  "outer apply of objects keeps an item with no element",
			input: "SELECT o.id, l.sku, l.quantity FROM sales.orders o OUTER APPLY l IN o.lines",
			want:  [][]string{{"o1", "s1", "2"}, {"o1", "s2", "1"}, {"o2", "", ""}, {"o3", "", ""}, {"o4", "", ""}, {"o5", "", ""}},
		},
		{
			name:  "outer apply of scalars named bare",
			input: "SELECT o.id, t FROM sales.orders o OUTER APPLY t IN o.tags",
			want:  [][]string{{"o1", "red"}, {"o1", "big"}, {"o2", ""}, {"o3", ""}, {"o4", ""}, {"o5", ""}},
		},
		{
			name:  "cross apply beside an outer one drops an item with no element",
			input: "SELECT o.id, l.sku, t FROM sales.orders o OUTER APPLY l IN o.lines CROSS APPLY t IN o.tags",
			want:  [][]string{{"o1", "s1", "red"}, {"o1", "s1", "big"}, {"o1", "s2", "red"}, {"o1", "s2", "big"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := drain(t, execute(t, ordersWithArrays(t), tt.input))

			assert.Equal(t, tt.want, rows(pages))
		})
	}
}

func TestAnApplyUnderSelectStarNamesAnElementsFields(t *testing.T) {
	pages := drain(t, execute(t, ordersWithArrays(t), "SELECT * FROM sales.orders o OUTER APPLY l IN o.lines OUTER APPLY t IN o.tags"))

	assert.Equal(t, []string{"o.id", "o.lines", "o.tags", "l.sku", "l.quantity", "t"}, pages[0].Columns,
		"an element's fields in the order it writes them")
	assert.Equal(t, []string{"s1", "2", "red"}, pages[0].Rows[0][3:])
}

func TestAnApplyElementSitsBesideItsItemInTheRawItem(t *testing.T) {
	pages := drain(t, execute(t, ordersWithArrays(t), "SELECT o.id, l.sku FROM sales.orders o OUTER APPLY l IN o.lines"))

	assert.Equal(t, `{"o":{"id":"o1"},"l":{"sku":"s1"}}`, string(pages[0].Raw[0]))
	assert.Equal(t, `{"o":{"id":"o2"}}`, string(pages[0].Raw[2]), "an absent element has no key")
}

func TestChainedAppliesReachANestedArray(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders": {page(t, 1,
			`{"id":"o1","lines":[{"sku":"s1","taxes":[{"rate":5},{"rate":7}]},{"sku":"s2"}]}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT o.id, l.sku, x.rate FROM sales.orders o OUTER APPLY l IN o.lines OUTER APPLY x IN l.taxes"))

	assert.Equal(t, [][]string{{"o1", "s1", "5"}, {"o1", "s1", "7"}, {"o1", "s2", ""}}, rows(pages))
}

func TestAnApplyReachesAnArrayAtANestedPath(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders": {page(t, 1, `{"id":"o1","shipping":{"stops":["Lima","Osaka"]}}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT o.id, s FROM sales.orders o OUTER APPLY s IN o.shipping.stops"))

	assert.Equal(t, [][]string{{"o1", "Lima"}, {"o1", "Osaka"}}, rows(pages))
}

func TestAJoinKeyThroughAnApplyAliasJoinsOrderLinesToProducts(t *testing.T) {
	pages := drain(t, execute(t, ordersWithArrays(t), `SELECT o.id, l.quantity, p.name
FROM sales.orders o CROSS APPLY l IN o.lines
JOIN sales.products p ON l.sku = p.id`))

	assert.Equal(t, [][]string{{"o1", "2", "Lamp"}, {"o1", "1", "Desk"}}, rows(pages))
}

func TestAHeldInputCountsItsRowsAfterExpansion(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.customers": {page(t, 1, `{"id":"c1"}`)},
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","tags":["a","b","c"]}`)},
	})
	text := `SELECT cu.id, t FROM sales.customers cu LEFT JOIN sales.orders o ON cu.id = o.customerId OUTER APPLY t IN o.tags`
	cursor, err := query.Engine{Connection: conn, MaxJoinRows: 2}.Execute(context.Background(), plan(t, text))
	require.NoError(t, err)

	_, err = cursor.NextPage(context.Background())

	require.ErrorIs(t, err, query.ErrJoinTooLarge, "one order, three rows once its tags are applied")
}

func TestAnOuterApplyChargesItsLeafAlone(t *testing.T) {
	total, leaves := charges(drain(t, execute(t, ordersWithArrays(t), "SELECT o.id, t FROM sales.orders o OUTER APPLY t IN o.tags")))

	assert.InDelta(t, 1, total, 1e-9)
	assert.Equal(t, map[string]float64{"sales.orders": 1}, leaves)
}
