package cosmos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.ItemWriter = (*connection)(nil)

// maxExactInteger is the largest integer a float64 holds exactly. The SDK
// builds a numeric key from a float64, so a larger one would be filed under
// a key its own body disagrees with.
const maxExactInteger = 1 << 53

var errInexactKey = errors.New("an integer partition key past 2^53 cannot be sent exactly")

// OpenItemSink reads the container's key paths once, for every upsert to
// come.
func (c *connection) OpenItemSink(ctx context.Context, path []string) (adapter.ItemSink, error) {
	op := "upsert into " + pathText(path)
	container, err := c.containerAt(op, path)
	if err != nil {
		return nil, err
	}
	resp, err := container.Read(ctx, nil)
	if err != nil {
		return nil, wrap(op, err)
	}
	return &sink{op: op, container: container, paths: slices.Clone(resp.ContainerProperties.PartitionKeyDefinition.Paths)}, nil
}

// sink upserts through azcore's retry policy, which waits out a throttle
// the service names a delay for: an upsert sent twice leaves the item as
// sending it once would have.
type sink struct {
	op        string
	container *azcosmos.ContainerClient
	paths     []string
}

func (s *sink) Upsert(ctx context.Context, item json.RawMessage) (float64, error) {
	key, err := s.partitionKey(item)
	if err != nil {
		return 0, fmt.Errorf("cosmos: %s: %w: %w", s.op, adapter.ErrItemRefused, err)
	}
	resp, err := s.container.UpsertItem(ctx, key, item, &azcosmos.ItemOptions{EnableContentResponseOnWrite: false})
	if err != nil {
		return chargeOf(err), s.refusal(err)
	}
	return float64(resp.RequestCharge), nil
}

func (s *sink) partitionKey(item json.RawMessage) (azcosmos.PartitionKey, error) {
	values, err := adapter.PartitionKeyValues(item, s.paths)
	if err != nil {
		return azcosmos.PartitionKey{}, err
	}
	for _, value := range values {
		if !exactNumber(value) {
			return azcosmos.PartitionKey{}, errInexactKey
		}
	}
	return PartitionKey(values)
}

// exactNumber reports whether value is anything but an integer too large
// for a float64 to hold: a string, a boolean, null, a number with a fraction
// or an exponent, or an integer within 2^53.
func exactNumber(value json.RawMessage) bool {
	digits := strings.TrimPrefix(string(value), "-")
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return true
	}
	n, err := strconv.ParseInt(string(value), 10, 64)
	return err == nil && n <= maxExactInteger && n >= -maxExactInteger
}

// refusal marks what the service refused for the item's own sake: a
// malformed body or one too large. Anything else says nothing about the
// next item.
func (s *sink) refusal(err error) error {
	wrapped := wrap(s.op, err)
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && (respErr.StatusCode == http.StatusBadRequest || respErr.StatusCode == http.StatusRequestEntityTooLarge) {
		return fmt.Errorf("%w: %w", adapter.ErrItemRefused, wrapped)
	}
	return wrapped
}

// chargeOf is what a refused request still cost.
func chargeOf(err error) float64 {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.RawResponse == nil {
		return 0
	}
	charge, parseErr := strconv.ParseFloat(respErr.RawResponse.Header.Get("x-ms-request-charge"), 64)
	if parseErr != nil {
		return 0
	}
	return charge
}
