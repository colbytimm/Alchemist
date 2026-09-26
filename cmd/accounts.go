package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// Profiles serves the TUI its accounts, reading config.toml afresh on every call.
type Profiles struct {
	Store   config.Store
	Keyring config.Keyring
}

// Accounts lists every profile, sorted by name.
func (p Profiles) Accounts() ([]tui.Account, error) {
	cfg, err := p.Store.Load()
	if err != nil {
		return nil, err
	}
	return accounts(cfg), nil
}

func accounts(cfg config.Config) []tui.Account {
	accounts := make([]tui.Account, 0, len(cfg.Profiles))
	for _, name := range cfg.Names() {
		accounts = append(accounts, account(cfg.Profiles[name]))
	}
	return accounts
}

func account(profile config.Profile) tui.Account {
	return tui.Account{
		Name:         profile.Name,
		Endpoint:     profile.Endpoint,
		SkipVerify:   profile.InsecureSkipVerify,
		Database:     profile.Database,
		MaxJoinRows:  profile.MaxJoinRows,
		Writers:      profile.Writers,
		SampleFields: profile.SamplesFields(),
		ReadOnly:     profile.IsReadOnly(),
	}
}

// Open connects the profile called name, or asks for its key with tui.ErrCredentialsNeeded.
func (p Profiles) Open(ctx context.Context, name string) (adapter.Connection, error) {
	cfg, err := p.Store.Load()
	if err != nil {
		return nil, err
	}
	profile, err := cfg.Profile(name)
	if err != nil {
		return nil, err
	}
	secret, err := config.SecretResolver{Keyring: p.Keyring}.Resolve(profile)
	if errors.Is(err, config.ErrSecretNotFound) {
		return nil, fmt.Errorf("cmd: profile %q: %w", profile.Name, tui.ErrCredentialsNeeded)
	}
	if err != nil {
		return nil, err
	}
	return connect(ctx, profile.Adapter, profile.Settings(secret))
}

// Connect connects, pings, and only then saves what the connect form
// submitted. The account it reports is the profile as saved, so the session
// holds what the file says rather than what the form implied.
func (p Profiles) Connect(ctx context.Context, form panes.ConnectForm) (tui.Account, adapter.Connection, error) {
	saved, err := p.profileFor(form)
	if err != nil {
		return tui.Account{}, nil, err
	}
	conn, err := connect(ctx, saved.Adapter, saved.Settings(config.Secret{Key: form.Key}))
	if err != nil {
		return tui.Account{}, nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		closeFailed(conn)
		return tui.Account{}, nil, err
	}
	if err := p.save(saved, form); err != nil {
		closeFailed(conn)
		return tui.Account{}, nil, err
	}
	return account(saved), conn, nil
}

// saving serializes the read-modify-write of config.toml: two connect forms
// may finish at once, and neither may drop the other's profile.
var saving sync.Mutex

func (p Profiles) save(profile config.Profile, form panes.ConnectForm) error {
	saving.Lock()
	defer saving.Unlock()
	var key string
	if form.StoreKey {
		key = form.Key
	}
	return config.SaveProfile(p.Store, p.Keyring, profile, key)
}

// profileFor is the profile a submitted form creates or completes. It never
// overwrites a profile that works: a name whose key resolves is refused, one
// whose key is nowhere completes that profile, and any other adds one.
func (p Profiles) profileFor(form panes.ConnectForm) (config.Profile, error) {
	cfg, err := p.Store.Load()
	if err != nil {
		return config.Profile{}, err
	}
	profile, exists := cfg.Profiles[form.Profile]
	if !exists {
		profile = config.Profile{
			Name:               form.Profile,
			Adapter:            cosmos.Name,
			Endpoint:           form.Endpoint,
			InsecureSkipVerify: form.SkipVerify,
		}
		// A name the config would refuse is refused before any connecting.
		_, err := cfg.Put(profile)
		return profile, err
	}
	if err := p.checkKeyless(profile); err != nil {
		return config.Profile{}, err
	}
	if profile.Endpoint != form.Endpoint {
		// Allowing writes was decided for the old endpoint, not this one.
		profile.ReadOnly = nil
	}
	profile.Endpoint, profile.InsecureSkipVerify = form.Endpoint, form.SkipVerify
	return profile, nil
}

// checkKeyless refuses a profile whose key resolves. One whose keychain cannot
// be read is completed: SaveProfile files the key first, so a keychain that
// still cannot take it leaves the profile as it was.
func (p Profiles) checkKeyless(profile config.Profile) error {
	if _, err := (config.SecretResolver{Keyring: p.Keyring}).Resolve(profile); err == nil {
		return fmt.Errorf("cmd: profile %q: %w", profile.Name, config.ErrProfileExists)
	}
	return nil
}
