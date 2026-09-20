// Package config holds the named connection profiles Alchemist launches
// with. Profiles live in a TOML file; their secrets never do — see
// docs/plan/06-config-profiles.md.
package config

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
)

// Errors matchable with errors.Is.
var (
	ErrInvalidConfig    = errors.New("invalid config")
	ErrNoProfiles       = errors.New("no profiles defined")
	ErrNoDefaultProfile = errors.New("no default profile: pass a profile name or set default_profile")
	ErrProfileNotFound  = errors.New("profile not found")
	ErrProfileExists    = errors.New("profile already exists")
)

// profileNamePattern keeps a name usable as a keychain account and, once
// upper-cased, as part of an environment variable name.
var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Config is the whole config file.
type Config struct {
	DefaultProfile string             `toml:"default_profile,omitempty"`
	Profiles       map[string]Profile `toml:"profiles,omitempty"`
}

// Profile is one named connection. Name is the table key in the file, not a
// field of it.
type Profile struct {
	Name               string `toml:"-"`
	Adapter            string `toml:"adapter"`
	Endpoint           string `toml:"endpoint"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify,omitempty"`
	Database           string `toml:"database,omitempty"`
	PageSize           int    `toml:"page_size,omitzero"`
	// MaxJoinRows caps the side of a cross-container join held in memory.
	MaxJoinRows int `toml:"max_join_rows,omitzero"`
}

// Profile returns the profile called name, or the default profile when name
// is empty.
func (c Config) Profile(name string) (Profile, error) {
	if len(c.Profiles) == 0 {
		return Profile{}, fmt.Errorf("config: %w", ErrNoProfiles)
	}
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		return Profile{}, fmt.Errorf("config: %w", ErrNoDefaultProfile)
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("config: profile %q: %w", name, ErrProfileNotFound)
	}
	return profile, nil
}

// Names lists the profiles in a stable order.
func (c Config) Names() []string {
	return slices.Sorted(maps.Keys(c.Profiles))
}

// Add adds p, which must not exist yet. The first profile becomes the default.
func (c Config) Add(p Profile) (Config, error) {
	if _, exists := c.Profiles[p.Name]; exists {
		return Config{}, fmt.Errorf("config: profile %q: %w", p.Name, ErrProfileExists)
	}
	return c.Put(p)
}

// Put adds p, or replaces the profile of that name. The first profile becomes
// the default.
func (c Config) Put(p Profile) (Config, error) {
	if err := p.validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	c = c.clone()
	c.Profiles[p.Name] = p
	if c.DefaultProfile == "" {
		c.DefaultProfile = p.Name
	}
	return c, nil
}

// Remove drops the profile called name, and clears the default when it
// pointed there.
func (c Config) Remove(name string) (Config, error) {
	if _, ok := c.Profiles[name]; !ok {
		return Config{}, fmt.Errorf("config: profile %q: %w", name, ErrProfileNotFound)
	}
	c = c.clone()
	delete(c.Profiles, name)
	if c.DefaultProfile == name {
		c.DefaultProfile = ""
	}
	return c, nil
}

// clone detaches the profile map, so a Config derived from another never
// edits it in place.
func (c Config) clone() Config {
	c.Profiles = maps.Clone(c.Profiles)
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c
}

// named stamps every profile with its table key.
func (c Config) named() Config {
	c = c.clone()
	for name, profile := range c.Profiles {
		profile.Name = name
		c.Profiles[name] = profile
	}
	return c
}

func (c Config) validate() error {
	for _, name := range c.Names() {
		if err := c.Profiles[name].validate(); err != nil {
			return err
		}
	}
	if c.DefaultProfile == "" {
		return nil
	}
	if _, ok := c.Profiles[c.DefaultProfile]; !ok {
		return fmt.Errorf("default_profile %q: %w", c.DefaultProfile, ErrProfileNotFound)
	}
	return nil
}

func (p Profile) validate() error {
	switch {
	case !profileNamePattern.MatchString(p.Name):
		return fmt.Errorf("profile name %q: use letters, digits, - and _: %w", p.Name, ErrInvalidConfig)
	case p.Adapter == "":
		return fmt.Errorf("profile %q: adapter is required: %w", p.Name, ErrInvalidConfig)
	case p.Endpoint == "":
		return fmt.Errorf("profile %q: endpoint is required: %w", p.Name, ErrInvalidConfig)
	case p.PageSize < 0:
		return fmt.Errorf("profile %q: page_size must be positive: %w", p.Name, ErrInvalidConfig)
	case p.MaxJoinRows < 0:
		return fmt.Errorf("profile %q: max_join_rows must be positive: %w", p.Name, ErrInvalidConfig)
	}
	return nil
}

// Settings is the adapter settings map for this profile with secret filled
// in. page_size is left to the adapter's default when the profile sets none.
func (p Profile) Settings(secret Secret) map[string]string {
	settings := map[string]string{
		"endpoint":             p.Endpoint,
		"key":                  secret.Key,
		"connection_string":    secret.ConnectionString,
		"insecure_skip_verify": strconv.FormatBool(p.InsecureSkipVerify),
	}
	if p.PageSize > 0 {
		settings["page_size"] = strconv.Itoa(p.PageSize)
	}
	return settings
}
