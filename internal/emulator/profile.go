package emulator

import (
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
)

// ProfileOutcome is what EnsureProfile did.
type ProfileOutcome int

const (
	ProfileAdded ProfileOutcome = iota
	ProfileMoved
	ProfileUnchanged
	// ProfileNotOurs is a profile called ProfileName that start did not
	// write, left as the user made it.
	ProfileNotOurs
)

// Profile is the profile start writes for a container published on port. It
// needs no key prompt and no keychain, and allows writes because its
// endpoint is local.
func Profile(port int) config.Profile {
	return config.Profile{
		Name:         ProfileName,
		Adapter:      cosmos.Name,
		Endpoint:     Endpoint(port),
		WellKnownKey: true,
	}
}

// EnsureProfile adds Profile(port) to cfg, or moves start's own profile to
// port. A profile of that name the user made is left alone.
func EnsureProfile(cfg config.Config, port int) (config.Config, ProfileOutcome, error) {
	existing, ok := cfg.Profiles[ProfileName]
	if !ok {
		cfg, err := cfg.Put(Profile(port))
		return cfg, ProfileAdded, err
	}
	if !existing.WellKnownKey || !config.IsLocalEndpoint(existing.Endpoint) {
		return cfg, ProfileNotOurs, nil
	}
	if existing.Endpoint == Endpoint(port) {
		return cfg, ProfileUnchanged, nil
	}
	existing.Endpoint = Endpoint(port)
	cfg, err := cfg.Put(existing)
	return cfg, ProfileMoved, err
}
