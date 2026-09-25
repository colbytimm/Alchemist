package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// KeychainService is the service every profile's key is filed under in the
// OS keychain; the account is the profile name.
const KeychainService = "alchemist"

// EnvConnectionStringVar is the last resort for ad-hoc use: a whole Cosmos
// connection string, honored only for a profile on the account it names.
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

// Resolve returns the first secret found for profile. A keychain that cannot
// be reached is skipped rather than fatal, and is named in the error when
// nothing else is found either.
func (r SecretResolver) Resolve(profile Profile) (Secret, error) {
	key, keychainErr := r.Keyring.Get(profile.Name)
	if keychainErr == nil {
		return Secret{Key: key, Source: SourceKeychain}, nil
	}
	if key := os.Getenv(EnvKeyVar(profile.Name)); key != "" {
		return Secret{Key: key, Source: "$" + EnvKeyVar(profile.Name)}, nil
	}
	cs := os.Getenv(EnvConnectionStringVar)
	if cs != "" && namesAccount(cs, profile.Endpoint) {
		return Secret{ConnectionString: cs, Source: "$" + EnvConnectionStringVar}, nil
	}
	notFound := fmt.Errorf("config: profile %q: no secret in the keychain, $%s or $%s%s: %w",
		profile.Name, EnvKeyVar(profile.Name), EnvConnectionStringVar, otherAccountNote(cs), ErrSecretNotFound)
	if errors.Is(keychainErr, ErrSecretNotFound) {
		return Secret{}, notFound
	}
	return Secret{}, errors.Join(notFound, keychainErr)
}

// otherAccountNote explains a connection string that was set but not used.
func otherAccountNote(connectionString string) string {
	if connectionString == "" {
		return ""
	}
	return " (set, but not for this account)"
}

// namesAccount reports whether connectionString's AccountEndpoint is the
// account at endpoint. A connection string carries its own endpoint, so one
// for another account would connect a profile somewhere other than it says.
func namesAccount(connectionString, endpoint string) bool {
	for _, field := range strings.Split(connectionString, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if ok && strings.EqualFold(name, "AccountEndpoint") {
			return sameAccount(value, endpoint)
		}
	}
	return false
}

func sameAccount(a, b string) bool {
	left, errLeft := url.Parse(strings.TrimSpace(a))
	right, errRight := url.Parse(strings.TrimSpace(b))
	if errLeft != nil || errRight != nil || left.Hostname() == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		portOf(left) == portOf(right)
}

// portOf is u's port, or the one its scheme implies.
func portOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}
