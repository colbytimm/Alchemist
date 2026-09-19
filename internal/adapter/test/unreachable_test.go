package adapter_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// The account every fixture names, which no reason may carry.
const (
	host    = "example.documents.azure.com"
	address = "https://" + host + ":443"
)

func requestFailed(leaf error) error {
	return &url.Error{Op: "Get", URL: address, Err: leaf}
}

func dialFailed(leaf error) error {
	return requestFailed(&net.OpError{Op: "dial", Net: "tcp", Err: leaf})
}

func TestUnreachableNamesTheReasonWithoutTheAddress(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "connection refused",
			err:  dialFailed(&os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}),
			want: "connection refused",
		},
		{
			name: "no such host",
			err:  dialFailed(&net.DNSError{Err: "no such host", Name: host, IsNotFound: true}),
			want: "no such host",
		},
		{
			name: "dial timed out",
			err:  dialFailed(os.ErrDeadlineExceeded),
			want: "i/o timeout",
		},
		{
			name: "certificate not trusted",
			err:  requestFailed(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
			want: "tls: failed to verify certificate: x509: certificate signed by unknown authority",
		},
		{
			name: "certificate for another host",
			err:  requestFailed(&tls.CertificateVerificationError{Err: x509.HostnameError{Host: host}}),
			want: "certificate is not for this host",
		},
		{
			name: "wrapped by the SDK",
			err:  fmt.Errorf("failed to retrieve account properties: %w", dialFailed(syscall.ECONNRESET)),
			want: "connection reset by peer",
		},
		{
			name: "deadline passed mid-retry, transport error printed beside it",
			// %v is the point: the SDK prints the transport error as text.
			err:  fmt.Errorf("%w: underlying transport error: %v", context.DeadlineExceeded, dialFailed(syscall.ECONNREFUSED)), //nolint:errorlint
			want: "timed out",
		},
		{
			name: "request canceled",
			err:  requestFailed(context.Canceled),
			want: "canceled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unreachable, ok := adapter.Unreachable(tt.err)

			require.True(t, ok)
			assert.Equal(t, tt.want, unreachable.Reason)
			assert.Equal(t, "account unreachable: "+tt.want, unreachable.Error())
			assert.NotContains(t, unreachable.Error(), host)
			assert.ErrorIs(t, unreachable, tt.err, "the original stays in the chain")
		})
	}
}

func TestUnreachableLeavesOtherErrorsAlone(t *testing.T) {
	_, ok := adapter.Unreachable(errors.New("401 Unauthorized"))

	assert.False(t, ok)
}
