// Package cmd wires the CLI. The root command launches the TUI; concrete
// adapters are constructed here and injected — never inside internal/tui.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
		Example: "  alchemist                 # the default profile, or the connect form\n" +
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

	logger.Info("session started", "account", launch.name)
	program := tea.NewProgram(
		tui.New(tui.Options{
			Icons:        s.icons(),
			Accounts:     launch.accounts,
			ListAccounts: launch.list,
			Launch:       launch.name,
			Open:         launch.open,
			Connect:      launch.connect,
			Manage:       management,
			Form:         launch.form,
			Logger:       logger,
			History:      s.historyStore(logger, stateDir),
		}),
		tea.WithAltScreen(),
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)
	final, err := program.Run()
	// However the program ended — a quit, a signal, a cancelled context — the
	// connections it holds are closed here; a quit has closed them already.
	if session, ok := final.(tui.Model); ok {
		session.CloseConnections()
	}
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

// launch is what a session starts with: the accounts it knows, the one it
// starts on — empty on a first run, which opens the connect form — and how
// to connect them. The TUI opens every connection itself.
type launch struct {
	accounts []tui.Account
	list     func() ([]tui.Account, error)
	name     string
	open     tui.Opener
	connect  tui.Connector
	form     panes.ConnectForm
}

// resolveLaunch picks the account: --adapter names an adapter to run with no
// profile at all; otherwise the profile in args, or the default one.
func (s sessionFlags) resolveLaunch(ctx context.Context, args []string, keyring config.Keyring) (launch, error) {
	if s.adapter != "" {
		if len(args) > 0 {
			return launch{}, errors.New("cmd: a profile and --adapter are alternatives; pass one or the other")
		}
		return adapterLaunch(ctx, s.adapter, keyring)
	}
	store, err := config.DefaultStore()
	if err != nil {
		return launch{}, err
	}
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	return profileLaunch(name, Profiles{Store: store, Keyring: keyring})
}

// adapterLaunch starts a session on one account, named for the adapter, that
// needs no config. The adapter is tried once here, and the connection
// dropped, so a backend that cannot connect without a profile says so before
// the TUI starts. The profiles are reachable too when the config can be
// found; without it, adding one says why it cannot.
func adapterLaunch(ctx context.Context, name string, keyring config.Keyring) (launch, error) {
	conn, err := connect(ctx, name, nil)
	if err != nil {
		return launch{}, connectError(err)
	}
	// The trial connection only proved the adapter connects; the TUI opens
	// its own, so how this one closes changes nothing.
	_ = conn.Close()
	session := launch{accounts: []tui.Account{{Name: name}}, name: name}
	store, err := config.DefaultStore()
	if err != nil {
		session.open = adapterOpener(name, nil)
		session.connect = func(context.Context, panes.ConnectForm) (adapter.Connection, error) { return nil, err }
		return session, nil
	}
	profiles := Profiles{Store: store, Keyring: keyring}
	session.open = adapterOpener(name, profiles.Open)
	session.connect = profiles.Connect
	session.list = func() ([]tui.Account, error) {
		accounts, err := profiles.Accounts()
		// The session's account of that name is the adapter's, whatever a
		// profile of the same name says.
		return slices.DeleteFunc(accounts, func(a tui.Account) bool { return a.Name == name }), err
	}
	return session, nil
}

// adapterOpener connects the adapter for its own account and hands every
// other to profiles, when there are any.
func adapterOpener(name string, profiles tui.Opener) tui.Opener {
	return func(ctx context.Context, account string) (adapter.Connection, error) {
		switch {
		case account == name:
			return connect(ctx, name, nil)
		case profiles == nil:
			return nil, fmt.Errorf("cmd: account %q: %w", account, config.ErrProfileNotFound)
		}
		return profiles(ctx, account)
	}
}

// profileLaunch starts on the named profile, or the default one, or opens the
// connect form when no profile exists yet. An unknown name fails here, before
// anything is created on disk.
func profileLaunch(name string, profiles Profiles) (launch, error) {
	cfg, err := profiles.Store.Load()
	if err != nil {
		return launch{}, err
	}
	profile, err := cfg.Profile(name)
	if errors.Is(err, config.ErrNoProfiles) {
		return launch{
			list:    profiles.Accounts,
			open:    profiles.Open,
			connect: profiles.Connect,
			form:    panes.ConnectForm{Profile: name, StoreKey: true},
		}, nil
	}
	if err != nil {
		return launch{}, err
	}
	return launch{
		accounts: accounts(cfg),
		list:     profiles.Accounts,
		name:     profile.Name,
		open:     profiles.Open,
		connect:  profiles.Connect,
	}, nil
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
