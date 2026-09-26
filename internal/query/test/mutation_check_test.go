package query_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/query"
)

func setting(n int) string {
	changes := make([]string, n)
	for i := range changes {
		changes[i] = fmt.Sprintf("o.f%d = %d", i, i)
	}
	return strings.Join(changes, ", ")
}

func TestCheckMutationProblems(t *testing.T) {
	tests := []struct {
		name     string
		changes  string
		keyPaths []string
		want     []string
	}{
		{name: "ten operations", changes: "SET " + setting(10), keyPaths: customerKey},
		{
			name: "eleven operations", changes: "SET " + setting(11), keyPaths: customerKey,
			want: []string{"a patch takes at most 10 operations; this statement has 11. Two statements are two writes per item"},
		},
		{name: "id", changes: `SET o.id = "x"`, keyPaths: customerKey, want: []string{"cannot change id: that is a delete and a create"}},
		{
			name: "the partition key", changes: `SET o.customerId = "x"`, keyPaths: customerKey,
			want: []string{"cannot change the partition key /customerId: that is a delete and a create"},
		},
		{
			name: "the parent of a nested key", changes: "UNSET o.shipTo", keyPaths: []string{"/shipTo/region"},
			want: []string{"cannot change the partition key /shipTo/region: that is a delete and a create"},
		},
		{
			name: "the child of a key", changes: "SET o.tenant.name = 1", keyPaths: []string{"/tenant", "/user"},
			want: []string{"cannot change the partition key /tenant: that is a delete and a create"},
		},
		{
			name: "the second path of a hierarchical key", changes: "SET o.user = 1", keyPaths: []string{"/tenant", "/user"},
			want: []string{"cannot change the partition key /user: that is a delete and a create"},
		},
		{name: "a field that only starts like the key", changes: "SET o.customerIdOld = 1", keyPaths: customerKey},
		{name: "a system field", changes: `SET o._etag = "x"`, keyPaths: customerKey, want: []string{"cannot set _etag: the service owns it"}},
		{name: "a system field unset", changes: "UNSET o._ts", keyPaths: customerKey, want: []string{"cannot unset _ts: the service owns it"}},
		{name: "a path twice", changes: "SET o.x = 1 UNSET o.x", keyPaths: customerKey, want: []string{"o.x appears twice"}},
		{
			name: "overlapping paths", changes: `SET o.shipTo = {}, o.shipTo.region = "w"`, keyPaths: customerKey,
			want: []string{"o.shipTo and o.shipTo.region overlap"},
		},
		{
			name: "every problem at once", changes: `SET o.id = 1, o.customerId = 2, o._rid = 3`, keyPaths: customerKey,
			want: []string{
				"cannot change id: that is a delete and a create",
				"cannot change the partition key /customerId: that is a delete and a create",
				"cannot set _rid: the service owns it",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := parseMutation(t, "UPDATE sales.orders o "+tt.changes+` WHERE o.customerId = "c01"`)

			check := query.CheckMutation(m, tt.keyPaths)

			assert.Equal(t, tt.want, check.Problems)
		})
	}
}

func TestCheckMutationWarnsOfAnUnpinnedPartition(t *testing.T) {
	tests := []struct {
		where string
		warn  string
	}{
		{where: "o.total < 50", warn: "The WHERE does not pin /customerId: the selection reads every partition."},
		{where: `o.customerId = "c01" AND o.total < 50`},
		{where: `"c01" = o.customerId`},
		{where: `o["customerId"] = "c01"`},
		{where: `o.customerId != "c01"`, warn: "The WHERE does not pin /customerId"},
		{where: `o.customerId = "c01" OR o.total < 50`, warn: "The WHERE does not pin /customerId"},
		{where: `(o.customerId = "c01")`, warn: "The WHERE does not pin /customerId"},
		{where: "true", warn: "WHERE true: every item in sales.orders is a target."},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			m := parseMutation(t, "UPDATE sales.orders o SET o.x = 1 WHERE "+tt.where)

			check := query.CheckMutation(m, customerKey)

			if tt.warn == "" {
				assert.Empty(t, check.Warnings)
				return
			}
			assert.Len(t, check.Warnings, 1)
			assert.Contains(t, check.Warnings[0], tt.warn)
		})
	}
}

func TestCheckMutationWarnsOfANestedSet(t *testing.T) {
	m := parseMutation(t, `UPDATE sales.orders o SET o.ship.region = "w", o.lines[0] = 1, o.flag = true WHERE o.customerId = "c01"`)

	check := query.CheckMutation(m, customerKey)

	assert.Equal(t, []string{
		"SET o.ship.region needs o.ship on each item: a patch creates only the last step of a path, so an item without it is skipped, not written.",
		"SET o.lines[0] needs o.lines on each item: a patch creates only the last step of a path, so an item without it is skipped, not written.",
	}, check.Warnings)
}
