package complete_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/query"
)

var (
	orders  = []string{"sales", "orders"}
	archive = []string{"sales", "archive"}
)

func container(database, name, partitionKey string) adapter.Node {
	return adapter.Node{
		Kind: adapter.NodeContainer,
		Name: name,
		Path: []string{database, name},
		Meta: map[string]string{adapter.MetaPartitionKey: partitionKey},
	}
}

// loadedIndex holds the sales database with two containers, and the fields
// of orders as one page showed them.
func loadedIndex() *complete.Index {
	index := complete.NewIndex()
	index.SetDatabases([]string{"sales", "telemetry"})
	index.SetContainers("sales", []adapter.Node{
		container("sales", "orders", "/customerId"),
		container("sales", "archive", "/customerId"),
	})
	index.AddFields(orders, []adapter.Field{
		{Path: "id", Kind: "string"}, {Path: "customerId", Kind: "string"}, {Path: "currency", Kind: "string"},
		{Path: "customer", Kind: "object"}, {Path: "customer.name", Kind: "string"}, {Path: "customer.tier", Kind: "number"},
		{Path: "lines", Kind: "array"}, {Path: "lines[]", Kind: "object"}, {Path: "lines[].sku", Kind: "string"},
		{Path: "order-id", Kind: "string"},
	})
	return index
}

func texts(suggestions []complete.Suggestion) []string {
	listed := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		listed = append(listed, s.Text)
	}
	return listed
}

func field(alias query.Alias, path []string, word string) query.Completion {
	return query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{alias}, Path: path, Word: word}
}

func TestPrefixMatchesOutrankSubstringMatches(t *testing.T) {
	got := loadedIndex().Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "cu"))

	assert.Equal(t, []string{"customerId", "currency", "customer"}, texts(got),
		"customerId, first seen, leads the prefix matches; nothing else contains cu")

	got = loadedIndex().Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "id"))
	assert.Equal(t, []string{"id", "customerId", "order-id"}, texts(got), "substring matches follow, in source order")
}

func TestMatchingIgnoresCase(t *testing.T) {
	got := loadedIndex().Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "CUSTOMERI"))

	assert.Equal(t, []string{"customerId"}, texts(got))
	assert.Equal(t, "customerId", got[0].Insert, "an identifier is inserted exactly as observed")
}

func TestKeywordsFollowTheTypedCase(t *testing.T) {
	tests := []struct {
		word string
		want string
	}{
		{word: "sel", want: "select"},
		{word: "SEL", want: "SELECT"},
		{word: "Sel", want: "SELECT"},
		{word: "", want: "SELECT"},
	}
	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			got := complete.NewIndex().Suggest(query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"SELECT"}, Word: tt.word})

			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0].Insert)
			assert.Equal(t, complete.KindKeyword, got[0].Kind)
		})
	}
}

func TestAFieldThatIsNotAnIdentifierIsInsertedInBracketForm(t *testing.T) {
	got := loadedIndex().Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "order-"))

	require.Len(t, got, 1)
	assert.Equal(t, "order-id", got[0].Text)
	assert.Equal(t, `["order-id"]`, got[0].Insert)
	assert.True(t, got[0].Bracketed, "the dot before the word goes with it")
}

func TestFieldsAreListedUnderTheirPath(t *testing.T) {
	alias := query.Alias{Name: "c", Scopes: [][]string{orders}}

	assert.Equal(t, []string{"name", "tier"}, texts(loadedIndex().Suggest(field(alias, []string{"customer"}, ""))))
	assert.Empty(t, loadedIndex().Suggest(field(alias, []string{"lines"}, "")), "an array has no named children")

	element := query.Alias{Name: "t", Scopes: [][]string{orders}, Path: []string{"lines[]"}}
	assert.Equal(t, []string{"sku"}, texts(loadedIndex().Suggest(field(element, nil, ""))))
}

func TestTopLevelFieldsStopAtTheFirstDot(t *testing.T) {
	alias := query.Alias{Name: "o", Scopes: [][]string{orders}}
	index := loadedIndex()

	top := index.Suggest(query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{alias}, TopLevel: true})
	nested := index.Suggest(query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{alias}, Path: []string{"customer"}, TopLevel: true})

	assert.Contains(t, texts(top), "customer", "an object is still a top-level field")
	assert.Empty(t, nested)
}

func TestThePartitionKeyIsAFieldBeforeAnyItemIsSeen(t *testing.T) {
	index := complete.NewIndex()
	index.SetContainers("sales", []adapter.Node{container("sales", "orders", "/tenant/id,/region")})

	got := index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, ""))

	require.Equal(t, []string{"region"}, texts(got), "a nested key path is a child of its parent")
	assert.Equal(t, "field · partition key", got[0].Detail)
	nested := index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, []string{"tenant"}, ""))
	assert.Equal(t, []string{"id"}, texts(nested))
}

func TestAnObservedPartitionKeyTakesItsKindAndKeepsItsPlace(t *testing.T) {
	got := loadedIndex().Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, ""))

	require.Equal(t, []string{"customerId", "id", "currency", "customer", "lines", "order-id"}, texts(got))
	assert.Equal(t, "string · partition key", got[0].Detail)
	assert.Equal(t, "object", got[3].Detail)
	assert.Equal(t, "array", got[4].Detail)
}

func TestAKindThatVariesIsShownAsAField(t *testing.T) {
	index := complete.NewIndex()
	index.AddFields(orders, []adapter.Field{{Path: "n", Kind: "number"}})
	index.AddFields(orders, []adapter.Field{{Path: "n", Kind: "string"}})

	got := index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, ""))

	require.Len(t, got, 1, "a field observed twice appears once")
	assert.Equal(t, "field", got[0].Detail)
}

func TestAUnionAliasListsFieldsOfEveryContainerAndSaysWhichHaveThem(t *testing.T) {
	index := loadedIndex()
	index.AddFields(archive, []adapter.Field{{Path: "id", Kind: "string"}, {Path: "archivedAt", Kind: "string"}})

	got := index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders, archive}}, nil, ""))

	listed := texts(got)
	assert.Equal(t, []string{"customerId", "id", "currency", "customer", "lines", "order-id", "archivedAt"}, listed)
	assert.Equal(t, "string · partition key", got[0].Detail, "the key of both is in both")
	assert.Equal(t, "string", got[1].Detail, "id is in both")
	assert.Equal(t, "string · orders", got[2].Detail)
	assert.Equal(t, "string · archive", got[6].Detail)
}

func TestADeletedContainerLosesItsFields(t *testing.T) {
	index := loadedIndex()

	index.SetContainers("sales", []adapter.Node{container("sales", "archive", "/customerId")})

	assert.Empty(t, index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "")))
	assert.Equal(t, []string{"archive"}, texts(index.Suggest(query.Completion{Kind: query.CompleteContainer, Database: "sales"})))
}

func TestADeletedDatabaseLosesItsContainersAndFields(t *testing.T) {
	index := loadedIndex()

	index.SetDatabases([]string{"telemetry"})

	assert.False(t, index.HasContainers("sales"))
	assert.Empty(t, index.Suggest(field(query.Alias{Name: "c", Scopes: [][]string{orders}}, nil, "")))
	assert.Equal(t, []string{"telemetry"}, texts(index.Suggest(query.Completion{Kind: query.CompleteDatabase})))
}

func TestAnAliasWithNoScopeHasNoFields(t *testing.T) {
	assert.Empty(t, loadedIndex().Suggest(field(query.Alias{Name: "c"}, nil, "")))
}

func TestASourcePositionOffersDatabasesThenTheScopeContainer(t *testing.T) {
	index := loadedIndex()
	index.SetScope(orders)

	got := index.Suggest(query.Completion{Kind: query.CompleteSource})

	require.Equal(t, []string{"sales", "telemetry", "c"}, texts(got))
	assert.Equal(t, complete.KindDatabase, got[0].Kind)
	assert.Equal(t, "scope · sales.orders", got[2].Detail)
	assert.Equal(t, []string{"sales", "telemetry"}, texts(index.Suggest(query.Completion{Kind: query.CompleteDatabase})),
		"a further source position offers no shortcut")
}

func TestAnUnknownDatabaseHasNoContainers(t *testing.T) {
	index := loadedIndex()

	assert.Empty(t, index.Suggest(query.Completion{Kind: query.CompleteContainer, Database: "hr"}))
	assert.False(t, index.HasContainers("hr"))
	assert.True(t, index.HasContainers("sales"))
}

func TestAReferenceListsAliasesThenTheirQualifiedFields(t *testing.T) {
	aliases := []query.Alias{
		{Name: "o", Scopes: [][]string{orders}},
		{Name: "t", Scopes: [][]string{orders}, Path: []string{"lines[]"}},
	}

	got := loadedIndex().Suggest(query.Completion{Kind: query.CompleteReference, Aliases: aliases})

	require.Equal(t, []string{"o", "t", "o.customerId", "o.id", "o.currency", "o.customer", "o.lines", `o["order-id"]`, "t.sku"}, texts(got))
	assert.Equal(t, "sales.orders", got[0].Detail)
	assert.Equal(t, "sales.orders · lines[]", got[1].Detail)
	assert.Equal(t, complete.KindAlias, got[0].Kind)
	assert.Equal(t, complete.KindField, got[2].Kind)
	assert.False(t, got[7].Bracketed, "the alias is part of the insert, so no dot is replaced")
}

func TestAnExpressionAddsFunctionsAndKeywords(t *testing.T) {
	got := loadedIndex().Suggest(query.Completion{
		Kind:     query.CompleteExpression,
		Aliases:  []query.Alias{{Name: "c", Scopes: [][]string{orders}}},
		Keywords: []string{"TRUE", "NULL"},
		Word:     "st",
	})

	listed := texts(got)
	assert.NotContains(t, listed, "TRUE")
	assert.Less(t, position(listed, "ST_AREA"), position(listed, "STARTSWITH"), "prefix matches keep the table's order")
	assert.Less(t, position(listed, "StringToArray"), position(listed, "c.customer"), "a substring match on the field comes last")
	i := position(listed, "STARTSWITH")
	assert.Equal(t, "STARTSWITH(", got[i].Insert)
	assert.Equal(t, "STARTSWITH(string, prefix [, ignoreCase])", got[i].Detail)
	assert.Equal(t, complete.KindFunction, got[i].Kind)
}

func position(listed []string, want string) int {
	for i, text := range listed {
		if text == want {
			return i
		}
	}
	return -1
}

func TestNothingIsOfferedWhereNothingBelongs(t *testing.T) {
	assert.Empty(t, loadedIndex().Suggest(query.Completion{Kind: query.CompleteNothing, Word: "x"}))
}

func TestEveryFunctionOpensItsParenthesis(t *testing.T) {
	names := map[string]bool{}
	for _, f := range complete.Functions() {
		assert.False(t, names[f.Name], "%s is listed twice", f.Name)
		names[f.Name] = true
		assert.True(t, len(f.Signature) > len(f.Name) && f.Signature[:len(f.Name)+1] == f.Name+"(", "%s", f.Signature)
	}
}
