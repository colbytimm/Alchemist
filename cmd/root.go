// Package cmd wires the CLI. The root command launches the TUI; concrete
// adapters are constructed here and injected — never inside internal/tui.
package cmd

import (
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/tui"
)

var registerOnce sync.Once

// registerAdapters wires the concrete adapters into the registry exactly
// once; the guard keeps repeated NewRootCmd calls (tests) idempotent.
func registerAdapters() {
	registerOnce.Do(func() {
		_ = adapter.Register(cosmos.Name, func() adapter.Adapter { return cosmos.Adapter{} })
	})
}

// NewRootCmd builds the root command for the alchemist binary.
func NewRootCmd() *cobra.Command {
	registerAdapters()
	cmd := &cobra.Command{
		Use:           "alchemist",
		Short:         "A terminal IDE for Azure Cosmos DB",
		Long:          "Alchemist is a keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API):\nbrowse databases and containers, write SQL, and page through results.",
		Version:       fmt.Sprintf("%s (built %s)", app.Version, app.BuildDate),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := tea.NewProgram(
				tui.New(),
				tea.WithAltScreen(),
				tea.WithInput(cmd.InOrStdin()),
				tea.WithOutput(cmd.OutOrStdout()),
			)
			_, err := p.Run()
			return err
		},
	}
	cmd.SetVersionTemplate(fmt.Sprintf("%s {{.Version}}\n", app.Name))
	cmd.CompletionOptions.DisableDefaultCmd = true
	return cmd
}
