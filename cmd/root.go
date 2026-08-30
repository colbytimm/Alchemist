// Package cmd wires the CLI. The root command launches the TUI; concrete
// adapters are constructed here and injected — never inside internal/tui.
package cmd

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/logging"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// RegisterAdapters wires the concrete adapters into the registry. Calling it
// twice is a wiring bug and returns adapter.ErrDuplicateName.
func RegisterAdapters() error {
	registrations := []struct {
		name    string
		factory adapter.Factory
	}{
		{cosmos.Name, func() adapter.Adapter { return cosmos.Adapter{} }},
		{mock.Name, func() adapter.Adapter { return mock.New() }},
	}
	for _, r := range registrations {
		if err := adapter.Register(r.name, r.factory); err != nil {
			return fmt.Errorf("cmd: register adapters: %w", err)
		}
	}
	return nil
}

// NewRootCmd builds the root command for the alchemist binary.
func NewRootCmd() *cobra.Command {
	var session sessionFlags
	cmd := &cobra.Command{
		Use:           "alchemist",
		Short:         "A terminal IDE for Azure Cosmos DB",
		Long:          "Alchemist is a keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API):\nbrowse databases and containers, write SQL, and page through results.",
		Example:       "  alchemist --connection-string \"AccountEndpoint=https://...\"\n  alchemist --adapter mock",
		Version:       fmt.Sprintf("%s (built %s)", app.Version, app.BuildDate),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return session.run(cmd)
		},
	}
	session.bind(cmd.Flags())
	cmd.MarkFlagsMutuallyExclusive("connection-string", "endpoint")
	cmd.MarkFlagsRequiredTogether("endpoint", "key")
	cmd.SetVersionTemplate(fmt.Sprintf("%s {{.Version}}\n", app.Name))
	cmd.CompletionOptions.DisableDefaultCmd = true
	return cmd
}

// sessionFlags are the flags that configure one TUI session. The credential
// flags are temporary: profiles replace them in iteration 6.
type sessionFlags struct {
	adapter            string
	endpoint           string
	key                string
	connectionString   string
	insecureSkipVerify bool
	ascii              bool
	verbose            bool
}

func (s *sessionFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&s.adapter, "adapter", cosmos.Name,
		fmt.Sprintf("adapter to connect with (%s, %s)", cosmos.Name, mock.Name))
	flags.StringVar(&s.endpoint, "endpoint", "", "Cosmos account endpoint")
	flags.StringVar(&s.key, "key", "", "Cosmos account key")
	flags.StringVar(&s.connectionString, "connection-string", "", "Cosmos connection string")
	flags.BoolVar(&s.insecureSkipVerify, "insecure-skip-verify", false,
		"skip TLS verification (for the Cosmos emulator's self-signed certificate)")
	flags.BoolVar(&s.ascii, "ascii", false, "draw with ASCII glyphs instead of Unicode")
	flags.BoolVar(&s.verbose, "verbose", false, "log at debug level")
}

// run resolves the adapter and connects before touching the filesystem, so an
// invocation that never reaches the TUI leaves no log directory behind.
func (s sessionFlags) run(cmd *cobra.Command) error {
	factory, err := adapter.Get(s.adapter)
	if err != nil {
		return err
	}
	conn, err := factory().Connect(cmd.Context(), s.settings())
	if err != nil {
		return connectError(err)
	}
	defer func() { _ = conn.Close() }()

	logger, logFile, err := logging.Open(s.logLevel())
	if err != nil {
		return err
	}
	// Closing the log file is the last thing to happen; a failure there is
	// past the point where anything could act on it.
	defer func() { _ = logFile.Close() }()

	logger.Info("session started", "adapter", s.adapter)
	program := tea.NewProgram(
		tui.New(tui.Options{
			Icons:   s.icons(),
			Catalog: conn.Catalog(),
			Logger:  logger,
			Profile: s.adapter,
		}),
		tea.WithAltScreen(),
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)
	_, err = program.Run()
	return err
}

// connectError restates a credential failure in the flag names the user typed.
// The adapter can only name the setting keys it was handed, which say nothing
// about how to supply them, and running with no arguments at all lands here.
func connectError(err error) error {
	if !errors.Is(err, cosmos.ErrMissingCredentials) {
		return err
	}
	return fmt.Errorf(
		"pass --connection-string, or --endpoint with --key, or --adapter mock to browse fixture data: %w",
		cosmos.ErrMissingCredentials)
}

func (s sessionFlags) settings() map[string]string {
	return map[string]string{
		"endpoint":             s.endpoint,
		"key":                  s.key,
		"connection_string":    s.connectionString,
		"insecure_skip_verify": fmt.Sprint(s.insecureSkipVerify),
	}
}

func (s sessionFlags) icons() theme.IconSet {
	if s.ascii {
		return theme.ASCIIIcons()
	}
	return theme.Icons()
}

func (s sessionFlags) logLevel() log.Level {
	if s.verbose {
		return log.DebugLevel
	}
	return log.InfoLevel
}
