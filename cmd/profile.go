package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/query"
)

// noKey is what the profile table shows when no source resolves a key.
const noKey = "none"

func newProfileCmd(keyring config.Keyring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage connection profiles",
		Long: "Profiles are named connections kept in config.toml under $XDG_CONFIG_HOME/alchemist\n" +
			"(~/.config/alchemist by default). The file holds endpoints, never keys: a key lives\n" +
			"in the OS keychain, or in ALCHEMIST_<NAME>_KEY for a machine without one.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(
		newProfileListCmd(keyring),
		newProfileAddCmd(keyring),
		newProfileSetKeyCmd(keyring),
		newProfileRemoveCmd(keyring),
	)
	return cmd
}

func newProfileListCmd(keyring config.Keyring) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List profiles and where each one's key comes from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if len(cfg.Profiles) == 0 {
				return say(cmd, "no profiles in %s; add one with: alchemist profile add <name> --endpoint <url>", store.Path)
			}
			return writeProfileTable(cmd.OutOrStdout(), cfg, config.SecretResolver{Keyring: keyring})
		},
	}
}

// writeProfileTable never prints a key, only the source that resolves it, so
// the output is safe to paste into a bug report.
func writeProfileTable(w io.Writer, cfg config.Config, resolver config.SecretResolver) error {
	var rows strings.Builder
	rows.WriteString("NAME\tADAPTER\tENDPOINT\tKEY\n")
	for _, name := range cfg.Names() {
		profile := cfg.Profiles[name]
		fmt.Fprintf(&rows, "%s\t%s\t%s\t%s\n", profileLabel(cfg, name), profile.Adapter, profile.Endpoint, keySource(resolver, profile))
	}
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := io.WriteString(table, rows.String()); err != nil {
		return fmt.Errorf("cmd: write profiles: %w", err)
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("cmd: write profiles: %w", err)
	}
	return nil
}

func profileLabel(cfg config.Config, name string) string {
	if name == cfg.DefaultProfile {
		return name + " (default)"
	}
	return name
}

func keySource(resolver config.SecretResolver, profile config.Profile) string {
	secret, err := resolver.Resolve(profile)
	if err != nil {
		return noKey
	}
	return secret.Source
}

func newProfileAddCmd(keyring config.Keyring) *cobra.Command {
	var add addFlags
	cmd := &cobra.Command{
		Use:   "add <name> --endpoint <url>",
		Short: "Add a profile and store its key in the keychain",
		Example: "  alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify\n" +
			"  alchemist profile add prod --endpoint https://myaccount.documents.azure.com:443/ --default",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			add.profile.Name = args[0]
			return add.run(cmd, keyring)
		},
	}
	add.bind(cmd.Flags())
	return cmd
}

// addFlags are the inputs of profile add.
type addFlags struct {
	profile      config.Profile
	makeDefault  bool
	sampleFields bool
}

func (a *addFlags) bind(flags *pflag.FlagSet) {
	flags.BoolVar(&a.sampleFields, "sample-fields", true,
		"let autocomplete read a few items of a container for its fields (spends request units)")
	flags.StringVar(&a.profile.Adapter, "adapter", cosmos.Name, "adapter the profile connects with")
	flags.StringVar(&a.profile.Endpoint, "endpoint", "", "account endpoint URL")
	flags.BoolVar(&a.profile.InsecureSkipVerify, "insecure-skip-verify", false,
		"skip TLS verification (for the Cosmos emulator's self-signed certificate)")
	flags.StringVar(&a.profile.Database, "database", "", "database to open in the catalog on start")
	flags.IntVar(&a.profile.PageSize, "page-size", 0, "rows per result page (the adapter's default when 0)")
	flags.IntVar(&a.profile.MaxJoinRows, "max-join-rows", 0,
		fmt.Sprintf("rows a cross-container join may hold in memory (%d when 0)", query.DefaultMaxJoinRows))
	flags.BoolVar(&a.makeDefault, "default", false, "make this the default profile")
}

// run saves the profile before asking for its key, so a keychain that refuses
// the key still leaves a profile the environment variable can serve.
func (a addFlags) run(cmd *cobra.Command, keyring config.Keyring) error {
	if _, err := adapter.Get(a.profile.Adapter); err != nil {
		return err
	}
	if !a.sampleFields {
		a.profile.SampleFields = &a.sampleFields
	}
	store, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg, err = cfg.Add(a.profile)
	if err != nil {
		return err
	}
	if a.makeDefault {
		cfg.DefaultProfile = a.profile.Name
	}
	if err := store.Save(cfg); err != nil {
		return err
	}
	name := a.profile.Name
	key, err := newPrompter(cmd).secret("Key for profile " + name + " (leave empty to skip): ")
	if err != nil {
		return err
	}
	if key == "" {
		return say(cmd, "added profile %s with no key: set $%s, or run alchemist profile set-key %s",
			name, config.EnvKeyVar(name), name)
	}
	if err := keyring.Set(name, key); err != nil {
		return fmt.Errorf("added profile %s, but its key was not stored; set $%s instead: %w",
			name, config.EnvKeyVar(name), err)
	}
	return say(cmd, "added profile %s; key stored in the keychain", name)
}

func newProfileSetKeyCmd(keyring config.Keyring) *cobra.Command {
	return &cobra.Command{
		Use:   "set-key <name>",
		Short: "Store a profile's key in the keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setKey(cmd, keyring, args[0])
		},
	}
}

func setKey(cmd *cobra.Command, keyring config.Keyring, name string) error {
	_, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := cfg.Profile(name); err != nil {
		return err
	}
	key, err := newPrompter(cmd).secret("Key for profile " + name + ": ")
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("cmd: no key entered")
	}
	if err := keyring.Set(name, key); err != nil {
		return err
	}
	return say(cmd, "key for profile %s stored in the keychain", name)
}

func newProfileRemoveCmd(keyring config.Keyring) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a profile and its keychain entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeProfile(cmd, keyring, args[0])
		},
	}
}

func removeProfile(cmd *cobra.Command, keyring config.Keyring, name string) error {
	store, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg, err = cfg.Remove(name)
	if err != nil {
		return err
	}
	if err := store.Save(cfg); err != nil {
		return err
	}
	if err := keyring.Delete(name); err != nil && !errors.Is(err, config.ErrSecretNotFound) {
		return fmt.Errorf("removed profile %s, but not its keychain entry: %w", name, err)
	}
	return say(cmd, "removed profile %s", name)
}

// loadConfig opens the config file at its default location.
func loadConfig() (config.Store, config.Config, error) {
	store, err := config.DefaultStore()
	if err != nil {
		return config.Store{}, config.Config{}, err
	}
	cfg, err := store.Load()
	if err != nil {
		return config.Store{}, config.Config{}, err
	}
	return store, cfg, nil
}

// say writes one line for the person running the command.
func say(cmd *cobra.Command, format string, args ...any) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), format+"\n", args...); err != nil {
		return fmt.Errorf("cmd: write output: %w", err)
	}
	return nil
}
