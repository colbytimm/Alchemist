// Package cmd wires the CLI. The root command launches the TUI; concrete
// adapters are constructed here and injected — never inside internal/tui.
package cmd

import (
	"context"
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
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/logging"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
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

// NewRootCmd builds the root command. Profile keys are read from, and
// offered to, keyring.
func NewRootCmd(keyring config.Keyring) *cobra.Command {
	var session sessionFlags
	cmd := &cobra.Command{
		Use:   "alchemist [profile]",
		Short: "A terminal IDE for Azure Cosmos DB",
		Long: "Alchemist is a keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API):\n" +
			"browse databases and containers, write SQL, and page through results.\n\n" +
			"Run it with no profile and it asks for the account to connect to; run it with\n" +
			"one and it connects. alchemist profile does the same from the command line.",
		Example: "  alchemist                 # the default profile, or the connect screen\n" +
			"  alchemist prod            # the profile called prod\n" +
			"  alchemist --adapter mock  # fixture data, no profile needed",
		Version:       fmt.Sprintf("%s (built %s)", app.Version, app.BuildDate),
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return session.run(cmd, args, keyring)
		},
	}
	session.bind(cmd.Flags())
	cmd.AddCommand(newProfileCmd(keyring))
	cmd.SetVersionTemplate(fmt.Sprintf("%s {{.Version}}\n", app.Name))
	cmd.CompletionOptions.DisableDefaultCmd = true
	return cmd
}

// sessionFlags configure one TUI session.
type sessionFlags struct {
	adapter string
	ascii   bool
	verbose bool
	history bool
}

func (s *sessionFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&s.adapter, "adapter", "",
		fmt.Sprintf("connect an adapter with no profile (%s serves fixture data)", mock.Name))
	flags.BoolVar(&s.ascii, "ascii", false, "draw with ASCII glyphs instead of Unicode")
	flags.BoolVar(&s.verbose, "verbose", false, "log at debug level")
	flags.BoolVar(&s.history, "history", true,
		fmt.Sprintf("record every query run to %s in the state directory", history.FileName))
}

// run resolves what to connect to before touching the filesystem, so an
// invocation that never reaches the TUI leaves no log directory behind.
func (s sessionFlags) run(cmd *cobra.Command, args []string, keyring config.Keyring) error {
	launch, err := s.resolveLaunch(cmd.Context(), args, keyring)
	if err != nil {
		return err
	}
	if launch.connection != nil {
		// The session is over by the time this runs; a close failure has no
		// bearing on the exit path.
		defer func() { _ = launch.connection.Close() }()
	}

	stateDir, err := logging.Dir()
	if err != nil {
		return err
	}
	logger, logFile, err := logging.Open(stateDir, s.logLevel())
	if err != nil {
		return err
	}
	// Closing the log file is the last thing to happen; a failure there is
	// past the point where anything could act on it.
	defer func() { _ = logFile.Close() }()

	logger.Info("session started", "profile", launch.profile)
	program := tea.NewProgram(
		tui.New(tui.Options{
			Icons:       s.icons(),
			Connection:  launch.connection,
			Connect:     launch.connect,
			Manage:      management,
			Form:        launch.form,
			Logger:      logger,
			History:     s.historyStore(logger, stateDir),
			Profile:     launch.profile,
			Database:    launch.database,
			MaxJoinRows: launch.maxJoinRows,
		}),
		tea.WithAltScreen(),
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)
	_, err = program.Run()
	return err
}

// management reports the optional interfaces a connection satisfies. The
// assertions live here rather than in internal/tui, which knows nothing of
// any concrete backend; a connection satisfying none leaves the TUI with
// no management bindings at all.
func management(conn adapter.Connection) tui.Management {
	admin, _ := conn.(adapter.CatalogAdmin)
	throughput, _ := conn.(adapter.ThroughputEditor)
	inspector, _ := conn.(adapter.Inspector)
	return tui.Management{Admin: admin, Throughput: throughput, Inspector: inspector}
}

// launch is what a session starts with: a live connection, or the connect
// screen that opens one.
type launch struct {
	profile     string
	database    string
	maxJoinRows int
	connection  adapter.Connection
	connect     tui.Connector
	form        panes.ConnectForm
}

// resolveLaunch picks the connection: --adapter names an adapter to run with
// no profile at all; otherwise the profile in args, or the default one.
func (s sessionFlags) resolveLaunch(ctx context.Context, args []string, keyring config.Keyring) (launch, error) {
	if s.adapter != "" {
		if len(args) > 0 {
			return launch{}, errors.New("cmd: a profile and --adapter are alternatives; pass one or the other")
		}
		conn, err := connect(ctx, s.adapter, nil)
		if err != nil {
			return launch{}, connectError(err)
		}
		return launch{profile: s.adapter, connection: conn}, nil
	}
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	return profileLaunch(ctx, name, keyring)
}

// profileLaunch connects the named profile, or opens the connect screen when
// no profile exists yet or the key of this one is nowhere to be found.
func profileLaunch(ctx context.Context, name string, keyring config.Keyring) (launch, error) {
	store, cfg, err := loadConfig()
	if err != nil {
		return launch{}, err
	}
	profile, err := cfg.Profile(name)
	if errors.Is(err, config.ErrNoProfiles) {
		return setupLaunch(store, keyring, config.Profile{Name: name, Adapter: cosmos.Name}), nil
	}
	if err != nil {
		return launch{}, err
	}
	secret, err := config.SecretResolver{Keyring: keyring}.Resolve(profile.Name)
	if errors.Is(err, config.ErrSecretNotFound) {
		return setupLaunch(store, keyring, profile), nil
	}
	if err != nil {
		return launch{}, err
	}
	conn, err := connect(ctx, profile.Adapter, profile.Settings(secret))
	if err != nil {
		return launch{}, err
	}
	return launch{
		profile:     profile.Name,
		database:    profile.Database,
		maxJoinRows: profile.MaxJoinRows,
		connection:  conn,
	}, nil
}

// setupLaunch opens the connect screen for profile, which may be no more than
// a name. What the screen submits is connected and pinged before anything is
// saved, so an attempt that did not connect leaves nothing behind.
func setupLaunch(store config.Store, keyring config.Keyring, profile config.Profile) launch {
	return launch{
		database:    profile.Database,
		maxJoinRows: profile.MaxJoinRows,
		form: panes.ConnectForm{
			Profile:    profile.Name,
			Endpoint:   profile.Endpoint,
			SkipVerify: profile.InsecureSkipVerify,
			StoreKey:   true,
		},
		connect: func(ctx context.Context, form panes.ConnectForm) (adapter.Connection, error) {
			saved := profile
			saved.Name, saved.Endpoint, saved.InsecureSkipVerify = form.Profile, form.Endpoint, form.SkipVerify
			conn, err := connect(ctx, saved.Adapter, saved.Settings(config.Secret{Key: form.Key}))
			if err != nil {
				return nil, err
			}
			if err := conn.Ping(ctx); err != nil {
				closeFailed(conn)
				return nil, err
			}
			var key string
			if form.StoreKey {
				key = form.Key
			}
			if err := config.SaveProfile(store, keyring, saved, key); err != nil {
				closeFailed(conn)
				return nil, err
			}
			return conn, nil
		},
	}
}

func connect(ctx context.Context, name string, settings map[string]string) (adapter.Connection, error) {
	factory, err := adapter.Get(name)
	if err != nil {
		return nil, err
	}
	return factory().Connect(ctx, settings)
}

// closeFailed releases a connection whose attempt is being reported as an
// error; a close failure would add nothing to it.
func closeFailed(conn adapter.Connection) {
	_ = conn.Close()
}

// connectError restates a credential failure as the fix: --adapter cosmos
// with no profile lands here, and the adapter can only name the setting keys
// it was handed.
func connectError(err error) error {
	if !errors.Is(err, cosmos.ErrMissingCredentials) {
		return err
	}
	return fmt.Errorf("%s needs a profile: run alchemist to set one up, or pass --adapter %s to browse fixture data: %w",
		cosmos.Name, mock.Name, cosmos.ErrMissingCredentials)
}

// historyStore readies the query log in the state directory. A session
// whose log cannot be opened runs without one and says so in the log file:
// a missing history is not worth refusing to start over.
func (s sessionFlags) historyStore(logger *log.Logger, dir string) history.Store {
	if !s.history {
		return history.Discard{}
	}
	store, err := history.Open(dir)
	if err != nil {
		logger.Warn("query history is off for this session", "error", err)
		return history.Discard{}
	}
	return store
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
