package cosmos

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// ServiceError is a request the service refused. Its text is the status and
// what the service said, without the request URL the SDK's own error prints
// first: an error is shown on screen and written to the history file, and
// neither should name the account. The SDK's error stays in the chain for
// errors.As.
type ServiceError struct {
	Op         string
	StatusCode int
	Message    string
	cause      error
}

func (e *ServiceError) Error() string {
	text := fmt.Sprintf("cosmos: %s: %d %s", e.Op, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message == "" {
		return text
	}
	return text + ": " + e.Message
}

func (e *ServiceError) Unwrap() error { return e.cause }

// wrap reports err under op, restating a refusal from the service as a
// ServiceError.
func wrap(op string, err error) error {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return fmt.Errorf("cosmos: %s: %w", op, err)
	}
	return &ServiceError{Op: op, StatusCode: respErr.StatusCode, Message: serviceMessage(respErr), cause: err}
}

// serviceMessage is what the service said about a refusal. The service and
// the emulator shape their bodies differently — {"message": …},
// {"errors": [{"message": …}]}, {"Errors": ["…"]} — and encoding/json
// matches field names case aside, so one decode reads all three. A body in
// no known shape is passed on whole rather than lost.
func serviceMessage(respErr *azcore.ResponseError) string {
	if respErr.RawResponse == nil {
		return ""
	}
	body, err := runtime.Payload(respErr.RawResponse)
	if err != nil {
		return ""
	}
	var payload struct {
		Message string            `json:"message"`
		Errors  []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return strings.TrimSpace(string(body))
	}
	if payload.Message != "" {
		return firstLine(payload.Message)
	}
	var texts []string
	for _, raw := range payload.Errors {
		if text := errorText(raw); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return strings.TrimSpace(string(body))
	}
	return strings.Join(texts, "; ")
}

// errorText reads one element of an errors list: a string from the
// emulator, an object with a message from the query parser.
func errorText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &detail) == nil {
		return detail.Message
	}
	return ""
}

// firstLine drops the activity id and request diagnostics the service
// appends to its message on lines of their own.
func firstLine(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return strings.TrimRight(line, "\r")
}
