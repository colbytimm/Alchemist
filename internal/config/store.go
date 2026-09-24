package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the config file inside the config directory.
const FileName = "config.toml"

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Store reads and writes the config file at Path.
type Store struct {
	Path string
}

// DefaultStore locates the config file: $XDG_CONFIG_HOME/alchemist when set,
// otherwise ~/.config/alchemist.
func DefaultStore() (Store, error) {
	dir, err := configDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Path: filepath.Join(dir, FileName)}, nil
}

func configDir() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "alchemist"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "alchemist"), nil
}

// Load parses the file. A file that does not exist yet is an empty config.
func (s Store) Load() (Config, error) {
	var cfg Config
	meta, err := toml.DecodeFile(s.Path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", s.Path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		return Config{}, fmt.Errorf("config: %s: unknown key %q: %w", s.Path, unknown[0].String(), ErrInvalidConfig)
	}
	cfg = cfg.named()
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", s.Path, err)
	}
	return cfg, nil
}

// SaveProfile puts p into the store and, when key is not empty, files it in
// the keyring. The profile is checked before the key goes anywhere, and the
// key is filed before the profile, so a refusal from any step leaves nothing
// behind.
func SaveProfile(store Store, keyring Keyring, p Profile, key string) error {
	cfg, err := store.Load()
	if err != nil {
		return err
	}
	cfg, err = cfg.Put(p)
	if err != nil {
		return err
	}
	if key == "" {
		return store.Save(cfg)
	}
	if err := keyring.Set(p.Name, key); err != nil {
		return err
	}
	if err := store.Save(cfg); err != nil {
		return errors.Join(err, keyring.Delete(p.Name))
	}
	return nil
}

// Save writes cfg, creating the directory on first use. Comments in an
// existing file are not preserved.
func (s Store) Save(cfg Config) error {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}
	if err := os.WriteFile(s.Path, buf.Bytes(), fileMode); err != nil {
		return fmt.Errorf("config: write %s: %w", s.Path, err)
	}
	return nil
}
