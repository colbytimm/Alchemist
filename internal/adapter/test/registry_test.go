package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// fakeAdapter is a minimal Adapter implementation for registry tests.
type fakeAdapter struct{ name string }

func (f fakeAdapter) Name() string { return f.name }

func (f fakeAdapter) Connect(context.Context, map[string]string) (adapter.Connection, error) {
	return nil, errors.New("fake: not implemented")
}

func TestRegisterAndGet(t *testing.T) {
	require.NoError(t, adapter.Register("test-fake", func() adapter.Adapter {
		return fakeAdapter{name: "test-fake"}
	}))

	factory, err := adapter.Get("test-fake")
	require.NoError(t, err)
	require.Equal(t, "test-fake", factory().Name())
}

func TestGetUnknown(t *testing.T) {
	_, err := adapter.Get("no-such-adapter")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown adapter")
}

func TestRegisterDuplicateRejected(t *testing.T) {
	factory := func() adapter.Adapter { return fakeAdapter{name: "test-dup"} }
	require.NoError(t, adapter.Register("test-dup", factory))
	err := adapter.Register("test-dup", factory)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already registered")
}

func TestRegisterInvalidArgs(t *testing.T) {
	require.Error(t, adapter.Register("", func() adapter.Adapter { return fakeAdapter{} }))
	require.Error(t, adapter.Register("test-nil-factory", nil))
}
