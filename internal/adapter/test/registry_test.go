package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

type fakeAdapter struct{ name string }

func (f fakeAdapter) Name() string { return f.name }

func (f fakeAdapter) Connect(context.Context, map[string]string) (adapter.Connection, error) {
	return nil, errors.New("fake: not implemented")
}

func fakeFactory(name string) adapter.Factory {
	return func() adapter.Adapter { return fakeAdapter{name: name} }
}

// seedRegistry registers name as setup. The registry is package-global, so
// under -count>1 the entry is already there.
func seedRegistry(t *testing.T, name string) {
	t.Helper()
	if err := adapter.Register(name, fakeFactory(name)); err != nil {
		require.ErrorIs(t, err, adapter.ErrDuplicateName)
	}
}

func TestRegisterAndGet(t *testing.T) {
	seedRegistry(t, "test-fake")

	factory, err := adapter.Get("test-fake")
	require.NoError(t, err)
	require.Equal(t, "test-fake", factory().Name())
}

func TestGetUnknown(t *testing.T) {
	_, err := adapter.Get("no-such-adapter")
	require.ErrorIs(t, err, adapter.ErrUnknownAdapter)
}

func TestRegisterDuplicateRejected(t *testing.T) {
	seedRegistry(t, "test-dup")
	require.ErrorIs(t, adapter.Register("test-dup", fakeFactory("test-dup")), adapter.ErrDuplicateName)
}

func TestRegisterInvalidArgs(t *testing.T) {
	require.Error(t, adapter.Register("", func() adapter.Adapter { return fakeAdapter{} }))
	require.Error(t, adapter.Register("test-nil-factory", nil))
}
