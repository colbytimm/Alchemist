package adapter

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
)

// UnreachableTitle is what an UnreachableError says before its reason, for
// a view that lays the two out on lines of their own.
const UnreachableTitle = "account unreachable"

// UnreachableError reports a request that never reached the account: the
// connection was refused, the host did not resolve, or the caller's deadline
// passed first. Reason says which, in a few words fit for the screen.
type UnreachableError struct {
	Reason string
	Err    error
}

func (e *UnreachableError) Error() string { return UnreachableTitle + ": " + e.Reason }

func (e *UnreachableError) Unwrap() error { return e.Err }

// Unreachable restates err as an UnreachableError when it says the request
// never reached the account, and reports false otherwise. The reason is the
// leaf of the net error, since every wrapper above it repeats the address.
func Unreachable(err error) (*UnreachableError, bool) {
	var urlErr *url.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &UnreachableError{Reason: "timed out", Err: err}, true
	case errors.Is(err, context.Canceled):
		return &UnreachableError{Reason: "canceled", Err: err}, true
	case errors.As(err, &urlErr):
		return &UnreachableError{Reason: transportReason(urlErr), Err: err}, true
	}
	return nil, false
}

func transportReason(urlErr *url.Error) string {
	var dnsErr *net.DNSError
	var sysErr *os.SyscallError
	var opErr *net.OpError
	switch {
	case errors.As(urlErr, &dnsErr):
		return dnsErr.Err
	case errors.As(urlErr, &sysErr):
		return sysErr.Err.Error()
	case errors.As(urlErr, &opErr):
		return opErr.Err.Error()
	}
	return urlErr.Err.Error()
}
