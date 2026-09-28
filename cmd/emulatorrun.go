package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/emulator"
)

const (
	defaultStartTimeout = 5 * time.Minute
	defaultLogLines     = 200
	statusProbeTimeout  = 10 * time.Second
)

// runtimeFlags pick the container runtime a command drives.
type runtimeFlags struct {
	name string
}

func (r *runtimeFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&r.name, "runtime", "",
		"docker or podman (default: $"+emulator.RuntimeEnvVar+", else docker when installed, else podman)")
}

// checkedRuntime is the chosen runtime, once it has shown it can reach its
// daemon.
func (r runtimeFlags) checkedRuntime(ctx context.Context) (emulator.Runtime, error) {
	runtime, err := emulator.DetectRuntime(r.name)
	if err != nil {
		return emulator.Runtime{}, err
	}
	return runtime, runtime.Check(ctx)
}

func (r runtimeFlags) manager(cmd *cobra.Command, keyring config.Keyring) (emulator.Manager, error) {
	runtime, err := r.checkedRuntime(cmd.Context())
	if err != nil {
		return emulator.Manager{}, err
	}
	profiles, err := defaultProfiles(keyring)
	if err != nil {
		return emulator.Manager{}, err
	}
	return emulator.NewManager(runtime, emulatorProbe(profiles), cmd.ErrOrStderr()), nil
}

func defaultProfiles(keyring config.Keyring) (Profiles, error) {
	store, err := config.DefaultStore()
	if err != nil {
		return Profiles{}, err
	}
	return Profiles{Store: store, Keyring: keyring}, nil
}

// emulatorProbe connects the emulator profile afresh on every attempt, with
// the key, TLS and settings the app will use, and lists its databases, which
// only an emulator whose database has started can answer. A fresh connection
// matters: the SDK client keeps the first account read that failed.
func emulatorProbe(profiles Profiles) emulator.Probe {
	return func(ctx context.Context) error {
		conn, err := profiles.Open(ctx, emulator.ProfileName)
		if err != nil {
			return err
		}
		// The ping's answer is all the probe reports.
		defer func() { _ = conn.Close() }()
		return conn.Ping(ctx)
	}
}

func newEmulatorStartCmd(keyring config.Keyring) *cobra.Command {
	var (
		runtime runtimeFlags
		options emulator.StartOptions
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Pull, run and wait for the emulator, and add the emulator profile",
		Long: "start pulls the image when it is missing, creates or starts the container, and adds the\n" +
			"emulator profile. It returns once a query through that profile succeeds, so it is also the\n" +
			"way to check a container that is already running.",
		Example: "  alchemist emulator start\n" +
			"  alchemist emulator start --port 9081 --recreate\n" +
			"  alchemist emulator start --pull",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return startEmulator(cmd, keyring, runtime, options, timeout)
		},
	}
	runtime.bind(cmd.Flags())
	cmd.Flags().IntVar(&options.Port, "port", 0,
		fmt.Sprintf("host port to publish the emulator on, on 127.0.0.1 only (default: the container's port, else %d)", emulator.DefaultPort))
	cmd.Flags().DurationVar(&timeout, "timeout", defaultStartTimeout, "how long to wait for the emulator to answer")
	cmd.Flags().BoolVar(&options.Recreate, "recreate", false, "delete and create the container again, keeping its data")
	cmd.Flags().BoolVar(&options.Pull, "pull", false, "pull a newer image, and recreate the container when there is one")
	return cmd
}

func startEmulator(cmd *cobra.Command, keyring config.Keyring, runtime runtimeFlags, options emulator.StartOptions, timeout time.Duration) error {
	m, err := runtime.manager(cmd, keyring)
	if err != nil {
		return err
	}
	port, err := m.Start(cmd.Context(), options)
	if err != nil {
		return err
	}
	profile, err := ensureEmulatorProfile(cmd, port)
	if err != nil {
		return err
	}
	if err := m.Wait(cmd.Context(), profile.Endpoint, timeout); err != nil {
		return err
	}
	return say(cmd, "the emulator is ready at %s; open it with: alchemist emulator", profile.Endpoint)
}

// ensureEmulatorProfile adds or moves the emulator profile, says what it did,
// and returns the profile as saved.
func ensureEmulatorProfile(cmd *cobra.Command, port int) (config.Profile, error) {
	store, cfg, err := loadConfig()
	if err != nil {
		return config.Profile{}, err
	}
	cfg, outcome, err := emulator.EnsureProfile(cfg, port)
	if err != nil {
		return config.Profile{}, err
	}
	profile := cfg.Profiles[emulator.ProfileName]
	switch outcome {
	case emulator.ProfileUnchanged:
		return profile, nil
	case emulator.ProfileNotOurs:
		return profile, say(cmd, "profile %s exists and points at %s; left as is", profile.Name, profile.Endpoint)
	}
	if err := store.Save(cfg); err != nil {
		return config.Profile{}, err
	}
	if outcome == emulator.ProfileMoved {
		return profile, say(cmd, "profile %s now points at %s", profile.Name, profile.Endpoint)
	}
	return profile, say(cmd, "added profile %s at %s", profile.Name, profile.Endpoint)
}

func newEmulatorStopCmd(keyring config.Keyring) *cobra.Command {
	var runtime runtimeFlags
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the emulator container, keeping its data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := runtime.manager(cmd, keyring)
			if err != nil {
				return err
			}
			found, err := m.Stop(cmd.Context())
			switch {
			case err != nil:
				return err
			case !found.Exists():
				return say(cmd, "no emulator container to stop")
			case !found.Running():
				return say(cmd, "the emulator is not running")
			}
			return say(cmd, "stopped the emulator; its data stays in volume %s", emulator.VolumeName)
		},
	}
	runtime.bind(cmd.Flags())
	return cmd
}

func newEmulatorLogsCmd(keyring config.Keyring) *cobra.Command {
	var (
		runtime runtimeFlags
		options emulator.LogOptions
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the emulator container's log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := runtime.manager(cmd, keyring)
			if err != nil {
				return err
			}
			return m.Logs(cmd.Context(), options, cmd.OutOrStdout())
		},
	}
	runtime.bind(cmd.Flags())
	cmd.Flags().BoolVarP(&options.Follow, "follow", "f", false, "keep printing the log as it grows")
	cmd.Flags().IntVar(&options.Tail, "tail", defaultLogLines, "how many of the last lines to print")
	return cmd
}

func newEmulatorRemoveCmd(keyring config.Keyring) *cobra.Command {
	var (
		runtime runtimeFlags
		options emulator.RemoveOptions
	)
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Delete the emulator container, and with --data its data",
		Long: "remove deletes the container. Its data stays in the volume " + emulator.VolumeName + "\n" +
			"for the next start, unless --data deletes it too. Snapshots are never touched.",
		Example: "  alchemist emulator remove\n" +
			"  alchemist emulator remove --data --image",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := runtime.manager(cmd, keyring)
			if err != nil {
				return err
			}
			removed, err := m.Remove(cmd.Context(), options)
			if err != nil {
				return err
			}
			return say(cmd, "%s", describeRemoved(removed))
		},
	}
	runtime.bind(cmd.Flags())
	cmd.Flags().BoolVar(&options.Data, "data", false, "delete the data volume too")
	cmd.Flags().BoolVar(&options.Image, "image", false, "delete the emulator image too")
	return cmd
}

func describeRemoved(removed emulator.Removed) string {
	var lines []string
	if removed.Container {
		lines = append(lines, "removed container "+emulator.ContainerName)
	}
	if removed.Volume {
		lines = append(lines, "removed volume "+emulator.VolumeName)
	}
	if removed.Image {
		lines = append(lines, "removed image "+emulator.Image)
	}
	if len(lines) == 0 {
		return "nothing to remove"
	}
	return strings.Join(lines, "\n")
}

func newEmulatorStatusCmd(keyring config.Keyring) *cobra.Command {
	var runtime runtimeFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report the runtime, the container, whether the emulator answers, and its profile",
		Long: "status reports what it can: with no runtime it still tries the emulator profile's\n" +
			"endpoint, so it serves an emulator running outside a container too.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return emulatorStatus(cmd, keyring, runtime)
		},
	}
	runtime.bind(cmd.Flags())
	return cmd
}

func emulatorStatus(cmd *cobra.Command, keyring config.Keyring, runtime runtimeFlags) error {
	store, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var rows strings.Builder
	container := emulator.Container{State: emulator.StateAbsent}
	rt, err := runtime.checkedRuntime(cmd.Context())
	if err != nil {
		fmt.Fprintf(&rows, "runtime\t%s\n", firstLine(err))
		rows.WriteString("container\tunknown\n")
	} else {
		fmt.Fprintf(&rows, "runtime\t%s\n", rt.Name)
		container, err = rt.Inspect(cmd.Context())
		fmt.Fprintf(&rows, "container\t%s\n", describeContainer(container, err))
	}
	profile, ok := cfg.Profiles[emulator.ProfileName]
	if !ok {
		rows.WriteString("endpoint\tunknown\nprofile\tnone: run alchemist emulator start\n")
		return writeTable(cmd.OutOrStdout(), rows.String())
	}
	answer := emulatorProbe(Profiles{Store: store, Keyring: keyring}).Within(cmd.Context(), statusProbeTimeout)
	fmt.Fprintf(&rows, "endpoint\t%s: %s\n", profile.Endpoint, describeAnswer(answer, container))
	fmt.Fprintf(&rows, "profile\t%s (key: %s)\n", profile.Name, keySource(config.SecretResolver{Keyring: keyring}, profile))
	return writeTable(cmd.OutOrStdout(), rows.String())
}

func describeContainer(c emulator.Container, err error) string {
	switch {
	case err != nil:
		return firstLine(err)
	case !c.Exists():
		return "none"
	case !c.Managed:
		return fmt.Sprintf("%s was not created by Alchemist", emulator.ContainerName)
	case c.Stopped():
		return fmt.Sprintf("%s (code %d)", c.State, c.ExitCode)
	}
	return fmt.Sprintf("%s, port %d", c.State, c.HostPort)
}

func describeAnswer(answer error, c emulator.Container) string {
	switch {
	case answer == nil:
		return "ready"
	case c.Running():
		return "starting: " + firstLine(answer)
	}
	return "not answering: " + firstLine(answer)
}

func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}
