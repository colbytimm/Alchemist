package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// KeychainService is the service every profile's key is filed under in the
// OS keychain; the account is the profile name.
const KeychainService = "alchemist"

// EnvConnectionStringVar is the last resort for ad-hoc use: a whole Cosmos
// connection string, honored for any profile.
const EnvConnectionStringVar = "COSMOS_CONNECTION_STRING"

// SourceKeychain is the Secret.Source of a key read from the OS keychain; one
// read from the environment is named by its variable instead.
const SourceKeychain = "keychain"

var ErrSecretNotFound = errors.New("secret not found")

// Keyring stores one secret per profile. Get and Delete return
// ErrSecretNotFound for a profile with no entry.
type Keyring interface {
	Get(profile string) (string, error)
	Set(profile, secret string) error
	Delete(profile string) error
}

// SystemKeyring is the OS keychain: Keychain on macOS, Secret Service on
// Linux, Credential Manager on Windows.
func SystemKeyring() Keyring { return systemKeyring{} }

type systemKeyring struct{}

func (systemKeyring) Get(profile string) (string, error) {
	secret, err := keyring.Get(KeychainService, profile)
	if err != nil {
		return "", keychainError("read", profile, err)
	}
	return secret, nil
}

func (systemKeyring) Set(profile, secret string) error {
	if err := keyring.Set(KeychainService, profile, secret); err != nil {
		return keychainError("store", profile, err)
	}
	return nil
}

func (systemKeyring) Delete(profile string) error {
	if err := keyring.Delete(KeychainService, profile); err != nil {
		return keychainError("delete", profile, err)
	}
	return nil
}

func keychainError(op, profile string, err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		err = ErrSecretNotFound
	}
	return fmt.Errorf("config: keychain %s %q: %w", op, profile, err)
}

// Secret is a resolved credential. Exactly one of Key and ConnectionString
// is set. Source says where it came from and is safe to print.
type Secret struct {
	Key              string
	ConnectionString string
	Source           string
}

// EnvKeyVar names the environment variable holding the key for profile:
// ALCHEMIST_<NAME>_KEY, upper-cased, with dashes as underscores.
func EnvKeyVar(profile string) string {
	return "ALCHEMIST_" + strings.ToUpper(strings.ReplaceAll(profile, "-", "_")) + "_KEY"
}

// SecretResolver finds a profile's secret: the keychain first, then the
// profile's own environment variable, then EnvConnectionStringVar.
type SecretResolver struct {
	Keyring Keyring
}

// Resolve returns the first secret found. A keychain that cannot be reached
// is skipped rather than fatal, and is named in the error when nothing else
// is found either.
func (r SecretResolver) Resolve(profile string) (Secret, error) {
	key, keychainErr := r.Keyring.Get(profile)
	if keychainErr == nil {
		return Secret{Key: key, Source: SourceKeychain}, nil
	}
	if key := os.Getenv(EnvKeyVar(profile)); key != "" {
		return Secret{Key: key, Source: "$" + EnvKeyVar(profile)}, nil
	}
	if cs := os.Getenv(EnvConnectionStringVar); cs != "" {
		return Secret{ConnectionString: cs, Source: "$" + EnvConnectionStringVar}, nil
	}
	notFound := fmt.Errorf("config: profile %q: no secret in the keychain, $%s or $%s: %w",
		profile, EnvKeyVar(profile), EnvConnectionStringVar, ErrSecretNotFound)
	if errors.Is(keychainErr, ErrSecretNotFound) {
		return Secret{}, notFound
	}
	return Secret{}, errors.Join(notFound, keychainErr)
}
