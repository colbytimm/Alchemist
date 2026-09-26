package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui/panes"
	"github.com/colbytimm/alchemist/internal/writers"
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
		newProfileSetReadOnlyCmd(),
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
	if err := writeTable(w, rows.String()); err != nil {
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
	readOnly     bool
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
		fmt.Sprintf("rows a cross-container join may hold in memory, across all the sides held (%d when 0)", query.DefaultMaxJoinRows))
	flags.IntVar(&a.profile.Writers, "writers", 0,
		fmt.Sprintf("item writes a clone into this account keeps in flight, from 1 to %d (%d when 0)", writers.MaxSize, writers.DefaultSize))
	flags.IntVar(&a.profile.MaxMutationItems, "max-mutation-items", 0,
		fmt.Sprintf("items one update may select before it is refused (%d when 0)", mutate.DefaultMaxTargets))
	flags.StringVar(&a.profile.Diagnostics, "diagnostics", "",
		"underline what the editor flags: curly, underline, or off (curly when empty)")
	flags.BoolVar(&a.makeDefault, "default", false, "make this the default profile")
	flags.BoolVar(&a.readOnly, "read-only", false,
		"refuse every write on this account (unset: read-only unless the endpoint is this machine)")
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
	if cmd.Flags().Changed("read-only") {
		a.profile.ReadOnly = &a.readOnly
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

func newProfileSetReadOnlyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-read-only <name> <true|false>",
		Short: "Allow or refuse writes on a profile's account",
		Long: "A profile with no read_only setting is read-only unless its endpoint is this machine,\n" +
			"as the emulator's is. Writing to any other account is a decision made here, per profile.",
		Example: "  alchemist profile set-read-only emulator false",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			readOnly, err := strconv.ParseBool(args[1])
			if err != nil {
				return fmt.Errorf("cmd: read-only %q: give true or false", args[1])
			}
			if err := setReadOnly(args[0], readOnly); err != nil {
				return err
			}
			if readOnly {
				return say(cmd, "profile %s is read-only", args[0])
			}
			return say(cmd, "profile %s allows writes", args[0])
		},
	}
}

func setReadOnly(name string, readOnly bool) error {
	store, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, err := cfg.Profile(name)
	if err != nil {
		return err
	}
	profile.ReadOnly = &readOnly
	if cfg, err = cfg.Put(profile); err != nil {
		return err
	}
	return store.Save(cfg)
}

func newProfileRemoveCmd(keyring config.Keyring) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a profile and its keychain entry, keeping its saved queries and snapshots",
		Long: "remove keeps the profile's saved queries and snapshots, and says where: a profile added\n" +
			"again under the same name picks them back up. --purge deletes them too, since someone\n" +
			"purging an account expects copies of its data to go with it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			snapshots, err := snapshotRoot(cmd)
			if err != nil {
				return err
			}
			if err := removeProfile(keyring, name); err != nil {
				return err
			}
			queries, err := savedQueries()
			if err != nil {
				return err
			}
			if purge {
				return purgeAccountData(cmd, queries, snapshots, name)
			}
			return reportKeptData(cmd, queries, snapshots, name)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "delete the profile's saved queries and snapshots too")
	return cmd
}

func removeProfile(keyring config.Keyring, name string) error {
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
	return nil
}

func savedQueries() (saved.Dir, error) {
	dir, err := config.Dir()
	if err != nil {
		return saved.Dir{}, err
	}
	return saved.Open(filepath.Join(dir, saved.DirName)), nil
}

// reportKeptData says where the profile's saved queries and snapshots were
// left, since a profile added again under the same name picks them back up.
func reportKeptData(cmd *cobra.Command, queries saved.Dir, snapshots, name string) error {
	listing, err := queries.List(name)
	if err != nil {
		return fmt.Errorf("removed profile %s, but could not look for its saved queries: %w", name, err)
	}
	usage, err := snapshot.AccountUsage(snapshots, name)
	if err != nil {
		return fmt.Errorf("removed profile %s, but could not look for its snapshots: %w", name, err)
	}
	lines := []string{"removed profile " + name}
	if listing.Files() > 0 {
		lines = append(lines, fmt.Sprintf("kept %s in %s", countQueries(listing.Files()), queries.AccountPath(name)))
	}
	if usage.Snapshots > 0 {
		lines = append(lines, fmt.Sprintf("kept %s (%s) in %s", countSnapshots(usage.Snapshots), panes.FormatBytes(usage.OnDisk()),
			snapshot.Location{Root: snapshots, Account: name}.AccountDir()))
	}
	if len(lines) > 1 {
		lines = append(lines, "remove them too with: alchemist profile remove "+name+" --purge")
	}
	return say(cmd, "%s", strings.Join(lines, "\n"))
}

func purgeAccountData(cmd *cobra.Command, queries saved.Dir, snapshots, name string) error {
	removed, err := queries.RemoveAccount(name)
	if err != nil {
		return fmt.Errorf("removed profile %s, but not its saved queries: %w", name, err)
	}
	usage, err := snapshot.AccountUsage(snapshots, name)
	if err != nil {
		return fmt.Errorf("removed profile %s, but could not look for its snapshots: %w", name, err)
	}
	if err := snapshot.RemoveAccount(snapshots, name); err != nil {
		return fmt.Errorf("removed profile %s, but not its snapshots: %w", name, err)
	}
	var purged []string
	if removed > 0 {
		purged = append(purged, countQueries(removed))
	}
	if usage.Snapshots > 0 {
		purged = append(purged, countSnapshots(usage.Snapshots))
	}
	if len(purged) == 0 {
		return say(cmd, "removed profile %s", name)
	}
	return say(cmd, "removed profile %s and its %s", name, strings.Join(purged, " and "))
}

func countSnapshots(n int) string {
	if n == 1 {
		return "1 snapshot"
	}
	return fmt.Sprintf("%d snapshots", n)
}

func countQueries(n int) string {
	if n == 1 {
		return "1 saved query"
	}
	return fmt.Sprintf("%d saved queries", n)
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
