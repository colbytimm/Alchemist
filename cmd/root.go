package cmd

import (
	"github.com/colbytimm/alchemist/app"
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
)

func Root() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "alchemist",
		Long:    "\n" + aurora.Magenta(app.Name).String() + " is a command line tool for querying Cosmos DB in your terminal",
		Version: app.Version,
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(AddAccountCmd())
	rootCmd.AddCommand(ListAccountCmd())

	return rootCmd
}
