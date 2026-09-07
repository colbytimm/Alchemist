package cosmos_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// refusingAccount is a connection to an account that answers every request
// with the one refusal given, so the adapter's error shaping can be driven
// offline. It returns the host the requests go to, which must never appear
// in an error.
func refusingAccount(t *testing.T, status int, body string) (adapter.Connection, string) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	conn, err := cosmos.Adapter{}.Connect(context.Background(), map[string]string{
		"endpoint": server.URL, "key": testKey, "insecure_skip_verify": "true",
	})
	require.NoError(t, err)
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	return conn, address.Host
}

func queryPage(conn adapter.Connection, ctx context.Context) error {
	cursor, err := conn.Query(ctx, adapter.Query{Text: "SELECT * FROM c", Scope: []string{"sales", "orders"}})
	if err != nil {
		return err
	}
	_, err = cursor.NextPage(ctx)
	return err
}

func TestARefusalSaysWhatTheServiceSaidAndNotWhereItWasSent(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "a query the parser rejected, as the emulator reports it",
			status: http.StatusBadRequest,
			body:   `{"errors":[{"code":"SC1001","message":"Syntax error, incorrect syntax near 'SELEC'.","severity":"Error"}]}`,
			want:   "cosmos: query page: 400 Bad Request: Syntax error, incorrect syntax near 'SELEC'.",
		},
		{
			name:   "a missing resource, as the emulator reports it",
			status: http.StatusNotFound,
			body:   `{"Errors":["Owner resource does not exist. Database: sales, Collection: orders"]}`,
			want:   "cosmos: query page: 404 Not Found: Owner resource does not exist. Database: sales, Collection: orders",
		},
		{
			name:   "a message with diagnostics on further lines, as the service reports it",
			status: http.StatusBadRequest,
			body:   "{\"code\":\"BadRequest\",\"message\":\"Message: {\\\"errors\\\":[]}\\r\\nActivityId: 7c1, Request URI: /apps/x\"}",
			want:   `cosmos: query page: 400 Bad Request: Message: {"errors":[]}`,
		},
		{
			name:   "a body in no known shape",
			status: http.StatusConflict,
			body:   "  something else entirely\n",
			want:   "cosmos: query page: 409 Conflict: something else entirely",
		},
		{
			name:   "no body at all",
			status: http.StatusUnauthorized,
			want:   "cosmos: query page: 401 Unauthorized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, host := refusingAccount(t, tt.status, tt.body)

			err := queryPage(conn, context.Background())

			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}

func TestARefusalKeepsTheStatusAndTheSDKErrorForCallers(t *testing.T) {
	conn, _ := refusingAccount(t, http.StatusNotFound, `{"Errors":["gone"]}`)

	err := queryPage(conn, context.Background())

	var serviceErr *cosmos.ServiceError
	require.ErrorAs(t, err, &serviceErr)
	assert.Equal(t, http.StatusNotFound, serviceErr.StatusCode)
	assert.Equal(t, "gone", serviceErr.Message)
	var respErr *azcore.ResponseError
	assert.True(t, errors.As(err, &respErr), "the SDK's error stays in the chain")
}

func TestEveryRequestPathShapesARefusal(t *testing.T) {
	database := adapter.Node{Kind: adapter.NodeDatabase, Name: "sales", Path: []string{"sales"}}
	tests := []struct {
		name string
		call func(adapter.Connection, context.Context) error
		want string
	}{
		{name: "ping", call: adapter.Connection.Ping, want: "cosmos: ping: 401 Unauthorized"},
		{name: "list databases", call: func(conn adapter.Connection, ctx context.Context) error {
			_, err := conn.Catalog().Root(ctx)
			return err
		}, want: "cosmos: list databases: 401 Unauthorized"},
		{name: "list containers", call: func(conn adapter.Connection, ctx context.Context) error {
			_, err := conn.Catalog().Children(ctx, database)
			return err
		}, want: `cosmos: list containers of "sales": 401 Unauthorized`},
		{name: "query page", call: queryPage, want: "cosmos: query page: 401 Unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, host := refusingAccount(t, http.StatusUnauthorized, "")

			err := tt.call(conn, context.Background())

			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}
