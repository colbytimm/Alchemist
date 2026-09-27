package emulator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/emulator"
)

func TestProfileUsesTheWellKnownKey(t *testing.T) {
	assert.Equal(t, config.Profile{
		Name:         "emulator",
		Adapter:      "cosmos",
		Endpoint:     "http://localhost:9081",
		WellKnownKey: true,
	}, emulator.Profile(9081))
}

func TestEnsureProfileAddsTheFirstProfileAsTheDefault(t *testing.T) {
	cfg, outcome, err := emulator.EnsureProfile(config.Config{}, 8081)

	require.NoError(t, err)
	assert.Equal(t, emulator.ProfileAdded, outcome)
	assert.Equal(t, "emulator", cfg.DefaultProfile)
	assert.Equal(t, emulator.Profile(8081), cfg.Profiles["emulator"])
}

func TestEnsureProfileMovesItsOwnProfileToThePort(t *testing.T) {
	ours, err := config.Config{}.Add(emulator.Profile(8081))
	require.NoError(t, err)

	cfg, outcome, err := emulator.EnsureProfile(ours, 9081)

	require.NoError(t, err)
	assert.Equal(t, emulator.ProfileMoved, outcome)
	assert.Equal(t, "http://localhost:9081", cfg.Profiles["emulator"].Endpoint)
}

func TestEnsureProfileLeavesItsOwnProfileOnTheSamePort(t *testing.T) {
	ours, err := config.Config{}.Add(emulator.Profile(8081))
	require.NoError(t, err)

	cfg, outcome, err := emulator.EnsureProfile(ours, 8081)

	require.NoError(t, err)
	assert.Equal(t, emulator.ProfileUnchanged, outcome)
	assert.Equal(t, ours, cfg)
}

func TestEnsureProfileLeavesTheUsersProfileAlone(t *testing.T) {
	users, err := config.Config{}.Add(config.Profile{
		Name: "emulator", Adapter: "cosmos", Endpoint: "https://localhost:8081", InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	cfg, outcome, err := emulator.EnsureProfile(users, 9081)

	require.NoError(t, err)
	assert.Equal(t, emulator.ProfileNotOurs, outcome)
	assert.Equal(t, users, cfg)
}
