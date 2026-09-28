//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/emulator"
	"github.com/colbytimm/alchemist/internal/sample"
)

// These tests share the emulator the rest of the suite uses, so none of them
// stops or removes it.

// emulatorProfileConfig writes the profile alchemist emulator start writes,
// for the emulator the suite runs against, into a private config.
func emulatorProfileConfig(t *testing.T) {
	t.Helper()
	if err := cmd.RegisterAdapters(); err != nil && !errors.Is(err, adapter.ErrDuplicateName) {
		require.NoError(t, err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	profile := emulator.Profile(emulator.DefaultPort)
	profile.Endpoint = settings()["endpoint"]
	cfg, err := config.Config{}.Add(profile)
	require.NoError(t, err)
	store, err := config.DefaultStore()
	require.NoError(t, err)
	require.NoError(t, store.Save(cfg))
}

func runAlchemist(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cmd.NewRootCmd(emptyKeyring{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestIntegrationEmulatorStatusReportsReady(t *testing.T) {
	emulatorProfileConfig(t)

	out, err := runAlchemist(t, "emulator", "status")

	require.NoError(t, err)
	assert.Contains(t, out, settings()["endpoint"]+": ready")
	assert.Contains(t, out, "key: "+config.SourceWellKnown)
}

func TestIntegrationEmulatorStartOnTheRunningContainerChangesNothing(t *testing.T) {
	if os.Getenv("COSMOS_ENDPOINT") != "" {
		t.Skip("COSMOS_ENDPOINT names an emulator alchemist emulator start did not run")
	}
	emulatorProfileConfig(t)
	store, err := config.DefaultStore()
	require.NoError(t, err)
	before, err := store.Load()
	require.NoError(t, err)
	started := time.Now()

	out, err := runAlchemist(t, "emulator", "start")

	require.NoError(t, err)
	assert.Less(t, time.Since(started), 15*time.Second)
	assert.Contains(t, out, "ready")
	after, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestIntegrationSampleSeedsTheEmulator(t *testing.T) {
	conn, err := cosmos.Adapter{}.Connect(context.Background(), settings())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	waitForEmulator(t, conn)
	admin, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok)
	writer, ok := conn.(adapter.ItemWriter)
	require.True(t, ok)
	target := sample.Target{Catalog: conn.Catalog(), Admin: admin, Writer: writer}

	require.NoError(t, sample.Replace(context.Background(), target, func(sample.Seeded) {}))

	client := seedClient(t)
	assert.Equal(t, 60, countItems(t, client, "sales", "orders"))
	assert.Equal(t, 300, countItems(t, client, "telemetry", "events"))
}
