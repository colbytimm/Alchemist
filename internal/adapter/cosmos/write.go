package cosmos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// unknownWriteError is a write that was sent and never answered: it may
// have been applied. Its text, like a refusal's, leaves out the request URL.
type unknownWriteError struct {
	op     string
	reason string
	cause  error
}

func (e *unknownWriteError) Error() string { return "cosmos: " + e.op + ": " + e.reason }

func (e *unknownWriteError) Unwrap() []error { return []error{adapter.ErrWriteOutcomeUnknown, e.cause} }

// PartitionKey folds a key's JSON components into the SDK's, in order.
func PartitionKey(key adapter.PartitionKey) (azcosmos.PartitionKey, error) {
	folded := azcosmos.NewPartitionKey()
	for _, raw := range key {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return azcosmos.PartitionKey{}, fmt.Errorf("cosmos: partition key value %s: %w", raw, err)
		}
		switch v := value.(type) {
		case nil:
			folded = folded.AppendNull()
		case string:
			folded = folded.AppendString(v)
		case bool:
			folded = folded.AppendBool(v)
		case json.Number:
			number, err := v.Float64()
			if err != nil {
				return azcosmos.PartitionKey{}, fmt.Errorf("cosmos: partition key value %s: %w", raw, err)
			}
			folded = folded.AppendNumber(number)
		default:
			return azcosmos.PartitionKey{}, fmt.Errorf("cosmos: partition key value %s: not a string, number, boolean or null", raw)
		}
	}
	return folded, nil
}

// withoutRetries turns off azcore's generic retry policy for one call. The
// Cosmos policy already refuses to replay a write; azcore's would replay it
// on 408, 429 and 5xx, and a write must be sent at most once.
func withoutRetries(ctx context.Context) context.Context {
	return policy.WithRetryOptions(ctx, policy.RetryOptions{MaxRetries: -1})
}

// WriteError classifies a failed write. One that was sent and not answered
// — a 408 or 5xx, a dropped connection, a passed deadline — wraps
// adapter.ErrWriteOutcomeUnknown; anything else is guaranteed not applied: a
// refusal of the whole request, or one that never left, which a timeout is
// not, whatever it means for a read.
func WriteError(op string, err error) error {
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		if respErr.StatusCode == http.StatusRequestTimeout || respErr.StatusCode >= http.StatusInternalServerError {
			return &unknownWriteError{op: op, reason: refusal(respErr), cause: err}
		}
		return wrap(op, err)
	}
	unreachable, ok := adapter.Unreachable(err)
	if neverSent(err) && ok {
		return unreachable
	}
	reason := "no answer"
	if ok {
		reason = unreachable.Reason
	}
	return &unknownWriteError{op: op, reason: reason, cause: err}
}

// neverSent reports a request that failed before a connection was made.
func neverSent(err error) bool {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) || errors.As(err, &opErr) && opErr.Op == "dial"
}
