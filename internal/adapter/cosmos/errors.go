package cosmos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// requestError restates a failed request without the request URL the SDK
// prints, so neither the screen nor the history file names the account; the
// SDK's error stays in the chain for errors.As.
type requestError struct {
	op      string
	message string
	cause   error
}

func (e *requestError) Error() string { return "cosmos: " + e.op + ": " + e.message }

func (e *requestError) Unwrap() error { return e.cause }

func wrap(op string, err error) error {
	message, ok := describe(err)
	if !ok {
		return fmt.Errorf("cosmos: %s: %w", op, err)
	}
	return &requestError{op: op, message: message, cause: err}
}

// describe restates the failures whose SDK text carries the request URL: a
// refusal from the service, a connection that never reached it, and a
// context that ended mid-request — which the SDK reports with the last
// transport error, URL and all, printed beside it.
func describe(err error) (string, bool) {
	var respErr *azcore.ResponseError
	var urlErr *url.Error
	switch {
	case errors.As(err, &respErr):
		return refusal(respErr), true
	case errors.As(err, &urlErr):
		return transportFailure(urlErr), true
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded.Error(), true
	case errors.Is(err, context.Canceled):
		return context.Canceled.Error(), true
	}
	return "", false
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

// transportFailure is the leaf of a connection that never reached the
// service; every wrapper above it repeats the URL or the host.
func transportFailure(urlErr *url.Error) string {
	var dnsErr *net.DNSError
	if errors.As(urlErr, &dnsErr) {
		return "lookup: " + dnsErr.Err
	}
	var opErr *net.OpError
	if errors.As(urlErr, &opErr) {
		return opErr.Op + ": " + opErr.Err.Error()
	}
	return urlErr.Err.Error()
}
