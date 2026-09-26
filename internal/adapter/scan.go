package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ItemScanner reads every item of a container, a page at a time, in an order
// the backend keeps stable for as long as a ScanPosition is outstanding.
// Optional: callers find it with a comma-ok type assertion.
type ItemScanner interface {
	ScanItems(ctx context.Context, request ScanRequest) (ItemScan, error)
}

// ScanRequest is what to read. Every field's zero value is the whole
// container from its beginning, so a field added later changes nothing for a
// caller that leaves it unset.
type ScanRequest struct {
	Container []string
	From      ScanPosition // zero value: the beginning
	PageSize  int32        // zero: the connection's page size
}

// ScanPosition resumes a scan after the page that carried it. It is opaque,
// and valid only for the container, the backend and the request that issued
// it.
type ScanPosition string

// ItemScan has Cursor's shape, so whoever holds one follows the same
// ownership rule: the goroutine reading a page owns the scan until it hands
// it back.
type ItemScan interface {
	NextPage(ctx context.Context) (ItemPage, error)
	HasMore() bool
	Close() error
}

type ItemPage struct {
	Items         []json.RawMessage // whole items as stored, system fields included
	Next          ScanPosition      // empty on the last page
	RequestCharge float64
}

// ItemWriter writes whole items into an existing container. Optional:
// callers find it with a comma-ok type assertion.
type ItemWriter interface {
	OpenItemSink(ctx context.Context, container []string) (ItemSink, error)
}

// ItemSink upserts items into one container, each under the partition key
// its own values name, and reports what each write cost. It is safe for
// concurrent use.
type ItemSink interface {
	Upsert(ctx context.Context, item json.RawMessage) (requestCharge float64, err error)
}

// ErrItemRefused is an item the target cannot take for reasons of its own,
// such as a partition key it has no value for: the next item may well
// succeed.
var ErrItemRefused = errors.New("item refused")

// ThrottledError is a read or a write the backend refused for rate, after
// the adapter's own retries. RetryAfter is zero when the backend named no
// delay.
type ThrottledError struct {
	RetryAfter time.Duration
	Err        error
}

// Error is the backend's own account of the refusal, which already says
// that it was for rate.
func (e *ThrottledError) Error() string { return e.Err.Error() }

func (e *ThrottledError) Unwrap() error { return e.Err }
