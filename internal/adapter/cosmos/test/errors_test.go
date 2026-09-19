package cosmos_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// stallFor is how long a stalling account holds a request, well past the
// deadline the test gives it.
const stallFor = time.Second

// The requests the adapter makes, each named the way its error names it.
var requests = []struct {
	name string
	call func(ctx context.Context, conn adapter.Connection) error
}{
	{name: "ping", call: func(ctx context.Context, conn adapter.Connection) error {
		return conn.Ping(ctx)
	}},
	{name: "list databases", call: func(ctx context.Context, conn adapter.Connection) error {
		_, err := conn.Catalog().Root(ctx)
		return err
	}},
	{name: `list containers of "sales"`, call: func(ctx context.Context, conn adapter.Connection) error {
		_, err := conn.Catalog().Children(ctx, adapter.Node{Kind: adapter.NodeDatabase, Name: "sales", Path: []string{"sales"}})
		return err
	}},
	{name: "query page", call: queryPage},
}

func queryPage(ctx context.Context, conn adapter.Connection) error {
	cursor, err := conn.Query(ctx, adapter.Query{Text: "SELECT * FROM c", Scope: []string{"sales", "orders"}})
	if err != nil {
		return err
	}
	_, err = cursor.NextPage(ctx)
	return err
}

// account connects the adapter to a server standing in for a Cosmos
// account, and returns the host its requests go to, which must never appear
// in an error.
func account(t *testing.T, handler http.Handler) (adapter.Connection, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return connectTo(t, server.URL)
}

func connectTo(t *testing.T, endpoint string) (adapter.Connection, string) {
	t.Helper()
	conn, err := cosmos.Adapter{}.Connect(context.Background(), map[string]string{
		"endpoint": endpoint, "key": testKey, "insecure_skip_verify": "true",
	})
	require.NoError(t, err)
	address, err := url.Parse(endpoint)
	require.NoError(t, err)
	return conn, address.Host
}

// refusing answers every request with the one refusal given.
func refusing(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

// stalling answers the account lookup the SDK makes first, and then holds
// every request until the client gives up on it.
func stalling() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		select {
		case <-time.After(stallFor):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusRequestTimeout)
	})
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
		{
			name:   "a status the standard library has no text for",
			status: 449,
			body:   `{"message":"retry the request"}`,
			want:   "cosmos: query page: 449: retry the request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, host := account(t, refusing(tt.status, tt.body))

			err := queryPage(context.Background(), conn)

			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}

func TestARefusalKeepsTheSDKErrorInTheChain(t *testing.T) {
	conn, _ := account(t, refusing(http.StatusNotFound, `{"Errors":["gone"]}`))

	err := queryPage(context.Background(), conn)

	var respErr *azcore.ResponseError
	require.ErrorAs(t, err, &respErr)
	assert.Equal(t, http.StatusNotFound, respErr.StatusCode)
}

func TestEveryRequestPathShapesARefusal(t *testing.T) {
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			conn, host := account(t, refusing(http.StatusUnauthorized, ""))

			err := request.call(context.Background(), conn)

			require.Error(t, err)
			assert.Equal(t, "cosmos: "+request.name+": 401 Unauthorized", err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}

// TestADeadEndpointIsReportedWithoutItsAddress waits out the SDK, which
// retries a connection it cannot open for some ten seconds before giving
// up — once: it remembers, so the later requests fail at once.
func TestADeadEndpointIsReportedWithoutItsAddress(t *testing.T) {
	closed := httptest.NewTLSServer(http.NotFoundHandler())
	endpoint := closed.URL
	closed.Close()
	conn, host := connectTo(t, endpoint)

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			err := request.call(context.Background(), conn)

			require.Error(t, err)
			assert.Equal(t, "cosmos: "+request.name+": dial: connect: connection refused", err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}

func TestARequestTheCallerGaveUpOnIsReportedWithoutItsAddress(t *testing.T) {
	conn, host := account(t, stalling())

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), stallFor/10)
			t.Cleanup(cancel)

			err := request.call(ctx, conn)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Equal(t, "cosmos: "+request.name+": context deadline exceeded", err.Error())
			assert.NotContains(t, err.Error(), host)
		})
	}
}
