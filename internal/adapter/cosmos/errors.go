package cosmos

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// refusalError restates a request the service refused without the request
// URL the SDK prints, so neither the screen nor the history file names the
// account.
type refusalError struct {
	op      string
	message string
	cause   error
}

func (e *refusalError) Error() string { return "cosmos: " + e.op + ": " + e.message }

func (e *refusalError) Unwrap() error { return e.cause }

// wrap reports err under op. A refusal and a request that never reached
// the service are the two whose SDK text carries the request URL; the
// second is restated for every adapter alike, since the TUI offers a retry
// for it.
func wrap(op string, err error) error {
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return &refusalError{op: op, message: refusal(respErr), cause: err}
	}
	if unreachable, ok := adapter.Unreachable(err); ok {
		return unreachable
	}
	return fmt.Errorf("cosmos: %s: %w", op, err)
}

// notFound reports a request the service answered 404. A resource with no
// throughput offer of its own is refused that way, as is a missing one.
func notFound(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound
}

func refusal(respErr *azcore.ResponseError) string {
	text := strconv.Itoa(respErr.StatusCode)
	if status := http.StatusText(respErr.StatusCode); status != "" {
		text += " " + status
	}
	if message := serviceMessage(respErr); message != "" {
		text += ": " + message
	}
	return text
}

// serviceMessage is what the service said about a refusal. The service and
// the emulator shape their bodies differently — {"message": …},
// {"errors": [{"message": …}]}, {"Errors": ["…"]} — and encoding/json
// matches field names case aside, so one decode reads all three.
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
