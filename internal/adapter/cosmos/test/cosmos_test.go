package cosmos_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// testKey is a syntactically valid (base64) account key for offline tests.
var testKey = base64.StdEncoding.EncodeToString([]byte("not-a-real-key"))

func TestParseSettingsValidation(t *testing.T) {
	cases := []struct {
		name    string
		raw     map[string]string
		wantErr string
	}{
		{name: "empty", raw: map[string]string{}, wantErr: "connection_string, or endpoint and key"},
		{name: "endpoint only", raw: map[string]string{"endpoint": "https://x"}, wantErr: "connection_string, or endpoint and key"},
		{name: "key only", raw: map[string]string{"key": "k"}, wantErr: "connection_string, or endpoint and key"},
		{name: "bad page_size", raw: map[string]string{"connection_string": "cs", "page_size": "abc"}, wantErr: "page_size"},
		{name: "zero page_size", raw: map[string]string{"connection_string": "cs", "page_size": "0"}, wantErr: "page_size"},
		{name: "endpoint and key", raw: map[string]string{"endpoint": "https://x", "key": "k"}},
		{name: "connection string", raw: map[string]string{"connection_string": "cs"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := cosmos.ParseSettings(tc.raw)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int32(100), s.PageSize)
		})
	}
}

func TestParseSettingsTLSSkipOnlyExplicitTrue(t *testing.T) {
	for value, want := range map[string]bool{
		"true": true, "TRUE": false, "True": false, "1": false, "yes": false, "false": false, "": false,
	} {
		s, err := cosmos.ParseSettings(map[string]string{
			"connection_string": "cs", "insecure_skip_verify": value,
		})
		require.NoError(t, err)
		assert.Equal(t, want, s.InsecureSkipVerify, "insecure_skip_verify=%q", value)
	}
}

func TestParseSettingsPageSize(t *testing.T) {
	s, err := cosmos.ParseSettings(map[string]string{"connection_string": "cs", "page_size": "25"})
	require.NoError(t, err)
	assert.Equal(t, int32(25), s.PageSize)
}

func TestConnectValidatesSettings(t *testing.T) {
	_, err := cosmos.Adapter{}.Connect(context.Background(), map[string]string{})
	require.Error(t, err)
}

func TestConnectAndQueryOffline(t *testing.T) {
	conn, err := cosmos.Adapter{}.Connect(context.Background(), map[string]string{
		"endpoint": "https://localhost:8081", "key": testKey, "insecure_skip_verify": "true",
	})
	require.NoError(t, err)
	require.NotNil(t, conn.Catalog())

	_, err = conn.Query(context.Background(), adapter.Query{Text: "SELECT * FROM c", Scope: []string{"only-db"}})
	require.ErrorContains(t, err, "scope")

	cursor, err := conn.Query(context.Background(), adapter.Query{Text: "SELECT * FROM c", Scope: []string{"db", "items"}})
	require.NoError(t, err)
	assert.True(t, cursor.HasMore())
	require.NoError(t, cursor.Close())
	require.NoError(t, conn.Close())
	assert.Equal(t, cosmos.Name, cosmos.Adapter{}.Name())
}

func TestPageBuilderColumnUnionLocking(t *testing.T) {
	b := cosmos.NewPageBuilder()

	page1, err := b.Build([][]byte{
		[]byte(`{"id":"1","amount":42,"nested":{"x":1},"tags":["a","b"]}`),
		[]byte(`{"id":"2","note":"plain text","missing_from_first":null}`),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "amount", "nested", "tags", "note", "missing_from_first"}, page1.Columns)
	require.Len(t, page1.Rows, 2)
	assert.Equal(t, []string{"1", "42", `{"x":1}`, `["a","b"]`, "", ""}, page1.Rows[0])
	assert.Equal(t, []string{"2", "", "", "", "plain text", ""}, page1.Rows[1])
	require.Len(t, page1.Raw, 2)

	// Later pages append newly seen keys after the locked first-page order.
	page2, err := b.Build([][]byte{[]byte(`{"brand_new":true,"id":"3"}`)})
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "amount", "nested", "tags", "note", "missing_from_first", "brand_new"}, page2.Columns)
	assert.Equal(t, []string{"3", "", "", "", "", "", "true"}, page2.Rows[0])
}

func TestPageBuilderRejectsMalformedItems(t *testing.T) {
	_, err := cosmos.NewPageBuilder().Build([][]byte{[]byte(`[1,2]`)})
	require.ErrorContains(t, err, "not a JSON object")
	_, err = cosmos.NewPageBuilder().Build([][]byte{[]byte(`{"id":`)})
	require.Error(t, err)
}

func TestPinnedKey(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		pkPath string
		want   string
	}{
		{name: "double quoted", text: `SELECT * FROM c WHERE c.pk = "x"`, pkPath: "/pk", want: "x"},
		{name: "single quoted", text: `SELECT * FROM c WHERE c.pk = 'y'`, pkPath: "/pk", want: "y"},
		{name: "conjunction ok", text: `SELECT * FROM c WHERE c.pk = "x" AND c.n > 3`, pkPath: "/pk", want: "x"},
		{name: "no where", text: `SELECT c.pk = "x" FROM c`, pkPath: "/pk", want: ""},
		{name: "or disables", text: `SELECT * FROM c WHERE c.pk = "x" OR c.n > 3`, pkPath: "/pk", want: ""},
		{name: "not disables", text: `SELECT * FROM c WHERE NOT c.pk = "x"`, pkPath: "/pk", want: ""},
		{name: "two pins disable", text: `SELECT * FROM c WHERE c.pk = "x" AND c.pk = "y"`, pkPath: "/pk", want: ""},
		{name: "other field", text: `SELECT * FROM c WHERE c.name = "x"`, pkPath: "/pk", want: ""},
		{name: "field is prefix", text: `SELECT * FROM c WHERE c.pkx = "x"`, pkPath: "/pk", want: ""},
		{name: "case sensitive field", text: `SELECT * FROM c WHERE c.PK = "x"`, pkPath: "/pk", want: ""},
		{name: "gte not equality", text: `SELECT * FROM c WHERE c.pk >= "x"`, pkPath: "/pk", want: ""},
		{name: "nested pk path", text: `SELECT * FROM c WHERE c.pk = "x"`, pkPath: "/a/b", want: ""},
		{name: "empty pk path", text: `SELECT * FROM c WHERE c.pk = "x"`, pkPath: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cosmos.PinnedKey(tc.text, tc.pkPath))
		})
	}
}
