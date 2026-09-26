package mock_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

const openOrders = `o.status = "open"`

var c01 = adapter.PartitionKey{json.RawMessage(`"c01"`)}

func statusIs(status string) func(json.RawMessage) bool {
	return func(item json.RawMessage) bool {
		var head struct {
			Status string `json:"status"`
		}
		return json.Unmarshal(item, &head) == nil && head.Status == status
	}
}

func itemEditor(t *testing.T, a *mock.Adapter) adapter.ItemEditor {
	t.Helper()
	e, ok := connectTo(t, a).(adapter.ItemEditor)
	require.True(t, ok, "a mock connection edits items")
	return e
}

func setStatus(value string) json.RawMessage {
	return json.RawMessage(`[{"op":"set","path":"/status","value":"` + value + `"}]`)
}

func patchOpen(id string, condition string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationPatch, ID: id, Body: setStatus("archived"), Condition: condition}
}

func stored(t *testing.T, a *mock.Adapter, id string) map[string]json.RawMessage {
	t.Helper()
	for _, item := range a.Items(ordersPath) {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(item, &fields))
		if string(fields["id"]) == `"`+id+`"` {
			return fields
		}
	}
	return nil
}

func TestAFilteredScanKeepsWhatThePredicateMatches(t *testing.T) {
	a := mock.New(mock.WithPredicate(openOrders, statusIs("open")),
		mock.WithItems(ordersPath, order("o1", "c01"), json.RawMessage(`{"id":"o2","customerId":"c01","status":"shipped"}`), order("o3", "c02")))

	_, got := scanAll(t, connectTo(t, a), adapter.ScanRequest{
		Container: ordersPath, PageSize: 1, Filter: adapter.ScanFilter{Alias: "o", Predicate: openOrders},
	})

	assert.Equal(t, []string{"o1", "o3"}, got)
}

func TestAScanByAnUnregisteredPredicateNamesIt(t *testing.T) {
	_, err := scanner(t, connect(t)).ScanItems(context.Background(), adapter.ScanRequest{
		Container: ordersPath, Filter: adapter.ScanFilter{Alias: "o", Predicate: "o.x = 1"},
	})

	require.ErrorContains(t, err, `"o.x = 1"`)
}

func TestAnEditRechecksItsConditionAgainstTheStore(t *testing.T) {
	condition := "FROM o WHERE (" + openOrders + ")"
	tests := []struct {
		name    string
		status  string
		wantErr error
		want    string
	}{
		{name: "an item that still matches is patched", status: "open", want: `"archived"`},
		{name: "an item that stopped matching is left as it is", status: "shipped", wantErr: adapter.ErrPreconditionFailed, want: `"shipped"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := mock.New(mock.WithPredicate(openOrders, statusIs("open")), mock.WithItems(ordersPath, order("o1", "c01")))
			require.NoError(t, a.PutItem(ordersPath, json.RawMessage(`{"id":"o1","customerId":"c01","status":"`+tc.status+`"}`)))

			result, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, patchOpen("o1", condition))

			require.ErrorIs(t, err, tc.wantErr)
			assert.NotEmpty(t, result.Status)
			assert.Equal(t, tc.want, string(stored(t, a, "o1")["status"]))
		})
	}
}

func TestAnEditNeedsTheFieldsItsConditionAsksFor(t *testing.T) {
	a := mock.New(mock.WithPredicate(openOrders, statusIs("open")), mock.WithItems(ordersPath, order("o1", "c01")))
	remove := adapter.Operation{
		Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(`[{"op":"remove","path":"/note"}]`),
		Condition: "FROM o WHERE (" + openOrders + ") AND IS_DEFINED(o.note)",
	}

	_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, remove)

	require.ErrorIs(t, err, adapter.ErrPreconditionFailed)
}

func TestAnEditConditionedOnAScalarWhereAnObjectMustBeIsLeftAsItIs(t *testing.T) {
	a := mock.New(mock.WithPredicate(openOrders, statusIs("open")),
		mock.WithItems(ordersPath, json.RawMessage(`{"id":"o1","customerId":"c01","status":"open","ship":"none"}`)))
	set := adapter.Operation{
		Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(`[{"op":"set","path":"/ship/region","value":"w"}]`),
		Condition: "FROM o WHERE (" + openOrders + ") AND IS_OBJECT(o.ship)",
	}

	_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, set)

	require.ErrorIs(t, err, adapter.ErrPreconditionFailed)
}

func TestAnEditConditionTheEmulatorRefusesIsRefused(t *testing.T) {
	a := mock.New(mock.WithPredicate(openOrders, statusIs("open")), mock.WithPredicate("true", statusIs("open")),
		mock.WithItems(ordersPath, order("o1", "c01")))
	for _, condition := range []string{
		"FROM o WHERE (" + openOrders + `) AND IS_DEFINED(o["note"])`,
		"FROM o WHERE (" + openOrders + ") AND IS_DEFINED(o.lines[0])",
		"FROM o WHERE (true) AND IS_DEFINED(o.status)",
	} {
		t.Run(condition, func(t *testing.T) {
			_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, patchOpen("o1", condition))

			require.Error(t, err)
			assert.NotErrorIs(t, err, adapter.ErrPreconditionFailed)
		})
	}
}

func TestAnEditConditionedOnGuardsAloneReadsAsTrue(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))

	_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, patchOpen("o1", "FROM o WHERE IS_DEFINED(o.status)"))

	require.NoError(t, err)
}

func TestAnEditOfAMissingItemIsNotFound(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))

	result, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, adapter.PartitionKey{json.RawMessage(`"c02"`)}, patchOpen("o1", ""))

	require.ErrorIs(t, err, adapter.ErrItemNotFound)
	assert.Equal(t, "404 Not Found", result.Status)
}

func TestADeleteHonorsItsVersion(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))
	_, meta, err := adapter.SplitSystemFields(a.Items(ordersPath)[0])
	require.NoError(t, err)
	e := itemEditor(t, a)

	_, stale := e.EditItem(context.Background(), ordersPath, c01, adapter.Operation{Kind: adapter.OperationDelete, ID: "o1", IfMatch: `"old"`})
	_, current := e.EditItem(context.Background(), ordersPath, c01, adapter.Operation{Kind: adapter.OperationDelete, ID: "o1", IfMatch: meta.Version})

	require.ErrorIs(t, stale, adapter.ErrPreconditionFailed)
	require.NoError(t, current)
	assert.Empty(t, a.Items(ordersPath))
}

func TestAnEditOfAnyOtherKindIsUnsupported(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))

	_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, adapter.Operation{Kind: adapter.OperationReplace, ID: "o1", Body: order("o1", "c01")})

	require.ErrorIs(t, err, adapter.ErrUnsupported)
}

func TestInjectedEditOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		option  mock.Option
		check   func(t *testing.T, err error)
		applied bool
	}{
		{
			name:   "a conflict leaves the item",
			option: mock.WithEditConflict("o1"),
			check:  func(t *testing.T, err error) { assert.ErrorIs(t, err, adapter.ErrPreconditionFailed) },
		},
		{
			name:   "a refusal leaves the item",
			option: mock.WithEditRefusal("o1"),
			check: func(t *testing.T, err error) {
				var injected *mock.InjectedError
				assert.ErrorAs(t, err, &injected)
			},
		},
		{
			name:    "an unknown outcome applies the edit",
			option:  mock.WithEditUnknown("o1"),
			check:   func(t *testing.T, err error) { assert.ErrorIs(t, err, adapter.ErrWriteOutcomeUnknown) },
			applied: true,
		},
		{
			name:   "a throttle names its wait",
			option: mock.WithEditThrottle("o1", 1, time.Second),
			check: func(t *testing.T, err error) {
				var throttled *adapter.ThrottledError
				require.ErrorAs(t, err, &throttled)
				assert.Equal(t, time.Second, throttled.RetryAfter)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := mock.New(tc.option, mock.WithItems(ordersPath, order("o1", "c01")))
			e := itemEditor(t, a)

			_, err := e.EditItem(context.Background(), ordersPath, c01, patchOpen("o1", ""))

			tc.check(t, err)
			assert.Equal(t, tc.applied, string(stored(t, a, "o1")["status"]) == `"archived"`)
			_, again := e.EditItem(context.Background(), ordersPath, c01, patchOpen("o1", ""))
			require.NoError(t, again, "a fault lasts the times it was given")
			assert.Equal(t, []string{"o1", "o1"}, a.EditedIDs())
		})
	}
}

func TestAnEditConditionedOnAnUnregisteredPredicateIsRefused(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))

	_, err := itemEditor(t, a).EditItem(context.Background(), ordersPath, c01, patchOpen("o1", "FROM o WHERE (o.x = 1)"))

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "WithPredicate"), err.Error())
}
