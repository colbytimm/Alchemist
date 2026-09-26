package adapter

import (
	"context"
	"errors"
)

// ItemEditor applies one operation to one existing item. Optional: callers
// find it with a comma-ok type assertion.
type ItemEditor interface {
	// EditItem runs a patch or a delete, and refuses any other kind with
	// ErrUnsupported. It never retries. The result's RequestCharge and
	// Status are set whenever the backend answered, error or not. An error
	// is ErrPreconditionFailed or ErrItemNotFound for an item left as it
	// was, a *ThrottledError for one to try again, one wrapping
	// ErrWriteOutcomeUnknown for a write that may have been applied, and
	// anything else for a refusal.
	EditItem(ctx context.Context, container []string, key PartitionKey, op Operation) (OperationResult, error)
}

var (
	// ErrPreconditionFailed is an item that no longer satisfies the
	// operation's Condition or IfMatch. Nothing was written.
	ErrPreconditionFailed = errors.New("item changed since it was selected")
	// ErrItemNotFound is an item that is gone. Nothing was written.
	ErrItemNotFound = errors.New("item not found")
)
