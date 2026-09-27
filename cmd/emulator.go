package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/emulator"
	"github.com/colbytimm/alchemist/internal/sample"
)

const emulatorLong = "Runs the Azure Cosmos DB emulator in a Docker or Podman container, and adds the\n" +
	"emulator profile, which connects with the emulator's published key. alchemist emulator\n" +
	"with no subcommand opens the app on that profile, as alchemist emulator always has.\n\n" +
	"The container listens on 127.0.0.1 only, with telemetry off. Its data lives in the\n" +
	"volume " + emulator.VolumeName + ", which only remove --data deletes."

var errNoEmulatorProfile = errors.New("no emulator profile: run alchemist emulator start")

// newEmulatorCmd is a group whose own RunE launches the emulator profile: a
// subcommand named emulator shadows a profile of that name, which the docs
// have long told users to create and open with alchemist emulator.
func newEmulatorCmd(keyring config.Keyring) *cobra.Command {
	var session sessionFlags
	cmd := &cobra.Command{
		Use:   "emulator",
		Short: "Run the local Cosmos DB emulator, or open the app on it",
		Long:  emulatorLong,
		Example: "  alchemist emulator start   # pull, run, wait until it answers, add the emulator profile\n" +
			"  alchemist emulator seed    # load the sample sales, telemetry and hr databases\n" +
			"  alchemist emulator         # open the app on it",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[emulator.ProfileName]; !ok {
				return errNoEmulatorProfile
			}
			return session.run(cmd, []string{emulator.ProfileName}, keyring)
		},
	}
	session.bind(cmd.Flags())
	cmd.AddCommand(newEmulatorSeedCmd(keyring))
	return cmd
}

func newEmulatorSeedCmd(keyring config.Keyring) *cobra.Command {
	var (
		profile string
		replace bool
	)
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Create the sample sales, telemetry and hr databases",
		Long: "seed connects the profile and creates the sample databases. It refuses an account that\n" +
			"already holds any of them unless --replace is given, and any endpoint not on this machine.",
		Example: "  alchemist emulator seed\n" +
			"  alchemist emulator seed --replace",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			seed := sample.Seed
			if replace {
				seed = sample.Replace
			}
			return seedProfile(cmd, keyring, profile, seed)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", emulator.ProfileName, "the profile to seed; its endpoint must be on this machine")
	cmd.Flags().BoolVar(&replace, "replace", false, "drop and recreate sample databases that already exist")
	return cmd
}

type seedFunc func(ctx context.Context, t sample.Target, report func(sample.Seeded)) error

func seedProfile(cmd *cobra.Command, keyring config.Keyring, name string, seed seedFunc) error {
	store, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, err := emulatorProfile(cfg, name)
	if err != nil {
		return err
	}
	if !config.IsLocalEndpoint(profile.Endpoint) {
		return fmt.Errorf("cmd: profile %s points at %s: seed only runs against an emulator on this machine", name, profile.Endpoint)
	}
	conn, err := Profiles{Store: store, Keyring: keyring}.Open(cmd.Context(), name)
	if err != nil {
		return err
	}
	// Seeding is done or has failed by the time the connection closes.
	defer func() { _ = conn.Close() }()
	target, err := sampleTarget(conn)
	if err != nil {
		return err
	}
	var writeErr error
	report := func(s sample.Seeded) {
		if writeErr == nil {
			writeErr = say(cmd, "%s.%s: %d items, partitioned on %s", s.Database, s.Container, s.Items, s.PartitionKey)
		}
	}
	err = seed(cmd.Context(), target, report)
	var exists *sample.ExistsError
	if errors.As(err, &exists) {
		return fmt.Errorf("%w: pass --replace to drop and recreate them", err)
	}
	if err != nil {
		return err
	}
	return writeErr
}

// emulatorProfile is the profile called name, with the fix for a missing
// emulator profile.
func emulatorProfile(cfg config.Config, name string) (config.Profile, error) {
	profile, err := cfg.Profile(name)
	missing := errors.Is(err, config.ErrProfileNotFound) || errors.Is(err, config.ErrNoProfiles)
	if missing && name == emulator.ProfileName {
		return config.Profile{}, errNoEmulatorProfile
	}
	return profile, err
}

func sampleTarget(conn adapter.Connection) (sample.Target, error) {
	admin, canAdmin := conn.(adapter.CatalogAdmin)
	writer, canWrite := conn.(adapter.ItemWriter)
	if !canAdmin || !canWrite {
		return sample.Target{}, fmt.Errorf("cmd: seed: this adapter cannot create databases and write items: %w", adapter.ErrUnsupported)
	}
	return sample.Target{Catalog: conn.Catalog(), Admin: admin, Writer: writer}, nil
}
