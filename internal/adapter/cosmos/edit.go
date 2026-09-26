package cosmos

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.ItemEditor = (*connection)(nil)

// EditItem sends one patch or one delete, once. A key it cannot send
// exactly is refused unsent as no partition key: the item cannot be
// addressed through this SDK.
func (c *connection) EditItem(ctx context.Context, path []string, key adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	name := fmt.Sprintf("%s %s in %s", op.Kind, op.ID, pathText(path))
	container, err := c.containerAt(name, path)
	if err != nil {
		return adapter.OperationResult{}, err
	}
	folded, err := exactPartitionKey(key)
	if err != nil {
		return adapter.OperationResult{}, fmt.Errorf("cosmos: %s: %w", name, err)
	}
	send, err := editRequest(container, folded, op)
	if err != nil {
		return adapter.OperationResult{}, fmt.Errorf("cosmos: %s: %w", name, err)
	}
	resp, err := send(withoutRetries(ctx))
	if err != nil {
		return failedResult(err), editError(name, err)
	}
	return adapter.OperationResult{
		Outcome:       adapter.OperationApplied,
		Status:        statusLine(resp.RawResponse.StatusCode),
		ETag:          string(resp.ETag),
		RequestCharge: float64(resp.RequestCharge),
	}, nil
}

func exactPartitionKey(key adapter.PartitionKey) (azcosmos.PartitionKey, error) {
	for _, value := range key {
		if !exactNumber(value) {
			return azcosmos.PartitionKey{}, fmt.Errorf("%w: %w", adapter.ErrNoPartitionKey, errInexactKey)
		}
	}
	return PartitionKey(key)
}

// editRequest prepares op for sending, and refuses one that cannot be sent
// before anything is.
func editRequest(container *azcosmos.ContainerClient, key azcosmos.PartitionKey, op adapter.Operation) (func(context.Context) (azcosmos.ItemResponse, error), error) {
	options := &azcosmos.ItemOptions{}
	if op.IfMatch != "" {
		etag := azcore.ETag(op.IfMatch)
		options.IfMatchEtag = &etag
	}
	switch op.Kind {
	case adapter.OperationPatch:
		patch, err := patchOperations(op.Body, op.Condition)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) (azcosmos.ItemResponse, error) {
			return container.PatchItem(ctx, key, op.ID, patch, options)
		}, nil
	case adapter.OperationDelete:
		return func(ctx context.Context) (azcosmos.ItemResponse, error) {
			return container.DeleteItem(ctx, key, op.ID, options)
		}, nil
	}
	return nil, adapter.ErrUnsupported
}

// failedResult is what the service said of a write it refused, when it
// said anything.
func failedResult(err error) adapter.OperationResult {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return adapter.OperationResult{}
	}
	return adapter.OperationResult{
		Outcome:       adapter.OperationFailed,
		Status:        statusLine(respErr.StatusCode),
		RequestCharge: chargeOf(err),
	}
}

// editError names the two refusals that leave the item as it was and say
// so, and classifies the rest as any write's failure is.
func editError(op string, err error) error {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return WriteError(op, err)
	}
	switch respErr.StatusCode {
	case http.StatusPreconditionFailed:
		return &refusalError{op: op, message: refusal(respErr), cause: err, kind: adapter.ErrPreconditionFailed}
	case http.StatusNotFound:
		return &refusalError{op: op, message: refusal(respErr), cause: err, kind: adapter.ErrItemNotFound}
	}
	return WriteError(op, err)
}
