package query_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

const (
	ordersCustomers = "FROM sales.orders o %s sales.customers cu ON o.customerId = cu.id"
	leftJoin        = "SELECT * FROM sales.orders o LEFT JOIN sales.customers cu ON o.customerId = cu.id"
	rightJoin       = "SELECT * FROM telemetry.alerts a RIGHT JOIN telemetry.devices d ON a.deviceId = d.id"
	fullJoin        = "SELECT * FROM sales.orders o FULL JOIN sales.customers cu ON o.customerId = cu.id"
)

func TestEveryJoinKindPlansItsStep(t *testing.T) {
	ordersToCustomers := func(kind query.JoinKind) []query.JoinStep {
		return []query.JoinStep{{Kind: kind, LeftKey: ref("o", "customerId"), RightKey: ref("cu", "id")}}
	}
	tests := []struct {
		name  string
		input string
		want  []query.JoinStep
	}{
		{name: "inner", input: "SELECT * " + fmt.Sprintf(ordersCustomers, "INNER JOIN"), want: ordersToCustomers(query.InnerJoin)},
		{name: "left", input: leftJoin, want: ordersToCustomers(query.LeftOuterJoin)},
		{name: "left outer", input: "SELECT * " + fmt.Sprintf(ordersCustomers, "LEFT OUTER JOIN"), want: ordersToCustomers(query.LeftOuterJoin)},
		{name: "right", input: "SELECT * " + fmt.Sprintf(ordersCustomers, "RIGHT JOIN"), want: ordersToCustomers(query.RightOuterJoin)},
		{name: "right outer", input: "SELECT * " + fmt.Sprintf(ordersCustomers, "RIGHT OUTER JOIN"), want: ordersToCustomers(query.RightOuterJoin)},
		{name: "full", input: fullJoin, want: ordersToCustomers(query.FullOuterJoin)},
		{name: "full outer", input: "SELECT * " + fmt.Sprintf(ordersCustomers, "FULL OUTER JOIN"), want: ordersToCustomers(query.FullOuterJoin)},
		{
			name:  "cross",
			input: "SELECT d.name AS department, p.name AS product FROM hr.departments d CROSS JOIN sales.products p",
			want:  []query.JoinStep{{Kind: query.CrossJoin}},
		},
		{
			name:  "a mixed chain",
			input: leftJoin + " JOIN sales.regions r ON cu.region = r.id RIGHT JOIN sales.products p ON o.sku = p.id",
			want: []query.JoinStep{
				{Kind: query.LeftOuterJoin, LeftKey: ref("o", "customerId"), RightKey: ref("cu", "id")},
				{Kind: query.InnerJoin, Left: 1, LeftKey: ref("cu", "region"), RightKey: ref("r", "id")},
				{Kind: query.RightOuterJoin, LeftKey: ref("o", "sku"), RightKey: ref("p", "id")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)

			require.NoError(t, err)
			assert.True(t, plan.Simulated())
			assert.Equal(t, tt.want, joinOf(t, plan).Steps)
		})
	}
}

func TestOuterJoinConditionsArePushedDownOnlyToPreservedInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "the preserved side of LEFT",
			input: leftJoin + ` WHERE o.status = "open"`,
			want:  []string{`SELECT * FROM o WHERE (o.status = "open")`, "SELECT * FROM cu"},
		},
		{
			name:  "the preserved side of RIGHT",
			input: rightJoin + ` WHERE d.site = "Lima"`,
			want:  []string{"SELECT * FROM a", `SELECT * FROM d WHERE (d.site = "Lima")`},
		},
		{
			name:  "an ON condition on the joined side of LEFT",
			input: leftJoin + " AND cu.vip = true",
			want:  []string{"SELECT * FROM o", "SELECT * FROM cu WHERE (cu.vip = true)"},
		},
		{
			name:  "an ON condition on the joined side of INNER",
			input: "SELECT * " + fmt.Sprintf(ordersCustomers, "JOIN") + ` AND cu.region = "west" AND cu.vip`,
			want:  []string{"SELECT * FROM o", `SELECT * FROM cu WHERE (cu.region = "west") AND (cu.vip)`},
		},
		{
			name:  "a condition reading no input",
			input: fullJoin + " WHERE 1 = 1",
			want:  []string{"SELECT * FROM o WHERE (1 = 1)", "SELECT * FROM cu WHERE (1 = 1)"},
		},
		{
			name:  "the preserved inputs of a mixed chain",
			input: leftJoin + ` JOIN sales.products p ON o.sku = p.id WHERE o.total > 5 AND p.category = "lamps"`,
			want: []string{
				"SELECT * FROM o WHERE (o.total > 5)",
				"SELECT * FROM cu",
				`SELECT * FROM p WHERE (p.category = "lamps")`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)

			require.NoError(t, err)
			var got []string
			for i, leaf := range plan.Leaves {
				got = append(got, leaf.Query.Text)
				assert.Equal(t, leaf.Query.Text != "SELECT * FROM "+leaf.Alias, plan.Leaves[i].Filtered)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTheAbsentSideTestIsKeptForTheJoin(t *testing.T) {
	plan, err := query.BuildPlan(leftJoin + ` WHERE NOT IS_DEFINED(cu) AND o.status = "open"`)

	require.NoError(t, err)
	assert.Equal(t, []string{"cu"}, joinOf(t, plan).Absent)
	assert.Equal(t, []bool{true, false}, filtered(plan))
}

func TestALoneCrossApplyIsTheServicesOwnJoin(t *testing.T) {
	plan, err := query.BuildPlan("SELECT o.id, l.sku FROM sales.orders o CROSS APPLY l IN o.lines CROSS APPLY t IN l.taxes")

	require.NoError(t, err)
	assert.False(t, plan.Simulated())
	require.Len(t, plan.Leaves, 1)
	assert.Equal(t, "SELECT o.id, l.sku FROM o JOIN l IN o.lines JOIN t IN l.taxes", plan.Leaves[0].Query.Text)
}

func TestOuterApplyIsSimulatedOverTheItemsOfItsSource(t *testing.T) {
	plan, err := query.BuildPlan("SELECT o.id, l.sku, t FROM sales.orders o OUTER APPLY l IN o.lines CROSS APPLY t IN o.tags")

	require.NoError(t, err)
	require.True(t, plan.Simulated())
	assert.Equal(t, &query.Join{
		Inputs: []query.Source{{
			Alias: "o",
			Rows:  &query.Scan{Leaf: 0},
			Applies: []query.Apply{
				{Alias: "l", Array: ref("o", "lines"), Outer: true},
				{Alias: "t", Array: ref("o", "tags")},
			},
		}},
		Columns: []query.JoinColumn{
			{Alias: "o", Field: "id"},
			{Alias: "l", Field: "sku"},
			{Alias: "t"},
		},
	}, plan.Root)
	assert.Equal(t, "SELECT * FROM o", plan.Leaves[0].Query.Text)
}

func TestAJoinKeyMayReadAnApplyAlias(t *testing.T) {
	plan, err := query.BuildPlan(`SELECT o.id, l.quantity, p.name
FROM sales.orders o CROSS APPLY l IN o.lines
JOIN sales.products p ON l.sku = p.id`)

	require.NoError(t, err)
	assert.Equal(t, []query.JoinStep{{LeftKey: ref("l", "sku"), RightKey: ref("p", "id")}}, joinOf(t, plan).Steps)
}
